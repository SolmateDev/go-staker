package main

import (
	"os"

	st "github.com/SolmateDev/go-staker/script"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
)

type Controller struct {
	Create ControllerCreate `cmd name:"create" help:"Create a controller and change the controller settings"`
	Status ControllerStatus `cmd name:"status" help:"Print the admin, token balance of the controller"`
}

type ControllerStatus struct {
}

func (r *ControllerStatus) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx
	rpcClient := kongCtx.Clients.Rpc
	wsClient := kongCtx.Clients.Ws
	controller, err := state.CreateController(ctx, rpcClient, wsClient, state.VERSION_1)
	if err != nil {
		return err
	}

	ans, err := controller.Print()
	if err != nil {
		return err
	}

	os.Stdout.WriteString(ans)

	return nil
}

type ControllerCreate struct {
	Payer   string `name:"payer" short:"p" help:"the account paying SOL fees"`
	Admin   string `name:"admin" short:"a" help:"the account with administrative privileges"`
	Cranker string `option name:"cranker" help:"who is allowed to crank"`
	Mint    string `name:"mint" short:"m" help:"the mint of the token account to which fees are paid to and by validators"`
}

func (r *ControllerCreate) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx

	payer, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Payer)
	if err != nil {
		return err
	}
	admin, err := sgo.PrivateKeyFromSolanaKeygenFile(r.Admin)
	if err != nil {
		return err
	}
	mint, err := sgo.PublicKeyFromBase58(r.Mint)
	if err != nil {
		return err
	}

	var cranker sgo.PrivateKey
	if len(r.Cranker) == 0 {
		cranker = admin
	} else {
		cranker, err = sgo.PrivateKeyFromSolanaKeygenFile(r.Cranker)
		if err != nil {
			return err
		}
	}

	rpcClient := kongCtx.Clients.Rpc
	wsClient := kongCtx.Clients.Ws

	err = st.CreateController(ctx, rpcClient, wsClient, state.VERSION_1, payer, admin, cranker, mint)
	if err != nil {
		return err
	}

	return nil
}
