package router

import (
	"context"
	"errors"
	"time"

	rg "github.com/SolmateDev/go-staker/ds/ring"
	sgo "github.com/SolmateDev/solana-go"
	bin "github.com/gagliardetto/binary"
	log "github.com/sirupsen/logrus"
)

type User struct {
	id        sgo.PublicKey
	internalC chan<- func(*userInternal)
}

func (e1 Router) AddUser(id sgo.PublicKey, ringBufferSize uint64) (User, error) {

	internalC := make(chan func(*userInternal), 10)

	u1 := User{internalC: internalC, id: id}

	errorC := make(chan error, 1)
	e1.internalC <- func(in *internal) {
		errorC <- in.add_user(u1, internalC, id, ringBufferSize)
	}

	return u1, <-errorC
}

func (in *internal) add_user(u1 User, internalC <-chan func(*userInternal), id sgo.PublicKey, size uint64) error {
	_, present := in.users[id.String()]
	if present {
		return errors.New("user already exists")
	}
	errorC := make(chan error, 1)
	go loopUser(in.ctx, errorC, internalC, id, size)

	err := <-errorC
	if err != nil {
		return err
	}

	in.users[id.String()] = u1

	return nil
}

type userInternal struct {
	id         sgo.PublicKey
	ctx        context.Context
	errorC     chan<- error
	ring       *rg.Ring[*txEntry]
	proportion uint16
	bandwidth  uint16

	closeSignalCList   []chan<- error
	deleteRingElementC chan<- uint
}

func loopUser(ctxOutside context.Context, startErrorC chan<- error, internalC <-chan func(*userInternal), id sgo.PublicKey, size uint64) {
	var err error
	ctx, cancel := context.WithCancel(ctxOutside)
	doneC := ctx.Done()
	defer cancel()

	errorC := make(chan error, 1)
	deleteRingElementC := make(chan uint, 1)

	ui := new(userInternal)
	ui.proportion = 0
	ui.ring, err = rg.Create[*txEntry](size)
	if err != nil {
		startErrorC <- err
		return
	}
	ui.errorC = errorC
	ui.ctx = ctx
	ui.closeSignalCList = make([]chan<- error, 0)
	ui.bandwidth = 0

	startErrorC <- nil
out:
	for {
		select {
		case <-doneC:
			break out
		case err = <-errorC:
			break out
		case req := <-internalC:
			req(ui)
		case id := <-deleteRingElementC:
			v, err := ui.ring.GetById(id)
			if err == nil {
				v.statusC <- TX_STATUS_CANCELED
				v.errorC <- nil
				v.isDone = true
			} else {
				err = nil
			}
		}
	}

	ui.finish(err)
}

func (u1 User) Id() sgo.PublicKey {
	return u1.id
}

func (in *userInternal) finish(err error) {
	log.Debug(err)

	for i := 0; i < len(in.closeSignalCList); i++ {
		in.closeSignalCList[i] <- err
	}
}

type TxStatus = uint8
type TxResult = error

const (
	TX_STATUS_PENDING  = 0
	TX_STATUS_SENT     = 1
	TX_STATUS_RESULT   = 2
	TX_STATUS_CANCELED = 3
)

type txEntry struct {
	data    []byte
	tx      sgo.Transaction
	start   time.Time
	cancelC <-chan struct{}
	statusC chan<- TxStatus
	errorC  chan<- TxResult
	isDone  bool
}

type TxEntryChannel struct {
	cancelC chan<- struct{}
	StatusC <-chan TxStatus
	ErrorC  <-chan TxResult
}

type cancelEntry struct {
	i uint
}

func (tec TxEntryChannel) Cancel() {
	tec.cancelC <- struct{}{}
}

func newTxEntry(data []byte, tx sgo.Transaction) (*txEntry, TxEntryChannel) {
	cancelC := make(chan struct{}, 1)
	statusC := make(chan TxStatus, 3)
	errorC := make(chan error, 1)
	in := &txEntry{
		data:    data,
		tx:      tx,
		start:   time.Now(),
		cancelC: cancelC,
		statusC: statusC,
		errorC:  errorC,
		isDone:  false,
	}
	out := TxEntryChannel{
		cancelC: cancelC,
		StatusC: statusC,
		ErrorC:  errorC,
	}
	return in, out
}

type UserBandwidth = uint16 // x / 1000 * validator bandwidth

func (u1 User) UpdateBandwidth(bandwidth uint16) {
	u1.internalC <- func(ui *userInternal) {
		ui.bandwidth = bandwidth
	}
}

func (u1 User) Bandwidth() uint16 {
	ansC := make(chan uint16, 1)
	u1.internalC <- func(ui *userInternal) {
		ansC <- ui.bandwidth
	}
	return <-ansC
}

func (u1 User) SendTx(txdata []byte, timeout time.Duration) (TxEntryChannel, error) {
	tx, err := sgo.TransactionFromDecoder(bin.NewBinDecoder(txdata))
	if err != nil {
		return TxEntryChannel{}, err
	}

	if !tx.IsSigner(u1.id) {
		return TxEntryChannel{}, errors.New("user not signer")
	}

	in, out := newTxEntry(txdata, *tx)
	errorC := make(chan error, 1)
	u1.internalC <- func(ui *userInternal) {
		errorC <- ui.send_tx(in, timeout)
	}

	return out, <-errorC
}

func (ui *userInternal) send_tx(entry *txEntry, timeout time.Duration) error {
	id, err := ui.ring.Append(entry)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ui.ctx)

	go loopSendTxCancel(ctx, cancel, entry.cancelC, ui.deleteRingElementC, id, timeout)

	return nil
}

func loopSendTxCancel(ctx context.Context, cancel context.CancelFunc, cancelC <-chan struct{}, deleteRingElementC chan<- uint, id uint, timeout time.Duration) {
	doneC := ctx.Done()
	defer cancel()
	select {
	case <-cancelC:
	case <-doneC:
	case <-time.After(timeout):
	}
	deleteRingElementC <- id
}

type popResult struct {
	list [][]byte
	err  error
}

func (u1 User) PopTx(num uint) ([][]byte, error) {
	if num == 0 {
		return nil, errors.New("num cannot be zero")
	}
	ansC := make(chan popResult, 1)
	u1.internalC <- func(ui *userInternal) {
		l := num
		if ui.ring.Length() < num {
			l = ui.ring.Length()
		}
		txList := make([][]byte, l)
		for i := uint(0); i < l; i++ {
			v, err2 := ui.ring.Pop()
			if err2 != nil {
				ansC <- popResult{err: nil}
				return
			}
			txList[i] = make([]byte, len(v.data))
			copy(txList[i], v.data)
		}
		ansC <- popResult{list: txList, err: nil}
	}
	p := <-ansC
	return p.list, p.err
}
