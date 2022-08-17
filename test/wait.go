package test

import "sync"

func AddWait(wg *sync.WaitGroup, errorC <-chan error) {
	wg.Add(1)
	go loopWait(wg, errorC)
}
func loopWait(wg *sync.WaitGroup, errorC <-chan error) {
	<-errorC
	wg.Done()
}
