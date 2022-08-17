package proxy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/SolmateDev/go-staker/db"
	"github.com/SolmateDev/go-staker/limiter"
	pbj "github.com/SolmateDev/go-staker/proto/job"
	"github.com/SolmateDev/solana-go"
	sgo "github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

func HashTransaction(data []byte) sgo.Hash {
	h := sha256.New()
	return sgo.HashFromBytes(h.Sum(data))
}

func calculateTxPopCount(tps float64) uint64 {
	// seconds per slot=2.5
	return uint64(math.RoundToEven(float64(TX_PROCESSING_WINDOW_SIZE) * 2.5 * tps))
}

type processSettings struct {
	ctx            context.Context
	rpc            *sgorpc.Client
	ws             *sgows.Client
	retries        uint
	submissionTime int64
	jobC           chan<- limiter.JobSubmission
	finishC        chan<- struct{}
	db             db.WorkDb
}

const TX_RETRIES = 50

func processTxBatch(ctx context.Context, errorC chan<- error, rpcClient *sgorpc.Client, wsClient *sgows.Client, userTxBuffer limiter.ItemBuffer[limiter.JobSubmission], workDb db.WorkDb) {

	finishC := make(chan struct{}, 1)
	jobC := make(chan limiter.JobSubmission, 100)
	doneC := ctx.Done()
	var err error
	ps := &processSettings{
		ctx:            ctx,
		rpc:            rpcClient,
		ws:             wsClient,
		retries:        TX_RETRIES,
		submissionTime: time.Now().Unix(),
		jobC:           jobC,
		finishC:        finishC,
		db:             workDb,
	}
	ps.iterate(userTxBuffer)
out:
	for {
		select {
		case <-doneC:
			break out
		case <-finishC:
			break out
		case submission := <-jobC:
			submission.JobC <- ps.single(&submission)
		}
	}
	if err != nil {
		errorC <- err
	}
}

// this iterate executes in a separate goroutine (see buffer.Iterate function)
func (ps *processSettings) iterate(userTxBuffer limiter.ItemBuffer[limiter.JobSubmission]) {
	jobC := ps.jobC
	finishC := ps.finishC
	userTxBuffer.Iterate(func(pk solana.PublicKey, su *limiter.SingleUser[limiter.JobSubmission], totalTps float64) {
		list := su.Receiver.Pop(calculateTxPopCount(su.Tps))
		for i := 0; i < len(list); i++ {
			jobC <- list[i]
		}
		finishC <- struct{}{}
	})
}

// do the rpc call sequentially, but use a separate goroutine to wait for the confirmation of the transaction
// the rpc endpoint is rate limited, so we slow ourselves down on purpose
func (ps *processSettings) single(submission *limiter.JobSubmission) *pbj.Job {
	dataCopy := make([]byte, len(submission.Data))
	copy(dataCopy, submission.Data)
	ans := &pbj.Job{

		Data: dataCopy,
		Status: &pbj.StatusWithTime{
			Time: ps.submissionTime,
		},
	}
	retries := ps.retries
	sig, err := ps.rpc.SendRawTransactionWithOpts(ps.ctx, submission.Data, sgorpc.TransactionOpts{
		SkipPreflight: true,
		MaxRetries:    &retries,
	})
	ans.Id = HashTransaction(submission.Data).String()

	workDb := ps.db

	if err == nil {
		ans.Status.Status = pbj.Status_NEW
	} else {
		ans.Status.Status = pbj.Status_FAILED
	}

	go workDb.Put(ans)

	if err == nil {
		sub, err := ps.ws.SignatureSubscribe(sig, sgorpc.CommitmentFinalized)
		if err != nil {
			ans.Status.Status = pbj.Status_FAILED
			return ans
		}
		closeC := sub.CloseSignal()
		streamC := sub.RecvStream()

		go loopWaitForTxConfirmation(ps.ctx, closeC, streamC, submission.CancelC, submission.JobC, ans.Id, workDb)
	}

	return ans
}

func loopWaitForTxConfirmation(ctx context.Context, closeErrorC <-chan error, streamC <-chan sgows.Result, cancelC <-chan struct{}, jobC chan<- *pbj.Job, id string, workDb db.WorkDb) {
	doneC := ctx.Done()

	ans := &pbj.Job{
		Id:     id,
		Data:   []byte{},
		Status: &pbj.StatusWithTime{},
	}

	var err error
	sendReplyToJobC := false
	select {
	case <-doneC:
	case <-cancelC:
	case err = <-closeErrorC:
	case d := <-streamC:
		sendReplyToJobC = true
		x, ok := d.(*sgows.SignatureResult)
		if ok {
			if x.Value.Err == nil {
				err = nil
			} else {
				err = fmt.Errorf("%+v", x.Value.Err)
			}
		} else {
			err = errors.New("bad signature result")
		}
	}
	ans.Status.Time = time.Now().Unix()
	if err != nil {
		ans.Status.Status = pbj.Status_FAILED
	} else {
		ans.Status.Status = pbj.Status_FINISHED
	}

	if sendReplyToJobC {
		jobC <- ans
	}

	workDb.Update(ans.Id, ans.Status.Status, time.Unix(ans.Status.Time, 0))
}
