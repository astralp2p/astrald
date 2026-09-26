package messaging

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// renewHosting runs the renewal pass once, then every Config.RenewalInterval
// until ctx ends.
//
// why the node renews: it holds the key of every mailbox identity it minted, so
// it signs a fresh contract as the issuer, as create_identity did.
//
// note: Load gives RenewalInterval a positive value — see
// Config.withHostingDefaults.
func (mod *Module) renewHosting(ctx *astral.Context) {
	mod.renewMailboxes(ctx, time.Now())

	ticker := time.NewTicker(mod.config.RenewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mod.renewMailboxes(ctx, time.Now())
		}
	}
}

// renewMailboxes renews every mailbox the index names whose hosting contract is
// inside the renewal window at now. A failed renewal is logged, and the next
// pass tries it again. The old contract serves until it lapses.
//
// why the mirror is read and not the table: a withdrawal drops the mirror
// first, so a mailbox withdrawn before the pass, or whose withdrawal failed
// half-way, is not renewed while the node runs.
//
// note: a restart mirrors the row a half-way withdrawal kept, so the mailbox is
// served and renewed again until its deletion is run again.
func (mod *Module) renewMailboxes(ctx *astral.Context, now time.Time) {
	for key, entry := range mod.mailboxes.Clone() {
		if !entry.dueAt(now, mod.config.HostingDuration) {
			continue
		}

		identity, err := astral.ParseIdentity(key)
		if err != nil {
			mod.log.Error("hosting of %v: %v", key, err)
			continue
		}

		if err = mod.renewMailbox(ctx, identity, entry); err != nil {
			mod.log.Error("hosting of %v: renewal failed, the next pass retries it: %v", identity, err)
		}
	}
}

// renewMailbox signs the identity a fresh hosting contract on the terms
// signHosting writes and moves the index to it. A mailbox withdrawn meanwhile
// stays withdrawn.
//
// note: a contract signed for a mailbox withdrawn meanwhile stays indexed with
// auth and serves nothing, like the contract a withdrawal leaves behind.
func (mod *Module) renewMailbox(ctx *astral.Context, identity *astral.Identity, old mailbox) error {
	renewed, err := mod.signHosting(ctx, identity)
	if err != nil {
		return err
	}

	moved, err := mod.moveMailbox(identity, old, renewed)
	if err != nil {
		return err
	}
	if !moved {
		mod.log.Logv(1, "hosting of %v: withdrawn during its renewal", identity)
		return nil
	}

	mod.log.Logv(1, "hosting of %v renewed until %v", identity, renewed.ExpiresAt)

	return nil
}
