package messaging

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

// Renewal, verified against the real auth module: every contract a pass signs
// is signed with the mailbox identity's key the keyring holds, and indexed only
// when its signatures verify.

// dueParticipant provisions a mailbox whose hosting contract has ten minutes
// left, then sets the module's hosting_duration to an hour: the contract is
// inside the renewal window, and a renewed one is not.
func dueParticipant(t *testing.T, mod *Module) (*astral.Identity, mailbox) {
	t.Helper()

	mod.config.HostingDuration = 10 * time.Minute
	u := hostedParticipant(t, mod)
	mod.config.HostingDuration = time.Hour

	entry, _ := mod.mailboxes.Get(u.String())
	return u, entry
}

// runModule starts the module's Run and answers the function that stops it and
// waits for it to return.
func runModule(t *testing.T, mod *Module) (stop func()) {
	t.Helper()

	ctx, cancel := mod.ctx.WithCancel()
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = mod.Run(ctx)
	}()

	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return once its context ended")
		}
	}
}

// eventually fails the test unless cond holds within five seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%v: not within five seconds", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// indexNames answers whether the identity's index row and its mirror both name
// the contract entry names, with its expiry.
func indexNames(t *testing.T, mod *Module, identity *astral.Identity, entry mailbox) bool {
	t.Helper()

	row, err := mod.db.FindMailbox(identity)
	if err != nil {
		t.Fatalf("index row: %v", err)
	}
	mirror, ok := mod.mailboxes.Get(identity.String())

	return ok && row.ContractID.IsEqual(entry.ContractID) && mirror.ContractID.IsEqual(entry.ContractID) &&
		row.ExpiresAt.Equal(entry.ExpiresAt.UTC()) && mirror.ExpiresAt.Equal(entry.ExpiresAt)
}

// renewedContract answers the one hosting contract the issuer gave this node
// other than old, or fails the test.
func renewedContract(t *testing.T, mod *Module, issuer *astral.Identity, old mailbox) (*auth.SignedContract, mailbox) {
	t.Helper()

	list := hostingContracts(t, mod, issuer)
	if len(list) != 2 {
		t.Fatalf("%v hosting contracts indexed, want the old one and the renewed one", len(list))
	}

	for _, sc := range list {
		id, err := astral.ResolveObjectID(sc)
		if err != nil {
			t.Fatalf("contract id: %v", err)
		}
		if !id.IsEqual(old.ContractID) {
			return sc, mailbox{ContractID: id, ExpiresAt: sc.ExpiresAt.Time()}
		}
	}
	t.Fatal("the old contract is indexed twice")
	return nil, mailbox{}
}

// Run renews a contract inside the window at once, without waiting for a tick:
// a fresh contract on the same terms, issued and signed as the mailbox
// identity, indexed, and the index row and its mirror moved to it. The old
// contract stays indexed, and the mailbox is served throughout.
func TestRunRenewsAContractInsideTheWindow(t *testing.T) {
	mod := testMessagingModule(t)
	u, old := dueParticipant(t, mod)
	mod.config.RenewalInterval = time.Hour
	created, err := mod.db.FindMailbox(u)
	if err != nil {
		t.Fatalf("index row: %v", err)
	}
	before := time.Now()

	stop := runModule(t, mod)
	defer stop()
	eventually(t, "the index moved off the old contract", func() bool { return !indexNames(t, mod, u, old) })

	renewed, entry := renewedContract(t, mod, u, old)
	if !renewed.Issuer.IsEqual(u) {
		t.Fatalf("the renewed contract is issued by %v, want the mailbox identity %v", renewed.Issuer, u)
	}
	if err = authorityOf(mod).VerifyIssuer(renewed); err != nil {
		t.Fatalf("the renewed contract is not signed as the mailbox identity: %v", err)
	}
	checkHostingContract(t, mod, renewed, before)

	if !indexNames(t, mod, u, entry) {
		t.Fatal("the index row and its mirror do not both name the renewed contract")
	}
	row, _ := mod.db.FindMailbox(u)
	if !row.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("the row's created_at moved from %v to %v", created.CreatedAt, row.CreatedAt)
	}
	if !mod.hosts(u) {
		t.Fatal("the renewed mailbox is not hosted")
	}
}

// A contract outside the window is left alone: the pass signs nothing and the
// index does not move. One minute on either side of the edge — a quarter of
// hosting_duration left — tells the window from a pass that renews nothing.
func TestARenewalPassLeavesAContractOutsideTheWindow(t *testing.T) {
	mod := testMessagingModule(t)
	mod.config.HostingDuration = time.Hour
	u := hostedParticipant(t, mod)
	old, _ := mod.mailboxes.Get(u.String())
	edge := old.ExpiresAt.Add(-mod.config.HostingDuration / 4)
	asked := keysOf(mod).signsAsked(u)

	mod.renewMailboxes(mod.ctx, edge.Add(-time.Minute))

	if n := keysOf(mod).signsAsked(u) - asked; n != 0 {
		t.Fatalf("the pass asked %v signatures as the mailbox identity outside the window, want none", n)
	}
	if n := len(hostingContracts(t, mod, u)); n != 1 {
		t.Fatalf("%v hosting contracts indexed outside the window, want the one", n)
	}
	if !indexNames(t, mod, u, old) {
		t.Fatal("the index moved outside the window")
	}

	mod.renewMailboxes(mod.ctx, edge.Add(time.Minute))

	if indexNames(t, mod, u, old) {
		t.Fatal("the pass did not renew a contract a minute inside the window; the check above proves nothing")
	}
}

// Renewal never renews a withdrawn mailbox: not one delete_identity withdrew
// before the pass, not one whose withdrawal dropped the mirror and failed to
// delete the row, and not one withdrawn while its renewal was being signed,
// whether that withdrawal completed or failed to delete the row.
func TestARenewalPassNeverRenewsAWithdrawnMailbox(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	deleted, _ := dueParticipant(t, mod)
	halfway, stale := dueParticipant(t, mod)
	signing, read := dueParticipant(t, mod)

	if err := mod.DeleteIdentity(mod.ctx, deleted); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	mod.mailboxes.Delete(halfway.String())

	mod.renewMailboxes(mod.ctx, time.Now())

	if n := len(hostingContracts(t, mod, deleted)); n != 1 {
		t.Fatalf("%v hosting contracts for the deleted mailbox, want the one it had", n)
	}
	if _, err := mod.db.FindMailbox(deleted); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("the deleted mailbox's row: got %v, want none", err)
	}
	if n := len(hostingContracts(t, mod, halfway)); n != 1 || !stale.ContractID.IsEqual(mustRow(t, mod, halfway).ContractID) {
		t.Fatalf("a mailbox whose withdrawal failed half-way was renewed: %v contracts", n)
	}
	if indexNames(t, mod, signing, read) {
		t.Fatal("the third mailbox was not renewed; the pass proves nothing")
	}

	// the renewal read the index, then the mailbox was withdrawn
	current, _ := mod.mailboxes.Get(signing.String())
	if err := mod.withdrawMailbox(signing); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if err := mod.renewMailbox(mod.ctx, signing, current); err != nil {
		t.Fatalf("renew: %v", err)
	}

	if _, err := mod.db.FindMailbox(signing); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("the withdrawn mailbox's row: got %v, want none", err)
	}
	if _, ok := mod.mailboxes.Get(signing.String()); ok || mod.hosts(signing) {
		t.Fatal("a renewal signed across a withdrawal served the mailbox again")
	}

	// the renewal read the index, then a withdrawal dropped the mirror and
	// failed to delete the row
	halfSigning, halfRead := dueParticipant(t, mod)
	mod.mailboxes.Delete(halfSigning.String())
	if err := mod.renewMailbox(mod.ctx, halfSigning, halfRead); err != nil {
		t.Fatalf("renew: %v", err)
	}

	if !mustRow(t, mod, halfSigning).ContractID.IsEqual(halfRead.ContractID) {
		t.Fatal("a renewal signed across a half-way withdrawal moved the row")
	}
	if _, ok := mod.mailboxes.Get(halfSigning.String()); ok || mod.hosts(halfSigning) {
		t.Fatal("a renewal signed across a half-way withdrawal served the mailbox again")
	}
}

// A renewal moves the index under the lock the index guard and a withdrawal
// take: while a reader holds the index, a renewal signs and indexes its
// contract and then waits, and it moves the index once the reader lets go.
func TestARenewalMovesTheIndexUnderTheIndexLock(t *testing.T) {
	mod := testMessagingModule(t)
	u, old := dueParticipant(t, mod)

	mod.mu.RLock()
	done := make(chan error, 1)
	go func() { done <- mod.renewMailbox(mod.ctx, u, old) }()

	eventually(t, "the renewal indexed its contract", func() bool {
		return len(hostingContracts(t, mod, u)) == 2
	})
	select {
	case err := <-done:
		mod.mu.RUnlock()
		t.Fatalf("the renewal ended while a reader held the index: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	held := indexNames(t, mod, u, old)
	mod.mu.RUnlock()

	if !held {
		t.Fatal("the renewal moved the index while a reader held it")
	}
	if err := <-done; err != nil {
		t.Fatalf("renew: %v", err)
	}
	if indexNames(t, mod, u, old) {
		t.Fatal("the renewal did not move the index once the reader let go")
	}
}

// Run with a renewal_interval of zero or less in messaging.yaml renews at Run
// and keeps running: Load gave the interval its default.
func TestRunWithANonPositiveRenewalIntervalRenewsAtRun(t *testing.T) {
	for _, interval := range []string{"0s", "-1h"} {
		t.Run(interval, func(t *testing.T) {
			mod := loadConfigured(t, "renewal_interval: "+interval)
			keys := newKeyring()
			wireTestModule(t, mod, keys.mint(), keys)
			u, old := dueParticipant(t, mod)

			stop := runModule(t, mod)
			defer stop()
			eventually(t, "the pass at Run renewed the contract", func() bool {
				return !indexNames(t, mod, u, old)
			})
		})
	}
}

// mustRow answers the identity's index row.
func mustRow(t *testing.T, mod *Module, identity *astral.Identity) *dbMailbox {
	t.Helper()

	row, err := mod.db.FindMailbox(identity)
	if err != nil {
		t.Fatalf("index row: %v", err)
	}
	return row
}

// A renewal that fails leaves the old contract serving and is tried again on
// the next tick: with the mailbox identity's key withheld, the pass at Run and
// the ticks after it sign nothing and move nothing, and once the key is back a
// tick renews the contract.
func TestAFailedRenewalKeepsTheOldContractAndRetries(t *testing.T) {
	mod := testMessagingModule(t)
	u, old := dueParticipant(t, mod)
	mod.config.RenewalInterval = 50 * time.Millisecond
	restore := keysOf(mod).withhold(u)
	asked := keysOf(mod).signsAsked(u)

	stop := runModule(t, mod)
	defer stop()
	eventually(t, "the pass at Run and a tick asked a signature", func() bool {
		return keysOf(mod).signsAsked(u)-asked >= 2
	})

	if !indexNames(t, mod, u, old) {
		t.Fatal("a failed renewal moved the index")
	}
	if n := len(hostingContracts(t, mod, u)); n != 1 {
		t.Fatalf("%v hosting contracts after failed renewals, want the old one", n)
	}
	if !mod.hosts(u) {
		t.Fatal("the old contract stopped serving after a failed renewal")
	}

	restore()
	eventually(t, "a tick after the key returned renewed the contract", func() bool {
		return !indexNames(t, mod, u, old)
	})

	_, entry := renewedContract(t, mod, u, old)
	if !indexNames(t, mod, u, entry) || !mod.hosts(u) {
		t.Fatal("the retried renewal did not move the index to the renewed contract")
	}
}
