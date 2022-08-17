package admin

import (
	"context"
	"errors"
	"io"

	"github.com/SolmateDev/go-staker/limiter"
	pba "github.com/SolmateDev/go-staker/proto/admin"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type Server struct {
	pba.UnimplementedValidatorServer
	ctx          context.Context
	controller   state.Controller
	userTxBuffer limiter.ItemBuffer[limiter.JobSubmission]
	internalC    chan<- func(*internal)
	reqLogC      chan<- util.ResponseChannel[*pba.LogLine]
}

func Attach(ctx context.Context, grpcServer *grpc.Server, controller state.Controller, subSlot state.SlotHome, rpcClient *sgorpc.Client, wsClient *sgows.Client, validator sgo.PublicKey, admin sgo.PrivateKey, userTxBuffer limiter.ItemBuffer[limiter.JobSubmission]) error {
	log.Debug("creating owner grpc server")

	internalC := make(chan func(*internal), 10)
	homeLog := util.CreateSubHome[*pba.LogLine]()
	reqLogC := homeLog.ReqC
	e1 := Server{
		ctx: ctx, controller: controller, internalC: internalC, reqLogC: reqLogC, userTxBuffer: userTxBuffer,
	}
	router := controller.Router
	pipeline, err := router.PipelineByValidator(validator)
	if err != nil {
		return err
	}

	go loopInternal(ctx, internalC, rpcClient, wsClient, admin, controller, pipeline, subSlot, homeLog)

	pba.RegisterValidatorServer(grpcServer, e1)

	return nil
}

func (e1 Server) GetDefault(ctx context.Context, req *pba.Empty) (*pba.Settings, error) {
	return nil, errors.New("not implemented yet")
}

func (e1 Server) SetDefault(ctx context.Context, req *pba.Settings) (*pba.Settings, error) {
	return nil, errors.New("not implemented yet")
}

func (e1 Server) GetLogStream(req *pba.Empty, stream pba.Validator_GetLogStreamServer) error {
	ctx := stream.Context()
	sub := util.SubscriptionRequest(e1.reqLogC, func(x *pba.LogLine) bool { return true })
	doneC := ctx.Done()
	var err error
out:
	for {
		select {
		case <-doneC:
			break out
		case err = <-sub.ErrorC:
			break out
		case d := <-sub.StreamC:
			err = stream.Send(d)
			if err == io.EOF {
				err = nil
				break out
			} else if err != nil {
				break out
			}
		}
	}
	return err
}
