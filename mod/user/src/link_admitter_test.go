package user

import (
	"errors"
	"fmt"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// orderedNodes records CloseLinks and EnforceLinkPolicy calls on a shared log.
type orderedNodes struct {
	nodesmod.Module
	steps chan string
}

func (n *orderedNodes) CloseLinks(*astral.Identity) error {
	n.steps <- "close"
	return nil
}

func (n *orderedNodes) EnforceLinkPolicy() {
	n.steps <- "enforce"
}

func (n *orderedNodes) IsLinked(*astral.Identity) bool { return true }

// orderedPusher answers each push of subject's own ban with the next of
// results (nil after the last) and records it on the shared log.
type orderedPusher struct {
	objectsmod.Module
	subject *astral.Identity
	steps   chan string
	results []error
}

func (p *orderedPusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	ban, ok := obj.(*user.SignedExpulsion)
	if !ok || !target.IsEqual(p.subject) || !ban.Subject.IsEqual(p.subject) {
		return nil
	}
	p.steps <- "push"
	if len(p.results) == 0 {
		return nil
	}
	err := p.results[0]
	p.results = p.results[1:]
	return err
}

// orderedFixture wires a receiver fixture to an ordered pusher and nodes module.
func orderedFixture(t *testing.T, subject *astral.Identity, results ...error) (*receiverFixture, chan string) {
	t.Helper()
	f := newReceiverFixture(t, astral.GenerateIdentity())
	steps := make(chan string, 2*pushAttempts)
	f.mod.Deps.Nodes = &orderedNodes{steps: steps}
	f.mod.Deps.Objects = &orderedPusher{subject: subject, steps: steps, results: results}
	f.mod.after = func(time.Duration) <-chan time.Time { return time.After(0) }
	return f, steps
}

// requireSteps reads len(want) steps and fails unless they equal want in order,
// or on any further step within quiet.
func requireSteps(t *testing.T, steps chan string, want ...string) {
	t.Helper()
	for i, w := range want {
		select {
		case got := <-steps:
			if got != w {
				t.Fatalf("step %d is %q; want %q (all: %v)", i, got, w, want)
			}
		case <-time.After(effectTimeout):
			t.Fatalf("step %d (%q) did not happen; want %v", i, w, want)
		}
	}
	select {
	case got := <-steps:
		t.Fatalf("unexpected step %q after %v", got, want)
	case <-time.After(quiet):
	}
}

func requireAdmitted(t *testing.T, mod *Module, id *astral.Identity) {
	t.Helper()
	if err := mod.AdmitLink(id); err != nil {
		t.Fatalf("AdmitLink(%v) = %v; want nil", id, err)
	}
}

func requireRefused(t *testing.T, mod *Module, id *astral.Identity) {
	t.Helper()
	if err := mod.AdmitLink(id); !errors.Is(err, user.ErrExpelled) {
		t.Fatalf("AdmitLink(%v) = %v; want ErrExpelled", id, err)
	}
}

// TestAdmitLinkRefusesOnlyBannedSubjectsThatAnswered: AdmitLink admits every
// node until a node the active user banned answers its own ban; then it
// refuses that node alone. An answered node the active user has not banned,
// and every node once the node runs without a user, is admitted.
func TestAdmitLinkRefusesOnlyBannedSubjectsThatAnswered(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	banned, other := f.ban(t), astral.GenerateIdentity()
	f.mod.refreshBans()

	requireAdmitted(t, f.mod, banned)
	requireAdmitted(t, f.mod, other)

	f.mod.onBanAnswered(banned)
	f.mod.onBanAnswered(other)
	requireRefused(t, f.mod, banned)
	requireAdmitted(t, f.mod, other)
	requireAdmitted(t, f.mod, f.nodeID)
	if got := f.nodes.enforced.Load(); got != 2 {
		t.Fatalf("EnforceLinkPolicy ran %d times; want once per answer", got)
	}

	if err := f.mod.config.ActiveContract.Clear(nil); err != nil {
		t.Fatalf("clear active contract: %v", err)
	}
	f.mod.refreshBans()
	requireAdmitted(t, f.mod, banned)
}

// TestExpelClosesTheSubjectsLinksAfterItAnswers: on the issuer, Expel admits
// the subject's links while the push of its ban fails, and enforces the link
// policy only after the subject answered the push; from then on the subject
// is refused.
func TestExpelClosesTheSubjectsLinksAfterItAnswers(t *testing.T) {
	for _, answer := range []error{nil, objectsapi.ErrPushRejected} {
		t.Run(fmt.Sprint(answer), func(t *testing.T) {
			subject := astral.GenerateIdentity()
			f, steps := orderedFixture(t, subject, errUnreachable, answer)
			f.mod.Deps.Crypto = signingCrypto{}

			if _, err := f.mod.Expel(astral.NewContext(nil), subject); err != nil {
				t.Fatalf("Expel: %v", err)
			}
			requireSteps(t, steps, "push", "push", "enforce")
			requireRefused(t, f.mod, subject)
		})
	}
}

// TestExpelKeepsAdmittingASubjectThatNeverAnswers: a subject that no push
// reaches stays admitted, so a later link can still carry its ban.
func TestExpelKeepsAdmittingASubjectThatNeverAnswers(t *testing.T) {
	subject := astral.GenerateIdentity()
	results := make([]error, pushAttempts)
	for i := range results {
		results[i] = errUnreachable
	}
	f, steps := orderedFixture(t, subject, results...)
	f.mod.Deps.Crypto = signingCrypto{}
	if _, err := f.mod.Expel(astral.NewContext(nil), subject); err != nil {
		t.Fatalf("Expel: %v", err)
	}

	want := make([]string, pushAttempts)
	for i := range want {
		want[i] = "push"
	}
	requireSteps(t, steps, want...)
	requireAdmitted(t, f.mod, subject)
}

// TestMemberRefusesTheSubjectAfterItAnswersOnANewLink: a member that receives
// a ban closes its links with the subject once, as before; the subject's next
// link is admitted and carries the subject its ban, and the subject's answer
// closes that link and refuses later ones. A push that does not reach the
// subject leaves it admitted.
func TestMemberRefusesTheSubjectAfterItAnswersOnANewLink(t *testing.T) {
	cases := []struct {
		answer error
		steps  []string
		refuse bool
	}{
		{nil, []string{"push", "enforce"}, true},
		{objectsmod.ErrPushRejected, []string{"push", "enforce"}, true},
		{errUnreachable, []string{"push"}, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.answer), func(t *testing.T) {
			subject := astral.GenerateIdentity()
			f, steps := orderedFixture(t, subject, c.answer)

			drop := &recordingDrop{sender: astral.GenerateIdentity(), object: sampleSignedValid(f.userID, subject)}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject(ban): %v", err)
			}
			requireSteps(t, steps, "close")
			requireAdmitted(t, f.mod, subject)

			if err := f.mod.ReceiveObject(localLinkEvent(f, subject, 1)); err != nil {
				t.Fatalf("ReceiveObject(link event): %v", err)
			}
			requireSteps(t, steps, c.steps...)
			if c.refuse {
				requireRefused(t, f.mod, subject)
			} else {
				requireAdmitted(t, f.mod, subject)
			}
		})
	}
}

// sampleSignedValid is sampleSigned with the signature markerCrypto accepts.
func sampleSignedValid(issuer, subject *astral.Identity) *user.SignedExpulsion {
	signed := sampleSigned(issuer, subject)
	signed.IssuerSig = validSig()
	return signed
}

// BenchmarkAdmitLink: AdmitLink over 1000 answered bans reads memory only; the
// module has no database, so any database access panics.
func BenchmarkAdmitLink(b *testing.B) {
	id := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: id}, log: log.New(id)}
	set := map[string]struct{}{}
	var subjects []*astral.Identity
	for range 1000 {
		s := astral.GenerateIdentity()
		set[s.String()] = struct{}{}
		_ = mod.banAnswers.Add(s.String())
		subjects = append(subjects, s)
	}
	mod.banned.Store(&set)
	stranger := astral.GenerateIdentity()

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := mod.AdmitLink(subjects[i%len(subjects)]); err == nil {
			b.Fatal("admitted a banned subject that answered")
		}
		if err := mod.AdmitLink(stranger); err != nil {
			b.Fatal("refused a node that is not banned")
		}
	}
}
