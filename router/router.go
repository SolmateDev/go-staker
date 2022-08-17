package router

import (
	"context"

	cba "github.com/SolmateDev/go-solmate-cba"

	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type Router struct {
	internalC chan<- func(*internal)
}

type Configuration struct {
	Rpc *sgorpc.Client
	Ws  *sgows.Client
}

func Create(ctx context.Context, config *Configuration) (Router, error) {
	internalC := make(chan func(*internal), 10)

	go loopInternal(ctx, internalC, config)

	e1 := Router{
		internalC: internalC,
	}

	return e1, nil
}

type Allocation struct {
	Allocation uint64
}

type BidListUpdate struct {
	Period     cba.Period
	Allocation map[string]Allocation
}

func (e1 Router) Update(update BidListUpdate) {
	e1.internalC <- func(in *internal) {
		in.update_bid(update)
	}
}

func (in *internal) update_bid(update BidListUpdate) {

}

func (e1 Router) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	e1.internalC <- func(in *internal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}
