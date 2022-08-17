package main

import (
	ckr "github.com/SolmateDev/go-staker/cranker"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
)

type Cranker struct {
	BalanceThreshold uint64 `arg name:"minbal" help:"what is the balance threshold at which the program needs to exit with an error code"`
	Key              string `arg name:"key" help:"the file path of the private key"`
}

func (r *Cranker) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx

	rpcClient := kongCtx.Clients.Rpc
	wsClient := kongCtx.Clients.Ws

	crankerKey, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Key)
	if err != nil {
		return err
	}

	controller, err := state.CreateController(ctx, rpcClient, wsClient, state.VERSION_1)
	if err != nil {
		return err
	}
	controllerData, err := controller.Data()
	if err != nil {
		return err
	}
	crankerVault, _, err := sgo.FindAssociatedTokenAddress(crankerKey.PublicKey(), controllerData.PcMint)
	if err != nil {
		return err
	}
	bh, err := rpcClient.GetLatestBlockhash(ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	_, err = ckr.CreateCranker(ctx, rpcClient, wsClient, ckr.CrankInfo{
		ControllerId:     controller.Id(),
		Controller:       controllerData,
		CrankerKey:       crankerKey,
		CrankerPcFund:    crankerVault,
		LatestBlockHash:  bh.Value.Blockhash,
		BalanceThreshold: r.BalanceThreshold,
	}, controller)
	if err != nil {
		return err
	}
	return nil
}
