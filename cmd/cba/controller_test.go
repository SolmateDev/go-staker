package main_test

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	cba "github.com/SolmateDev/go-solmate-cba"
	svr "github.com/SolmateDev/go-staker/proxy"
	"github.com/SolmateDev/go-staker/script"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/test"
	"github.com/SolmateDev/go-staker/test/scenarios/basic"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func configFromEnv() (*util.Configuration, error) {
	ans := new(util.Configuration)
	var err error

	ans.ValidatorAdmin = new(util.ValidatorAdminConfig)
	ans.ValidatorAdmin.Key, err = sgo.PrivateKeyFromBase58(os.Getenv("VALIDATOR_ADMIN_FP"))
	if err != nil {
		ans.ValidatorAdmin.Key, err = sgo.PrivateKeyFromSolanaKeygenFile(os.Getenv("VALIDATOR_ADMIN_FP"))
		if err != nil {
			return nil, err
		}
	}

	ans.Cranker = new(util.CrankerConfig)
	ans.Cranker.Key, err = sgo.PrivateKeyFromBase58(os.Getenv("CRANKER_FP"))
	if err != nil {
		ans.Cranker.Key, err = sgo.PrivateKeyFromSolanaKeygenFile(os.Getenv("CRANKER_FP"))
		if err != nil {
			return nil, err
		}
	}

	ans.GrpcListenUrl = "0.0.0.0:50051"

	ans.ProgramIdCba, err = sgo.PublicKeyFromBase58(os.Getenv("PROGRAM_ID_CBA"))
	if err != nil {
		return nil, err
	}

	ans.RpcUrl = "http://localhost:8899"
	ans.WsUrl = "ws://localhost:8900"

	return ans, nil
}

func TestController(t *testing.T) {
	var err error
	err = godotenv.Load("../../.env")
	if err != nil {
		t.Fatal(err)
	}
	err = godotenv.Load("../../.tmpenv")
	if err != nil {
		t.Fatal(err)
	}
	log.SetLevel(log.DebugLevel)

	config, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	cba.SetProgramID(config.ProgramIdCba)
	log.Debugf("program id=%s", cba.ProgramID.String())

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	// connect to the validator
	rpcClient := sgorpc.New(config.RpcUrl)
	wsClient, err := sgows.Connect(ctx, config.WsUrl)
	if err != nil {
		t.Fatal(err)
	}

	go loopProgram(ctx, wsClient)

	payer, err := sgo.NewRandomPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	err = script.Airdrop(ctx, rpcClient, wsClient, payer.PublicKey(), 100*sgo.LAMPORTS_PER_SOL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sgo.NewRandomPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	err = script.Airdrop(ctx, rpcClient, wsClient, admin.PublicKey(), 100*sgo.LAMPORTS_PER_SOL)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("validator fp=%s", os.Getenv("VALIDATOR_FP"))
	validator, err := sgo.PrivateKeyFromBase58(os.Getenv("VALIDATOR_FP"))
	if err != nil {
		validator, err = sgo.PrivateKeyFromSolanaKeygenFile(os.Getenv("VALIDATOR_FP"))
		if err != nil {
			t.Fatal(err)
		}
	}
	config.Validator = validator.PublicKey()

	// create mint
	usdcMintAuthority, err := sgo.NewRandomPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	usdcMintResult, err := script.CreateMint(ctx, rpcClient, wsClient, payer, usdcMintAuthority, 2)
	if err != nil {
		t.Fatal(err)
	}
	mint := *usdcMintResult.Id

	err = script.CreateController(ctx, rpcClient, wsClient, state.VERSION_1, payer, admin, admin, mint)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := state.CreateController(ctx, rpcClient, wsClient, state.VERSION_1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("controller id is %s", controller.Id().String())
	time.Sleep(30 * time.Second)
	wg.Add(1)
	go func() {
		<-controller.CloseSignal()
		wg.Done()
	}()

	expectingPipelineId, _, err := state.PipelineId(controller.Id(), config.Validator)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("expecting pipeline id=%s", expectingPipelineId.String())
	log.Infof("expecting----pipeline id=%s", expectingPipelineId.String())
	log.Infof("expecting----pipeline id input=(%s;%s)", controller.Id(), config.Validator)
	sub := controller.Router.OnPipeline()
	ctxForAddValidator, cancelForAddValidator := context.WithTimeout(ctx, 90*time.Second)
	// add the validator first before starting the proxy
	err = addValidator(ctxForAddValidator, config, payer, validator, admin, cba.ProxyAddress{Url: []byte(config.GrpcListenUrl)})
	if err != nil {
		t.Fatal(err)
	}

out:
	for {
		select {
		case <-ctxForAddValidator.Done():
			t.Fatal("timed out, failed to get pipeline")
		case updatedPipelineGroup := <-sub.StreamC:
			if updatedPipelineGroup.Id.Equals(expectingPipelineId) {
				break out
			} else {
				t.Fatalf("received pipeline id=%s ", updatedPipelineGroup.Id.String())
			}
		case err = <-sub.ErrorC:
			break out
		}
	}
	cancelForAddValidator()
	sub.Unsubscribe()
	if err != nil {
		t.Log(err)
	}
	//time.Sleep(30 * time.Second)

	proxy, err := svr.Run(ctx, config, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() {
		<-proxy.CloseSignal()
		wg.Done()
	}()

	time.Sleep(30 * time.Second)

	bidderGroup, err := test.GenerateGroup(ctx, 5, config.RpcUrl, config.WsUrl, controller, usdcMintAuthority)
	if err != nil {
		t.Fatal(err)
	}

	bidderToUsers := make(map[uint64]*test.BiddingGroup)
	for userId, user := range bidderGroup.Users {
		x, err := test.GenerateGroup(ctx, 10, config.RpcUrl, config.WsUrl, controller, usdcMintAuthority)
		if err != nil {
			t.Fatal(err)
		}
		bidderToUsers[userId] = &test.BiddingGroup{
			Bidder: user,
			Group:  x,
		}
	}

	err = basic.Prep(ctx, bidderToUsers)
	if err != nil {
		t.Fatal(err)
	}

}

func addValidator(ctx context.Context, config *util.Configuration, payer sgo.PrivateKey, validator sgo.PrivateKey, admin sgo.PrivateKey, address cba.ProxyAddress) error {
	rpcClient := sgorpc.New(config.RpcUrl)
	wsClient, err := sgows.Connect(ctx, config.WsUrl)
	if err != nil {
		return err
	}

	client, err := script.Create(ctx, &script.Configuration{Version: state.VERSION_1}, rpcClient, wsClient)
	if err != nil {
		return err
	}
	err = client.AddValidator(payer, validator, admin, address, state.Rate{N: 1, D: 100})
	if err != nil {
		return err
	}
	return nil
}

func loopProgram(ctx context.Context, wsClient *sgows.Client) {
	sub, err := wsClient.ProgramSubscribe(cba.ProgramID, sgorpc.CommitmentFinalized)
	if err != nil {
		log.Debug(err)
		return
	}
	doneC := ctx.Done()
	errorC := sub.CloseSignal()
	streamC := sub.RecvStream()

	//log.Debugf("controller=%s and pipeline=%s", hex.EncodeToString(cba.ControllerDiscriminator[:]), hex.EncodeToString(cba.PipelineDiscriminator[:]))
out:
	for {
		select {
		case <-doneC:
		case err = <-errorC:
			break out
		case d := <-streamC:
			x, ok := d.(*sgows.ProgramResult)
			if !ok {
				err = errors.New("bad program result")
				break out
			}
			data := x.Value.Account.Data.GetBinary()
			log.Debugf("....length=%d and discriminator=%s", len(data), hex.EncodeToString(data[0:8]))

		}
	}
	if err != nil {
		log.Debug(err)
	}
}
