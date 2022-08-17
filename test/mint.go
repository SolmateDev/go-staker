package test

import (
	"context"

	sgo "github.com/SolmateDev/solana-go"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type Minter struct {
	Id       sgo.PublicKey
	requestC chan<- mintRequest
}

func (m Minter) Send(ctx context.Context, dst sgo.PublicKey, amount uint64) <-chan error {
	errorC := make(chan error, 1)
	m.requestC <- mintRequest{
		destination: dst, amount: amount, ctx: ctx, errorC: errorC,
	}
	return errorC
}

type minterInternal struct {
	ctx       context.Context
	rpc       *sgorpc.Client
	ws        *sgows.Client
	mint      sgo.PublicKey
	authority sgo.PrivateKey
}

type mintRequest struct {
	destination sgo.PublicKey
	amount      uint64
	ctx         context.Context
	errorC      chan<- error
}

func CreateMinter(ctx context.Context, rpcUrl string, wsUrl string, mint sgo.PublicKey, authorityKey sgo.PrivateKey) (Minter, error) {
	requestC := make(chan mintRequest, 10)

	rpcClient := sgorpc.New(rpcUrl)

	_, err := rpcClient.RequestAirdrop(ctx, authorityKey.PublicKey(), 10*sgo.LAMPORTS_PER_SOL, sgorpc.CommitmentConfirmed)
	if err != nil {
		return Minter{}, err
	}

	wsClient, err := sgows.Connect(ctx, wsUrl)
	if err != nil {
		return Minter{}, err
	}

	go loopMinter(ctx, rpcClient, wsClient, mint, authorityKey, requestC)

	return Minter{
		requestC: requestC,
		Id:       mint,
	}, nil

}

func loopMinter(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, mint sgo.PublicKey, authority sgo.PrivateKey, requestC <-chan mintRequest) {

	in := new(minterInternal)
	in.ctx = ctx
	in.rpc = rpcClient
	in.ws = wsClient
	in.mint = mint
	in.authority = authority

	doneC := ctx.Done()
out:
	for {
		select {
		case <-doneC:
			break out
		case req := <-requestC:
			go loopMintTo(in.rpc, in.ws, req, in.mint, in.authority)
		}
	}
}

func loopMintTo(rpcClient *sgorpc.Client, wsClient *sgows.Client, req mintRequest, mint sgo.PublicKey, authority sgo.PrivateKey) {
	instruction := sgotkn.NewMintToInstruction(
		req.amount, mint, req.destination, authority.PublicKey(), []sgo.PublicKey{authority.PublicKey()},
	).Build()
	l, err := rpcClient.GetLatestBlockhash(req.ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		req.errorC <- err
		return
	}
	tx, err := sgo.NewTransactionBuilder().SetFeePayer(authority.PublicKey()).AddInstruction(instruction).SetRecentBlockHash(l.Value.Blockhash).Build()
	if err != nil {
		req.errorC <- err
		return
	}
	_, err = tx.Sign(func(pub sgo.PublicKey) *sgo.PrivateKey {
		if pub.Equals(authority.PublicKey()) {
			return &authority
		}
		return nil
	})
	if err != nil {
		req.errorC <- err
		return
	}
	sig, err := rpcClient.SendTransaction(req.ctx, tx)
	if err != nil {
		req.errorC <- err
		return
	}
	err = WaitSig(req.ctx, sig, wsClient)
	if err != nil {
		req.errorC <- err
		return
	}
	req.errorC <- nil
	return
}
