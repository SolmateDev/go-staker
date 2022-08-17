package script

import (
	"context"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	log "github.com/sirupsen/logrus"
)

func CreateController(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, version state.CbaVersion, payer sgo.PrivateKey, adminKey sgo.PrivateKey, crankAuthority sgo.PrivateKey, mint sgo.PublicKey) error {
	keyMap := make(map[string]sgo.PrivateKey)
	keyMap[payer.PublicKey().String()] = payer
	keyMap[adminKey.PublicKey().String()] = adminKey
	keyMap[crankAuthority.PublicKey().String()] = crankAuthority

	controllerId, _, err := state.ControllerId(version)
	if err != nil {
		return err
	}

	pcVault, _, err := state.PcVaultId(version)
	if err != nil {
		return err
	}
	log.Infof("controller id=%s", controllerId.String())
	log.Infof("pc vault=%s", pcVault.String())
	log.Infof("mint=%s", mint.String())
	log.Infof("crankAuthority=%s", mint.String())
	// keyMap[adminKey.PublicKey().String()] = adminKey

	txBuilder := sgo.NewTransactionBuilder()
	txBuilder.SetFeePayer(payer.PublicKey())

	i := cba.NewCreateInstructionBuilder()
	i.SetAdminAccount(adminKey.PublicKey())
	i.SetControllerAccount(controllerId)
	i.SetCrankAuthorityAccount(crankAuthority.PublicKey())
	i.SetPcMintAccount(mint)
	i.SetPcVaultAccount(pcVault)
	i.SetRentAccount(sgo.SysVarRentPubkey)
	i.SetClockAccount(sgo.SysVarClockPubkey)
	i.SetSystemProgramAccount(sgo.SystemProgramID)
	i.SetTokenProgramAccount(sgo.TokenProgramID)

	txBuilder.AddInstruction(i.Build())

	rh, err := rpcClient.GetLatestBlockhash(ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	txBuilder.SetRecentBlockHash(rh.Value.Blockhash)
	tx, err := txBuilder.Build()
	if err != nil {
		return err
	}
	_, err = tx.Sign(func(p sgo.PublicKey) *sgo.PrivateKey {
		x, present := keyMap[p.String()]
		if present {
			return &x
		}
		return nil
	})

	if err != nil {
		return err
	}
	sig, err := rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return err
	}
	err = util.WaitSig(ctx, wsClient, sig)
	if err != nil {
		return err
	}
	return nil

}
