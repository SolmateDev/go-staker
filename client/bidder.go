package client

import (
	"context"
	"errors"
	"io"
	"time"

	pbj "github.com/SolmateDev/go-staker/proto/job"
	"github.com/SolmateDev/go-staker/util"
	sgo "github.com/SolmateDev/solana-go"
	sgotkn "github.com/SolmateDev/solana-go/programs/token"
	"google.golang.org/grpc"
)

type Client struct {
	auth    *util.Authentication
	conn    *grpc.ClientConn
	user    sgo.PrivateKey
	vault   *sgotkn.Account
	jobWork pbj.WorkClient
}

func Create(ctx context.Context, conn *grpc.ClientConn, bidderKey sgo.PrivateKey, vault *sgotkn.Account) *Client {

	ans := new(Client)
	ans.conn = conn
	ans.user = bidderKey
	ans.vault = vault

	return ans
}

func (c *Client) Close() {
	c.conn.Close()
}

// Addd authentication credentials to the http2 headers
func (c *Client) Ctx(ctx context.Context) (context.Context, error) {
	var err error
	renew := false
	if c.auth == nil {
		renew = true
	} else if c.auth.IsExpired() {
		renew = true
	}

	if renew {
		c.auth, err = util.Authenticate(c.user, 30*time.Minute)
		if err != nil {
			return nil, err
		}
	}

	return c.auth.Ctx(ctx), nil
}

func (c *Client) SendTxNoWait(ctx context.Context, serializedTx []byte) error {
	work := c.jobWork
	job, err := work.SubmitJob(c.auth.Ctx(ctx), &pbj.SubmitRequest{
		Data: serializedTx,
	})
	if err != nil {
		return err
	}

	msg, err := job.Recv()
	if err != nil {
		return err
	}
	if msg.Status == nil {
		return errors.New("blank return")
	}
	if msg.Status.Status == pbj.Status_FAILED {
		return errors.New("transaction failed")
	}
	return nil
}

func (c *Client) WaitUntilFinished(ctx context.Context, txHash sgo.Hash) error {

	triggerC := make(chan struct{}, 1)
	doneC := ctx.Done()
	id := txHash.String()
	var err error
	var job *pbj.Job
out:
	for {
		select {
		case <-doneC:
		case <-triggerC:
			job, err = c.jobWork.GetStatus(ctx, &pbj.StatusRequest{Id: id})
			if err != nil {
				break out
			}
			if job.Status == nil {
				err = errors.New("blank status")
				break out
			} else if job.Status.Status == pbj.Status_FAILED {
				err = errors.New("tx failed")
				break out
			}
		}
	}
	return err
}

func loopCheck(ctx context.Context, txHash sgo.Hash, jobWork pbj.WorkClient, triggerC <-chan struct{}, errorC chan<- error) {
	id := txHash.String()
	doneC := ctx.Done()

	for {
		select {
		case <-doneC:
		case <-triggerC:
			job, err := jobWork.GetStatus(ctx, &pbj.StatusRequest{Id: id})
			if err != nil {
				errorC <- err
				break
			}
			if job.Status == nil {
				errorC <- errors.New("blank status")
				break
			} else if job.Status.Status == pbj.Status_FAILED {
				errorC <- errors.New("tx failed")
				break
			}
		}
	}

}

// send a signed, serialized transaction; this function blocks until the transaction has been processed
func (c *Client) SendTx(ctx context.Context, serializedTx []byte) <-chan error {
	work := c.jobWork

	errorC := make(chan error, 1)

	stream, err := work.SubmitJob(ctx, &pbj.SubmitRequest{
		Data: serializedTx,
	})
	if err != nil {
		errorC <- err
		return errorC
	}

	go streamUntilFinished(stream, errorC)
	return errorC
}

func streamUntilFinished(stream pbj.Work_SubmitJobClient, errorC chan<- error) {
	errorC <- streamInside(stream)
}

func streamInside(stream pbj.Work_SubmitJobClient) error {
	var err error
out:
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			err = nil
			break out
		} else if err != nil {
			break out
		}
		status := msg.Status
		if status == nil {
			return errors.New("bad status")
		}
		switch status.Status {
		case pbj.Status_NEW:
		case pbj.Status_STARTED:
		case pbj.Status_FINISHED:
			err = nil
			break out
		case pbj.Status_FAILED:
			err = errors.New("transaction failed")
			break out
		}
	}
	return err
}
