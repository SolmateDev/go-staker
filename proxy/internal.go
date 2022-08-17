package proxy

import (
	"context"
	"errors"
	"time"

	"github.com/SolmateDev/go-staker/client"
	"github.com/SolmateDev/go-staker/db"
	"github.com/SolmateDev/go-staker/limiter"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type internal struct {
	ctx                 context.Context
	errorC              chan<- error
	tpsC                chan<- tpsInfo
	closeSignalCList    []chan<- error
	rpc                 *sgorpc.Client
	ws                  *sgows.Client
	validatorId         sgo.PublicKey
	userTxBuffer        limiter.ItemBuffer[limiter.JobSubmission]
	validatorStats      *localValidatorStats
	networkStats        *tpsInfo
	validatorTPS        float64 // the number of transactions per 1000s that this validator can send to the network
	slot                uint64
	controller          state.Controller
	updateValidatorTPSC chan<- float64
	db                  db.WorkDb
}

type localValidatorStats struct {
	activeStake uint64
	totalStake  uint64
	skipRate    float64
}

const TX_PROCESSING_WINDOW_SIZE = uint64(2)
const BLOCK_CHECK_SLOT_INTERVAL = uint64(30)

// Track
// 1. update validator statistics
// 2. network statistics
// 3. from 1 and 2, get the validator allowed TPS
// 4. from 4, update the user tx buffers via updateValidatorTPSC
// 5. from 4, update the processing window
func loopInternal(
	ctx context.Context,
	internalC <-chan func(*internal),
	externalErrorC <-chan error,
	cancel context.CancelFunc,
	rpcClient *sgorpc.Client,
	wsClient *sgows.Client,
	userTxBuffer limiter.ItemBuffer[limiter.JobSubmission],
	validatorId sgo.PublicKey,
	controller state.Controller,
	updateValidatorTPSC chan<- float64,
	workDb db.WorkDb,
) {
	defer cancel()
	doneC := ctx.Done()
	tpsC := make(chan tpsInfo, 1)
	errorC := make(chan error, 1)
	newEpochC := make(chan struct{}, 1)
	in := new(internal)
	in.validatorId = validatorId
	in.ctx = ctx
	in.closeSignalCList = make([]chan<- error, 0)
	in.rpc = rpcClient
	in.ws = wsClient
	in.userTxBuffer = userTxBuffer
	in.errorC = errorC
	in.validatorStats = &localValidatorStats{totalStake: 1, activeStake: 0, skipRate: 0}
	in.tpsC = tpsC
	in.validatorTPS = 0
	in.controller = controller
	in.db = workDb

	//controller.Router.OnBid()

	var err error
	err = in.get_stats()
	if err != nil {
		return
	}

	sub, err := wsClient.SlotSubscribe()
	if err != nil {
		return
	}
	slotC := sub.RecvStream()
	slotErrorC := sub.CloseSignal()
	defer sub.Unsubscribe()

	go loopUpdateEpochSchedule(ctx, rpcClient, wsClient, newEpochC, in.errorC)

	lastTurn := uint64(0)
	lastBlockCheck := uint64(0)

out:
	for {
		select {
		case <-doneC:
			break out
		case <-newEpochC:
			// fill in in.validatorStats
			err = in.get_stats()
			if err != nil {
				break out
			}
			if in.networkStats != nil {
				in.on_stats()
			}
		case d := <-slotC:
			s, ok := d.(*sgows.SlotResult)
			if !ok {
				err = errors.New("bad slot result")
				break out
			}
			in.slot = s.Slot
			if in.networkStats != nil && in.validatorStats != nil && lastTurn+TX_PROCESSING_WINDOW_SIZE <= in.slot {
				lastTurn = s.Slot
				in.on_turn()
			}
			if lastBlockCheck+BLOCK_CHECK_SLOT_INTERVAL <= in.slot {
				go loopCalculateTps(in.ctx, in.rpc, in.ws, in.errorC, in.tpsC, lastBlockCheck, in.slot)
				lastBlockCheck = in.slot
			}
		case t := <-tpsC:
			in.networkStats = &t
			if in.validatorStats != nil {
				in.on_stats()
			}
		case err = <-slotErrorC:
			break out
		case err = <-externalErrorC:
			break out
		case req := <-internalC:
			req(in)
		}
	}
	in.finish(err)
}

// fill in validator stats
func (in *internal) get_stats() error {
	ctx, cancel := context.WithTimeout(in.ctx, 5*time.Second)
	defer cancel()
	ans, err := client.GetValidatorInfo(ctx, in.rpc, in.ws)
	if err != nil {
		return err
	}
	in.validatorStats.totalStake = ans.TotalStake
	x, present := ans.Validators[in.validatorId.String()]
	if present {
		in.validatorStats.activeStake = x.CurrentStake
		in.validatorStats.skipRate = x.SkipRate
	} else {
		in.validatorStats.activeStake = 0
		in.validatorStats.skipRate = 0
	}

	return nil
}

// with up-to-date validator and network stats, set the buffer size of all the user buffers and process window size
func (in *internal) on_stats() {
	// calculate the network tps
	validatorRate := float64(0)
	{
		a := in.validatorStats.activeStake
		b := in.validatorStats.totalStake
		r := b % a
		validatorRate = 1 / float64((b-r)/a)
	}
	networkTps := float64(0)
	{
		// TODO: calculate by tx count? tx fee as proxy for CPU cost? tx data size?
		timeLength := float64(in.networkStats.finish.Unix() - in.networkStats.start.Unix())
		totalTx := float64(in.networkStats.txCount)
		networkTps = totalTx / timeLength
	}

	in.validatorTPS = validatorRate * networkTps

	in.updateValidatorTPSC <- in.validatorTPS
}

// process transactions for the current processing window
func (in *internal) on_turn() {
	go processTxBatch(in.ctx, in.errorC, in.rpc, in.ws, in.userTxBuffer, in.db)
}

func loopUpdateEpochSchedule(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, newEpochC chan<- struct{}, errorC chan<- error) {
	errorC <- loopUpdateEpochSchedule_inside(ctx, rpcClient, wsClient, newEpochC)
}
func loopUpdateEpochSchedule_inside(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, newEpochC chan<- struct{}) error {
	doneC := ctx.Done()
	slotSub, err := wsClient.SlotSubscribe()
	if err != nil {
		return err
	}
	slotC := slotSub.RecvStream()
	errorC := slotSub.CloseSignal()

	slot, err := rpcClient.GetSlot(ctx, sgorpc.CommitmentConfirmed)
	if err != nil {

		return err
	}
	tMinus, err := getSlotsUntilNextEpoch(ctx, rpcClient)
	if err != nil {
		return err
	}
	nextSlot := slot + tMinus

out:
	for {
		select {
		case <-doneC:
		case d := <-slotC:
			x, ok := d.(*sgows.SlotResult)
			if !ok {
				err = errors.New("bad slot result")
				break out
			}
			slot = x.Slot
			if nextSlot <= slot {
				newEpochC <- struct{}{}
				tMinus, err = getSlotsUntilNextEpoch(ctx, rpcClient)
				if err != nil {
					return err
				}
				nextSlot = slot + tMinus
			}
		case err = <-errorC:
			break out
		}
	}
	return nil
}

func getSlotsUntilNextEpoch(ctx context.Context, rpcClient *sgorpc.Client) (uint64, error) {
	d, err := rpcClient.GetEpochSchedule(ctx)
	if err != nil {
		return 0, err
	}

	return d.LeaderScheduleSlotOffset, nil
}

func (in *internal) finish(err error) {
	log.Debug(err)
	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}
