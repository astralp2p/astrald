package user

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// TestSyncGateFoldsRequestsIntoOneRerun: three requests for one peer while its
// first sync blocks run the sync exactly twice; a request after the gate went
// idle runs it again.
func TestSyncGateFoldsRequestsIntoOneRerun(t *testing.T) {
	var g syncGate
	id := astral.GenerateIdentity()
	var runs atomic.Int32
	release := make(chan struct{})
	ran := make(chan struct{}, 8)
	fn := func() {
		if runs.Add(1) == 1 {
			<-release
		}
		ran <- struct{}{}
	}

	for range 3 {
		g.Run(id, fn)
	}
	close(release)

	for i := range 2 {
		select {
		case <-ran:
		case <-time.After(effectTimeout):
			t.Fatalf("run %v did not happen", i+1)
		}
	}
	select {
	case <-ran:
		t.Fatal("three requests during one run ran the sync three times; want two")
	case <-time.After(absenceWindow):
	}

	g.Run(id, fn)
	select {
	case <-ran:
	case <-time.After(effectTimeout):
		t.Fatal("a request to an idle gate ran no sync")
	}
	if n := runs.Load(); n != 3 {
		t.Fatalf("the sync ran %v times; want 3", n)
	}
}

// TestSyncGateRunsPeersApart: a blocked sync for one peer does not hold up another peer's.
func TestSyncGateRunsPeersApart(t *testing.T) {
	var g syncGate
	block := make(chan struct{})
	defer close(block)
	g.Run(astral.GenerateIdentity(), func() { <-block })

	ran := make(chan struct{})
	g.Run(astral.GenerateIdentity(), func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(effectTimeout):
		t.Fatal("a blocked sync for one peer held up another peer's")
	}
}
