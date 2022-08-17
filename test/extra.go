package test

import (
	"context"
	"errors"
	"fmt"

	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

func WaitSig(ctx context.Context, sig sgo.Signature, wsClient *sgows.Client) error {

	sub, err := wsClient.SignatureSubscribe(sig, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	doneC := ctx.Done()
	errorC := sub.CloseSignal()
	streamC := sub.RecvStream()
	select {
	case <-doneC:
	case err = <-errorC:
	case d := <-streamC:
		result, ok := d.(*sgows.SignatureResult)
		if !ok {
			err = errors.New("bad result")
		} else {
			if result.Value.Err != nil {
				err = fmt.Errorf("%S", result.Value.Err)
			}
		}
	}
	if err != nil {
		return err
	}
	return nil
}
