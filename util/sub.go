package util

import (
	"context"
	"errors"
	"fmt"

	cba "github.com/SolmateDev/go-solmate-cba"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

func Compare(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type Subscription[T any] struct {
	id      int
	deleteC chan<- int
	StreamC <-chan T
	ErrorC  <-chan error
}

func (s Subscription[T]) Unsubscribe() {
	s.deleteC <- s.id
}

type innerSubscription[T any] struct {
	id      int
	streamC chan<- T
	errorC  chan<- error
	filter  func(T) bool
}
type ResponseChannel[T any] struct {
	RespC  chan<- Subscription[T]
	filter func(T) bool
}

func SubscriptionRequest[T any](reqC chan<- ResponseChannel[T], filterCallback func(T) bool) Subscription[T] {
	respC := make(chan Subscription[T], 10)
	reqC <- ResponseChannel[T]{
		RespC:  respC,
		filter: filterCallback,
	}
	return <-respC
}

type SubHome[T any] struct {
	id      int
	subs    map[int]*innerSubscription[T]
	DeleteC chan int
	ReqC    chan ResponseChannel[T]
}

func CreateSubHome[T any]() *SubHome[T] {
	reqC := make(chan ResponseChannel[T], 10)
	return &SubHome[T]{
		id: 0, subs: make(map[int]*innerSubscription[T]), DeleteC: make(chan int, 10), ReqC: reqC,
	}
}

func (sh *SubHome[T]) Broadcast(value T) {
	for _, v := range sh.subs {
		if v.filter(value) {
			v.streamC <- value
		}
	}
}

func (sh *SubHome[T]) Delete(id int) {
	p, present := sh.subs[id]
	if present {
		p.errorC <- nil
		delete(sh.subs, id)
	}
}

func (sh *SubHome[T]) Receive(resp ResponseChannel[T]) {
	id := sh.id
	sh.id++
	streamC := make(chan T, 10)
	errorC := make(chan error, 1)
	sh.subs[id] = &innerSubscription[T]{
		id: id, streamC: streamC, errorC: errorC, filter: resp.filter,
	}
	resp.RespC <- Subscription[T]{id: id, StreamC: streamC, ErrorC: errorC, deleteC: sh.DeleteC}
}

// add the Id
type PipelineGroup struct {
	Id   sgo.PublicKey
	Data cba.Pipeline
}

type BidForBidder struct {
	// period
	LastPeriodStart uint64
	// the validator to which this bid belongs to
	Pipeline sgo.PublicKey
	// actual bid
	Bid cba.Bid
}

type BidSummary struct {
	// period
	LastPeriodStart uint64
	// the validator to which this bid belongs to
	Pipeline sgo.PublicKey
	// total deposits in the bid bucket
	TotalDeposits uint64
}

func WaitSig(ctx context.Context, wsClient *sgows.Client, sig sgo.Signature) error {

	sub, err := wsClient.SignatureSubscribe(sig, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case err = <-sub.CloseSignal():
	case d := <-sub.RecvStream():
		x, ok := d.(*sgows.SignatureResult)
		if !ok {
			err = errors.New("bad signature result")
		} else if x.Value.Err != nil {
			err = fmt.Errorf("%s", x.Value.Err)
		}
	}
	return err
}
