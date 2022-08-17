package state

import (
	"context"

	"github.com/SolmateDev/go-staker/util"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type SlotHome struct {
	reqC chan<- util.ResponseChannel[uint64]
	ctx  context.Context
}

func SubscribeSlot(ctxOutside context.Context, wsClient *sgows.Client) (SlotHome, error) {
	ctx, cancel := context.WithCancel(ctxOutside)
	sub, err := wsClient.SlotSubscribe()
	if err != nil {
		cancel()
		return SlotHome{}, err
	}
	home := util.CreateSubHome[uint64]()
	reqC := home.ReqC
	go loopSlot(ctx, home, sub, cancel)
	return SlotHome{
		reqC: reqC, ctx: ctx,
	}, nil
}

func (sh SlotHome) filter_OnSlot(s uint64) bool {
	return true
}

func (sh SlotHome) OnSlot() util.Subscription[uint64] {
	return util.SubscriptionRequest(sh.reqC, sh.filter_OnSlot)
}

func (sh SlotHome) CloseSignal() <-chan struct{} {
	return sh.ctx.Done()
}

func loopSlot(ctx context.Context, home *util.SubHome[uint64], sub *sgows.SlotSubscription, cancel context.CancelFunc) {
	defer cancel()
	doneC := ctx.Done()
	reqC := home.ReqC
	deleteC := home.DeleteC

	streamC := sub.RecvStream()
	closeC := sub.CloseSignal()

	var err error

out:
	for {
		select {
		case d := <-streamC:
			x, ok := d.(*sgows.SlotResult)
			if !ok {
				break out
			}
			home.Broadcast(x.Slot)
		case <-closeC:
			break out
		case <-doneC:
			break out
		case rC := <-reqC:
			home.Receive(rC)
		case id := <-deleteC:
			home.Delete(id)
		}
	}

	log.Debug(err)
}
