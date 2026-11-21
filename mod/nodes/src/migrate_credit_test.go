package nodes

import (
	"testing"

	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// credit returns the flow-control credit the writer's current buffer holds.
func credit(w *muxSessionWriter) int {
	buf := w.Buf()
	buf.mu.Lock()
	defer buf.mu.Unlock()
	return buf.wsize
}

// TestOldLinkCreditDoesNotGrowMigratingWriter: a Read frame the peer sends on the old
// link for the old buffer, arriving after Begin swapped in the new link's buffer, grants
// it nothing; after Resume the writer holds exactly the peer's advertised buffer, which
// is all the peer's new input buffer takes.
func TestOldLinkCreditDoesNotGrowMigratingWriter(t *testing.T) {
	f := newMigrationFixture(t)
	m, err := f.mod.newSessionMigrator(f.session)
	if err != nil {
		t.Fatalf("newSessionMigrator: %v", err)
	}
	if err := m.Begin(f.targetLink); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	f.oldLink.mux.handleRead(&frames.Read{Nonce: f.session.Nonce, Len: 64 * 1024})
	if got := credit(m.writer); got != 0 {
		t.Fatalf("the new link's buffer holds %v B of credit after an old-link Read; want 0", got)
	}

	if err := m.Attach(); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	m.SetPeerBuffer(defaultBufferSize)
	m.Resume()
	if got := credit(m.writer); got != defaultBufferSize {
		t.Fatalf("the writer holds %v B of credit after Resume; want the peer's buffer, %v B", got, defaultBufferSize)
	}
}
