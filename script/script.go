package script

import (
	"context"

	"github.com/SolmateDev/go-staker/state"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type Configuration struct {
	Version state.CbaVersion `json:"version"`
}

type Script struct {
	ctx        context.Context
	rpc        *sgorpc.Client
	ws         *sgows.Client
	controller state.Controller
}

func Create(ctx context.Context, config *Configuration, rpcClient *sgorpc.Client, wsClient *sgows.Client) (*Script, error) {

	controller, err := state.CreateController(ctx, rpcClient, wsClient, state.VERSION_1)
	if err != nil {
		return nil, err
	}
	e1 := &Script{
		ctx: ctx, rpc: rpcClient, ws: wsClient, controller: controller,
	}

	return e1, nil
}
