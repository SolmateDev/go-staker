package cranker

import (
	"context"
	"errors"
	"fmt"
	"time"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type NewPeriodUpdateSub struct {
	PeriodUpdateC <-chan uint64
	DeleteC       chan<- int
	Id            int
}

func (npus NewPeriodUpdateSub) delete() {
	npus.DeleteC <- npus.Id
}

// Crank a validator pipeline.
func loopCrank(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, ci CrankInfo, pipelineId sgo.PublicKey, periodId sgo.PublicKey, bidId sgo.PublicKey, router state.PipelineRouter, mainErrorC chan<- error) {
	i := 0
	errorC := make(chan error, 1)
	doneC := ctx.Done()

	bidSub := router.OnBid(ci.CrankerKey.PublicKey())
	defer bidSub.Unsubscribe()

	var err error

out:
	for i < 10 {
		go loopCrankAttemptOutside(ctx, rpcClient, wsClient, ci, pipelineId, periodId, bidId, errorC)
		select {
		case err = <-bidSub.ErrorC:
			break out
		case x := <-bidSub.StreamC:
			if x.Pipeline.Equals(pipelineId) {
				// someone else has cranked
				break out
			}
		case <-doneC:
			break
		case err = <-errorC:
			if err == nil {
				break out
			} else {
				time.Sleep(5 * time.Second)
			}
		}
	}

	if err != nil {
		log.Infof("crank error: %s", err)
		mainErrorC <- err
	}
}

func loopCrankAttemptOutside(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, ci CrankInfo, pipelineId sgo.PublicKey, periodId sgo.PublicKey, bidId sgo.PublicKey, errorC chan<- error) {
	errorC <- loopCrankAttempt(ctx, rpcClient, wsClient, ci, pipelineId, periodId, bidId)
}

func loopCrankAttempt(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, ci CrankInfo, pipelineId sgo.PublicKey, periodId sgo.PublicKey, bidId sgo.PublicKey) error {

	instruction := cba.NewCrankInstruction(
		ci.ControllerId,
		ci.Controller.PcVault,
		pipelineId,
		periodId,
		bidId,
		ci.CrankerPcFund,
		ci.CrankerKey.PublicKey(),
		sgo.TokenProgramID,
		sgo.SysVarClockPubkey,
	)

	log.Debugf("attempting to crank with cranker=%s and funds=%s", ci.CrankerKey.PublicKey().String(), ci.CrankerPcFund.String())
	//log.Debugf("latest hash=%s", ci.LatestBlockHash.String())
	tx, err := sgo.NewTransactionBuilder().AddInstruction(instruction.Build()).SetFeePayer(ci.CrankerKey.PublicKey()).SetRecentBlockHash(ci.LatestBlockHash).Build()
	if err != nil {
		//log.Debug(err)
		return err
	}
	_, err = tx.Sign(func(pub sgo.PublicKey) *sgo.PrivateKey {
		if pub.Equals(ci.CrankerKey.PublicKey()) {
			return &ci.CrankerKey
		}
		return nil
	})
	if err != nil {
		//log.Debug(err)
		return err
	}

	sig, err := rpcClient.SendTransactionWithOpts(ctx, tx, sgorpc.TransactionOpts{})
	if err != nil {
		//log.Debug(err)
		return err
	}
	sub, err := wsClient.SignatureSubscribe(sig, sgorpc.CommitmentConfirmed)
	if err != nil {
		//log.Debug(err)
		return err
	}

	doneC := ctx.Done()
	streamC := sub.RecvStream()

	select {
	case <-doneC:
		err = errors.New("time out")
	case d := <-streamC:
		result, ok := d.(*sgows.SignatureResult)
		if ok {
			if result.Value.Err != nil {
				err = fmt.Errorf("%s", result.Value.Err)
			}
		} else {
			err = errors.New("failed to deserialize")
		}
	}
	if err != nil {
		//log.Debug(err)
		return err
	}

	return nil
}
