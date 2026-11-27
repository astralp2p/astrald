package user

import (
	"github.com/astralp2p/astral-go/api/user"
)

// onSwarmChanged applies what SwarmState gained: its expels, and the swarm's members.
func (mod *Module) onSwarmChanged() {
	mod.receiveSwarmExpels()

	mod.refreshBans()
	mod.publishSwarm()
}

// receiveSwarmExpels stores and applies every Expulsion SwarmState holds that
// this node has not stored yet, as a pushed one is (receiveExpulsion).
//
// why: user.list_expelled, link admission and the ban veto read the stored
// bans, and a node that learns an expel only through SwarmState (one
// introduced or joined after it) would otherwise list none and admit the
// banned node's links.
func (mod *Module) receiveSwarmExpels() {
	ids, err := mod.SwarmEntries(user.EntryExpels)
	if err != nil {
		mod.log.Logv(1, "swarm state: list expels: %v", err)
	}
	for _, id := range ids {
		obj, err := mod.SwarmEntry(id)
		if err != nil {
			continue
		}
		se, ok := obj.(*user.SignedExpulsion)
		if !ok || se.IsNil() || mod.banOn(se.Subject) != nil {
			continue
		}
		// note: a refused one (no active contract yet, another issuer) is
		// read again on the next change
		if err = mod.receiveExpulsion(se); err != nil {
			mod.log.Logv(1, "swarm state: expel %v: %v", id, err)
		}
	}
}
