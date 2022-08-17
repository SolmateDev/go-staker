package cranker

import (
	"context"
	"errors"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type internal struct {
	ctx               context.Context
	errorC            chan<- error
	closeSignalCList  []chan<- error
	ci                CrankInfo
	rpc               *sgorpc.Client
	ws                *sgows.Client
	controller        state.Controller
	crankerBalanceSub *sgows.AccountSubscription
	crankerBalance    uint64
	slotSub           *sgows.SlotSubscription
	slot              uint64
	waitSlot          map[uint64]bool
	status            map[string]*pipelineStatus
}

type pipelineStatus struct {
	parent            state.Pipeline
	parentData        cba.Pipeline
	lastPeriodStart   uint64 // last period (by start) that has been cranked
	lastAttempedCrank uint64 // last crank we have attempted
	ring              *cba.PeriodRing
	nextCrank         uint64
}

func loopInternal(ctx context.Context, internalC <-chan func(*internal), startErrorC chan<- error, rpcClient *sgorpc.Client, wsClient *sgows.Client, ci CrankInfo, controller state.Controller) {
	var err error
	errorC := make(chan error, 1)
	in := new(internal)
	in.ctx = ctx
	in.errorC = errorC
	in.closeSignalCList = make([]chan<- error, 0)
	in.ci = ci
	in.rpc = rpcClient
	in.ws = wsClient
	in.controller = controller
	in.crankerBalance = 0
	in.slot = 0
	in.status = make(map[string]*pipelineStatus)

	err = in.init()
	startErrorC <- err
	if err != nil {
		return
	}

	doneC := ctx.Done()

	periodSub := in.controller.Router.OnPeriod(util.Zero())
	defer periodSub.Unsubscribe()

	bidSub := in.controller.Router.OnBidSummary()
	defer bidSub.Unsubscribe()

	balC := in.crankerBalanceSub.RecvStream()
	balErrorC := in.crankerBalanceSub.RecvErr()
	defer in.crankerBalanceSub.Unsubscribe()
	slotC := in.slotSub.RecvStream()
	slotErrorC := in.slotSub.CloseSignal()
	defer in.slotSub.Unsubscribe()

out:
	for {
		select {
		case err = <-bidSub.ErrorC:
			break out
		case x := <-bidSub.StreamC:
			in.on_bid(x)
		case err = <-periodSub.ErrorC:
			break out
		case x := <-periodSub.StreamC:
			in.on_period(x)
		case err = <-slotErrorC:
			break out
		case d := <-slotC:
			x, ok := d.(*sgows.SlotResult)
			if !ok {
				err = errors.New("bad slot result")
				break out
			}
			in.on_slot(x.Slot)
		case err = <-balErrorC:
			break out
		case d := <-balC:
			x, ok := d.(*sgows.AccountResult)
			if !ok {
				err = errors.New("bad account result")
				break out
			}
			in.on_balance(x.Value.Lamports)
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)
		}
	}

	in.finish(err)

}

func (in *internal) get_status(id sgo.PublicKey) (*pipelineStatus, error) {

	p, present := in.status[id.String()]
	if !present {
		p = new(pipelineStatus)
		pipeline, err := in.controller.Router.PipelineById(id)
		if err != nil {
			return nil, err
		}
		p.parent = pipeline
		p.parentData, err = pipeline.Data()
		if err != nil {
			return nil, err
		}
		p.lastAttempedCrank = 0
		p.lastPeriodStart = 0
		p.nextCrank = 0
		in.status[id.String()] = p

	}
	return p, nil
}

// upgrade last period crank (by start time)
func (in *internal) on_bid(bs util.BidSummary) {
	status, err := in.get_status(bs.Pipeline)
	if err != nil {
		log.Debug(err)
		return
	}
	status.lastPeriodStart = bs.LastPeriodStart
}

// on a period, set the next crank time
func (in *internal) on_period(pr cba.PeriodRing) {
	status, err := in.get_status(pr.Pipeline)
	if err != nil {
		log.Debug(err)
		return
	}
	status.ring = &pr

	// check what the next slot time is
	period, present := getNextPeriod(in.slot, pr)
	if !present {
		return
	}
	status.nextCrank = period.Start
}

// find out if we need to run a crank
func (in *internal) on_slot(s uint64) {
	in.slot = s
	for _, status := range in.status {
		// for each pipeline, get the period ring?
		if status.nextCrank != 0 && status.nextCrank <= in.slot && status.lastAttempedCrank < in.slot {
			in.crank(status)
		}
	}
}

// run the crank in a separate goroutine
func (in *internal) crank(status *pipelineStatus) {
	status.lastAttempedCrank = in.slot

	if in.crankerBalance <= in.ci.BalanceThreshold {
		log.Errorf("Funds have been depleted.  Current balance: %d", in.crankerBalance)
		return
	}

	go loopCrank(in.ctx, in.rpc, in.ws, in.ci, status.parent.Id, status.parentData.Periods, status.parentData.Bids, in.controller.Router, in.errorC)
}

func (in *internal) on_balance(b uint64) {
	in.crankerBalance = b
}

func (in *internal) init() error {
	var err error
	in.crankerBalanceSub, err = in.ws.AccountSubscribeWithOpts(in.ci.CrankerKey.PublicKey(), sgorpc.CommitmentFinalized, sgo.EncodingBase64)
	if err != nil {
		return err
	}
	r, err := in.rpc.GetBalance(in.ctx, in.ci.CrankerKey.PublicKey(), sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	in.crankerBalance = r.Value

	in.slotSub, err = in.ws.SlotSubscribe()
	if err != nil {
		return err
	}

	return nil
}

func (in *internal) finish(err error) {
	log.Debug(err)

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}

func getNextPeriod(slot uint64, pr cba.PeriodRing) (period cba.Period, present bool) {
	present = false

	for i := uint16(0); i < pr.Length; i++ {
		period = pr.Ring[(pr.Start+i)%uint16(len(pr.Ring))]
		// get the current period
		if period.Start <= slot && slot < period.Start+period.Length {
			// check if there is another periods
			if i < pr.Length-1 {
				period = pr.Ring[(pr.Start+i+1)%uint16(len(pr.Ring))]
				present = true
			}
			return
		}
	}

	return
}
