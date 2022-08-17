package limiter

import (
	"context"
	"math"

	pbj "github.com/SolmateDev/go-staker/proto/job"
	sgo "github.com/SolmateDev/solana-go"
	log "github.com/sirupsen/logrus"
)

type JobProcessStatus struct {
	Job *pbj.Job
}

func CreateJobProcessor(ctx context.Context) ItemBuffer[JobProcessStatus] {
	return CreateBufferByUser[JobProcessStatus](ctx)
}

type JobSubmission struct {
	Data    []byte
	JobC    chan<- *pbj.Job
	CancelC <-chan struct{}
	User    sgo.PublicKey
}

func CreateBidderTransactionBuffer(ctx context.Context) ItemBuffer[JobSubmission] {
	return CreateBufferByUser[JobSubmission](ctx)
}

// create map from PublicKey to Receiver
type ItemBuffer[T any] struct {
	internalC chan<- func(*internalBuffer[T])
}

func CreateBufferByUser[T any](ctx context.Context) ItemBuffer[T] {
	internalC := make(chan func(*internalBuffer[T]), 10)

	go loopInternalUserBuffer(ctx, internalC)

	return ItemBuffer[T]{
		internalC: internalC,
	}
}

type internalBuffer[T any] struct {
	ctx              context.Context
	errorC           chan<- error
	closeSignalCList []chan<- error
	validatorTps     float64
	users            map[string]*SingleUser[T]
}

type SingleUser[T any] struct {
	Tps                 float64 // float64 comes from the bandwidth allocation * latest-validator-tps
	BandwidthAllocation float64 // float64 comes from the cba.BidList update; equals float64(bidList.BandwidthAllocation / 1000)
	Receiver            Receiver[T]
}

// the capacity of the tx buffer for 1 user is the user's TPS * 150 seconds (ie 60s worth of transactions from a user)
// 150s is about the length of time that a Blockhash lasts before the transaction signature becomes invalid
const USER_RECEIVER_CAPACITY_MULTIPLER = float64(150)

func (su *SingleUser[T]) UpdateTps(tps float64) {
	su.Tps = su.BandwidthAllocation * tps
	// make sure we do not block here
	go su.Receiver.UpdateCapacity(uint64(math.RoundToEven(USER_RECEIVER_CAPACITY_MULTIPLER * su.Tps)))
}

func (su *SingleUser[T]) UpdateBandwidthAllocation(allocation float64, tps float64) {
	su.BandwidthAllocation = allocation
	su.Tps = allocation * tps
	go su.Receiver.UpdateCapacity(uint64(math.RoundToEven(USER_RECEIVER_CAPACITY_MULTIPLER * su.Tps)))
}

func loopInternalUserBuffer[T any](ctx context.Context, internalC <-chan func(*internalBuffer[T])) {

	var err error

	errorC := make(chan error, 1)
	doneC := ctx.Done()

	in := new(internalBuffer[T])
	in.ctx = ctx
	in.errorC = errorC
	in.closeSignalCList = make([]chan<- error, 0)
	in.users = make(map[string]*SingleUser[T])

out:
	for {
		select {
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(in)
		}
	}

	in.finish(err)
}

func (e1 ItemBuffer[T]) CloseSignal() <-chan error {
	signalC := make(chan error, 1)

	e1.internalC <- func(in *internalBuffer[T]) {
		in.closeSignalCList = append(in.closeSignalCList, signalC)
	}
	return signalC
}

func (in *internalBuffer[T]) finish(err error) {
	log.Debug(err)
	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}

// iterate through all user buffers in the internalBuffer goroutine
func (e1 ItemBuffer[T]) Iterate(cb func(sgo.PublicKey, *SingleUser[T], float64)) {
	e1.internalC <- func(in *internalBuffer[T]) {
		for k, v := range in.users {
			key := sgo.MustPublicKeyFromBase58(k)
			cb(key, v, in.validatorTps)
		}
	}
}

// Get the queue by user id
func (e1 ItemBuffer[T]) Get(userId string) SingleUser[T] {
	ansC := make(chan SingleUser[T], 1)
	e1.internalC <- func(in *internalBuffer[T]) {
		x, present := in.users[userId]
		if !present {
			y := CreateReceiver[T](in.ctx)
			x = &SingleUser[T]{
				Receiver:            y,
				BandwidthAllocation: 0,
			}
			y.UpdateCapacity(0)
			in.users[userId] = x
		}
		ansC <- *x
	}
	return <-ansC
}

// grab the Bandwidth allocation from BidList and update the queue capacities
// make sure to set the float64=bidList.BandwidthAllocation/1000
func (e1 ItemBuffer[T]) UpdateBandwidthAllocation(allocationMap map[string]float64) {
	e1.internalC <- func(in *internalBuffer[T]) {
		in.update_bandwidth_allocation(allocationMap)
	}
}

func (in *internalBuffer[T]) update_bandwidth_allocation(allocationMap map[string]float64) {
	for userId, allocation := range allocationMap {
		su, present := in.users[userId]
		if !present {
			y := CreateReceiver[T](in.ctx)
			su = &SingleUser[T]{
				Receiver:            y,
				BandwidthAllocation: 0,
				Tps:                 0,
			}
			in.users[userId] = su
		}
		su.UpdateBandwidthAllocation(allocation, in.validatorTps)
	}
}
