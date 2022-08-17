package state

import (
	"context"
	"errors"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	bin "github.com/gagliardetto/binary"
	log "github.com/sirupsen/logrus"
)

type PipelineRouter struct {
	//updatePipelineC   chan<- util.ResponseChannel[sgo.PublicKey]
	//updateBidC        chan<- util.ResponseChannel[util.BidForBidder]
	//updateBidSummaryC chan<- util.ResponseChannel[util.BidSummary]
	allGroup  SubscriptionProgramGroup
	internalC chan<- func(*pipelineRouterInternal)
}

func CreatePipelineRouter(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client) (PipelineRouter, error) {

	startErrorC := make(chan error, 1)
	subErrorC := make(chan error, 1)

	all, err := SubscribeProgramAll(ctx, rpcClient, wsClient, subErrorC)
	if err != nil {
		return PipelineRouter{}, err
	}

	pipelineG := util.SubscriptionRequest(all.PipelineC, func(pg util.PipelineGroup) bool {
		return true
	})
	bidListG := util.SubscriptionRequest(all.BidListC, func(bl cba.BidList) bool { return true })
	periodRingG := util.SubscriptionRequest(all.PeriodRingC, func(pr cba.PeriodRing) bool { return true })

	internalC := make(chan func(*pipelineRouterInternal), 10)

	go loopPipelineRouter(ctx, internalC, rpcClient, wsClient, subErrorC, startErrorC, pipelineG, bidListG, periodRingG)
	err = <-startErrorC
	if err != nil {
		return PipelineRouter{}, err
	}

	return PipelineRouter{
		internalC: internalC,
		allGroup:  *all,
	}, nil
}

func (e1 PipelineRouter) OnPipeline() util.Subscription[util.PipelineGroup] {
	return util.SubscriptionRequest(e1.allGroup.PipelineC, func(pg util.PipelineGroup) bool { return true })
}

// set pipeline to 0 to receive all periods
func (e1 PipelineRouter) OnPeriod(pipeline sgo.PublicKey) util.Subscription[cba.PeriodRing] {
	return util.SubscriptionRequest(e1.allGroup.PeriodRingC, func(pr cba.PeriodRing) bool {
		if pipeline.IsZero() {
			return true
		} else if pr.Pipeline.Equals(pipeline) {
			return true
		}
		return false
	})

}

func (e1 PipelineRouter) OnBid(bidder sgo.PublicKey) util.Subscription[cba.BidList] {
	return util.SubscriptionRequest(e1.allGroup.BidListC, func(bg cba.BidList) bool {
		for _, v := range bg.Book {
			if !v.IsBlank {
				if v.User.Equals(bidder) {
					return true
				}
			}
		}
		return false
	})
}

func (e1 PipelineRouter) OnBidSummary() util.Subscription[util.BidSummary] {
	return util.SubscriptionRequest(e1.allGroup.BidSummaryC, func(bg util.BidSummary) bool {
		return true
	})
}

func (e1 PipelineRouter) AddPipeline(id sgo.PublicKey, data cba.Pipeline) error {
	errorC := make(chan error, 1)
	e1.internalC <- func(in *pipelineRouterInternal) {
		errorC <- in.add(id, data)
	}
	return <-errorC
}

func (in *pipelineRouterInternal) add(id sgo.PublicKey, data cba.Pipeline) error {
	var err error
	p, present := in.router[id.String()]
	if present {
		p.Update(data)
		return nil
	}

	p, err = CreatePipeline(in.ctx, id, &data, in.rpc, in.ws)
	if err != nil {
		return err
	}
	in.router[id.String()] = p
	in.routerByValidator[data.Validator.String()] = p

	return nil
}

func (e1 PipelineRouter) PipelineById(id sgo.PublicKey) (Pipeline, error) {
	errorC := make(chan error, 1)
	ansC := make(chan Pipeline, 1)
	e1.internalC <- func(in *pipelineRouterInternal) {
		p, present := in.router[id.String()]
		if present {
			errorC <- nil
			ansC <- p
		} else {
			errorC <- errors.New("no such pipeline")
		}
	}
	err := <-errorC
	if err != nil {
		return Pipeline{}, err
	} else {
		return <-ansC, nil
	}
}

func (e1 PipelineRouter) PipelineByValidator(validator sgo.PublicKey) (Pipeline, error) {
	errorC := make(chan error, 1)
	ansC := make(chan Pipeline, 1)
	e1.internalC <- func(in *pipelineRouterInternal) {
		p, present := in.routerByValidator[validator.String()]
		if present {
			errorC <- nil
			ansC <- p
		} else {
			errorC <- errors.New("no such pipeline")
		}
	}
	err := <-errorC
	if err != nil {
		return Pipeline{}, err
	} else {
		return <-ansC, nil
	}
}

func (e1 PipelineRouter) AddBid(list cba.BidList) error {
	errorC := make(chan error, 1)
	e1.internalC <- func(in *pipelineRouterInternal) {
		errorC <- in.add_bid(list)
	}
	return <-errorC
}

func (in *pipelineRouterInternal) add_bid(list cba.BidList) error {
	p, present := in.router[list.Pipeline.String()]
	if !present {
		return errors.New("no pipeline")
	}
	p.UpdateBid(list)

	return nil
}

func (e1 PipelineRouter) AddPeriod(ring cba.PeriodRing) error {
	errorC := make(chan error, 1)
	e1.internalC <- func(in *pipelineRouterInternal) {
		errorC <- in.add_period(ring)
	}
	return <-errorC
}

func (in *pipelineRouterInternal) add_period(ring cba.PeriodRing) error {
	p, present := in.router[ring.Pipeline.String()]
	if !present {
		return errors.New("no pipeline")
	}
	p.UpdatePeriod(ring)
	return nil
}

type pipelineRouterInternal struct {
	ctx               context.Context
	closeSignalCList  []chan<- error
	errorC            chan<- error
	rpc               *sgorpc.Client
	ws                *sgows.Client
	router            map[string]Pipeline // map pipeline Id to Pipeline
	routerByValidator map[string]Pipeline // map validator Id to Pipeline
}

func loopPipelineRouter(
	ctx context.Context,
	internalC <-chan func(*pipelineRouterInternal),
	rpcClient *sgorpc.Client,
	wsClient *sgows.Client,
	allErrorC <-chan error,
	startErrorC chan<- error,
	pipelineG util.Subscription[util.PipelineGroup],
	bidListG util.Subscription[cba.BidList],
	periodRingG util.Subscription[cba.PeriodRing],
) {
	var err error
	doneC := ctx.Done()
	errorC := make(chan error, 1)

	in := new(pipelineRouterInternal)
	in.ctx = ctx
	in.errorC = errorC
	in.router = make(map[string]Pipeline)
	in.routerByValidator = make(map[string]Pipeline)
	in.closeSignalCList = make([]chan<- error, 0)
	in.rpc = rpcClient
	in.ws = wsClient

	// map pipelineId to ___
	unmatchedPeriodRings := make(map[string]cba.PeriodRing)
	unmatchedBidLists := make(map[string]cba.BidList)

	err = in.init()
	startErrorC <- err
	if err != nil {
		return
	}

out:
	for {
		select {
		case <-doneC:
			break out
		case err = <-allErrorC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)
		case err = <-bidListG.ErrorC:
			break out
		case list := <-bidListG.StreamC:
			// received bid list update
			log.Debug("received bid list_______")
			p, present := in.router[list.Pipeline.String()]
			if !present {
				log.Debug("pipeline not present")
				unmatchedBidLists[list.Pipeline.String()] = list
			} else {
				p.UpdateBid(list)
				in.on_bid(&list)
			}

		case err = <-periodRingG.ErrorC:
			break out
		case ring := <-periodRingG.StreamC:
			// received period ring update
			log.Debug("received period ring_____")

			p, present := in.router[ring.Pipeline.String()]
			if !present {
				log.Debug("pipeline not present")
				unmatchedPeriodRings[ring.Pipeline.String()] = ring
			} else {
				p.UpdatePeriod(ring)
				in.on_period(&ring)
			}

		case err = <-pipelineG.ErrorC:
			break out
		case pg := <-pipelineG.StreamC:
			// received pipeline update
			log.Debug("received pipeline update____")
			p, present := in.router[pg.Id.String()]
			if !present {
				data := pg.Data
				p, err = CreatePipeline(in.ctx, pg.Id, &data, in.rpc, in.ws)
				if err != nil {
					break out
				}
				in.router[p.Id.String()] = p
				in.routerByValidator[data.Validator.String()] = p

				x, present := unmatchedBidLists[p.Id.String()]
				if present {
					p.UpdateBid(x)
					delete(unmatchedBidLists, p.Id.String())
				}
				y, present := unmatchedPeriodRings[p.Id.String()]
				if present {
					p.UpdatePeriod(y)
					delete(unmatchedPeriodRings, p.Id.String())
				}
			}
			p.Update(pg.Data)
			in.on_pipeline(pg.Id, pg.Data)

		}
	}

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}

func (in *pipelineRouterInternal) on_pipeline(id sgo.PublicKey, data cba.Pipeline) {

}

func (in *pipelineRouterInternal) on_bid(bl *cba.BidList) {
	// do nothing
}

func (in *pipelineRouterInternal) on_period(pr *cba.PeriodRing) {
	// do nothing
}

// download all of the currently existing validator pipelines
func (in *pipelineRouterInternal) init() error {
	ctx := in.ctx
	//prefix := base58.Encode(cba.PipelineDiscriminator[:])
	a, err := in.rpc.GetProgramAccountsWithOpts(ctx, cba.ProgramID, &sgorpc.GetProgramAccountsOpts{
		Commitment: sgorpc.CommitmentFinalized,
		Encoding:   sgo.EncodingBase64,
		//Filters: []sgorpc.RPCFilter{
		//	{
		//		Memcmp: &sgorpc.RPCFilterMemcmp{Offset: 0, Bytes: []byte(cba.PipelineDiscriminator[:])},
		//	},
		//},
	})
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	for i := 0; i < len(a); i++ {
		data := a[i].Account.Data.GetBinary()
		if util.Compare(data[0:8], cba.PipelineDiscriminator[:]) {
			id := a[i].Pubkey
			y := new(cba.Pipeline)
			err = bin.UnmarshalBorsh(y, data)
			if err != nil {
				return err
			}
			err = in.add(id, *y)
			if err != nil {
				return err
			}
		}

	}
	return nil
}
