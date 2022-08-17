package admin

import (
	cba "github.com/SolmateDev/go-solmate-cba"
	ll "github.com/SolmateDev/go-staker/ds/list"
	pba "github.com/SolmateDev/go-staker/proto/admin"
)

func (in *internal) on_period(list *ll.Generic[cba.Period]) {
	in.list = list
}

func (in *internal) on_slot(slot uint64) {
	in.slot = slot

	// add period?
	tail, present := in.list.Tail()
	if !present {
		return
	}
	if tail.Start <= in.lastAddPeriod {
		return
	}

	if slot <= tail.Start+tail.Length+in.settings.Lookahead {

		pipeline := in.pipeline

		settings := &pba.Settings{
			Withhold:  in.settings.Withhold,
			Lookahead: in.settings.Lookahead,
			Length:    in.settings.Length,
			Rate: &pba.Rate{
				Numerator:   in.settings.Rate.Numerator,
				Denominator: in.settings.Rate.Denominator,
			},
		}

		go pipeline.AddPeriod(
			in.ctx,
			in.admin,
			settings,
			tail.Start+tail.Length,
		)
	}
}
