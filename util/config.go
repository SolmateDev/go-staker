package util

import (
	"errors"
	"os"

	sgo "github.com/SolmateDev/solana-go"
)

type Configuration struct {
	GrpcListenUrl    string
	ApiKey           string
	RpcUrl           string
	WsUrl            string
	ProgramIdCba     sgo.PublicKey
	ProgramIdWebsite sgo.PublicKey
	Validator        sgo.PublicKey
	ValidatorAdmin   *ValidatorAdminConfig
	Cranker          *CrankerConfig
}

func ConfigFromEnv() (*Configuration, error) {
	var x string
	var ok bool
	var err error
	ans := new(Configuration)

	ans.GrpcListenUrl, ok = os.LookupEnv("GRPC_LISTEN_URL")
	if !ok {
		return nil, errors.New("no grpc listen url")
	}

	ans.ApiKey, ok = os.LookupEnv("API_KEY")
	if !ok {
		ans.ApiKey = ""
	}

	ans.RpcUrl, ok = os.LookupEnv("RPC_URL")
	if !ok {
		return nil, errors.New("no json rpc url")
	}

	ans.WsUrl, ok = os.LookupEnv("WS_URL")
	if !ok {
		return nil, errors.New("no json rpc websocket url")
	}

	x, ok = os.LookupEnv("PROGRAM_ID_CBA")
	if !ok {
		return nil, errors.New("no program id for cba")
	}
	ans.ProgramIdCba, err = sgo.PublicKeyFromBase58(x)
	if err != nil {
		return nil, err
	}

	x, ok = os.LookupEnv("PROGRAM_ID_WEBSITE")
	if !ok {
		return nil, errors.New("no program id for website")
	}
	ans.ProgramIdWebsite, err = sgo.PublicKeyFromBase58(x)
	if err != nil {
		return nil, err
	}

	x, ok = os.LookupEnv("VALIDATOR_ID")
	if !ok {
		return nil, errors.New("no validator id for cba")
	}
	ans.Validator, err = sgo.PublicKeyFromBase58(x)
	if err != nil {
		return nil, err
	}

	err = ans.ValidatorAdminFromEnv()
	if err != nil {
		return nil, err
	}
	err = ans.CrankerFromEnv()
	if err != nil {
		return nil, err
	}

	return ans, nil
}

type ValidatorAdminConfig struct {
	Key sgo.PrivateKey
}

func (c *Configuration) ValidatorAdminFromEnv() error {
	var ok bool
	var err error
	var x string
	ans := new(ValidatorAdminConfig)
	x, ok = os.LookupEnv("VALIDATOR_ADMIN_FP")
	if !ok {
		return nil
	}
	ans.Key, err = sgo.PrivateKeyFromSolanaKeygenFile(x)
	if err != nil {
		return err
	}

	c.ValidatorAdmin = ans
	return nil
}

type CrankerConfig struct {
	Key   sgo.PrivateKey
	Funds sgo.PublicKey
}

func (c *Configuration) CrankerFromEnv() error {
	var ok bool
	var err error
	var x string
	ans := new(CrankerConfig)
	x, ok = os.LookupEnv("CRANKER_FP")
	if !ok {
		return nil
	}
	ans.Key, err = sgo.PrivateKeyFromSolanaKeygenFile(x)
	if err != nil {
		return err
	}
	c.Cranker = ans
	return nil
}
