package db

import (
	"time"

	pbj "github.com/SolmateDev/go-staker/proto/job"
	"github.com/SolmateDev/go-staker/util"
)

type WorkDb interface {
	util.Base

	Put(job *pbj.Job) error
	Get(id string) *pbj.Job
	Update(id string, status pbj.Status, time time.Time) error
}
