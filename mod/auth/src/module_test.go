package auth

import (
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// TestAddKeepsConcurrentRegistrations registers handlers for one action type
// from many goroutines at once, as modules do from their concurrent
// LoadDependencies, while readers look the handlers up. Every handler must
// be retained.
func TestAddKeepsConcurrentRegistrations(t *testing.T) {
	const (
		runs    = 50
		writers = 64
		readers = 8
	)
	actionType := testAction{}.ObjectType()

	for run := 0; run < runs; run++ {
		mod := &Module{}
		start := make(chan struct{})
		var wg sync.WaitGroup

		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				mod.Add(authmod.Func[*testAction](func(*astral.Context, *testAction) bool {
					return false
				}))
			}()
		}
		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for _, h := range mod.get(actionType) {
					_ = h
				}
			}()
		}

		close(start)
		wg.Wait()

		if got := len(mod.get(actionType)); got != writers {
			t.Fatalf("run %d: %d of %d handlers registered", run, got, writers)
		}
	}
}
