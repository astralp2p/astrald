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
	key := remote.String()
	if !mod.banAnswers.Contains(key) {
		return nil
	}

	banned := mod.banned.Load()
	if banned == nil {
		return nil
	}
	if _, ok := (*banned)[key]; !ok {
		return nil
	}
	return user.ErrExpelled
}

// refreshBans loads the subjects the active user banned into the set AdmitLink
// reads; with no active contract the set is empty.
// note: a failed read keeps the previous set.
func (mod *Module) refreshBans() {
	set := map[string]struct{}{}
	if ac := mod.ActiveContract(); ac != nil {
		read, err := mod.readExpelledSet(ac.Issuer)
		if err != nil {
			mod.log.Error("loading bans for link admission: %v", err)
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
