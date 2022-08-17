package script

import (
	"context"
	"encoding/json"
	"io/ioutil"

	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgotkn2 "github.com/SolmateDev/solana-go/programs/associated-token-account"
	sgosys "github.com/SolmateDev/solana-go/programs/system"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
	bin "github.com/gagliardetto/binary"
	log "github.com/sirupsen/logrus"
)

type CreateAccountResult struct {
	Instruction *sgosys.Instruction
	Key         sgo.PrivateKey
}

func CreateAccount(ctx context.Context, rpcClient *sgorpc.Client, size uint64, owner sgo.PublicKey, payer sgo.PrivateKey) (*CreateAccountResult, error) {
	key, err := sgo.NewRandomPrivateKey()
	if err != nil {
		return nil, err
	}
	lamports, err := rpcClient.GetMinimumBalanceForRentExemption(ctx, size, sgorpc.CommitmentFinalized)
	if err != nil {
		return nil, err
	}
	b := sgosys.NewCreateAccountInstructionBuilder()
	b.SetFundingAccount(payer.PublicKey())
	b.SetLamports(lamports)
	b.SetNewAccount(key.PublicKey())
	b.SetOwner(owner)
	b.SetSpace(size)
	log.Infof("create account (%s;%s;%s)", key.PublicKey().String(), payer.PublicKey().String(), owner.String())

	return &CreateAccountResult{Key: key, Instruction: b.Build()}, nil
}

type MintResult struct {
	Id        *sgo.PublicKey  `json:"id"`
	Data      *sgotkn.Mint    `json:"data"`
	Authority *sgo.PrivateKey `json:"authority"`
}

func MintFromFile(filePath string) (*MintResult, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	mr := new(MintResult)
	err = json.Unmarshal(data, mr)
	if err != nil {
		return nil, err
	}
	return mr, nil
}

func (mr *MintResult) MintTo(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, payer sgo.PrivateKey, owner sgo.PublicKey, amount uint64) error {
	keyMap := make(map[string]sgo.PrivateKey)
	keyMap[mr.Authority.PublicKey().String()] = *mr.Authority
	keyMap[payer.PublicKey().String()] = payer

	accountId, _, err := sgo.FindAssociatedTokenAddress(owner, *mr.Id)
	if err != nil {
		return err
	}

	i := sgotkn.NewMintToInstructionBuilder()
	i.SetAmount(amount)
	i.SetAuthorityAccount(mr.Authority.PublicKey())
	i.SetDestinationAccount(accountId)
	i.SetMintAccount(*mr.Id)

	txBuilder := sgo.NewTransactionBuilder()
	txBuilder.SetFeePayer(payer.PublicKey())
	rh, err := rpcClient.GetLatestBlockhash(ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	txBuilder.SetRecentBlockHash(rh.Value.Blockhash)

	txBuilder.AddInstruction(i.Build())

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

func Airdrop(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, destination sgo.PublicKey, amount uint64) error {
	sig, err := rpcClient.RequestAirdrop(ctx, destination, amount, sgorpc.CommitmentFinalized)
	if err != nil {
		return err
	}
	return util.WaitSig(ctx, wsClient, sig)
}

const MINT_SIZE = 4 + 40 + 8 + 1 + 1 + 4 + 40

/*exp
ort const MintLayout = struct<RawMint>([
    u32('mintAuthorityOption'),
    publicKey('mintAuthority'),
    u64('supply'),
    u8('decimals'),
    bool('isInitialized'),
    u32('freezeAuthorityOption'),
    publicKey('freezeAuthority'),
]);*/

const TOKEN_ACCOUNT_SIZE = 40 + 40 + 8 + 4 + 40 + 1 + 4 + 8 + 8 + 4 + 40

/*
   publicKey('mint'),
   publicKey('owner'),
   u64('amount'),
   u32('delegateOption'),
   publicKey('delegate'),
   u8('state'),
   u32('isNativeOption'),
   u64('isNative'),
   u64('delegatedAmount'),
   u32('closeAuthorityOption'),
   publicKey('closeAuthority'),
*/

func CreateMint(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, payer sgo.PrivateKey, authority sgo.PrivateKey, decimals uint8) (*MintResult, error) {
	keyMap := make(map[string]sgo.PrivateKey)
	keyMap[authority.PublicKey().String()] = authority
	keyMap[payer.PublicKey().String()] = payer

	var space uint64 = 82
	minLamports, err := rpcClient.GetMinimumBalanceForRentExemption(ctx, space, sgorpc.CommitmentConfirmed)
	if err != nil {
		return nil, err
	}

	mintAddressPrivateKey, err := sgo.NewRandomPrivateKey()
	if err != nil {
		return nil, err
	}
	keyMap[mintAddressPrivateKey.PublicKey().String()] = mintAddressPrivateKey
	mint := mintAddressPrivateKey.PublicKey()

	rh, err := rpcClient.GetLatestBlockhash(ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		return nil, err
	}
	tx, err := sgo.NewTransaction(
		[]sgo.Instruction{
			sgosys.NewCreateAccountInstructionBuilder().SetSpace(space).SetLamports(minLamports).SetOwner(sgotkn.ProgramID).SetFundingAccount(payer.PublicKey()).SetNewAccount(mint).Build(),
			// initialize the mint account
			sgotkn.NewInitializeMintInstructionBuilder().SetDecimals(decimals).SetMintAccount(mint).SetMintAuthority(authority.PublicKey()).SetFreezeAuthority(authority.PublicKey()).Build(),
		},
		rh.Value.Blockhash,
		sgo.TransactionPayer(payer.PublicKey()),
	)
	if err != nil {
		return nil, err
	}
	_, err = tx.Sign(func(p sgo.PublicKey) *sgo.PrivateKey {
		x, present := keyMap[p.String()]
		if present {
			return &x
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sig, err := rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return nil, err
	}

	err = util.WaitSig(ctx, wsClient, sig)
	if err != nil {
		return nil, err
	}

	d, err := rpcClient.GetAccountInfo(ctx, mintAddressPrivateKey.PublicKey())
	if err != nil {
		return nil, err
	}
	a := new(sgotkn.Mint)
	err = bin.UnmarshalBorsh(a, d.Value.Data.GetBinary())
	if err != nil {
		return nil, err
	}
	id := mintAddressPrivateKey.PublicKey()
	a_copy := authority

	return &MintResult{Id: &id, Data: a, Authority: &a_copy}, nil
}

func CreateTokenAccount(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, payer sgo.PrivateKey, owner sgo.PublicKey, mint sgo.PublicKey) (*sgotkn.Account, error) {
	keyMap := make(map[string]sgo.PrivateKey)
	keyMap[payer.PublicKey().String()] = payer
	//keyMap[owner.PublicKey().String()] = owner

	b := sgotkn2.NewCreateInstructionBuilder()
	b.SetMint(mint)
	b.SetPayer(payer.PublicKey())
	//b.SetWallet(owner.PublicKey())
	b.SetWallet(owner)

	txBuilder := sgo.NewTransactionBuilder()
	txBuilder.SetFeePayer(payer.PublicKey())

	rh, err := rpcClient.GetLatestBlockhash(ctx, sgorpc.CommitmentFinalized)
	if err != nil {
		return nil, err
	}
	txBuilder.SetRecentBlockHash(rh.Value.Blockhash)

	txBuilder.AddInstruction(b.Build())

	tx, err := txBuilder.Build()
	if err != nil {
		return nil, err
	}
	_, err = tx.Sign(func(p sgo.PublicKey) *sgo.PrivateKey {
		x, present := keyMap[p.String()]
		if present {
			return &x
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sig, err := rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return nil, err
	}
	err = util.WaitSig(ctx, wsClient, sig)
	if err != nil {
		return nil, err
	}
	accountId, _, err := sgo.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return nil, err
	}
	a := new(sgotkn.Account)
	d, err := rpcClient.GetAccountInfo(ctx, accountId)
	if err != nil {
		return nil, err
	}
	err = bin.UnmarshalBorsh(a, d.Value.Data.GetBinary())
	if err != nil {
		return nil, err
	}
	return a, nil

}

func GetTokenAccount(ctx context.Context, rpcClient *sgorpc.Client, owner sgo.PublicKey, mint sgo.PublicKey) (*sgotkn.Account, error) {
	accountId, _, err := sgo.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return nil, err
	}
	a := new(sgotkn.Account)
	d, err := rpcClient.GetAccountInfo(ctx, accountId)
	if err != nil {
		return nil, err
	}
	err = bin.UnmarshalBorsh(a, d.Value.Data.GetBinary())
	if err != nil {
		return nil, err
	}
	return a, nil
}
