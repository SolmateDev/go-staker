package state

import (
	"context"

	cba "github.com/SolmateDev/go-solmate-cba"
	pba "github.com/SolmateDev/go-staker/proto/admin"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	bin "github.com/gagliardetto/binary"
	log "github.com/sirupsen/logrus"
)

type periodResult struct {
	s *sgows.AccountSubscription
	p *cba.PeriodRing
}

func SubscribePeriodRing(wsClient *sgows.Client) (*sgows.ProgramSubscription, error) {
	//prefix := sgo.Base58(base58.Encode(cba.PeriodRingDiscriminator[:]))
	return wsClient.ProgramSubscribe(cba.ProgramID, sgorpc.CommitmentFinalized)
	//return wsClient.ProgramSubscribeWithOpts(cba.ProgramID, sgorpc.CommitmentConfirmed, sgo.EncodingBase64, []sgorpc.RPCFilter{
	//{
	//DataSize: util.STRUCT_SIZE_PERIOD_RING,
	//Memcmp: &sgorpc.RPCFilterMemcmp{
	//	Offset: 0, Bytes: cba.PeriodRingDiscriminator[:],
	//},
	//},
	//})

}

func GetPeriodRing(ctx context.Context, rpcClient *sgorpc.Client, router PipelineRouter) error {

	r, err := rpcClient.GetProgramAccountsWithOpts(ctx, cba.ProgramID, &sgorpc.GetProgramAccountsOpts{
		Commitment: sgorpc.CommitmentConfirmed,
		Encoding:   sgo.EncodingBase64,
		//Filters: []sgorpc.RPCFilter{
		//	{
		//		Memcmp: &sgorpc.RPCFilterMemcmp{
		//			Offset: 0, Bytes: []byte(cba.PeriodRingDiscriminator[:]),
		//		},
		//	},
		//},
	})
	if err != nil {
		return err
	}
	for i := 0; i < len(r); i++ {
		data := r[i].Account.Data.GetBinary()
		if util.Compare(data[0:8], cba.PeriodRingDiscriminator[0:8]) {
			ring := new(cba.PeriodRing)
			err = bin.UnmarshalBorsh(ring, r[i].Account.Data.GetBinary())
			if err != nil {
				return err
			}
			err = router.AddPeriod(*ring)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

// Execute a CBA instruction to add a period
func (e1 Pipeline) AddPeriod(
	ctx context.Context,
	admin sgo.PrivateKey,
	s *pba.Settings,
	start uint64,
) error {
	log.Debugf("adding period from %d", start)
	blockHash, err := e1.rpc.GetLatestBlockhash(ctx, sgorpc.CommitmentConfirmed)
	if err != nil {
		return err
	}

	data, err := e1.Data()
	if err != nil {
		return err
	}

	instruction := cba.NewAppendPeriodInstruction(
		s.Withhold,
		start,
		s.Length,
		s.Rate.Numerator,
		s.Rate.Denominator,
		data.Controller,
		e1.Id,
		data.Periods,
		admin.PublicKey(),
		sgo.TokenProgramID,
		sgo.SysVarClockPubkey,
	)

	tx, err := sgo.NewTransactionBuilder().AddInstruction(instruction.Build()).SetFeePayer(admin.PublicKey()).SetRecentBlockHash(blockHash.Value.Blockhash).Build()
	if err != nil {
		return err
	}

	_, err = tx.Sign(func(pub sgo.PublicKey) *sgo.PrivateKey {
		if pub.Equals(admin.PublicKey()) {
			return &admin
		}
		return nil
	})
	if err != nil {
		return err
	}

	sig, err := e1.rpc.SendTransactionWithOpts(ctx, tx, sgorpc.TransactionOpts{})
	if err != nil {
		//log.Debug(err)
		return err
	}
	sub, err := e1.ws.SignatureSubscribe(sig, sgorpc.CommitmentConfirmed)
	if err != nil {
		//log.Debug(err)
		return err
	}

	_, err = sub.Recv()
	if err != nil {
		//log.Debug(err)
		return err
	}
	return nil
}
