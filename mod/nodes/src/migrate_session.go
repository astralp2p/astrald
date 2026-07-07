package nodes

import (
	"github.com/cryptopunkscc/astral-go/api/nodes"
	nodesClient "github.com/cryptopunkscc/astral-go/api/nodes/client"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
)

// migrateSession migrates single session (initiator side).
func (mod *Module) migrateSession(ctx *astral.Context, session *session, targetLink *Link) (err error) {
	ch, err := nodesClient.New(session.RemoteIdentity, astrald.Default()).MigrateSession(ctx, nodesClient.MigrateSessionArgs{
		SessionID: session.Nonce,
		LinkID:    targetLink.id,
	})
	if err != nil {
		return err
	}

	defer ch.Close()

	migrator, err := mod.newSessionMigrator(session)
	if err != nil {
		return err
	}

	err = migrator.Begin(targetLink)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			session.Close()
		}
	}()

	err = ch.Send(&nodes.MigrateSignal{Signal: nodes.MigrateSignalReady, Buffer: astral.Uint32(defaultBufferSize)})
	if err != nil {
		return err
	}

	var peerBuffer astral.Uint32
	err = ch.Switch(nodes.ExpectMigrateSignal(nodes.MigrateSignalSwitched, &peerBuffer))
	if err != nil {
		return err
	}

	migrator.SetPeerBuffer(int(peerBuffer))
	err = migrator.SendMigrateFrame()
	if err != nil {
		return err
	}

	err = migrator.WaitClosed(ctx)
	if err != nil {
		return err
	}

	err = ch.Send(&nodes.MigrateSignal{Signal: nodes.MigrateSignalResume})
	if err != nil {
		return err
	}

	err = ch.Switch(nodes.ExpectMigrateSignal(nodes.MigrateSignalDone, nil))
	if err != nil {
		return err
	}

	err = migrator.Complete()
	if err != nil {
		return err
	}

	mod.log.Logv(1, "session %v migrated to link %v (initiator)", session.Nonce, targetLink.id)
	return nil
}
