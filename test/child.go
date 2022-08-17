package test

import (
	"context"
	"os"
	"os/exec"
)

type Child struct {
	cancel    context.CancelFunc
	internalC chan<- func(*internal)
}

// create a child command
func CreateCmd(ctxOutside context.Context, cmd *exec.Cmd) Child {
	ctx, cancel := context.WithCancel(ctxOutside)
	errorC := make(chan error, 1)
	go loopRun(cmd, errorC)
	internalC := make(chan func(*internal), 10)
	go loopInternal(ctx, internalC, errorC, cancel)

	return Child{
		cancel: cancel,
	}
}

func (c Child) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	c.internalC <- func(in *internal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

func (c Child) Close() {
	doneC := c.CloseSignal()
	c.cancel()
	<-doneC
}

func loopCancel(ctx context.Context, cmd *exec.Cmd) {
	<-ctx.Done()
	cmd.Process.Kill()
}

func loopRun(cmd *exec.Cmd, errorC chan<- error) {
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	errorC <- cmd.Run()
}

type internal struct {
	closeSignalCList []chan<- error
}

func loopInternal(ctx context.Context, internalC <-chan func(*internal), errorC <-chan error, cancel context.CancelFunc) {
	defer cancel()
	doneC := ctx.Done()
	in := new(internal)
	in.closeSignalCList = make([]chan<- error, 0)

	var err error

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

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}
