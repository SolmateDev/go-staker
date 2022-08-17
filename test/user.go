package test

import (
	"context"

	sgo "github.com/SolmateDev/solana-go"
	sgoaka "github.com/SolmateDev/solana-go/programs/associated-token-account"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type User struct {
	Id          uint64
	Key         sgo.PrivateKey
	PcVaultId   sgo.PublicKey
	PcVault     *sgotkn.Account
	PcVaultBump uint8
	ctx         context.Context
	rpc         *sgorpc.Client
	ws          *sgows.Client
	minter      Minter
}

// create random user that has an id, token account
func GenerateRandomUser(ctx context.Context, id uint64, rpcUrl string, wsClient *sgows.Client, minter Minter) (*User, error) {
	var err error
	u := new(User)
	u.Id = id
	u.ctx = ctx
	u.rpc = sgorpc.New(rpcUrl)
	u.ws = wsClient
	u.minter = minter

	u.Key, err = sgo.NewRandomPrivateKey()
	if err != nil {
		return nil, err
	}

	sig, err := u.rpc.RequestAirdrop(ctx, u.Key.PublicKey(), 10*sgo.LAMPORTS_PER_SOL, sgorpc.CommitmentFinalized)
	if err != nil {
		return nil, err
	}
	err = WaitSig(u.ctx, sig, u.ws)
	if err != nil {
		return nil, err
	}

	u.PcVaultId, u.PcVaultBump, err = sgo.FindAssociatedTokenAddress(u.Key.PublicKey(), minter.Id)
	if err != nil {
		return nil, err
	}

	err = u.create_token_account(minter.Id)
	if err != nil {
		return nil, err
	}

	u.PcVault = new(sgotkn.Account)
	err = u.rpc.GetAccountDataBorshInto(ctx, u.PcVaultId, u.PcVault)
	if err != nil {
		return nil, err
	}

	return u, nil
}

func (u *User) Mint(amount uint64) error {
	return <-u.minter.Send(u.ctx, u.PcVaultId, amount)
}

func (u *User) create_token_account(mint sgo.PublicKey) error {
	payer := u.Key
	wallet := u.Key
	lh, err := u.rpc.GetLatestBlockhash(u.ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	instruction := sgoaka.NewCreateInstruction(payer.PublicKey(), wallet.PublicKey(), mint).Build()
	tx, err := sgo.NewTransactionBuilder().AddInstruction(instruction).SetFeePayer(payer.PublicKey()).SetRecentBlockHash(lh.Value.Blockhash).Build()
	if err != nil {
		return err
	}
	_, err = tx.Sign(func(pub sgo.PublicKey) *sgo.PrivateKey {
		if pub.Equals(wallet.PublicKey()) {
			return &wallet
		} else if pub.Equals(payer.PublicKey()) {
			return &payer
		}
		return nil
	})
	if err != nil {
		return err
	}

	sig, err := u.rpc.SendTransaction(u.ctx, tx)
	if err != nil {
		return err
	}
	err = WaitSig(u.ctx, sig, u.ws)
	if err != nil {
		return err
	}
	return nil
}
