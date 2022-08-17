package util

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"

	sgo "github.com/SolmateDev/solana-go"

	"google.golang.org/grpc/metadata"
)

// the user adds authentication meta data to the request context
func Authenticate(user sgo.PrivateKey, length time.Duration) (*Authentication, error) {
	s := new(Secret)
	s.User = user
	a, err := s.Sign(length)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// the user creates this to create the authenticated contexts
type Secret struct {
	User sgo.PrivateKey
}

func (s *Secret) Sign(length time.Duration) (*Authentication, error) {
	var err error
	a := new(Authentication)
	a.User = s.User.PublicKey()
	a.Start = time.Now()
	if length == 0 {
		a.Length = DEFAULT_LENGTH
	} else {
		a.Length = length
	}
	payload, err := a.payload()
	if err != nil {
		return nil, err
	}
	a.Signature, err = s.User.Sign(payload)
	if err != nil {
		return nil, err
	}
	err = a.validate()
	if err != nil {
		return nil, err
	}
	return a, nil
}

// the server parses this from context
type Authentication struct {
	User      sgo.PublicKey
	Start     time.Time
	Length    time.Duration
	Signature sgo.Signature
}

const (
	HEADER_USER      string = "USER"
	HEADER_START     string = "START"
	HEADER_LENGTH    string = "LENGTH"
	HEADER_SIGNATURE string = "SIGNATURE"
)

const SIGNATURE_PREFIX = "notx"
const DEFAULT_LENGTH = 30 * time.Minute

const MAX_LENGTH = 60 * time.Minute
const MIN_LENGTH = 10 * time.Minute

// the staked validator proxy server scans meta data from the incoming request context to get the authentication information
func Parse(ctx context.Context) (*Authentication, error) {
	var err error
	a := new(Authentication)

	md, present := metadata.FromIncomingContext(ctx)
	if !present {
		return nil, errors.New("no authentication information")
	}
	var n int
	var x []string
	x = md.Get(HEADER_USER)
	if len(x) != 1 {
		return nil, errors.New("no user public key")
	}
	a.User, err = sgo.PublicKeyFromBase58(x[0])
	if err != nil {
		return nil, err
	}

	x = md.Get(HEADER_START)
	if len(x) != 1 {
		return nil, errors.New("no start time")
	}
	n, err = strconv.Atoi(x[0])
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, errors.New("bad time integer")
	}
	a.Start = time.Unix(int64(n), 0)

	x = md.Get(HEADER_LENGTH)
	if len(x) != 1 {
		return nil, errors.New("no start time")
	}
	n, err = strconv.Atoi(x[0])
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, errors.New("bad time integer")
	}
	a.Length = time.Duration(n) * time.Second

	x = md.Get(HEADER_SIGNATURE)
	if len(x) != 1 {
		return nil, errors.New("no signature")
	}
	a.Signature, err = sgo.SignatureFromBase58(x[0])
	if err != nil {
		return nil, err
	}

	err = a.validate()
	if err != nil {
		return nil, err
	}

	return a, nil
}

func (a *Authentication) IsExpired() bool {
	if a.Start.Add(a.Length).Before(time.Now()) {
		return true
	} else {
		return false
	}
}

// validate a newly created signature
func (a *Authentication) validate() error {
	now := time.Now()
	if a.Start.After(now.Add(10 * time.Second)) {
		return errors.New("start is too far in the future")
	} else if a.Start.Before(now.Add(-10 * time.Second)) {
		return errors.New("start is too far in the past")
	}

	if a.Length < MIN_LENGTH {
		return errors.New("length is too short")
	} else if MAX_LENGTH < a.Length {
		return errors.New("length is too long")
	}

	payload, err := a.payload()
	if err != nil {
		return err
	}
	if !a.Signature.Verify(a.User, payload) {
		return errors.New("signature failed to verify")
	}

	return nil
}

func copyLocal(dst []byte, src []byte, start int) error {
	if len(dst) < start+len(src) {
		return errors.New("out of bound")
	}
	for i := 0; i < len(src); i++ {
		dst[start+i] = src[i]
	}
	return nil
}

func (a *Authentication) payload() ([]byte, error) {
	var err error
	prefix := []byte(SIGNATURE_PREFIX)
	p := make([]byte, len(prefix)+sgo.PublicKeyLength+8+8)
	i := 0
	err = copyLocal(p, prefix, i)
	if err != nil {
		return nil, err
	}
	i += len(prefix)

	err = copyLocal(p, a.User.Bytes(), i)
	if err != nil {
		return nil, err
	}
	i += sgo.PublicKeyLength

	b_start := a.Start.Unix()
	if b_start < 0 {
		return nil, errors.New("bad start")
	}
	start := uint64(b_start)
	binary.BigEndian.PutUint64(p[i:], start)
	i += 8

	if a.Length < 0 {
		return nil, errors.New("negative length")
	}
	length := uint64(a.Length.Truncate(time.Second).Seconds())
	binary.BigEndian.PutUint64(p[i:], length)
	i += 8

	return p, nil
}
func (a *Authentication) Ctx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, HEADER_USER, a.User.String(), HEADER_START, fmt.Sprintf("%d", a.Start.Unix()), HEADER_LENGTH, fmt.Sprintf("%f", a.Length.Truncate(time.Second).Seconds()))
}
