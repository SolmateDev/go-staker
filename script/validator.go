package script

import (
	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	"github.com/SolmateDev/solana-go/programs/system"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	log "github.com/sirupsen/logrus"
)

type addValidatorPrep struct {
	bidKey            sgo.PrivateKey
	bidInstruction    *system.Instruction
	periodKey         sgo.PrivateKey
	periodInstruction *system.Instruction
}

func (e1 *Script) add_validator_prepare(keyMap map[string]sgo.PrivateKey, payer sgo.PrivateKey) (*addValidatorPrep, error) {
	prep := new(addValidatorPrep)
	var err error

	r_bids, err := CreateAccount(e1.ctx, e1.rpc, util.STRUCT_SIZE_BID_LIST, cba.ProgramID, payer)
	if err != nil {
		return nil, err
	}
	prep.bidKey = r_bids.Key
	keyMap[r_bids.Key.PublicKey().String()] = r_bids.Key
	prep.bidInstruction = r_bids.Instruction

	r_periods, err := CreateAccount(e1.ctx, e1.rpc, util.STRUCT_SIZE_PERIOD_RING, cba.ProgramID, payer)
	if err != nil {
		return nil, err
	}
	prep.periodKey = r_periods.Key
	keyMap[r_periods.Key.PublicKey().String()] = r_periods.Key
	prep.periodInstruction = r_periods.Instruction

	return prep, nil
}

func (e1 *Script) AddValidator(payer sgo.PrivateKey, validatorKey sgo.PrivateKey, adminKey sgo.PrivateKey, address cba.ProxyAddress, crankFee state.Rate) error {

	controllerData, err := e1.controller.Data()
	if err != nil {
		return err
	}

	keyMap := make(map[string]sgo.PrivateKey)
	keyMap[payer.PublicKey().String()] = payer
	keyMap[validatorKey.PublicKey().String()] = validatorKey
	keyMap[adminKey.PublicKey().String()] = adminKey

	prep, err := e1.add_validator_prepare(keyMap, payer)
	if err != nil {
		return err
	}

	pipelineId, _, err := state.PipelineId(e1.controller.Id(), validatorKey.PublicKey())
	if err != nil {
		return err
	}
	log.Infof("add-validator----pipeline id=%s", pipelineId.String())
	log.Infof("add-validator----pipeline id input=(%s;%s)", e1.controller.Id(), validatorKey.PublicKey())

	b := cba.NewAddValidatorInstructionBuilder()
	b.SetControllerAccount(e1.controller.Id())
	b.SetValidatorPipelineAccount(pipelineId)
	b.SetPcMintAccount(controllerData.PcMint)
	b.SetValidatorAccount(validatorKey.PublicKey())
	b.SetAdminAccount(adminKey.PublicKey())
	b.SetBidsAccount(prep.bidKey.PublicKey())
	b.SetPeriodsAccount(prep.periodKey.PublicKey())
	b.SetSystemProgramAccount(sgo.SystemProgramID)
	b.SetRentAccount(sgo.SysVarRentPubkey)

	b.SetCrankFeeRateNum(crankFee.N)
	b.SetCrankFeeRateDen(crankFee.D)
	b.SetAddress(address)

	{
		txBuilder := sgo.NewTransactionBuilder()
		txBuilder.SetFeePayer(payer.PublicKey())
		txBuilder.AddInstruction(prep.bidInstruction)
		txBuilder.AddInstruction(prep.periodInstruction)
		txBuilder.AddInstruction(b.Build())
		rh, err := e1.rpc.GetLatestBlockhash(e1.ctx, sgorpc.CommitmentFinalized)
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
		sig, err := e1.rpc.SendTransactionWithOpts(e1.ctx, tx, sgorpc.TransactionOpts{SkipPreflight: false})
		if err != nil {
			return err
		}
		err = util.WaitSig(e1.ctx, e1.ws, sig)
		if err != nil {
			return err
		}
	}

	return nil
}
