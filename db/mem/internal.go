package mem

import (
	"context"
	"errors"
	"time"

	pbj "github.com/SolmateDev/go-staker/proto/job"
	log "github.com/sirupsen/logrus"
)

type internal struct {
	ctx              context.Context
	errorC           chan<- error
	closeSignalCList []chan<- error
	work             map[string]*pbj.Job
}

type workInsert struct {
	job    *pbj.Job
	errorC chan<- error
}

type workReq struct {
	id    string
	respC chan<- *pbj.Job
}

type workUpdate struct {
	id     string
	time   time.Time
	status pbj.Status
	errorC chan<- error
}

func loopInternal(ctx context.Context, internalC <-chan func(*internal), newC <-chan workInsert, updateC <-chan workUpdate, reqC <-chan workReq) {
	var err error
	errorC := make(chan error, 1)
	doneC := ctx.Done()

	in := new(internal)
	in.work = make(map[string]*pbj.Job)

out:
	for {
		select {
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)

		case w := <-newC:
			if w.job == nil {
				w.errorC <- errors.New("blank job")
			} else {
				_, present := in.work[w.job.Id]
				if present {
					w.errorC <- errors.New("job already exists")
				} else {
					in.work[w.job.Id] = w.job
					w.errorC <- nil
				}
			}
		case w := <-updateC:
			x, present := in.work[w.id]
			if present {
				x.Status = &pbj.StatusWithTime{
					Status: w.status,
					Time:   w.time.Unix(),
				}
				w.errorC <- nil
			} else {
				w.errorC <- errors.New("job does not exist")
			}
		case w := <-reqC:
			x, present := in.work[w.id]
			if present {
				w.respC <- copyJobWork(x)
			} else {
				w.respC <- nil
			}
		}
	}
	in.finish(err)
}

func copyJobWork(job *pbj.Job) *pbj.Job {
	status := &pbj.StatusWithTime{}
	if job.Status != nil {
		status.Status = job.Status.Status
		status.Time = job.Status.Time
	}
	var data []byte
	if job.Data != nil {
		data = make([]byte, len(job.Data))
		copy(data, job.Data)
	} else {
		data = []byte{}
	}
	return &pbj.Job{
		Id:        job.Id,
		StartUser: job.StartUser,
		Status:    status,
		Data:      data,
	}
}

func (in *internal) finish(err error) {
	log.Debug(err)
	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}
