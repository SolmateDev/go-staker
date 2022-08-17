package cranker

import (
	"context"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type CrankInfo struct {
	ControllerId     sgo.PublicKey
	Controller       cba.Controller
	CrankerKey       sgo.PrivateKey
	CrankerPcFund    sgo.PublicKey
	LatestBlockHash  sgo.Hash
	BalanceThreshold uint64
}

type Cranker struct {
	ctx       context.Context
	internalC chan<- func(*internal)
	ci        CrankInfo
}

func CreateCranker(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, ci CrankInfo, controller state.Controller) (Cranker, error) {

	startErrorC := make(chan error, 1)
	internalC := make(chan func(*internal), 10)
	go loopInternal(ctx, internalC, startErrorC, rpcClient, wsClient, ci, controller)
	err := <-startErrorC
	if err != nil {
		return Cranker{}, err
	}

	return Cranker{ctx: ctx, internalC: internalC, ci: ci}, nil
}

func (e1 Cranker) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	e1.internalC <- func(in *internal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}
