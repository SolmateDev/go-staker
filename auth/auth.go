package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"math/rand"
	"time"

	cba "github.com/SolmateDev/go-solmate-cba"
	sgo "github.com/SolmateDev/solana-go"
	bin "github.com/gagliardetto/binary"
	"google.golang.org/grpc/metadata"
)

const (
	HEADER_AUTH      = "auth"
	HEADER_SIGNATURE = "signature"
)

// client add authentication information to the request header
func SetAuth(ctx context.Context, userKey sgo.PrivateKey) (context.Context, error) {

	x := new(cba.BidAuth)
	x.Nonce = int64(rand.Intn(math.MaxInt64))
	x.Start = time.Now().Unix()
	x.User = userKey.PublicKey()
	data, err := bin.MarshalBorsh(x)
	if err != nil {
		return ctx, err
	}
	sig, err := userKey.Sign(data)
	if err != nil {
		return ctx, err
	}

	return metadata.AppendToOutgoingContext(ctx, HEADER_AUTH, base64.StdEncoding.EncodeToString(data), HEADER_SIGNATURE, sig.String()), nil
}

const MAX_AGE = 30 * time.Minute

// server (proxy) gets authentication information from the request header
func GetAuth(ctx context.Context) (*cba.BidAuth, error) {
	db, present := metadata.FromIncomingContext(ctx)
	if !present {
		return nil, errors.New("no auth")
	}
	x := db.Get(HEADER_AUTH)
	if len(x) != 1 {
		return nil, errors.New("no auth or auth duplicated")
	}
	data, err := base64.StdEncoding.DecodeString(x[0])
	if err != nil {
		return nil, err
	}

	y := new(cba.BidAuth)
	err = bin.UnmarshalBorsh(y, data)
	if err != nil {
		return nil, err
	}

	if time.Now().Add(-1 * MAX_AGE).After(time.Unix(y.Start, 0)) {
		return nil, errors.New("token is expired")
	}

	a := db.Get(HEADER_SIGNATURE)
	if len(x) != 1 {
		return nil, errors.New("no signature")
	}

	sig, err := sgo.SignatureFromBase58(a[0])
	if err != nil {
		return nil, err
	}
	if !sig.Verify(y.User, data) {
		return nil, errors.New("signature failed to verify")
	}

	return y, nil
}
