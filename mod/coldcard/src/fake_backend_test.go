package coldcard

import (
	"fmt"
	"sync"
)

// fakeBackend is a DeviceBackend with no hardware: a test sets the serials it
// lists and the key it reads, and reads back the calls it made.
//
// why: the module reaches a device only through its backend, so the fake is the
// seam the loop and Scan tests observe.
type fakeBackend struct {
	mu      sync.Mutex
	serials []string
	pubKey  string
	sig     string
	listErr error

	lists   int
	lookups []string
	msgs    []string

	// scans carries one value per List call.
	scans chan struct{}
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		pubKey: "02abcdef",
		sig:    "AQID",
		scans:  make(chan struct{}, 64),
	}
}

func (f *fakeBackend) List() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lists++

	// why non-blocking: a scan never waits on the test, which may read fewer
	// signals than the loop emits.
	select {
	case f.scans <- struct{}{}:
	default:
	}

	if f.listErr != nil {
		return nil, f.listErr
	}

	return append([]string(nil), f.serials...), nil
}

func (f *fakeBackend) ReadPubKey(serial string, path string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lookups = append(f.lookups, fmt.Sprintf("%v %v", serial, path))

	return f.pubKey, nil
}

func (f *fakeBackend) SignMessage(serial string, msg string, path string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.msgs = append(f.msgs, fmt.Sprintf("%v %v %v", serial, path, msg))

	return f.sig, nil
}

func (f *fakeBackend) setSerials(serials ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.serials = serials
}

func (f *fakeBackend) setListErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.listErr = err
}

func (f *fakeBackend) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lists
}

func (f *fakeBackend) keyLookups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.lookups...)
}

func (f *fakeBackend) messages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.msgs...)
}
