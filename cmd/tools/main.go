package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	"github.com/alecthomas/kong"
	log "github.com/sirupsen/logrus"
)

type CLIContext struct {
	Ctx     context.Context
	Clients *Clients
}

type debugFlag bool

type RpcUrl string
type WsUrl string

var cli struct {
	Verbose debugFlag `help:"Set logging to verbose." default:"false" short:"v"`
	RpcUrl  RpcUrl    `option name:"rpc" help:"Connection information to a Solana validator Rpc endpoint with format protocol://host:port (ie http://localhost:8899)"`
	WsUrl   WsUrl     `name:"ws" help:"Connection information to a Solana validator Websocket endpoint with format protocol://host:port (ie ws://localhost:8900)" type:"string"`
	Airdrop Airdrop   `cmd name:"airdrop" help:"Receive SOL for free. (does not work on mainnet)."`
	Mint    Mint      `cmd name:"mint" help:"Create a mint"`
	Issue   Issue     `cmd name:"issue" help:"Issue tokens"`
	Balance Balance   `cmd name:"balance" help:"Get token balance"`
}

type Clients struct {
	ctx context.Context
	Rpc *sgorpc.Client
	Ws  *sgows.Client
}

func (d debugFlag) AfterApply(clients *Clients) error {
	if d {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	return nil
}

func (url RpcUrl) AfterApply(c *Clients) error {
	log.Infof("rpc url=%s", string(url))
	c.Rpc = sgorpc.New(string(url))
	return nil
}

func (url WsUrl) AfterApply(c *Clients) error {
	var err error
	log.Infof("ws url=%s", string(url))
	c.Ws, err = sgows.Connect(c.ctx, string(url))
	if err != nil {
		return err
	}
	return nil
}

func main() {

	signalC := make(chan os.Signal, 1)
	signal.Notify(signalC, syscall.SIGTERM, syscall.SIGINT)
	ctx, cancel := context.WithCancel(context.Background())
	go loopSignal(ctx, cancel, signalC)
	clients := &Clients{ctx: ctx}
	kongCtx := kong.Parse(&cli, kong.Bind(clients))

	err := kongCtx.Run(&CLIContext{Ctx: ctx, Clients: clients})

	kongCtx.FatalIfErrorf(err)
}

func loopSignal(ctx context.Context, cancel context.CancelFunc, signalC <-chan os.Signal) {
	defer cancel()
	doneC := ctx.Done()
	select {
	case <-doneC:
	case s := <-signalC:
		os.Stderr.WriteString(fmt.Sprintf("%s\n", s.String()))
	}
}
