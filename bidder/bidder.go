package bidder

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"

	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type Bidder struct {
	ctx        context.Context
	controller state.Controller
	bidder     sgo.PublicKey
	pcVault    sgo.PublicKey
	internalC  chan<- func(*internal)
}

type Configuration struct {
	// what is the maximum that can be bid
	Budget uint64 `json:"max_bid"`
	// what is the target ration between our deposit and the total deposit
	TargetTPS float64 `json:"target_tps"`
	// what is the maximum amount the bidder can change the bid to the positive or negative side (range is 1-255 out of 256)
	MaxDelta float64 `json:"max_delta"`
}

func ConfigFromFile(filePath string) (*Configuration, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	c := new(Configuration)
	err = json.Unmarshal(data, c)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func CreateBidder(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, configChannelGroup ConfigByHubChannelGroup, bidder sgo.PrivateKey, pcVault *sgotkn.Account, controller state.Controller) (Bidder, error) {

	internalC := make(chan func(*internal), 10)
	startErrorC := make(chan error, 1)

	go loopInternal(ctx, internalC, startErrorC, configChannelGroup.ErrorC, configChannelGroup.ConfigC, rpcClient, wsClient, bidder, pcVault, controller)

	return Bidder{
		ctx:        ctx,
		internalC:  internalC,
		controller: controller,
	}, nil
}

func (e1 Bidder) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	e1.internalC <- func(in *internal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

type ConfigByHubChannelGroup struct {
	ConfigC <-chan Configuration
	ErrorC  <-chan error
}

func CreateConfigPair() (ConfigByHubChannelGroup, chan<- Configuration) {
	configC := make(chan Configuration, 1)
	errorC := make(chan error, 1)
	return ConfigByHubChannelGroup{ConfigC: configC, ErrorC: errorC}, configC
}

func ConfigByHup(ctx context.Context, configFilePath string) ConfigByHubChannelGroup {
	configC := make(chan Configuration, 1)
	errorC := make(chan error, 1)
	go loopModifyConfig(ctx, configC, errorC, configFilePath)
	return ConfigByHubChannelGroup{
		ConfigC: configC,
		ErrorC:  errorC,
	}
}

// if someone hups, then read the updated config from the command line
func loopModifyConfig(ctx context.Context, configC chan<- Configuration, configReadErrorC chan<- error, configFilePath string) {
	doneC := ctx.Done()
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGHUP)
	var err error

	config, err := ConfigFromFile(configFilePath)
	if err != nil {
		configC <- *config
		return
	}

out:
	for {
		select {
		case <-doneC:
			break out
		case <-sigC:
			c, err := ConfigFromFile(configFilePath)
			if err != nil {
				break out
			}
			configC <- *c
		}
	}
	if err != nil {
		configReadErrorC <- err
	}
}
