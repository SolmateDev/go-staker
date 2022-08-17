package state

import (
	"context"
	"errors"
	"fmt"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	bin "github.com/gagliardetto/binary"
	log "github.com/sirupsen/logrus"
)

type CbaVersion string

const VERSION_1 CbaVersion = "1"

func ControllerId(version CbaVersion) (sgo.PublicKey, uint8, error) {
	if version != VERSION_1 {
		return sgo.PublicKey{}, 0, errors.New("bad version")
	}
	name := "controller"
	return sgo.FindProgramAddress([][]byte{[]byte(name[:])}, cba.ProgramID)
}

func PcVaultId(version CbaVersion) (sgo.PublicKey, uint8, error) {
	name := "pc_vault"
	return sgo.FindProgramAddress([][]byte{[]byte(name[:])}, cba.ProgramID)
}

type Controller struct {
	ctx               context.Context
	id                sgo.PublicKey
	internalC         chan<- func(*controllerInternal)
	Router            PipelineRouter
	updateControllerC chan<- util.ResponseChannel[cba.Controller]
	rpc               *sgorpc.Client
	ws                *sgows.Client
}

func (e1 Controller) Print() (string, error) {
	ans := ""

	ans += fmt.Sprintf("Account ID=%s\n", e1.id.String())
	data, err := e1.Data()
	if err != nil {
		return "", err
	}
	ans += fmt.Sprintf("Admin=%+v\n", data.Admin.String())
	ans += fmt.Sprintf("Vault Mint=%+v\n", data.PcMint.String())
	ans += fmt.Sprintf("Vault Id=%+v\n", data.PcVault.String())
	a := new(sgotkn.Account)
	x, err := e1.rpc.GetAccountInfo(e1.ctx, data.PcVault)
	if err != nil {
		return "", err
	}
	err = bin.UnmarshalBorsh(a, x.Value.Data.GetBinary())
	if err != nil {
		return "", err
	}
	ans += fmt.Sprintf("Vault Balance=%d\n", a.Amount)

	return ans, nil
}

func (e1 Controller) CloseSignal() <-chan error {
	signalC := make(chan error, 1)
	e1.internalC <- func(in *controllerInternal) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

func CreateController(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, version CbaVersion) (Controller, error) {
	controllerId, controllerBump, err := ControllerId(version)
	if err != nil {
		return Controller{}, err
	}
	log.Infof("controller id 2 =%s", controllerId.String())
	data := new(cba.Controller)
	err = rpcClient.GetAccountDataBorshInto(ctx, controllerId, data)
	if err != nil {
		return Controller{}, err
	}

	if data.ControllerBump != controllerBump {
		return Controller{}, errors.New("bump does not match")
	}

	internalC := make(chan func(*controllerInternal), 10)

	router, err := CreatePipelineRouter(ctx, rpcClient, wsClient)
	if err != nil {
		return Controller{}, err
	}

	sub, err := wsClient.AccountSubscribe(controllerId, sgorpc.CommitmentConfirmed)
	if err != nil {
		return Controller{}, err
	}
	home := util.CreateSubHome[cba.Controller]()
	reqC := home.ReqC
	go loopController(ctx, internalC, controllerId, *data, sub, home)

	return Controller{ctx: ctx, id: controllerId, internalC: internalC, Router: router, updateControllerC: reqC, rpc: rpcClient, ws: wsClient}, nil
}

func (e1 Controller) Id() sgo.PublicKey {
	return e1.id
}

func (e1 Controller) filter_onController(c cba.Controller) bool {
	return true
}

func (e1 Controller) OnData() util.Subscription[cba.Controller] {
	return util.SubscriptionRequest(e1.updateControllerC, e1.filter_onController)
}

func (e1 Controller) Data() (cba.Controller, error) {
	errorC := make(chan error, 1)
	ansC := make(chan cba.Controller, 1)
	e1.internalC <- func(in *controllerInternal) {
		if in.data == nil {
			errorC <- errors.New("no controller information")
		} else {
			errorC <- nil
			ansC <- *in.data
		}
	}
	err := <-errorC
	if err != nil {
		return cba.Controller{}, err
	}
	return <-ansC, nil
}

type controllerInternal struct {
	ctx              context.Context
	closeSignalCList []chan<- error
	errorC           chan<- error
	id               sgo.PublicKey
	data             *cba.Controller
	home             *util.SubHome[cba.Controller]
}

func loopController(ctx context.Context, internalC <-chan func(*controllerInternal), id sgo.PublicKey, data cba.Controller, sub *sgows.AccountSubscription, home *util.SubHome[cba.Controller]) {
	doneC := ctx.Done()
	errorC := make(chan error, 1)
	in := new(controllerInternal)
	in.ctx = ctx
	in.errorC = errorC
	in.closeSignalCList = make([]chan<- error, 0)
	in.id = id
	in.data = &data
	in.home = home

	streamC := sub.RecvStream()
	closeC := sub.RecvErr()

	var err error
out:
	for {
		select {
		case x := <-in.home.ReqC:
			in.home.Receive(x)
		case d := <-streamC:
			r, ok := d.(*sgows.AccountResult)
			if !ok {
				err = errors.New("bad account result")
				break out
			}
			data := new(cba.Controller)
			err = bin.UnmarshalBorsh(data, r.Value.Account.Data.GetBinary())
			if err != nil {
				break out
			}
			in.on_data(data)
		case err = <-closeC:
			break out
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)
		}
	}

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}

func (in *controllerInternal) on_data(data *cba.Controller) {
	in.data = data
}
