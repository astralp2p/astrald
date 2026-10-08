package apphost

import (
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// contractIndex is an auth module whose contract index holds contracts, and
// which records the filters of every lookup.
// note: the embedded nil interface panics on any other method, so an op that
// asks Authorize fails the test: apphost.hosts requires no permit.
type contractIndex struct {
	authmod.Module
	contracts []*auth.SignedContract

	mu      sync.Mutex
	lookups []*contractLookup
}

func (index *contractIndex) SignedContracts() authmod.ContractQueryBuilder {
	index.mu.Lock()
	defer index.mu.Unlock()
	lookup := &contractLookup{index: index}
	index.lookups = append(index.lookups, lookup)
	return lookup
}

func (index *contractIndex) recorded() []*contractLookup {
	index.mu.Lock()
	defer index.mu.Unlock()
	return slices.Clone(index.lookups)
}

// contractLookup filters the index the way the auth module's builder does:
// every filter set must match, and a contract matches an action when one of its
// permits names it.
type contractLookup struct {
	index   *contractIndex
	issuer  *astral.Identity
	subject *astral.Identity
	actions []string
}

func (l *contractLookup) WithIssuer(id *astral.Identity) authmod.ContractQueryBuilder {
	l.issuer = id
	return l
}

func (l *contractLookup) WithSubject(id *astral.Identity) authmod.ContractQueryBuilder {
	l.subject = id
	return l
}

func (l *contractLookup) WithAction(actions ...astral.Object) authmod.ContractQueryBuilder {
	for _, a := range actions {
		l.actions = append(l.actions, a.ObjectType())
	}
	return l
}

func (l *contractLookup) Find(*astral.Context) (found []*auth.SignedContract, _ error) {
	for _, c := range l.index.contracts {
		if l.issuer != nil && !c.Issuer.IsEqual(l.issuer) {
			continue
		}
		if l.subject != nil && !c.Subject.IsEqual(l.subject) {
			continue
		}
		if len(l.actions) > 0 && !slices.ContainsFunc(c.Permits, func(p *auth.Permit) bool {
			return slices.Contains(l.actions, string(p.Action))
		}) {
			continue
		}
		found = append(found, c)
	}
	return found, nil
}

func relayContract(app, host *astral.Identity) *auth.SignedContract {
	return contractFor(app, host, nodes.RelayForAction{}.ObjectType())
}

func contractFor(issuer, subject *astral.Identity, action string) *auth.SignedContract {
	return &auth.SignedContract{Contract: &auth.Contract{
		Issuer:  issuer,
		Subject: subject,
		Permits: []*auth.Permit{{Action: astral.String8(action)}},
	}}
}

// hostsModule is a module on node self whose contract index holds contracts.
func hostsModule(self *astral.Identity, contracts ...*auth.SignedContract) (*Module, *contractIndex) {
	index := &contractIndex{contracts: contracts}
	mod := &Module{
		Deps: Deps{Auth: index, Dir: &namingDir{}},
		node: &serveAppsNode{id: self},
		log:  log.New(nil),
	}
	return mod, index
}

// askHosts asks apphost.hosts for app as a local caller and returns every
// object of the reply up to and excluding EOS.
func askHosts(t *testing.T, mod *Module, app string) []astral.Object {
	t.Helper()

	r, w := io.Pipe()
	t.Cleanup(func() { r.Close() })

	routeOpen(t, mod.OpHosts, astral.GenerateIdentity(), "apphost.hosts?app="+app, w)

	ch := channel.New(struct {
		io.Reader
		io.Writer
	}{r, io.Discard})

	var reply []astral.Object
	for {
		obj, err := ch.Receive()
		if err != nil {
			t.Fatalf("apphost.hosts ended without EOS after %d objects: %v", len(reply), err)
		}
		if _, ok := obj.(*astral.EOS); ok {
			return reply
		}
		reply = append(reply, obj)
	}
}

// identities asserts that every object of reply is an identity and returns their string forms.
func identities(t *testing.T, reply []astral.Object) []string {
	t.Helper()

	var ids []string
	for _, obj := range reply {
		id, ok := obj.(*astral.Identity)
		if !ok {
			t.Fatalf("apphost.hosts sent %v; want only identities", obj.ObjectType())
		}
		ids = append(ids, id.String())
	}
	slices.Sort(ids)
	return ids
}

func sortedStrings(ids ...*astral.Identity) []string {
	var out []string
	for _, id := range ids {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
}

// TestHostsNamesTheSubjectsOfTheAppsRelayContracts: apphost.hosts looks up the
// relay-for contracts the app issued and names each subject once, this node
// among them. Another app's contract and the app's contract for another
// action name no host.
func TestHostsNamesTheSubjectsOfTheAppsRelayContracts(t *testing.T) {
	self, sibling, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	app, otherApp := astral.GenerateIdentity(), astral.GenerateIdentity()

	mod, index := hostsModule(self,
		relayContract(app, sibling),
		relayContract(app, self),
		relayContract(app, sibling),
		relayContract(otherApp, other),
		contractFor(app, other, auth.SeeObjectsAction{}.ObjectType()),
	)

	got := identities(t, askHosts(t, mod, app.String()))
	if want := sortedStrings(self, sibling); !slices.Equal(got, want) {
		t.Fatalf("apphost.hosts named %v; want %v", got, want)
	}

	lookups := index.recorded()
	if len(lookups) != 1 {
		t.Fatalf("apphost.hosts made %d index lookups; want 1", len(lookups))
	}
	lookup := lookups[0]
	if !lookup.issuer.IsEqual(app) || lookup.subject != nil {
		t.Fatalf("apphost.hosts looked up issuer %v subject %v; want issuer %v and any subject", lookup.issuer, lookup.subject, app)
	}
	if want := []string{nodes.RelayForAction{}.ObjectType()}; !slices.Equal(lookup.actions, want) {
		t.Fatalf("apphost.hosts looked up actions %v; want %v", lookup.actions, want)
	}
}

// TestHostsNamesThisNodeForAnAppItHosts: an app registered on this node alone
// is hosted by this node, unlike the relay candidates PreprocessQuery adds.
func TestHostsNamesThisNodeForAnAppItHosts(t *testing.T) {
	self, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := hostsModule(self, relayContract(app, self))

	got := identities(t, askHosts(t, mod, app.String()))
	if want := sortedStrings(self); !slices.Equal(got, want) {
		t.Fatalf("apphost.hosts named %v; want this node %v", got, want)
	}
}

// TestHostsResolvesTheAppByAlias: app takes a name, as every identity argument
// of apphost does.
func TestHostsResolvesTheAppByAlias(t *testing.T) {
	self, sibling, app := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := hostsModule(self, relayContract(app, sibling))
	mod.Dir = &namingDir{aliases: map[string]*astral.Identity{"player": app}}

	got := identities(t, askHosts(t, mod, "player"))
	if want := sortedStrings(sibling); !slices.Equal(got, want) {
		t.Fatalf("apphost.hosts named %v for the alias; want %v", got, want)
	}
}

// TestHostsOfAnUnknownAppIsEOSAlone: an app with no relay contract in the
// index has no known host, and the reply is EOS alone.
func TestHostsOfAnUnknownAppIsEOSAlone(t *testing.T) {
	self, sibling := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, _ := hostsModule(self, relayContract(astral.GenerateIdentity(), sibling))

	if reply := askHosts(t, mod, astral.GenerateIdentity().String()); len(reply) != 0 {
		t.Fatalf("apphost.hosts sent %d objects for an unknown app; want EOS alone", len(reply))
	}
}

// TestHostsRefusesAQueryOffALink: the answer names the nodes of this node's
// swarm, so a query that arrived over a link is rejected before the index is
// read and before a byte is written.
func TestHostsRefusesAQueryOffALink(t *testing.T) {
	self, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, index := hostsModule(self, relayContract(app, self))
	w := newRecordingWriter()

	err := routeAdminManageAppsFromNetwork(t, mod.OpHosts, astral.GenerateIdentity(), "apphost.hosts?app="+app.String(), w)

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("apphost.hosts answered a query off a link: got err %v, want a rejection", err)
	}
	if n := w.written(); n != 0 {
		t.Fatalf("apphost.hosts wrote %d bytes to a query off a link; want none", n)
	}
	if n := len(index.recorded()); n != 0 {
		t.Fatalf("apphost.hosts read the index %d times for a query off a link; want none", n)
	}
}
