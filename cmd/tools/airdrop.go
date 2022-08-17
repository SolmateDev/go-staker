package main

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/SolmateDev/go-staker/script"
	sgo "github.com/SolmateDev/solana-go"
	log "github.com/sirupsen/logrus"
)

type Airdrop struct {
	Destination string  `name:"destination" short:"d"`
	Amount      float64 `name:"amount" short:"a" help:"the amount to airdrop in SOL, not LAMPORTS"`
}

func (r *Airdrop) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx
	rpcClient := kongCtx.Clients.Rpc
	if rpcClient == nil {
		return errors.New("no rpc client")
	}
	wsClient := kongCtx.Clients.Ws
	if wsClient == nil {
		return errors.New("no ws client")
	}

	dst, err := sgo.PublicKeyFromBase58(r.Destination)
	if err != nil {
		return err
	}
	if r.Amount <= 0 {
		return errors.New("amount must be positive float")
	}

	amt := uint64(math.Round(r.Amount * float64(sgo.LAMPORTS_PER_SOL)))
	result, err := rpcClient.GetAccountInfo(ctx, dst)
	if err != nil {
		log.Debug(err)
	} else {
		os.Stderr.WriteString(fmt.Sprintf("old balance: %d lamports\n", result.Value.Lamports))
	}

	err = script.Airdrop(ctx, rpcClient, wsClient, dst, amt)
	if err != nil {
		return err
	}
	result, err = rpcClient.GetAccountInfo(ctx, dst)
	if err != nil {
		return err
	}
	os.Stderr.WriteString(fmt.Sprintf("new balance: %d lamports\n", result.Value.Lamports))

	return nil
}
