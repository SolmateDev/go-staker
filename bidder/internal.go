package bidder

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"time"

	cba "github.com/SolmateDev/go-solmate-cba"
	ct "github.com/SolmateDev/go-staker/client"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"

	sgo "github.com/SolmateDev/solana-go"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type internal struct {
	ctx              context.Context
	errorC           chan<- error
	closeSignalCList []chan<- error
	config           *Configuration
	rpc              *sgorpc.Client
	ws               *sgows.Client
	grpcClient       *ct.Client
	controller       state.Controller
	updatePipelineC  chan<- pipelineResult
	pipelines        map[string]state.Pipeline // mape pipeline id to Pipeline
	proxyConn        map[string]*ct.Client
	updateConnC      chan<- pipelineProxyConnection
	bidder           sgo.PrivateKey
	pcVault          *sgotkn.Account // the token account storing funds used to bid for bandwidth
}

type pipelineProxyConnection struct {
	id   sgo.PublicKey
	conn *grpc.ClientConn
}

func loopInternal(ctx context.Context, internalC <-chan func(*internal), startErrorC chan<- error, configReadErrorC <-chan error, configC <-chan Configuration, rpcClient *sgorpc.Client, wsClient *sgows.Client, bidder sgo.PrivateKey, pcVault *sgotkn.Account, controller state.Controller) {

	var err error
	doneC := ctx.Done()
	errorC := make(chan error, 1)
	updatePipelineC := make(chan pipelineResult, 1)
	updateConnC := make(chan pipelineProxyConnection, 10)

	in := new(internal)
	in.ctx = ctx
	in.errorC = errorC
	in.closeSignalCList = make([]chan<- error, 0)
	in.rpc = rpcClient
	in.ws = wsClient
	in.controller = controller
	in.pipelines = make(map[string]state.Pipeline)
	in.bidder = bidder
	in.pcVault = pcVault
	in.updatePipelineC = updatePipelineC
	in.updateConnC = updateConnC
	in.proxyConn = make(map[string]*ct.Client)

	bidSub := controller.Router.OnBid(in.bidder.PublicKey())
	defer bidSub.Unsubscribe()

	bidSummarySub := controller.Router.OnBidSummary()
	defer bidSummarySub.Unsubscribe()

	err = in.init()
	startErrorC <- err
	if err != nil {
		return
	}

out:
	for {
		select {
		case u := <-updateConnC:
			x, present := in.proxyConn[u.id.String()]
			if present {
				x.Close()
			}
			client := ct.Create(in.ctx, u.conn, in.bidder, in.pcVault)
			in.proxyConn[u.id.String()] = client
		case x := <-updatePipelineC:
			in.on_pipeline(x)
		case err = <-bidSummarySub.ErrorC:
			break out
		case x := <-bidSummarySub.StreamC:
			err = in.on_bid_summary(x)
			if err != nil {
				break out
			}
		case err = <-bidSub.ErrorC:
			break out
		case x := <-bidSub.StreamC:
		lookbid:
			for _, v := range x.Book {
				if v.User.Equals(in.bidder.PublicKey()) {
					err = in.on_bid(util.BidForBidder{
						LastPeriodStart: x.LastPeriodStart,
						Pipeline:        x.Pipeline,
						Bid:             v,
					})
					break lookbid
				}
			}
			if err != nil {
				break out
			}

		case err = <-configReadErrorC:
			break out
		case c := <-configC:
			in.config = &c
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

func (in *internal) on_pipeline(p pipelineResult) error {
	log.Debug("pipeline update")
	return nil
}

func (in *internal) pipeline_lookup(id sgo.PublicKey) (state.Pipeline, error) {
	var err error
	p, present := in.pipelines[id.String()]
	if present {
		return p, nil
	} else {
		p, err = in.controller.Router.PipelineById(id)
		if err != nil {
			return state.Pipeline{}, err
		}
		in.pipelines[id.String()] = p

		go loopPipelineUpdate(in.ctx, in.controller, p, in.errorC, in.updateConnC, id, in.updatePipelineC)
		return p, nil
	}

}

const PIPELINE_SLEEP_CONNECTION = 1 * time.Minute

type pipelineResult struct {
	id   sgo.PublicKey
	data cba.Pipeline
}

func loopPipelineUpdate(ctx context.Context, controller state.Controller, pipeline state.Pipeline, errorC chan<- error, connC chan<- pipelineProxyConnection, id sgo.PublicKey, resultC chan<- pipelineResult) {
	sub := pipeline.OnPipeline()
	defer sub.Unsubscribe()
	doneC := ctx.Done()
	var err error

	proxyAddress := cba.ProxyAddress{Url: []byte("")}

	data, err := pipeline.Data()
	if err != nil {
		errorC <- err
		return
	}
	var conn *grpc.ClientConn

	waitC := make(chan struct{}, 1)
	// for an attempted connection
	waitC <- struct{}{}

out:
	for {
		select {
		case <-waitC:
			// need a new connection
			conn, err = ConnectViaProxyAddress(ctx, data.Address)
			if err != nil {
				log.Debug(err)
				go loopWait(ctx, waitC, PIPELINE_SLEEP_CONNECTION, data.Address)
			} else {
				connC <- pipelineProxyConnection{id: id, conn: conn}
			}
		case <-doneC:
			break out
		case err = <-sub.ErrorC:
			break out
		case data = <-sub.StreamC:
			resultC <- pipelineResult{id: id, data: data}
			if string(data.Address.Url) != string(proxyAddress.Url) {
				proxyAddress = data.Address
				go loopWait(ctx, waitC, PIPELINE_SLEEP_CONNECTION, proxyAddress)
			}
		}
	}
	if err != nil {
		errorC <- err
	}
}

func ConnectViaProxyAddress(ctx context.Context, address cba.ProxyAddress) (*grpc.ClientConn, error) {
	url := string(address.Url)
	if strings.HasPrefix(url, "ssl://") {
		x := strings.Split(url, ":")
		if len(x) == 0 {
			return nil, errors.New("no host name")
		}
		config := &tls.Config{
			ServerName: x[0],
		}
		return grpc.DialContext(ctx, url[len("ssl://"):], grpc.WithTransportCredentials(credentials.NewTLS(config)))
	} else {
		return grpc.DialContext(ctx, url, grpc.WithInsecure())
	}

}

func loopWait(ctx context.Context, waitC chan<- struct{}, waitTime time.Duration, proxyAddress cba.ProxyAddress) {
	doneC := ctx.Done()
	select {
	case <-doneC:
	case <-time.After(waitTime):
	}
	waitC <- struct{}{}
}

func (in *internal) on_bid_summary(bg util.BidSummary) error {
	in.pipeline_lookup(bg.Pipeline)
	return nil
}

func (in *internal) on_bid(bg util.BidForBidder) error {
	in.pipeline_lookup(bg.Pipeline)
	return nil
}

func (in *internal) finish(err error) {
	log.Debug(err)
	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}

func (in *internal) init() error {

	return nil
}
