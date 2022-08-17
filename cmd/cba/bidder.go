package main

import (
	"errors"

	bdr "github.com/SolmateDev/go-staker/bidder"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	bin "github.com/gagliardetto/binary"

	sgotkn2 "github.com/SolmateDev/solana-go/programs/token"
)

type Bidder struct {
	Key           string `arg name:"key" help:"the private key of the wallet that owns tokens used to bid on bandwidth and also authenticates over grpc with the staked validator"`
	Configuration string `arg name:"config" help:"the file path to the configuration file"`
}

func (r *Bidder) Run(kongCtx *CLIContext) error {

	ctx := kongCtx.Ctx
	rpcClient := kongCtx.Clients.Rpc
	wsClient := kongCtx.Clients.Ws

	userKey, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Key)
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

	pcMint := controllerData.PcMint
	x, err := rpcClient.GetTokenAccountsByOwner(ctx, userKey.PublicKey(), &sgorpc.GetTokenAccountsConfig{Mint: &pcMint}, &sgorpc.GetTokenAccountsOpts{Commitment: sgorpc.CommitmentFinalized})
	if err != nil {
		return err
	}
	var userVault *sgotkn2.Account

backout:
	for i := 0; i < len(x.Value); i++ {
		userVault = new(sgotkn2.Account)
		err = bin.UnmarshalBorsh(userVault, x.Value[i].Account.Data.GetBinary())
		if err != nil {
			return err
		}
		if userVault.Mint.Equals(controllerData.PcMint) {
			break backout
		}
	}
	if userVault == nil {
		return errors.New("failed to find user vault")
	}

	configChannelGroup := bdr.ConfigByHup(ctx, r.Configuration)
	bidder, err := bdr.CreateBidder(ctx, rpcClient, wsClient, configChannelGroup, userKey, userVault, controller)
	if err != nil {
		return err
	}
	err = <-bidder.CloseSignal()
	if err != nil {
		return err
	}
	return nil
}
