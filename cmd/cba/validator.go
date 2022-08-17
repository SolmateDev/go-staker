package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/script"
	"github.com/SolmateDev/go-staker/state"
	sgo "github.com/SolmateDev/solana-go"
)

type ConfigRate state.Rate

type ValidatorAdmin struct {
	Payer        string     `name:"payer" help:"The private key that owns the SOL to pay the transaction fees."`
	ValidatorKey string     `name:"validator_key" help:"The validator key on which SOL is staked."`
	AdminKey     string     `name:"admin_key" help:"The admin key used to administrate the validator pipeline."`
	Host         string     `name:"host" help:"the host address of the Grpc Proxy server that will be receiving Grpc connections."`
	Port         uint16     `name:"port" help:"The port of the Grpc proxy server."`
	Ssl          bool       `name:"ssl" default:"false" help:"True if the grpc endpoint is SSL encrypted"`
	CrankFee     ConfigRate `name:"crank_fee" default:"0/1" help:"the crank fee"`
}

func (rate *ConfigRate) UnmarshalText(data []byte) error {

	x := strings.Split(string(data), "/")
	if len(x) != 2 {
		return errors.New("rate should be if form uint64/uint64")
	}
	num, err := strconv.ParseUint(x[0], 10, 8*8)
	if err != nil {
		return err
	}
	den, err := strconv.ParseUint(x[1], 10, 8*8)
	if err != nil {
		return err
	}
	rate.N = num
	rate.D = den
	return nil
}

func (r *ValidatorAdmin) Run(kongCtx *CLIContext) error {
	//Create(ctx context.Context, config *Configuration, rpcClient *sgorpc.Client, wsClient *sgows.Client)
	ctx := kongCtx.Ctx
	if kongCtx.Clients == nil {
		return errors.New("no rpc or ws client")
	}

	payer, err := sgo.PrivateKeyFromBase58(r.Payer)
	if err != nil {
		payer, err = sgo.PrivateKeyFromSolanaKeygenFile(r.Payer)
		if err != nil {
			return err
		}
	}

	validatorKey, err := sgo.PrivateKeyFromBase58(r.ValidatorKey)
	if err != nil {
		validatorKey, err = sgo.PrivateKeyFromSolanaKeygenFile(r.ValidatorKey)
		if err != nil {
			return err
		}
	}

	adminKey, err := sgo.PrivateKeyFromBase58(r.AdminKey)
	if err != nil {
		adminKey, err = sgo.PrivateKeyFromSolanaKeygenFile(r.AdminKey)
		if err != nil {
			return err
		}
	}

	client, err := script.Create(ctx, &script.Configuration{Version: state.VERSION_1}, kongCtx.Clients.Rpc, kongCtx.Clients.Ws)
	if err != nil {
		return err
	}
	err = client.AddValidator(payer, validatorKey, adminKey, cba.ProxyAddress{
		Url: []byte(fmt.Sprintf("%s:%d", r.Host, r.Port)),
	}, state.Rate(r.CrankFee))
	if err != nil {
		return err
	}
	return nil
}
