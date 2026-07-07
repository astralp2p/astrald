package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

type opMigrateSessionArgs struct {
	SessionID astral.Nonce `query:"required"`
	LinkID    astral.Nonce `query:"required"`
	Start     astral.Bool  `query:"optional"`
	Out       string       `query:"optional"`
}

// OpMigrateSession moves an open session onto another link. With Start set it drives
// the migration directly; otherwise it acts as the responder, exchanging ready/switched/
// resume/done signals with the initiator. Validates session and link state before starting.
func (mod *Module) OpMigrateSession(ctx *astral.Context, q *routing.IncomingQuery, args opMigrateSessionArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	session, ok := mod.getSession(args.SessionID)
	if !ok {
		return ch.Send(astral.Err(nodes.ErrSessionNotFound))
	}
	if !session.IsOpen() {
		return ch.Send(astral.Err(nodes.ErrInvalidSessionState))
	}

	targetLink := mod.getLinkByID(args.LinkID)
	if targetLink == nil {
		return ch.Send(astral.Err(nodes.ErrLinkNotFound))
	}

	link := mod.getSessionLink(session.Nonce)
	if link == nil {
		return ch.Send(astral.Err(nodes.ErrLinkNotFound))
	}

	if link.id == targetLink.id {
		return ch.Send(astral.Err(nodes.ErrSessionAlreadyOnLink))
	}

	if args.Start {
		mod.log.Log("migrate session %v to link %v (manual)", args.SessionID, args.LinkID)
		migrateCtx, cancel := ctx.WithTimeout(migrateSessionTimeout)
		defer cancel()
		err := mod.migrateSession(migrateCtx, session, targetLink)
		if err != nil {
			return ch.Send(astral.Err(err))
		}

		return ch.Send(&astral.Ack{})
	}

	migrator, err := mod.newSessionMigrator(session)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	var peerBuffer astral.Uint32
	err = ch.Switch(nodes.ExpectMigrateSignal(nodes.MigrateSignalReady, &peerBuffer))
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	migrator.SetPeerBuffer(int(peerBuffer))
	err = migrator.Begin(targetLink)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// note: submethod made for sake of defer (im thinking about other solution)
	migrateCtx, cancel := ctx.WithTimeout(migrateSessionTimeout)
	defer cancel()
	err = mod.respondMigration(migrateCtx, ch, session, migrator)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "session %v migrated to link %v (responder)", session.Nonce, targetLink.id)
	return nil
}

func (mod *Module) respondMigration(ctx *astral.Context, ch *channel.Channel, session *session, migrator *SessionMigrator) (err error) {
	defer func() {
		if err != nil {
			session.Close()
		}
	}()

	err = migrator.SendMigrateFrame()
	if err != nil {
		return err
	}

	err = ch.Send(&nodes.MigrateSignal{Signal: nodes.MigrateSignalSwitched, Buffer: astral.Uint32(defaultBufferSize)})
	if err != nil {
		return err
	}

	err = migrator.WaitClosed(ctx)
	if err != nil {
		return err
	}

	err = ch.Switch(nodes.ExpectMigrateSignal(nodes.MigrateSignalResume, nil))
	if err != nil {
		return err
	}

	err = migrator.Complete()
	if err != nil {
		return err
	}

	return ch.Send(&nodes.MigrateSignal{Signal: nodes.MigrateSignalDone})
}
