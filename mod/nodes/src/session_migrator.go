package nodes

import (
	"context"

	"github.com/astralp2p/astrald/mod/nodes"
)

// SessionMigrator moves an open session from one link to another.
// Call Begin, SendMigrateFrame, WaitClosed, then Complete in that order.
type SessionMigrator struct {
	mod            *Module
	session        *session
	reader         *muxSessionReader
	writer         *muxSessionWriter
	peerBuffer     int
	oldLink        *Link
	newLink        *Link
	oldInputBuffer *InputBuffer
}

func (mod *Module) newSessionMigrator(session *session) (*SessionMigrator, error) {
	reader, ok := session.reader.(*muxSessionReader)
	if !ok {
		return nil, nodes.ErrMigrationNotSupported
	}

	writer, ok := session.writer.(*muxSessionWriter)
	if !ok {
		return nil, nodes.ErrMigrationNotSupported
	}

	return &SessionMigrator{mod: mod, session: session, reader: reader, writer: writer}, nil
}

// Begin transitions the session to migrating, pauses the writer, and swaps
// reader/writer buffers onto target. Fails unless the session is open.
func (m *SessionMigrator) Begin(target *Link) error {
	if !m.session.swapState(stateOpen, stateMigrating) {
		m.mod.log.Logv(1, "session %v in state %v, cannot migrate", m.session.Nonce, m.session.getState())
		return nodes.ErrInvalidSessionState
	}
	m.session.cond.L.Lock()
	m.oldLink = m.mod.getSessionLink(m.session.Nonce)
	m.session.cond.L.Unlock()
	m.newLink = target

	m.mod.log.Logv(1, "pausing session %v", m.session.Nonce)
	m.writer.Pause()

	m.oldInputBuffer = m.reader.Buf()

	newMux := m.newLink.GetMux()
	newInputBuffer := NewInputBuffer(defaultBufferSize, newMux.sessionOnReadFunc(m.session.Nonce))
	newOutputBuffer := NewOutputBuffer(newMux.sessionOnWriteFunc(m.session.Nonce))
	resetFunc := newMux.sessionResetFunc(m.session.Nonce)

	m.writer.SwapBuf(newOutputBuffer, resetFunc)
	m.reader.SetNextBuffer(newInputBuffer)
	m.mod.log.Logv(1, "session %v migrating link %v → %v", m.session.Nonce, m.oldLink.id, target.id)

	return nil
}

// SendMigrateFrame tells the peer to migrate, sent on the old link after Begin.
func (m *SessionMigrator) SendMigrateFrame() error {
	m.mod.log.Logv(1, "sending migrate frame for session %v on link %v", m.session.Nonce, m.oldLink.id)
	return m.oldLink.GetMux().SendMigrateFrame(m.session.Nonce)
}

// WaitClosed blocks until the old link drains its input buffer or ctx ends.
func (m *SessionMigrator) WaitClosed(ctx context.Context) error {
	m.mod.log.Logv(1, "waiting for old input buffer to close for session %v", m.session.Nonce)
	select {
	case <-m.oldInputBuffer.Closed():
		m.mod.log.Logv(1, "old input buffer closed for session %v", m.session.Nonce)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *SessionMigrator) SetPeerBuffer(n int) {
	m.peerBuffer = n
}

// Complete reattaches the session to the new link's mux, reopens it, and
// resumes the writer grown to the peer's advertised buffer.
func (m *SessionMigrator) Complete() error {
	m.mod.log.Logv(1, "resuming session %v on link %v (peer buffer %v)", m.session.Nonce, m.newLink.id, m.peerBuffer)

	if m.oldLink != nil && m.newLink != nil && m.oldLink != m.newLink {
		oldMux := m.oldLink.GetMux()
		newMux := m.newLink.GetMux()

		sess, err := oldMux.removeSession(m.session.Nonce)
		if err != nil {
			return err
		}
		if err := newMux.addSession(sess); err != nil {
			return err
		}
	}

	m.session.setState(stateOpen)
	m.writer.Grow(m.peerBuffer)
	m.writer.Resume()
	return nil
}
