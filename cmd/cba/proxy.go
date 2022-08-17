package main

import (
	"errors"

	cba "github.com/SolmateDev/go-solmate-cba"
	svr "github.com/SolmateDev/go-staker/proxy"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	"github.com/google/uuid"
)

type Proxy struct {
	Host         string        `name:"host"  help:"host on which to listen for Grpc connections from bidders"`
	Port         uint16        `name:"port" help:"port on which to listen for Grpc connections from bidders"`
	ApiKey       string        `option name:"apikey" help:"used to authenticate with JSON RPC proxy"`
	ProgramIdCba sgo.PublicKey `name:"program_id_cba" help:"Specify the program id for the CBA program"`
	Validator    sgo.PublicKey `name:"apikey" help:"Specify the validator key"`
	CrankerKey   string        `option name:"cranker_key" help:"the private key of the cranker"`
	AdminKey     string        `option name:"admin_key" help:"The validator admin"`
	// ProgramIdWebsite sgo.PublicKey `name:"program_id_website" help:"Specify the program id for the website program"`
}

func (r *Proxy) config() (*util.Configuration, error) {
	ans := new(util.Configuration)
	var err error

	if 0 < len(r.ApiKey) {
		x, err := uuid.Parse(r.ApiKey)
		if err != nil {
			return nil, err
		}
		ans.ApiKey = x.String()
	}
	if 0 < len(r.CrankerKey) {
		y := new(util.CrankerConfig)
		y.Key, err = sgo.PrivateKeyFromBase58(r.CrankerKey)
		if err != nil {
			y.Key, err = sgo.PrivateKeyFromSolanaKeygenFile(r.CrankerKey)
			if err != nil {
				return nil, err
			}
		}
		ans.Cranker = y
	}
	if 0 < len(r.AdminKey) {
		y := new(util.ValidatorAdminConfig)
		y.Key, err = sgo.PrivateKeyFromBase58(r.CrankerKey)
		if err != nil {
			y.Key, err = sgo.PrivateKeyFromSolanaKeygenFile(r.CrankerKey)
			if err != nil {
				return nil, err
			}
		}
		ans.ValidatorAdmin = y
	}

	return ans, nil
}

func (r *Proxy) Run(kongCtx *CLIContext) error {
	ctx := kongCtx.Ctx

	cba.SetProgramID(r.ProgramIdCba)
	config, err := r.config()
	if err != nil {
		return err
	}
	if kongCtx.Clients == nil {
		return errors.New("no rpc or ws")
	}
	proxy, err := svr.Run(ctx, config, kongCtx.Clients.Rpc, kongCtx.Clients.Ws)
	if err != nil {
		return err
	}
	<-proxy.CloseSignal()
	return nil
}
