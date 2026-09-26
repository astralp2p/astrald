package auth

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// identityNode is an astral.Node that answers Identity only.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// signRule stands in for the crypto module: AuthorizeSigner answers the signer
// rule from listed caller→signer grants, and Sign counts what reaches a key.
// Every other crypto method panics. The rule itself is tested in
// mod/crypto/src.
type signRule struct {
	cryptomod.Module
	node   *astral.Identity
	grants map[[2]string]bool
	signed atomic.Uint64
}

func (r *signRule) AuthorizeSigner(_ *astral.Context, caller *astral.Identity, key *crypto.PublicKey) error {
	signer := secp256k1.Identity(key)
	switch {
	case signer.IsEqual(caller) && signer.IsEqual(r.node):
		return cryptomod.ErrNodeKeyNotSignable
	case signer.IsEqual(caller), r.grants[[2]string{caller.String(), signer.String()}]:
		return nil
	}
	return cryptomod.ErrForeignKey
}

func (r *signRule) Sign(*astral.Context, *crypto.PublicKey, crypto.SignableTextObject) (*crypto.Signature, error) {
	r.signed.Add(1)
	return dummySig(), nil
}

// claimStub is the user module's claim state.
type claimStub struct {
	ready chan struct{}
	user  *astral.Identity
}

func (s *claimStub) Ready() <-chan struct{}     { return s.ready }
func (s *claimStub) Identity() *astral.Identity { return s.user }

func readyClaim(user *astral.Identity) *claimStub {
	s := &claimStub{ready: make(chan struct{}), user: user}
	close(s.ready)
	return s
}

// guardRig is an unclaimed node holding sudo grants from holder to userKey
// and to other.
type guardRig struct {
	mod                               *Module
	crypto                            *signRule
	ctx                               *astral.Context
	node, userKey, app, holder, other *astral.Identity
}

func newGuardRig(t *testing.T) *guardRig {
	t.Helper()
	r := &guardRig{
		ctx:     astral.NewContext(nil),
		node:    astral.GenerateIdentity(),
		userKey: astral.GenerateIdentity(),
		app:     astral.GenerateIdentity(),
		holder:  astral.GenerateIdentity(),
		other:   astral.GenerateIdentity(),
	}
	r.crypto = &signRule{node: r.node, grants: map[[2]string]bool{
		{r.holder.String(), r.userKey.String()}: true,
		{r.holder.String(), r.other.String()}:   true,
	}}
	r.mod = testModule(t)
	r.mod.node = &identityNode{id: r.node}
	r.mod.Crypto = r.crypto
	r.mod.User = readyClaim(nil)
	return r
}

func contractOf(issuer, subject *astral.Identity, actions ...astral.Object) *auth.Contract {
	c := &auth.Contract{
		Issuer:    issuer,
		Subject:   subject,
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
	for _, a := range actions {
		c.Permits = append(c.Permits, &auth.Permit{Action: astral.String8(a.ObjectType())})
	}
	return c
}

func sudoOf(issuer, subject *astral.Identity) *auth.Contract {
	return contractOf(issuer, subject, &authmod.SudoAction{})
}

func claimOf(issuer, subject *astral.Identity) *auth.Contract {
	c, _ := user.NewNodeContract(issuer, subject, true, usermod.DefaultContractValidity)
	return c
}

// TestAuthorizeContractParties: outside the claim exception, the caller signs
// only as parties the signer rule lets it sign as, both of them, and a refusal
// names the party it refused.
func TestAuthorizeContractParties(t *testing.T) {
	r := newGuardRig(t)
	agent := astral.GenerateIdentity()

	for _, tc := range []struct {
		name   string
		caller *astral.Identity
		c      *auth.Contract
		want   error
		party  string
	}{
		{"self on both sides", r.app, contractOf(r.app, r.app, &auth.SeeObjectsAction{}), nil, ""},
		{"sudo on both sides", r.holder, sudoOf(r.userKey, r.other), nil, ""},
		{"sudo to the issuer only", r.holder, sudoOf(r.userKey, r.app), cryptomod.ErrForeignKey, "subject"},
		{"sudo to the subject only", r.holder, sudoOf(r.app, r.other), cryptomod.ErrForeignKey, "issuer"},
		{"an app forges user→app sudo", r.app, sudoOf(r.userKey, r.app), cryptomod.ErrForeignKey, "issuer"},
		{"an app forges node→app sudo", r.app, sudoOf(r.node, r.app), cryptomod.ErrForeignKey, "issuer"},
		{"an app names the user as subject", r.app, sudoOf(r.app, r.userKey), cryptomod.ErrForeignKey, "subject"},
		{"an app forges an agent's relay contract", r.app, contractOf(agent, r.node, &nodes.RelayForAction{}), cryptomod.ErrForeignKey, "issuer"},
		{"the node forges user→app sudo", r.node, sudoOf(r.userKey, r.app), cryptomod.ErrForeignKey, "issuer"},
		{"the node signs as itself", r.node, sudoOf(r.node, r.app), cryptomod.ErrNodeKeyNotSignable, "issuer"},
		{"a zero issuer", r.app, sudoOf(nil, r.app), auth.ErrInvalidContract, ""},
		{"a zero subject", r.app, sudoOf(r.app, &astral.Identity{}), auth.ErrInvalidContract, ""},
		{"a nil contract", r.app, nil, auth.ErrInvalidContract, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := r.mod.authorizeContract(r.ctx, tc.caller, tc.c)
			if !errors.Is(err, tc.want) {
				t.Fatalf("authorizeContract = %v; want %v", err, tc.want)
			}
			if tc.party == "" {
				return
			}
			// why: setup clients read "sign as issuer:" as a missing key, so a
			// refusal must not read as one.
			if !strings.HasPrefix(err.Error(), "authorize "+tc.party+": ") || strings.Contains(err.Error(), "sign as") {
				t.Fatalf("refusal %q does not name the %v as a refusal", err, tc.party)
			}
		})
	}
}

// TestAuthorizeContractNodeClaimState: the node's own session obtains the
// claim contract while the node is unclaimed, and not otherwise.
func TestAuthorizeContractNodeClaimState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, r *guardRig) (caller *astral.Identity)
		ok    bool
	}{
		{"the claim", func(t *testing.T, r *guardRig) *astral.Identity { return r.node }, true},
		{"asked by an app", func(t *testing.T, r *guardRig) *astral.Identity { return r.app }, false},
		{"on a claimed node", func(t *testing.T, r *guardRig) *astral.Identity {
			r.mod.User = readyClaim(r.other)
			return r.node
		}, false},
		{"without the user module", func(t *testing.T, r *guardRig) *astral.Identity {
			r.mod.User = nil
			return r.node
		}, false},
		{"before the user module is ready", func(t *testing.T, r *guardRig) *astral.Identity {
			r.mod.User = &claimStub{ready: make(chan struct{})}
			var cancel func()
			r.ctx, cancel = r.ctx.WithTimeout(50 * time.Millisecond)
			t.Cleanup(cancel)
			return r.node
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newGuardRig(t)
			caller := tc.setup(t, r)
			err := r.mod.authorizeContract(r.ctx, caller, claimOf(r.userKey, r.node))
			if (err == nil) != tc.ok {
				t.Fatalf("authorizeContract = %v; want ok=%v", err, tc.ok)
			}
		})
	}
}

// TestAuthorizeContractNodeClaimIssuer: on an unclaimed node, the claim
// contract is refused for an issuer that ever issued the node a relay contract
// or a node contract, expired ones included, and only for such an issuer.
func TestAuthorizeContractNodeClaimIssuer(t *testing.T) {
	relay := &auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType())}
	see := &auth.Permit{Action: astral.String8(auth.SeeObjectsAction{}.ObjectType())}

	for _, tc := range []struct {
		name string
		seed func(t *testing.T, r *guardRig)
		ok   bool
	}{
		{"issued by an app identity of this node", func(t *testing.T, r *guardRig) { seedContract(t, r.mod, r.userKey, r.node, relay) }, false},
		{"issued by an app identity whose relay contract expired", func(t *testing.T, r *guardRig) {
			seedExpired(t, r.mod, contractOf(r.userKey, r.node, &nodes.RelayForAction{}))
		}, false},
		{"issued by the user after the claim is cleared from the tree", func(t *testing.T, r *guardRig) {
			seedContract(t, r.mod, r.userKey, r.node, claimOf(r.userKey, r.node).Permits...)
		}, false},
		{"issued by a former user", func(t *testing.T, r *guardRig) { seedExpired(t, r.mod, claimOf(r.userKey, r.node)) }, false},
		{"issued by a former plain member's user", func(t *testing.T, r *guardRig) {
			c := claimOf(r.userKey, r.node)
			c.Permits = c.Permits[:1]
			seedExpired(t, r.mod, c)
		}, false},
		{"when the contract lookup fails", func(t *testing.T, r *guardRig) { closeDB(t, r.mod) }, false},
		{"issued by an identity holding another contract to the node", func(t *testing.T, r *guardRig) { seedContract(t, r.mod, r.userKey, r.node, see) }, true},
		{"issued by the user of another node", func(t *testing.T, r *guardRig) {
			seedContract(t, r.mod, r.userKey, r.other, claimOf(r.userKey, r.other).Permits...)
		}, true},
		{"issued by a stranger to a node with another former user", func(t *testing.T, r *guardRig) {
			seedExpired(t, r.mod, claimOf(r.other, r.node))
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newGuardRig(t)
			tc.seed(t, r)
			err := r.mod.authorizeContract(r.ctx, r.node, claimOf(r.userKey, r.node))
			if (err == nil) != tc.ok {
				t.Fatalf("authorizeContract = %v; want ok=%v", err, tc.ok)
			}
		})
	}
}

// TestAuthorizeContractNodeClaimExpiry: the claim contract passes while it
// expires past the shortest validity user.accept_membership accepts and no
// later than the default validity of user.new_node_contract, with the slack.
func TestAuthorizeContractNodeClaimExpiry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid time.Duration
		ok    bool
	}{
		{"expired", -time.Hour, false},
		{"within the shortest validity", usermod.MinimalContractLength / 2, false},
		{"just past the shortest validity", usermod.MinimalContractLength + time.Minute, true},
		{"the default validity", usermod.DefaultContractValidity, true},
		{"within the slack", usermod.DefaultContractValidity + claimExpirySlack/2, true},
		{"beyond the default validity", usermod.DefaultContractValidity + 2*claimExpirySlack, false},
		{"a century", 100 * usermod.DefaultContractValidity, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newGuardRig(t)
			c, _ := user.NewNodeContract(r.userKey, r.node, true, tc.valid)
			err := r.mod.authorizeContract(r.ctx, r.node, c)
			if (err == nil) != tc.ok {
				t.Fatalf("authorizeContract = %v; want ok=%v", err, tc.ok)
			}
		})
	}
}

// TestAuthorizeContractNodeClaimShape: on an unclaimed node, the node's own
// session is refused every contract that differs from the claim contract in
// its parties or its permits. Each clause of isNodeClaim and
// isManagementNodeContract fails at least one row of this test or the one
// above when it is removed.
func TestAuthorizeContractNodeClaimShape(t *testing.T) {
	sudo := astral.String8(authmod.SudoAction{}.ObjectType())

	for _, tc := range []struct {
		name string
		edit func(r *guardRig, c *auth.Contract)
	}{
		{"naming another subject", func(r *guardRig, c *auth.Contract) { c.Subject = r.app }},
		{"issued by the node", func(r *guardRig, c *auth.Contract) { c.Issuer = r.node }},
		{"carrying a sudo permit", func(_ *guardRig, c *auth.Contract) { c.Permits = append(c.Permits, &auth.Permit{Action: sudo}) }},
		{"a plain member contract", func(_ *guardRig, c *auth.Contract) { c.Permits = c.Permits[:1] }},
		{"another action in place of a permit", func(_ *guardRig, c *auth.Contract) { c.Permits[2].Action = sudo }},
		{"a wider delegation", func(_ *guardRig, c *auth.Contract) { c.Permits[1].Delegation = 2 }},
		{"a constrained permit", func(_ *guardRig, c *auth.Contract) { c.Permits[0].Constraints = constrainedPermit(0).Constraints }},
		{"a nil permit", func(_ *guardRig, c *auth.Contract) { c.Permits[2] = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newGuardRig(t)
			c := claimOf(r.userKey, r.node)
			tc.edit(r, c)
			if err := r.mod.authorizeContract(r.ctx, r.node, c); err == nil {
				t.Fatal("authorizeContract admitted a near miss of the claim contract")
			}
		})
	}
}

// closeDB closes the index database, so every contract lookup fails.
func closeDB(t *testing.T, mod *Module) {
	t.Helper()
	sqlDB, err := mod.db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	_ = sqlDB.Close()
}

// seedExpired stores c signed, with its expiry moved to an hour ago.
func seedExpired(t *testing.T, mod *Module, c *auth.Contract) {
	t.Helper()
	c.ExpiresAt = astral.Time(time.Now().Add(-time.Hour))
	sc := &auth.SignedContract{Contract: c, IssuerSig: dummySig(), SubjectSig: dummySig()}
	if err := mod.db.storeSignedContract(sc); err != nil {
		t.Fatalf("store contract: %v", err)
	}
}

// routeSignContract routes an auth.sign_contract query from caller with the
// given origin (nil sets none) and returns the op's input and output ends.
func routeSignContract(t *testing.T, mod *Module, caller *astral.Identity, origin any) (io.WriteCloser, io.ReadCloser, error) {
	t.Helper()
	op, err := routing.NewOp(mod.OpSignContract)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	q := astral.Launch(query.New(caller, mod.node.Identity(), "auth.sign_contract", nil))
	if origin != nil {
		q.Extra.Set("origin", origin)
	}
	out, outWriter := io.Pipe()
	in, err := op.RouteQuery(astral.NewContext(nil), q, outWriter)
	return in, out, err
}

// TestSignContractRefusesNonLocalOrigins: a peer's or an MCP client's query,
// or one of an origin the op does not know, is rejected before the op reads a
// contract; a local one is accepted.
func TestSignContractRefusesNonLocalOrigins(t *testing.T) {
	for _, tc := range []struct {
		origin   any
		rejected bool
	}{
		{nil, false},
		{"", false},
		{astral.OriginLocal, false},
		{astral.OriginNetwork, true},
		{astral.OriginMCP, true},
		{"somewhere-new", true},
	} {
		r := newGuardRig(t)
		in, out, err := routeSignContract(t, r.mod, r.app, tc.origin)

		var rejected *astral.ErrRejected
		if got := errors.As(err, &rejected); got != tc.rejected {
			t.Fatalf("origin %v: rejected=%v (%v); want %v", tc.origin, got, err, tc.rejected)
		}
		if !tc.rejected && err != nil {
			t.Fatalf("origin %v: route = %v; want accepted", tc.origin, err)
		}
		if in != nil {
			_ = channel.NewSender(in).Send(&astral.EOS{})
			_ = in.Close()
		}
		_ = out.Close()
	}
}

// TestSignContractSignsOnlyAfterAuthorizing: a refused contract reaches no key
// and its refusal reaches the caller; an authorized one is signed as both
// parties.
func TestSignContractSignsOnlyAfterAuthorizing(t *testing.T) {
	r := newGuardRig(t)
	in, out, err := routeSignContract(t, r.mod, r.app, nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	defer in.Close()
	defer out.Close()
	send, receive := channel.NewSender(in), channel.NewReceiver(out)

	if err = send.Send(sudoOf(r.userKey, r.app)); err != nil {
		t.Fatalf("send: %v", err)
	}
	answer, err := receive.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	refusal, ok := answer.(astral.Error)
	if !ok || !strings.HasPrefix(refusal.Error(), "authorize issuer: ") {
		t.Fatalf("a forged issuer answered %v; want an authorize issuer refusal", answer)
	}
	if n := r.crypto.signed.Load(); n != 0 {
		t.Fatalf("a refused contract reached %d signatures", n)
	}

	if err = send.Send(contractOf(r.app, r.app, &auth.SeeObjectsAction{})); err != nil {
		t.Fatalf("send: %v", err)
	}
	answer, err = receive.Receive()
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	sc, ok := answer.(*auth.SignedContract)
	if !ok || sc.IssuerSig == nil || sc.SubjectSig == nil {
		t.Fatalf("the caller's own contract answered %v; want it signed as both parties", answer)
	}
}
