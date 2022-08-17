package router

import (
	"context"

	log "github.com/sirupsen/logrus"
)

type internal struct {
	ctx              context.Context
	errorC           chan<- error
	config           *Configuration
	closeSignalCList []chan<- error
	users            map[string]User
}

func loopInternal(ctxOutside context.Context, internalC <-chan func(*internal), config *Configuration) {

	var err error
	ctx, cancel := context.WithCancel(ctxOutside)
	doneC := ctx.Done()
	defer cancel()

	errorC := make(chan error, 1)

	in := new(internal)
	in.config = config
	in.errorC = errorC
	in.ctx = ctx
	in.closeSignalCList = make([]chan<- error, 0)

out:
	for {
		select {
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

func (in *internal) finish(err error) {
	log.Debug(err)

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}
