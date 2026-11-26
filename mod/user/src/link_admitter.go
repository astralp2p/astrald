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
	if !mod.isBanned(remote) {
		return nil
	}
	return user.ErrExpelled
}

// isBanned reports whether id is a subject the active user banned. It is the
// veto the auth module runs at every level of a walk, AdmitLink's ban test and
// OpIntroduce's refusal of a banned caller, so the three agree.
//
// It is false with no active contract, for the user, and for this node. why this
// node: it stores its own ban before leaveSwarm clears the contract, and in
// that window it must not refuse its own identity, which every local call is
// promoted to; if the clear fails the window would never close.
//
// note: it reads the set refreshBans keeps, in memory. A set not loaded yet,
// before Run loads it, is read here from the database. A set that cannot be
// read leaves the identity unbanned, as AdmitLink does, and the next question
// reads again.
func (mod *Module) isBanned(id *astral.Identity) bool {
	ac := mod.ActiveContract()
	if ac == nil || id.IsEqual(mod.Identity()) || id.IsEqual(mod.node.Identity()) {
		return false
	}

	banned := mod.banned.Load()
	if banned == nil {
		if banned = mod.loadBans(mod.Identity()); banned == nil {
			return false
		}
	}
	_, ok := (*banned)[id.String()]
	return ok
}

// loadBans fills the ban set on its first use; nil if it cannot be read.
// why CompareAndSwap: a refreshBans that ran meanwhile stored a set at least as
// new as this read, so this one never replaces it.
func (mod *Module) loadBans(issuer *astral.Identity) *map[string]struct{} {
	read, err := mod.readExpelledSet(issuer)
	if err != nil {
		mod.log.Error("loading bans: %v", err)
		return nil
	}
	mod.banned.CompareAndSwap(nil, &read)
	return mod.banned.Load()
}

// refreshBans loads the subjects the active user banned into the set isBanned
// reads; with no active contract the set is empty.
// note: a failed read keeps the previous set.
func (mod *Module) refreshBans() {
	set := map[string]struct{}{}
	if ac := mod.ActiveContract(); ac != nil {
		read, err := mod.readExpelledSet(mod.Identity())
		if err != nil {
			mod.log.Error("loading bans: %v", err)
			return
		}
		set = read
	}
	mod.banned.Store(&set)
}

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
