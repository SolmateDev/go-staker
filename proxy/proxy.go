// The proxy receives transactions from bandwidth buyers and forwards those transactions onto the validator via JSON RPC send_tx call.
package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/SolmateDev/go-staker/db/mem"
	"github.com/SolmateDev/go-staker/limiter"
	"github.com/SolmateDev/go-staker/proxy/admin"

	"github.com/SolmateDev/go-staker/proxy/work"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type Proxy struct {
	internalC chan<- func(*internal)
	errorC    chan<- error
}

func (s *Proxy) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	s.internalC <- func(in *internal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

func (s *Proxy) Close() {
	doneC := s.CloseSignal()
	s.errorC <- errors.New("close signal")
	<-doneC
}

func Run(ctxOutside context.Context, config *util.Configuration, rpcClient *sgorpc.Client, wsClient *sgows.Client) (Proxy, error) {
	var err error

	ctx, cancel := context.WithCancel(ctxOutside)

	if rpcClient == nil && wsClient == nil {
		if len(config.ApiKey) == 0 {
			rpcClient = sgorpc.New(config.RpcUrl)
			wsClient, err = sgows.Connect(ctx, config.WsUrl)
		} else {
			//headers:=http.Header{"Jwt":}
			headers := http.Header{}
			rpcClient = sgorpc.NewWithHeaders(config.RpcUrl, headers)
			wsClient, err = sgows.ConnectWithHeaders(ctx, config.WsUrl, headers)
		}
	} else if rpcClient == nil || wsClient == nil {
		cancel()
		return Proxy{}, errors.New("either rpc or ws client is missing")
	}

	subSlot, err := state.SubscribeSlot(ctx, wsClient)
	if err != nil {
		cancel()
		return Proxy{}, err
	}

	grpcServer := grpc.NewServer()
	errorC := make(chan error, 5)
	reflection.Register(grpcServer)
	lis, err := net.Listen("tcp", config.GrpcListenUrl)
	if err != nil {
		cancel()
		return Proxy{}, err
	}

	userTxBuffer := limiter.CreateBidderTransactionBuffer(ctx)

	controller, err := state.CreateController(ctx, rpcClient, wsClient, state.VERSION_1)
	if err != nil {
		cancel()
		return Proxy{}, err
	}

	validatorPipeline, err := controller.Router.PipelineByValidator(config.Validator)
	if err != nil {
		cancel()
		return Proxy{}, err
	}

	tpsUpdateErrorC := make(chan error, 1)

	go loopStopOnError(ctx, cancel, tpsUpdateErrorC)

	if config.ValidatorAdmin != nil {
		vc := config.ValidatorAdmin
		err = admin.Attach(ctx, grpcServer, controller, subSlot, rpcClient, wsClient, config.Validator, vc.Key, userTxBuffer)
		if err != nil {
			cancel()
			return Proxy{}, err
		}
	}

	workDb, err := mem.CreateInMemoryDb(ctx)
	if err != nil {
		cancel()
		return Proxy{}, err
	}

	err = work.Attach(ctx, grpcServer, controller, userTxBuffer, workDb)
	if err != nil {
		cancel()
		return Proxy{}, err
	}

	// send tps from loopInternal to loopUpdateTpsBandwidthAllocation ;
	updateValidatorTPSC := make(chan float64, 1)
	go loopUpdateTpsBandwidthAllocation(ctx, tpsUpdateErrorC, userTxBuffer, updateValidatorTPSC, validatorPipeline)

	internalC := make(chan func(*internal), 10)
	go loopInternal(ctx, internalC, errorC, cancel, rpcClient, wsClient, userTxBuffer, config.Validator, controller, updateValidatorTPSC, workDb)

	go loopClose(ctx, lis)

	go loopListen(grpcServer, lis, errorC)

	if err != nil {
		errorC <- err
		return Proxy{}, err
	}

	return Proxy{
		internalC: internalC,
	}, nil
}

func loopStopOnError(ctx context.Context, cancel context.CancelFunc, errorC <-chan error) {
	doneC := ctx.Done()
	select {
	case <-errorC:
	case <-doneC:
	}
	cancel()
}

func loopListen(grpcServer *grpc.Server, lis net.Listener, errorC chan<- error) {
	errorC <- grpcServer.Serve(lis)
}

func loopClose(ctx context.Context, lis net.Listener) {
	<-ctx.Done()
	err := lis.Close()
	if err != nil {
		log.Debug(err)
	}
}
