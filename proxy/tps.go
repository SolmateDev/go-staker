package proxy

import (
	"context"
	"errors"
	"sync"
	"time"

	cba "github.com/SolmateDev/go-solmate-cba"
	"github.com/SolmateDev/go-staker/limiter"
	"github.com/SolmateDev/go-staker/state"
	"github.com/SolmateDev/solana-go"
	sgorpc "github.com/SolmateDev/solana-go/rpc"
	sgows "github.com/SolmateDev/solana-go/rpc/ws"
)

type tpsInfo struct {
	networkTps    uint64
	first         uint64
	last          uint64
	start         time.Time
	finish        time.Time
	txCount       uint64
	totalTxFee    uint64
	txDataSize    uint64
	failedTxCount uint64
}

func loopCalculateTps(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, errorC chan<- error, ansC chan<- tpsInfo, startSlot uint64, currentSlot uint64) {
	ans, err := loopCalculateTps_internal(ctx, rpcClient, wsClient, startSlot, currentSlot)
	if err != nil {
		errorC <- err
	} else {
		ansC <- *ans
	}
}

func loopCalculateTps_internal(ctx context.Context, rpcClient *sgorpc.Client, wsClient *sgows.Client, startSlot uint64, currentSlot uint64) (*tpsInfo, error) {

	r, err := rpcClient.GetBlocks(ctx, startSlot, &currentSlot, sgorpc.CommitmentConfirmed)
	if err != nil {
		return nil, err
	}
	errorC := make(chan error, 1)
	blockC := make(chan blockData, 10)
	wg := &sync.WaitGroup{}
	ctxGet, cancelGet := context.WithTimeout(ctx, 60*time.Second)
	defer cancelGet()
	for i := 0; i < len(r); i++ {
		wg.Add(1)
		go loopGetSingleBlock(ctxGet, wg, rpcClient, r[i], blockC, errorC)
	}
	doneC := ctxGet.Done()
	finishC := make(chan struct{}, 1)
	go func() {
		wg.Wait()
		finishC <- struct{}{}
	}()

	ans := &tpsInfo{
		networkTps:    0,
		first:         startSlot,
		last:          currentSlot,
		txCount:       0,
		totalTxFee:    0,
		txDataSize:    0,
		failedTxCount: 0,
	}
	startTime := time.Now()
	lastTime := time.Unix(0, 0)
	var firstBlock *blockData
out:
	for {
		select {
		case <-finishC:
			break out
		case <-doneC:
			err = errors.New("time out")
		case err = <-errorC:
			break out
		case b := <-blockC:
			ans.failedTxCount += b.failedTxCount
			ans.totalTxFee += b.totalTxFee
			ans.txCount += b.txCount
			ans.txDataSize += b.txDataSize
			if b.blockTime.After(lastTime) {
				lastTime = b.blockTime
			}
			if b.blockTime.Before(startTime) {
				startTime = b.blockTime
				firstBlock = &b
			}
		}
	}
	timePeriod := lastTime.Unix() - startTime.Unix()
	if timePeriod <= 0 {
		return nil, errors.New("cannot have negative time window")
	}
	if firstBlock != nil {
		ans.failedTxCount -= firstBlock.failedTxCount
		ans.totalTxFee -= firstBlock.totalTxFee
		ans.txCount -= firstBlock.txCount
		ans.txDataSize -= firstBlock.txDataSize
	}
	ans.start = startTime
	ans.finish = lastTime
	return ans, nil
}

type blockData struct {
	blockTime     time.Time
	txCount       uint64
	totalTxFee    uint64
	txDataSize    uint64
	failedTxCount uint64
}

func loopGetSingleBlock(ctx context.Context, wg *sync.WaitGroup, rpcClient *sgorpc.Client, slot uint64, blockC chan<- blockData, errorC chan<- error) {
	defer wg.Done()
	doneC := ctx.Done()
	block, err := rpcClient.GetBlock(ctx, slot)
	if err != nil {
		errorC <- err
		return
	}
	data := new(blockData)
	data.txCount = uint64(len(block.Transactions))
	if block.BlockTime == nil {
		errorC <- errors.New("bad block time")
		return
	} else {
		data.blockTime = block.BlockTime.Time()
	}

	data.failedTxCount = 0
	data.totalTxFee = 0
	data.txDataSize = 0
	for i := 0; i < len(block.Transactions); i++ {
		data.totalTxFee += block.Transactions[i].Meta.Fee
		if block.Transactions[i].Meta.Err != nil {
			data.failedTxCount++
		}
		data.txDataSize += uint64(len(block.Transactions[i].Transaction.GetBinary()))
	}

	select {
	case <-doneC:
	case blockC <- *data:
	}

}

// Subscribe and update the validator TPS number and use it to update the process window in loopInternal and the user transaction buffer sizes
func loopUpdateTpsBandwidthAllocation(ctx context.Context, errorC chan<- error, userTxBuffer limiter.ItemBuffer[limiter.JobSubmission], updateValidatorTPSC <-chan float64, validatorPipeline state.Pipeline) {

	sub := validatorPipeline.OnBid()
	defer sub.Unsubscribe()
	doneC := ctx.Done()
	var err error
out:
	for {
		select {
		case err = <-sub.ErrorC:
			break out
		case x := <-sub.StreamC:
			// Sync the new bandwidth allocation -> user.pubkey mapping with what is in userTxBuffer; update the user tx buffer size
			// TODO: do some computer science stuff here to make this syncing more efficient
			presentMap := make(map[string]cba.Bid)
			for i := 0; i < len(x.Book); i++ {
				presentMap[x.Book[i].User.String()] = x.Book[i]
			}
			go userTxBuffer.Iterate(func(userId solana.PublicKey, su *limiter.SingleUser[limiter.JobSubmission], validatorTps float64) {
				y, present := presentMap[userId.String()]
				if present {
					su.UpdateBandwidthAllocation(float64(y.BandwidthAllocation)/1000, validatorTps)
				} else {
					su.UpdateBandwidthAllocation(0, validatorTps)
				}
			})
		case <-doneC:
			break out
		case tps := <-updateValidatorTPSC:
			go userTxBuffer.Iterate(func(userId solana.PublicKey, su *limiter.SingleUser[limiter.JobSubmission], validatorTps float64) {
				su.UpdateTps(tps)
			})
		}
	}
	errorC <- err
}
