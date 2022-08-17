package mem

import (
	"context"
	"errors"
	"time"

	"github.com/SolmateDev/go-staker/db"
	pbj "github.com/SolmateDev/go-staker/proto/job"
)

type external struct {
	internalC chan<- func(*internal)
	newC      chan<- workInsert
	updateC   chan<- workUpdate
	reqC      chan<- workReq
}

func CreateInMemoryDb(ctx context.Context) (db.WorkDb, error) {
	internalC := make(chan func(*internal), 10)
	newC := make(chan workInsert, 10)
	updateC := make(chan workUpdate, 10)
	reqC := make(chan workReq, 10)
	e1 := external{
		internalC: internalC, newC: newC, updateC: updateC, reqC: reqC,
	}

	go loopInternal(ctx, internalC, newC, updateC, reqC)

	return e1, nil
}

func (e1 external) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	e1.internalC <- func(in *internal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

func (e1 external) Get(id string) *pbj.Job {
	ansC := make(chan *pbj.Job, 1)
	e1.reqC <- workReq{
		id: id, respC: ansC,
	}
	return <-ansC
}

func (e1 external) Update(id string, status pbj.Status, t time.Time) error {
	errorC := make(chan error, 1)
	e1.updateC <- workUpdate{
		id: id, time: t, status: status,
		errorC: errorC,
	}
	return <-errorC
}

func (e1 external) Put(job *pbj.Job) error {
	if len(job.StartUser) == 0 {
		return errors.New("no user")
	}
	if job.Status == nil {
		return errors.New("no status")
	}
	if job.Status.Time == 0 {
		return errors.New("no status time")
	}
	if len(job.Id) == 0 {
		return errors.New("no job id")
	}
	if len(job.Data) == 0 {
		return errors.New("no job data")
	}
	errorC := make(chan error, 1)
	jobcp := copyJobWork(job)
	e1.newC <- workInsert{
		job: jobcp, errorC: errorC,
	}
	return <-errorC
}
