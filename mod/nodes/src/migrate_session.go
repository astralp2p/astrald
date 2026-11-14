package nodes

import (
	"github.com/astralp2p/astral-go/api/nodes"
	nodesClient "github.com/astralp2p/astral-go/api/nodes/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// migrateSession migrates single session (initiator side).
func (mod *Module) migrateSession(ctx *astral.Context, session *session, targetLink *Link) error {
	responder, err := mod.migrationResponder(session)
	if err != nil {
		return err
	}

	ch, err := nodesClient.New(responder, astrald.Default()).MigrateSession(ctx, nodesClient.MigrateSessionArgs{
		SessionID: session.Nonce,
		LinkID:    targetLink.id,
	})
	if err != nil {
		return err
	}

	defer ch.Close()

	return mod.initiateMigration(ctx, ch, session, targetLink)
}

// migrationResponder returns the node across the session's link, which runs the responder side.
//
// why not session.RemoteIdentity: on the node that serves a relayed query it is the relayed
// caller (Mux.handleRelayQuery), an identity no link leads to.
func (mod *Module) migrationResponder(session *session) (*astral.Identity, error) {
	link := mod.getSessionLink(session.Nonce)
	if link == nil {
		return nil, nodesmod.ErrLinkNotFound
	}
	return link.GetMux().RemoteIdentity(), nil
}

// initiateMigration runs the initiator side of the migration signal exchange over ch.
func (mod *Module) initiateMigration(ctx *astral.Context, ch *channel.Channel, session *session, targetLink *Link) (err error) {
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
	err = ch.Switch(nodes.ExpectMigrateSignal(nodes.MigrateSignalSwitched, &peerBuffer), channel.FailOnEOF())
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

	// why: the responder resumes writing on the new link as soon as it reads Resume;
	// the session must already be on the new link's mux, or its first data frames draw a Reset.
	err = migrator.Attach()
	if err != nil {
		return err
	}

	err = ch.Send(&nodes.MigrateSignal{Signal: nodes.MigrateSignalResume})
	if err != nil {
		return err
	}

	err = ch.Switch(nodes.ExpectMigrateSignal(nodes.MigrateSignalDone, nil), channel.FailOnEOF())
	if err != nil {
		return err
	}

	migrator.Resume()

	mod.log.Logv(1, "session %v migrated to link %v (initiator)", session.Nonce, targetLink.id)
	return nil
}
