package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"io/ioutil"

	"github.com/SolmateDev/go-staker/script"
	sgo "github.com/SolmateDev/solana-go"
	log "github.com/sirupsen/logrus"
)

type Balance struct {
	Mint  string `name:"mint" help:"the account ID of the mint"`
	Owner string `name:"owner"   help:"the owner of the account that will be receiving tokens. aka the destination"`
}

func (r *Balance) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx
	rpcClient := kongCtx.Clients.Rpc
	if rpcClient == nil {
		return errors.New("no rpc client")
	}
	owner, err := sgo.PublicKeyFromBase58(r.Owner)
	if err != nil {
		return err
	}
	mint, err := sgo.PublicKeyFromBase58(r.Mint)
	accountId, _, err := sgo.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return err
	}
	a, err := script.GetTokenAccount(ctx, rpcClient, owner, mint)
	if err != nil {
		return err
	}

	os.Stdout.WriteString(fmt.Sprintf("%s\n%d\n", accountId, a.Amount))
	return nil
}

type Issue struct {
	Payer  string `name:"payer"  help:"the account paying SOL fees"`
	Mint   string `name:"mint" help:"the file containing the output from the mint command that contains the metadata and authority key"`
	Owner  string `name:"owner"   help:"the owner of the account that will be receiving tokens. aka the destination"`
	Amount uint64 `name:"amount" short:"a" help:"the amount to issue"`
}

func (r *Issue) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx
	rpcClient := kongCtx.Clients.Rpc
	if rpcClient == nil {
		return errors.New("no rpc client")
	}
	wsClient := kongCtx.Clients.Ws
	if wsClient == nil {
		return errors.New("no ws client")
	}

	mr, err := script.MintFromFile(r.Mint)
	if err != nil {
		return err
	}
	payer, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Payer)
	if err != nil {
		return err
	}
	owner, err := sgo.PublicKeyFromBase58(r.Owner)
	if err != nil {
		return err
	}

	a, err := script.GetTokenAccount(ctx, rpcClient, owner, *mr.Id)
	if err != nil {
		log.Debug(err)
		os.Stdout.WriteString("no token account exists, creating one to receive tokens\n")
		_, err = script.CreateTokenAccount(ctx, rpcClient, wsClient, payer, owner, *mr.Id)
		if err != nil {
			return err
		}
	} else {
		os.Stdout.WriteString(fmt.Sprintf("Old balance: %d\n", a.Amount))
	}

	err = mr.MintTo(ctx, rpcClient, wsClient, payer, owner, r.Amount)
	if err != nil {
		return err
	}
	a, err = script.GetTokenAccount(ctx, rpcClient, owner, *mr.Id)
	if err != nil {
		return err
	} else {
		os.Stdout.WriteString(fmt.Sprintf("New balance: %d\n", a.Amount))
	}
	return nil
}

type Mint struct {
	Payer     string `name:"payer"  help:"the account paying SOL fees"`
	Authority string `name:"authority" help:"the account with administrative privileges"`
	Decimal   uint8  `name:"decimal" short:"d" help:"the decimal count" default:"0"`
	Output    string `option name:"out" short:"o" default:""`
}

func (r *Mint) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx
	rpcClient := kongCtx.Clients.Rpc
	if rpcClient == nil {
		return errors.New("no rpc client")
	}
	wsClient := kongCtx.Clients.Ws
	if wsClient == nil {
		return errors.New("no ws client")
	}

	payer, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Payer)
	if err != nil {
		return err
	}

	authority, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Authority)
	if err != nil {
		return err
	}

	mintResult, err := script.CreateMint(ctx, rpcClient, wsClient, payer, authority, r.Decimal)
	if err != nil {
		return err
	}

	d, err := json.Marshal(mintResult)
	if err != nil {
		return err
	}

	if len(r.Output) == 0 {
		os.Stdout.WriteString(string(d) + "\n")
	} else {
		err = ioutil.WriteFile(r.Output, d, 0644)
		if err != nil {
			return err
		}
	}

	return nil
}
