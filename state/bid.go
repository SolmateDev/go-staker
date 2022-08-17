package state

import (
	"context"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	bin "github.com/gagliardetto/binary"
)

func SubscribeBidList(wsClient *sgows.Client) (*sgows.ProgramSubscription, error) {
	return wsClient.ProgramSubscribe(cba.ProgramID, sgorpc.CommitmentFinalized)
	//return wsClient.ProgramSubscribeWithOpts(cba.ProgramID, sgorpc.CommitmentConfirmed, sgo.EncodingBase64, []sgorpc.RPCFilter{
	//{
	//	DataSize: util.STRUCT_SIZE_BID_LIST,
	//Memcmp: &sgorpc.RPCFilterMemcmp{
	//	Offset: 0, Bytes: cba.BidListDiscriminator[:],
	//},
	//},
	//})

}

func GetBidList(ctx context.Context, rpcClient *sgorpc.Client, router PipelineRouter) error {
	//prefix := sgo.Base58(base58.Encode(cba.BidListDiscriminator[:]))
	r, err := rpcClient.GetProgramAccountsWithOpts(ctx, cba.ProgramID, &sgorpc.GetProgramAccountsOpts{
		Commitment: sgorpc.CommitmentConfirmed,
		Encoding:   sgo.EncodingBase64,
		//Filters: []sgorpc.RPCFilter{
		//	{
		//		Memcmp: &sgorpc.RPCFilterMemcmp{
		//			Offset: 0, Bytes: prefix,
		//		},
		//	},
		//},
	})
	if err != nil {
		return err
	}
	for i := 0; i < len(r); i++ {
		data := r[i].Account.Data.GetBinary()
		//if data[0:8]!=cba.BidListDiscriminator[0:8]{
		if util.Compare(data[0:8], cba.BidListDiscriminator[0:8]) {
			list := new(cba.BidList)
			err = bin.UnmarshalBorsh(list, data)
			if err != nil {
				return err
			}
			err = router.AddBid(*list)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
