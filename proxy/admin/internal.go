package admin

import (
	"context"
	"fmt"

	cba "github.com/SolmateDev/go-solmate-cba"
	ll "github.com/SolmateDev/go-staker/ds/list"
	pba "github.com/SolmateDev/go-staker/proto/admin"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type internal struct {
	ctx              context.Context
	errorC           chan<- error
	closeSignalCList []chan<- error
	rpc              *sgorpc.Client
	ws               *sgows.Client
	settings         *pba.Settings
	controller       state.Controller
	pipeline         state.Pipeline
	lastAddPeriod    uint64
	list             *ll.Generic[cba.Period]
	slot             uint64
	admin            sgo.PrivateKey
	homeLog          *util.SubHome[*pba.LogLine]
}

func DefaultPeriodSettings() *pba.Settings {
	return &pba.Settings{
		Withhold:  0,
		Lookahead: 3 * 16000,
		Length:    16000,
		Rate: &pba.Rate{
			Numerator:   5,
			Denominator: 100,
		},
	}
}

func loopInternal(ctx context.Context, internalC <-chan func(*internal), rpcClient *sgorpc.Client, wsClient *sgows.Client, admin sgo.PrivateKey, controller state.Controller, pipeline state.Pipeline, subSlot state.SlotHome, homeLog *util.SubHome[*pba.LogLine]) {
	var err error
	doneC := ctx.Done()
	errorC := make(chan error, 1)

	in := new(internal)
	in.errorC = errorC
	in.closeSignalCList = make([]chan<- error, 0)
	in.settings = DefaultPeriodSettings()
	in.lastAddPeriod = 0
	in.list = ll.CreateGeneric[cba.Period]()
	in.slot = 0
	in.admin = admin
	in.controller = controller
	in.pipeline = pipeline
	in.homeLog = homeLog

	slotSubChannelGroup := subSlot.OnSlot()
	defer slotSubChannelGroup.Unsubscribe()

out:
	for {
		select {
		case id := <-in.homeLog.DeleteC:
			in.homeLog.Delete(id)
		case x := <-in.homeLog.ReqC:
			in.homeLog.Receive(x)
		case err = <-slotSubChannelGroup.ErrorC:
			break out
		case slot := <-slotSubChannelGroup.StreamC:
			in.log(pba.Severity_DEBUG, fmt.Sprintf("slot=%d", slot))
			in.slot = slot
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)
		}
	}

	in.finish(err)
}

func (in *internal) log(level pba.Severity, message string) {
	in.homeLog.Broadcast(&pba.LogLine{Level: level, Message: message})
}

func (in *internal) finish(err error) {
	log.Debug(err)
	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}
