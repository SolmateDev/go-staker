package state

import (
	"context"
	"encoding/binary"
	"errors"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/util"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	bin "github.com/gagliardetto/binary"
)

type SubscriptionProgramGroup struct {
	ControllerC chan<- util.ResponseChannel[cba.Controller]
	PipelineC   chan<- util.ResponseChannel[util.PipelineGroup]
	BidListC    chan<- util.ResponseChannel[cba.BidList]
	BidSummaryC chan<- util.ResponseChannel[util.BidSummary]
	PeriodRingC chan<- util.ResponseChannel[cba.PeriodRing]
}

type internalSubscriptionProgramGroup struct {
	controller *util.SubHome[cba.Controller]
	pipeline   *util.SubHome[util.PipelineGroup]
	bidList    *util.SubHome[cba.BidList]
	bidSummary *util.SubHome[util.BidSummary]
	periodRing *util.SubHome[cba.PeriodRing]
}

/*	updateBidC        chan<- util.ResponseChannel[util.BidGroup]
	updatePeriodC     chan<- util.ResponseChannel[cba.PeriodRing]
	updateBidSummaryC chan<- util.ResponseChannel[util.BidSummary]
*/
func SubscribeProgramAll(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, errorC chan<- error) (*SubscriptionProgramGroup, error) {

	sub, err := wsClient.ProgramSubscribe(cba.ProgramID, sgorpc.CommitmentFinalized)
	if err != nil {
		return nil, err
	}

	ans := new(SubscriptionProgramGroup)
	in := new(internalSubscriptionProgramGroup)
	in.controller = util.CreateSubHome[cba.Controller]()
	ans.ControllerC = in.controller.ReqC

	in.pipeline = util.CreateSubHome[util.PipelineGroup]()
	ans.PipelineC = in.pipeline.ReqC
	in.bidList = util.CreateSubHome[cba.BidList]()
	ans.BidListC = in.bidList.ReqC
	in.bidSummary = util.CreateSubHome[util.BidSummary]()
	ans.BidSummaryC = in.bidSummary.ReqC
	in.periodRing = util.CreateSubHome[cba.PeriodRing]()
	ans.PeriodRingC = in.periodRing.ReqC

	go loopSubscribePipeline(ctx, sub, in, errorC)

	return ans, nil
}

func loopSubscribePipeline(ctx context.Context, sub *sgows.ProgramSubscription, in *internalSubscriptionProgramGroup, errorC chan<- error) {

	doneC := ctx.Done()
	streamC := sub.RecvStream()
	streamErrorC := sub.CloseSignal()

	var err error

	D_controller := binary.BigEndian.Uint64(cba.ControllerDiscriminator[:])
	D_pipeline := binary.BigEndian.Uint64(cba.PipelineDiscriminator[:])
	D_bidlist := binary.BigEndian.Uint64(cba.BidListDiscriminator[:])
	D_periodring := binary.BigEndian.Uint64(cba.PeriodRingDiscriminator[:])

	//bidSubMap:=make(map[string]x)

out:
	for {
		select {

		case <-doneC:
			break out
		case id := <-in.controller.DeleteC:
			in.controller.Delete(id)
		case r := <-in.controller.ReqC:
			in.controller.Receive(r)
		case id := <-in.pipeline.DeleteC:
			in.pipeline.Delete(id)
		case r := <-in.pipeline.ReqC:
			in.pipeline.Receive(r)
		case id := <-in.periodRing.DeleteC:
			in.periodRing.Delete(id)
		case r := <-in.periodRing.ReqC:
			in.periodRing.Receive(r)
		case id := <-in.bidList.DeleteC:
			in.bidList.Delete(id)
		case r := <-in.bidList.ReqC:
			in.bidList.Receive(r)
		case id := <-in.bidSummary.DeleteC:
			in.bidSummary.Delete(id)
		case r := <-in.bidSummary.ReqC:
			in.bidSummary.Receive(r)
		case err = <-streamErrorC:
			break out
		case d := <-streamC:
			x, ok := d.(*sgows.ProgramResult)
			if !ok {
				err = errors.New("bad program result")
				break out
			}
			data := x.Value.Account.Data.GetBinary()

			if 8 <= len(data) {
				switch binary.BigEndian.Uint64(data[0:8]) {
				case D_controller:
					y := new(cba.Controller)
					err = bin.UnmarshalBorsh(y, data)
					if err != nil {
						break out
					}
					in.controller.Broadcast(*y)
				case D_pipeline:
					y := new(cba.Pipeline)
					err = bin.UnmarshalBorsh(y, data)
					if err != nil {
						break out
					}
					in.pipeline.Broadcast(util.PipelineGroup{
						Id:   x.Value.Pubkey,
						Data: *y,
					})
				case D_bidlist:
					y := new(cba.BidList)
					err = bin.UnmarshalBorsh(y, data)
					if err != nil {
						break out
					}
					in.bidSummary.Broadcast(util.BidSummary{
						LastPeriodStart: y.LastPeriodStart,
						Pipeline:        y.Pipeline,
						TotalDeposits:   y.TotalDeposits,
					})
					in.bidList.Broadcast(*y)
				case D_periodring:
					y := new(cba.PeriodRing)
					err = bin.UnmarshalBorsh(y, data)
					if err != nil {
						break out
					}
					in.periodRing.Broadcast(*y)
				default:
				}
			}

		}
	}

	errorC <- err
}
