package test

import (
	"context"
	"errors"
	"os"

	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

type BiddingGroup struct {
	Bidder *User
	Group  *Group
}

type Group struct {
	PcMint sgo.PublicKey
	minter Minter
	Users  map[uint64]*User
}

type resultUserGeneration struct {
	e error
	u *User
}

func GetMintAuthorityKey() (sgo.PrivateKey, error) {
	x, present := os.LookupEnv("MINT_FP")
	if !present {
		return sgo.PrivateKey{}, errors.New("no mint")
	}

	mint, err := sgo.PrivateKeyFromSolanaKeygenFile(x)
	if err != nil {
		return sgo.PrivateKey{}, err
	}
	return mint, nil
}

// generate a group of users with private keys and PcMint Token accounts
func GenerateGroup(ctx context.Context, count uint64, rpcUrl string, wsUrl string, controller state.Controller, mintAuthority sgo.PrivateKey) (*Group, error) {
	var err error

	controllerData, err := controller.Data()
	if err != nil {
		return nil, err
	}

	minter, err := CreateMinter(ctx, rpcUrl, wsUrl, controllerData.PcMint, mintAuthority)
	if err != nil {
		return nil, err
	}

	g := new(Group)
	g.PcMint = controllerData.PcMint
	g.Users = make(map[uint64]*User)

	ctxLoop, cancelLoop := context.WithCancel(ctx)
	reqC := make(chan uint64, 50)
	ansC := make(chan resultUserGeneration, 50)
	defer cancelLoop()
	kmax := uint64(50)
	if kmax < count {
		kmax = count
	}

	for k := uint64(0); k < kmax; k++ {
		wsClient, err := sgows.Connect(ctx, wsUrl)
		if err != nil {
			return nil, err
		}
		go loopGenerateUser(ctxLoop, ctx, rpcUrl, wsClient, minter, reqC, ansC)
	}

	doneC := ctx.Done()

	i := uint64(0)
	j := uint64(0)
out:
	for j < count {
		if i < count {
			select {
			case <-doneC:
				err = errors.New("prematurely done")
				break out
			case a := <-ansC:
				j++
				err = a.e
				if err != nil {
					break out
				}
				log.Debugf("user id=%d", a.u.Id)
				g.Users[a.u.Id] = a.u
			case reqC <- i:
				i++
			}
		} else {
			select {
			case <-doneC:
				err = errors.New("prematurely done")
				break out
			case a := <-ansC:
				j++
				err = a.e
				if err != nil {
					break out
				}
				log.Debugf("(i=%d;j=%d) user id=%d", i, j, a.u.Id)
				g.Users[a.u.Id] = a.u
			}
		}
	}

	if err != nil {
		return nil, err
	}

	return g, nil
}

func loopGenerateUser(ctxLoop context.Context, ctx context.Context, rpcUrl string, wsClient *sgows.Client, minter Minter, reqC <-chan uint64, ansC chan<- resultUserGeneration) {
	doneC := ctxLoop.Done()
out:
	for {
		select {
		case <-doneC:
			break out
		case id := <-reqC:
			u, err := GenerateRandomUser(ctx, id, rpcUrl, wsClient, minter)
			if err == nil {
				err = u.Mint(100 * 100)
			}
			ansC <- resultUserGeneration{u: u, e: err}

		}
	}
}
