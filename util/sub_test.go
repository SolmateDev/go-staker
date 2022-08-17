package util_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SolmateDev/go-staker/util"
)

func TestSubscription(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
	})
	home := util.CreateSubHome[int]()
	reqC := home.ReqC

	TEST_VALUE := 2

	go loopSubHome(ctx, home, TEST_VALUE)

	for i := 0; i < 2; i++ {
		sub := util.SubscriptionRequest(reqC, func(v int) bool { return true })
		var err error
		select {
		case <-time.After(30 * time.Second):
			err = errors.New("time out")
		case err = <-sub.ErrorC:
		case d := <-sub.StreamC:
			if d != TEST_VALUE+i {
				t.Fatal("value does not match")
			}
			sub.Unsubscribe()
			select {
			case err = <-sub.ErrorC:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("time out")
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}

}

func loopSubHome(ctx context.Context, home *util.SubHome[int], value int) {

	doneC := ctx.Done()

	for {
		select {
		case <-doneC:
			break
		case r := <-home.ReqC:
			home.Receive(r)
		case id := <-home.DeleteC:
			home.Delete(id)
		case <-time.After(2 * time.Second):
			home.Broadcast(value)
			value++
		}

	}
}
