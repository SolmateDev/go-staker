package work

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"time"

	"github.com/SolmateDev/go-staker/auth"
	"github.com/SolmateDev/go-staker/db"
	"github.com/SolmateDev/go-staker/errormsg"
	"github.com/SolmateDev/go-staker/limiter"
	pbj "github.com/SolmateDev/go-staker/proto/job"
	"github.com/SolmateDev/go-staker/state"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type Server struct {
	pbj.UnimplementedWorkServer
	ctx          context.Context
	controller   state.Controller
	userTxBuffer limiter.ItemBuffer[limiter.JobSubmission]
	em           errormsg.ErrorMessage
	db           db.WorkDb
}

func Attach(ctx context.Context, grpcServer *grpc.Server, controller state.Controller, userTxBuffer limiter.ItemBuffer[limiter.JobSubmission], workDb db.WorkDb) error {
	log.Debug("creating work grpc server")
	rand.Seed(time.Now().UnixNano())

	em := errormsg.CreateFromEnv()

	e1 := Server{
		ctx: ctx, controller: controller, userTxBuffer: userTxBuffer, em: em, db: workDb,
	}

	pbj.RegisterWorkServer(grpcServer, e1)

	return nil
}

func (e1 Server) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	trackerC := e1.controller.CloseSignal()
	go loopClose(e1.ctx, signalC, trackerC)
	return signalC
}

func loopClose(ctx context.Context, signalC chan<- error, trackerC <-chan error) {
	var err error
	doneC := ctx.Done()
	select {
	case <-doneC:
	case err = <-trackerC:
	}
	signalC <- err
}

func (e1 Server) SubmitJob(req *pbj.SubmitRequest, stream pbj.Work_SubmitJobServer) error {
	ctx := stream.Context()
	x, err := auth.GetAuth(ctx)
	if err != nil {
		return e1.em.ErrorWithCode(http.StatusUnauthorized, err)
	}

	su := e1.userTxBuffer.Get(x.User.String())
	if req.Data == nil {
		return e1.em.ErrorWithCode(http.StatusBadRequest, errors.New("no transactions"))
	}

	//errorC := make(chan error, 1)
	jobC := make(chan *pbj.Job, 1)
	closeC := make(chan struct{}, 1)
	errorC := su.Receiver.Send(limiter.JobSubmission{
		Data:    req.Data,
		JobC:    jobC,
		CancelC: closeC,
		User:    x.User,
	})

	defer func() {
		closeC <- struct{}{}
	}()

	var job *pbj.Job
out:
	for {
		select {
		case err = <-errorC:
			break out
		case <-ctx.Done():
			err = errors.New("timed out")
			break out
		case job = <-jobC:
			err = stream.Send(job)
			if err != nil {
				break out
			}
		}
	}
	if err != nil {
		return err
	}

	return nil
}

func (e1 Server) GetStatus(ctx context.Context, req *pbj.StatusRequest) (*pbj.Job, error) {

	x, err := auth.GetAuth(ctx)
	if err != nil {
		return nil, e1.em.ErrorWithCode(http.StatusUnauthorized, err)
	}
	job := e1.db.Get(req.GetId())
	if job == nil {
		return nil, errors.New("job not found")
	}
	if job.StartUser != x.User.String() {
		return nil, errors.New("job not found")
	}

	return job, nil
}
