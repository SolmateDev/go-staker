package limiter

import (
	"context"

	log "github.com/sirupsen/logrus"
)

type Receiver[T any] struct {
	ctx       context.Context
	internalC chan<- func(*internal[T])
	trafficC  chan<- trafficRequest[T]
}

func CreateReceiver[T any](ctx context.Context) Receiver[T] {
	internalC := make(chan func(*internal[T]), 10)
	trafficC := make(chan trafficRequest[T], 10)
	go loopInternal(ctx, internalC, trafficC)
	return Receiver[T]{
		ctx: ctx, internalC: internalC, trafficC: trafficC,
	}
}

func (e1 Receiver[T]) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	e1.internalC <- func(in *internal[T]) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

func (e1 Receiver[T]) Pop(count uint64) []T {
	ansC := make(chan []T, 1)
	e1.internalC <- func(in *internal[T]) {
		ansC <- in.list.PopArray(count)
	}
	return <-ansC
}

func (e1 Receiver[T]) UpdateCapacity(rate uint64) {
	e1.internalC <- func(in *internal[T]) {
		in.list.UpdateSize(rate)
	}
}

// return the capacity and current size of the queue
func (e1 Receiver[T]) QueueStatus() (uint64, uint64) {
	ansC := make(chan [2]uint64, 1)
	e1.internalC <- func(in *internal[T]) {
		ansC <- [2]uint64{in.list.max_size, in.list.size}
	}
	x := <-ansC
	return x[0], x[1]
}

func (e1 Receiver[T]) Send(value T) <-chan error {
	replyC := make(chan trafficReply, 1)
	e1.trafficC <- trafficRequest[T]{
		value: value, replyC: replyC,
	}
	reply := <-replyC
	return reply.errorC
}

type trafficRequest[T any] struct {
	value  T
	replyC chan<- trafficReply // this channel tracks if this item is kicked out of the linked list upon the list size shrinking
}

type trafficReply struct {
	errorC <-chan error
}

type internal[T any] struct {
	ctx              context.Context
	closeSignalCList []chan<- error
	errorC           chan<- error
	list             *LinkedList[T]
}

func loopInternal[T any](ctx context.Context, internalC <-chan func(*internal[T]), trafficC <-chan trafficRequest[T]) {

	var err error

	errorC := make(chan error, 1)
	doneC := ctx.Done()

	in := new(internal[T])
	in.ctx = ctx
	in.errorC = errorC
	in.list = CreateLinkedList[T](0)

out:
	for {
		select {
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case t := <-trafficC:
			t.replyC <- trafficReply{
				errorC: in.list.Append(t.value),
			}
		case req := <-internalC:
			req(in)
		}
	}

	in.finish(err)
}

func (in *internal[T]) finish(err error) {

	log.Debug(err)

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}

}
