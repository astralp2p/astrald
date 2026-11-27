package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// AdmitLink refuses a link with a node the active user banned once that node
// answered a push of its own ban from this node; it admits every other node
// (astral-docs protocols/user/README.md, link admission of expelled nodes).
// why the answer: until it, the subject's links carry its ban to it (Expel's
// push, the expulsion sync at a new link and the sweep).
// note: the nodes module calls it on every inbound handshake, so it reads only
// memory.
func (mod *Module) AdmitLink(remote *astral.Identity) error {
	if !mod.banAnswers.Contains(remote.String()) {
		return nil
	}
	if !mod.IsBanned(remote) {
		return nil
	}
	return user.ErrExpelled
}

// IsBanned reports whether id is a node the active user banned: an Expulsion
// the user key signed names it (Expelled). It is the veto the auth module runs
// at every level of a walk, AdmitLink's ban test, OpIntroduce's refusal of a
// banned caller and mod/nodes' test of the links to close, so they agree.
//
// It is false with no active contract, for the user, and for this node. why this
// node: a node expelled from its swarm stores its own ban before leaveSwarm
// clears the contract, and in that window it must not refuse its own
// identity, which every local call is promoted to; if the clear fails the
// window would never close.
//
// It is true for every other identity while SwarmState is not open: the view
// is empty then, so it cannot tell who is banned, and the auth module keeps
// contracts across a restart. why: a failed open would otherwise leave the veto
// off for the node's whole lifetime.
func (mod *Module) IsBanned(id *astral.Identity) bool {
	if mod.ActiveContract() == nil || id.IsEqual(mod.Identity()) || id.IsEqual(mod.node.Identity()) {
		return false
	}
	if mod.openStore() == nil {
		return true
	}
	return mod.Expelled(id)
}

// isBanned is the name deps.go registers as the auth veto.
// note: remove it with the deps.go change that registers mod.IsBanned.
func (mod *Module) isBanned(id *astral.Identity) bool { return mod.IsBanned(id) }

// onBanAnswered records that subject answered the push of its own ban and
// closes this node's links with it.
func (mod *Module) onBanAnswered(subject *astral.Identity) {
	if err := mod.banAnswers.Add(subject.String()); err != nil {
		return
	}
	mod.log.Logv(1, "%v answered its ban, refusing its links", subject)
	mod.Nodes.EnforceLinkPolicy()
}

// isBanAnswer reports whether err, the result of a push, is an answer of the
// target: it accepted the object or refused it.
func isBanAnswer(err error) bool {
	return err == nil || isPushRejected(err)
}
