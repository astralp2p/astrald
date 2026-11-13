package vfs

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

func TestLockVFSSerializesOneVFS(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()

	var inside, overlaps int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			unlock := mod.lockVFS(id)
			defer unlock()

			mu.Lock()
			inside++
			if inside > 1 {
				overlaps++
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if overlaps != 0 {
		t.Fatalf("%d changes overlapped in one VFS", overlaps)
	}
}

func TestLockVFSDoesNotBlockAnotherVFS(t *testing.T) {
	mod := testModule(t)

	unlock := mod.lockVFS(astral.GenerateIdentity())
	defer unlock()

	got := make(chan struct{})
	go func() {
		defer close(got)
		mod.lockVFS(astral.GenerateIdentity())()
	}()

	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("a lock on one VFS blocked another VFS")
	}
}

func TestLockVFSBlocksUntilReleased(t *testing.T) {
	mod := testModule(t)
	id := astral.GenerateIdentity()

	unlock := mod.lockVFS(id)

	got := make(chan struct{})
	go func() {
		defer close(got)
		mod.lockVFS(id)()
	}()

	select {
	case <-got:
		t.Fatal("the second lock was granted while the first was held")
	case <-time.After(50 * time.Millisecond):
	}

	unlock()

	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the second lock was not granted after the release")
	}
}
