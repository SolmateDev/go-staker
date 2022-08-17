package client

import (
	"context"

	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type ValidatorStatistics struct {
	Id           sgo.PublicKey
	CurrentStake uint64
	SkipRate     float64
}

type NetworkStatistics struct {
	TotalSlots int64
	TotalStake uint64
	Validators map[string]*ValidatorStatistics
}

func GetValidatorInfo(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client) (*NetworkStatistics, error) {
	ans := new(NetworkStatistics)
	ans.Validators = make(map[string]*ValidatorStatistics)

	total_active_stake := uint64(0)
	total_delinquent_stake := uint64(0)

	voteAccountResult, err := rpcClient.GetVoteAccounts(ctx, &sgorpc.GetVoteAccountsOpts{
		Commitment: sgorpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, err
	}

	for i := 0; i < len(voteAccountResult.Current); i++ {
		validatorId := voteAccountResult.Current[i].NodePubkey

		x, present := ans.Validators[validatorId.String()]
		if !present {
			x = new(ValidatorStatistics)
			x.Id = validatorId
			x.SkipRate = 0
			ans.Validators[validatorId.String()] = x
		}
		stake := voteAccountResult.Current[i].ActivatedStake
		x.CurrentStake += stake
		total_active_stake += stake
	}

	for i := 0; i < len(voteAccountResult.Delinquent); i++ {
		validatorId := voteAccountResult.Delinquent[i].NodePubkey
		x, present := ans.Validators[validatorId.String()]
		if !present {
			x = new(ValidatorStatistics)
			x.Id = validatorId
			x.SkipRate = 0
			ans.Validators[validatorId.String()] = x
		}
		stake := voteAccountResult.Delinquent[i].ActivatedStake
		x.CurrentStake -= stake
		total_delinquent_stake += stake
	}
	ans.TotalStake = total_active_stake - total_delinquent_stake

	b, err := rpcClient.GetBlockProductionWithOpts(ctx, &sgorpc.GetBlockProductionOpts{
		Commitment: sgorpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, err
	}

	totalSlots := int64(0)

	for identity, v := range b.Value.ByIdentity {
		leader_slots := v[0]
		blocks_produced := v[1]
		totalSlots += leader_slots
		x, present := ans.Validators[identity.String()]
		if present {
			x.SkipRate = 100 * (float64(leader_slots) - float64(blocks_produced)) / float64(leader_slots)
		}
	}

	return ans, nil
}
