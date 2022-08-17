package util

import sgo "github.com/SolmateDev/solana-go"

func Zero() sgo.PublicKey {
	x := make([]byte, sgo.PublicKeyLength)
	for i := 0; i < len(x); i++ {
		x[i] = 0
	}
	return sgo.PublicKeyFromBytes(x)
}
