package state

import (
	"context"
	"errors"
	"time"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type Rate struct {
	N uint64
	D uint64
}

func PipelineId(controllerId sgo.PublicKey, validator sgo.PublicKey) (ans sgo.PublicKey, bump uint8, err error) {

	validatorName := []byte("validator_pipeline")
	return sgo.FindProgramAddress([][]byte{
		validatorName,
		controllerId.Bytes(),
		validator.Bytes(),
	}, cba.ProgramID)
}

type Pipeline struct {
	Id                sgo.PublicKey
	internalC         chan<- func(*pipelineInternal)
	rpc               *sgorpc.Client
	ws                *sgows.Client
	updatePipelineC   chan<- util.ResponseChannel[cba.Pipeline]
	updatePeriodRingC chan<- util.ResponseChannel[cba.PeriodRing]
	updateBidListC    chan<- util.ResponseChannel[cba.BidList]
}

func CreatePipeline(ctx context.Context, id sgo.PublicKey, data *cba.Pipeline, rpcClient *sgorpc.Client, wsClient *sgows.Client) (Pipeline, error) {
	internalC := make(chan func(*pipelineInternal), 10)

	pipelineHome := util.CreateSubHome[cba.Pipeline]()
	periodHome := util.CreateSubHome[cba.PeriodRing]()
	bidHome := util.CreateSubHome[cba.BidList]()

	go loopPipelineInternal(ctx, internalC, id, data, pipelineHome, periodHome, bidHome)
	return Pipeline{
		Id: id, internalC: internalC,
		rpc:               rpcClient,
		ws:                wsClient,
		updatePipelineC:   pipelineHome.ReqC,
		updatePeriodRingC: periodHome.ReqC,
		updateBidListC:    bidHome.ReqC,
	}, nil
}

func (e1 Pipeline) Data() (cba.Pipeline, error) {
	errorC := make(chan error, 1)
	ansC := make(chan cba.Pipeline, 1)
	e1.internalC <- func(in *pipelineInternal) {
		if in.data == nil {
			errorC <- errors.New("no data for pipeline")
		} else {
			errorC <- nil
			ansC <- *in.data
		}
	}
	err := <-errorC
	if err != nil {
		return cba.Pipeline{}, err
	}
	return <-ansC, nil
}

func (e1 Pipeline) Update(data cba.Pipeline) {
	e1.internalC <- func(in *pipelineInternal) {
		in.on_data(data)
	}
}

func (e1 Pipeline) filter_OnPipeline(p cba.Pipeline) bool {
	return true
}

func (e1 Pipeline) OnPipeline() util.Subscription[cba.Pipeline] {
	return util.SubscriptionRequest(e1.updatePipelineC, e1.filter_OnPipeline)
}

// get alerted when the pipeline data has been changed
func (in *pipelineInternal) on_data(data cba.Pipeline) {
	in.data = &data
	in.pipelineHome.Broadcast(data)
}

func (e1 Pipeline) UpdatePeriod(ring cba.PeriodRing) {
	e1.internalC <- func(in *pipelineInternal) {
		in.on_period(ring)
	}
}

func (e1 Pipeline) filter_OnPeriod(p cba.PeriodRing) bool {
	return true
}

// get alerted when a new period has been appended
func (e1 Pipeline) OnPeriod() util.Subscription[cba.PeriodRing] {

	return util.SubscriptionRequest(e1.updatePeriodRingC, e1.filter_OnPeriod)
}

func (in *pipelineInternal) on_period(ring cba.PeriodRing) {
	in.periods = &ring
	in.periodHome.Broadcast(ring)
}

func (e1 Pipeline) UpdateBid(list cba.BidList) {
	e1.internalC <- func(in *pipelineInternal) {
		in.on_bid(list)
	}
}

func (e1 Pipeline) filter_OnBid(p cba.BidList) bool {
	return true
}

// get alerted when a bid has been inserted
func (e1 Pipeline) OnBid() util.Subscription[cba.BidList] {
	return util.SubscriptionRequest(e1.updateBidListC, e1.filter_OnBid)
}

func (in *pipelineInternal) on_bid(list cba.BidList) {
	in.bids = &list
	in.bidHome.Broadcast(list)
}

type pipelineInternal struct {
	ctx          context.Context
	errorC       chan<- error
	id           sgo.PublicKey
	data         *cba.Pipeline
	bids         *cba.BidList
	periods      *cba.PeriodRing
	pipelineHome *util.SubHome[cba.Pipeline]
	periodHome   *util.SubHome[cba.PeriodRing]
	bidHome      *util.SubHome[cba.BidList]
}

const PROXY_MAX_CONNECTION_ATTEMPT = 10
const PROXY_RECONNECT_SLEEP = 10 * time.Second

func loopPipelineInternal(ctx context.Context, internalC <-chan func(*pipelineInternal), id sgo.PublicKey, data *cba.Pipeline, pipelineHome *util.SubHome[cba.Pipeline], periodHome *util.SubHome[cba.PeriodRing], bidHome *util.SubHome[cba.BidList]) {

	var err error

	errorC := make(chan error, 1)
	doneC := ctx.Done()

	in := new(pipelineInternal)
	in.ctx = ctx
	in.errorC = errorC
	in.id = id
	in.data = data
	in.pipelineHome = pipelineHome
	in.periodHome = periodHome
	in.bidHome = bidHome

out:
	for {
		select {
		case x := <-in.pipelineHome.ReqC:
			in.pipelineHome.Receive(x)
		case x := <-in.periodHome.ReqC:
			in.periodHome.Receive(x)
		case x := <-in.bidHome.ReqC:
			in.bidHome.Receive(x)
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)
		}
	}

	if err != nil {
		log.Debug(err)
	}
}

func SubscribePipeline(wsClient *sgows.Client) (*sgows.ProgramSubscription, error) {
	return wsClient.ProgramSubscribe(cba.ProgramID, sgorpc.CommitmentFinalized)
	//return wsClient.ProgramSubscribeWithOpts(cba.ProgramID, sgorpc.CommitmentFinalized, sgo.EncodingBase64, []sgorpc.RPCFilter{
	//{
	//DataSize: util.STRUCT_SIZE_PIPELINE,
	//Memcmp: &sgorpc.RPCFilterMemcmp{
	//	Offset: 0,
	//	Bytes:  cba.PipelineDiscriminator[:],
	//},
	//},
	//})
}
