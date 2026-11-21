package user

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// effectTimeout bounds the wait for an effect the receiver starts in a goroutine.
const effectTimeout = 5 * time.Second

// absenceWindow is how long a test watches for an asynchronous effect that must not happen.
const absenceWindow = 200 * time.Millisecond

// note: signatures here are markers, not cryptography. A signature is valid when
// its data is validSigData; mod/auth and mod/crypto own the real verification.
const validSigData = "valid"

func validSig() *crypto.Signature {
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: astral.Bytes16(validSigData)}
}

func forgedSig() *crypto.Signature {
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: astral.Bytes16("forged")}
}

func isValidSig(sig *crypto.Signature) bool {
	return sig != nil && string(sig.Data) == validSigData
}

// indexAuth is an auth module holding an in-memory contract index. VerifyContract
// checks signature markers; IndexContract appends to the index.
type indexAuth struct {
	authmod.Module

	mu      sync.Mutex
	index   []*auth.SignedContract
	indexed []*auth.SignedContract
}

func (a *indexAuth) VerifyContract(sc *auth.SignedContract) error {
	if !isValidSig(sc.IssuerSig) || !isValidSig(sc.SubjectSig) {
		return errors.New("invalid signature")
	}
	return nil
}

func (a *indexAuth) IndexContract(_ *astral.Context, sc *auth.SignedContract) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.index = append(a.index, sc)
	a.indexed = append(a.indexed, sc)
	return nil
}

// seed adds a contract to the index without recording it as received.
func (a *indexAuth) seed(sc *auth.SignedContract) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.index = append(a.index, sc)
}

func (a *indexAuth) received() []*auth.SignedContract {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*auth.SignedContract(nil), a.indexed...)
}

func (a *indexAuth) SignedContracts() authmod.ContractQueryBuilder {
	return &indexQuery{auth: a}
}

type indexQuery struct {
	auth    *indexAuth
	issuer  *astral.Identity
	subject *astral.Identity
	actions []astral.Object
}

func (q *indexQuery) WithIssuer(id *astral.Identity) authmod.ContractQueryBuilder {
	q.issuer = id
	return q
}

func (q *indexQuery) WithSubject(id *astral.Identity) authmod.ContractQueryBuilder {
	q.subject = id
	return q
}

func (q *indexQuery) WithAction(actions ...astral.Object) authmod.ContractQueryBuilder {
	q.actions = append(q.actions, actions...)
	return q
}

func (q *indexQuery) Find(*astral.Context) (list []*auth.SignedContract, _ error) {
	q.auth.mu.Lock()
	defer q.auth.mu.Unlock()

	for _, sc := range q.auth.index {
		if q.issuer != nil && !sc.Issuer.IsEqual(q.issuer) {
			continue
		}
		if q.subject != nil && !sc.Subject.IsEqual(q.subject) {
			continue
		}
		if !q.permitsAll(sc) {
			continue
		}
		list = append(list, sc)
	}
	return list, nil
}

func (q *indexQuery) permitsAll(sc *auth.SignedContract) bool {
	for _, action := range q.actions {
		if len(sc.HasPermit(action.ObjectType())) == 0 {
			return false
		}
	}
	return true
}

// markerCrypto verifies signature markers for expulsions.
type markerCrypto struct {
	cryptomod.Module
}

func (markerCrypto) Verify(_ *crypto.PublicKey, sig *crypto.Signature, _ crypto.SignableTextObject) error {
	if !isValidSig(sig) {
		return errors.New("invalid signature")
	}
	return nil
}

// endpointSync is a call to UpdateNodeEndpoints.
type endpointSync struct {
	resolver *astral.Identity
	target   *astral.Identity
}

// recordingNodes records endpoint syncs and closed links, and reports a link to
// the linked identities alone.
type recordingNodes struct {
	nodesmod.Module
	syncs chan endpointSync
	// refreshes receives the subject of every RefreshMemberEndpoints call.
	refreshes chan *astral.Identity
	closed    chan *astral.Identity
	linked    []*astral.Identity
	// enforced counts the EnforceLinkPolicy calls.
	enforced atomic.Int32
}

func (n *recordingNodes) IsLinked(id *astral.Identity) bool {
	return slicesContainsIdentity(n.linked, id)
}

func (n *recordingNodes) UpdateNodeEndpoints(_ *astral.Context, resolver, target *astral.Identity) (int, error) {
	n.syncs <- endpointSync{resolver: resolver, target: target}
	return 0, nil
}

func (n *recordingNodes) RefreshMemberEndpoints(subject *astral.Identity) {
	n.refreshes <- subject
}

func (n *recordingNodes) CloseLinks(id *astral.Identity) error {
	n.closed <- id
	return nil
}

// EnforceLinkPolicy counts the enforcement passes.
func (n *recordingNodes) EnforceLinkPolicy() {
	n.enforced.Add(1)
}

// recordingScheduler records scheduled tasks and runs none.
type recordingScheduler struct {
	scheduler.Module
	tasks chan scheduler.Task
}

func (s *recordingScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	s.tasks <- task
	return nil, errors.New("recording scheduler runs no tasks")
}

// recordingDrop is an objects.Drop that records its Accept calls.
type recordingDrop struct {
	sender *astral.Identity
	object astral.Object

	mu      sync.Mutex
	accepts []bool
}

func (d *recordingDrop) SenderID() *astral.Identity { return d.sender }
func (d *recordingDrop) Object() astral.Object      { return d.object }

func (d *recordingDrop) Accept(save bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accepts = append(d.accepts, save)
	return nil
}

func (d *recordingDrop) accepted() []bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]bool(nil), d.accepts...)
}

// receiverFixture is a user module whose dependencies record every effect of ReceiveObject.
type receiverFixture struct {
	mod       *Module
	nodeID    *astral.Identity
	userID    *astral.Identity
	auth      *indexAuth
	nodes     *recordingNodes
	scheduler *recordingScheduler
}

// newReceiverFixture builds a module on nodeID. A non-nil userID becomes the current user.
func newReceiverFixture(t *testing.T, userID *astral.Identity) *receiverFixture {
	t.Helper()

	f := &receiverFixture{
		nodeID: astral.GenerateIdentity(),
		userID: userID,
		auth:   &indexAuth{},
		nodes: &recordingNodes{
			syncs:     make(chan endpointSync, 8),
			refreshes: make(chan *astral.Identity, 8),
			closed:    make(chan *astral.Identity, 8),
		},
		scheduler: &recordingScheduler{tasks: make(chan scheduler.Task, 8)},
	}

	db := testDB(t)
	// note: an sqlite :memory: database is per connection; one connection keeps goroutines on the same database.
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	f.mod = &Module{
		Deps: Deps{Auth: f.auth, Crypto: markerCrypto{}, Nodes: f.nodes, Scheduler: f.scheduler},
		ctx:  ctx,
		node: &identityNode{id: f.nodeID},
		log:  log.New(f.nodeID),
		db:   db,
		// note: no digest by default, so a sync pushes every forwardable contract.
		askDigest: func(*astral.Context, *astral.Identity) (relayDigest, error) {
			return nil, errors.New("no digest")
		},
	}

	if userID != nil {
		err = f.mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: userID}})
		if err != nil {
			t.Fatalf("seed active contract: %v", err)
		}
	}

	return f
}

// addSibling indexes a node contract from the current user for a new node and returns the node.
func (f *receiverFixture) addSibling() *astral.Identity {
	sibling := astral.GenerateIdentity()
	f.auth.seed(nodeContract(f.userID, sibling))
	return sibling
}

// requireNoEffect fails when ReceiveObject indexed, accepted, synced or scheduled anything.
func (f *receiverFixture) requireNoEffect(t *testing.T, drop *recordingDrop) {
	t.Helper()

	if n := len(f.auth.received()); n != 0 {
		t.Errorf("indexed %d contracts; want none", n)
	}
	if a := drop.accepted(); len(a) != 0 {
		t.Errorf("accepted the drop %v; want no Accept call", a)
	}
	select {
	case s := <-f.nodes.syncs:
		t.Errorf("synced endpoints of %v; want none", s.target)
	case id := <-f.nodes.refreshes:
		t.Errorf("refreshed endpoints of %v; want none", id)
	case task := <-f.scheduler.tasks:
		t.Errorf("scheduled %v; want nothing", task)
	case id := <-f.nodes.closed:
		t.Errorf("closed links to %v; want none", id)
	default:
	}
}

func contractWith(issuer, subject *astral.Identity, actions ...string) *auth.SignedContract {
	contract := &auth.Contract{
		Issuer:    issuer,
		Subject:   subject,
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
	for _, action := range actions {
		contract.Permits = append(contract.Permits, &auth.Permit{Action: astral.String8(action)})
	}

	return &auth.SignedContract{Contract: contract, IssuerSig: validSig(), SubjectSig: validSig()}
}

func nodeContract(issuer, subject *astral.Identity) *auth.SignedContract {
	return contractWith(issuer, subject, user.SwarmMembershipAction{}.ObjectType())
}

func relayForContract(issuer, subject *astral.Identity) *auth.SignedContract {
	return contractWith(issuer, subject, nodes.RelayForAction{}.ObjectType())
}

// TestReceiveSignedContractRejects covers every refused contract: each is pushed by
// a swarm sibling, which the replaced rule admitted for any contract, and each
// must leave no index entry, no persistence, no endpoint sync and no sibling link.
func TestReceiveSignedContractRejects(t *testing.T) {
	cases := []struct {
		name     string
		noUser   bool
		contract func(f *receiverFixture, sender *astral.Identity) *auth.SignedContract
	}{
		{"node contract without a current user", true, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return nodeContract(astral.GenerateIdentity(), astral.GenerateIdentity())
		}},
		{"node contract from another user", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return nodeContract(astral.GenerateIdentity(), astral.GenerateIdentity())
		}},
		{"node contract issued by a sibling", false, func(f *receiverFixture, sender *astral.Identity) *auth.SignedContract {
			return nodeContract(sender, astral.GenerateIdentity())
		}},
		{"node contract without the issuer signature", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			sc := nodeContract(f.userID, astral.GenerateIdentity())
			sc.IssuerSig = nil
			return sc
		}},
		{"node contract without the subject signature", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			sc := nodeContract(f.userID, astral.GenerateIdentity())
			sc.SubjectSig = nil
			return sc
		}},
		{"node contract with a forged signature", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			sc := nodeContract(f.userID, astral.GenerateIdentity())
			sc.SubjectSig = forgedSig()
			return sc
		}},
		{"expired node contract", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			sc := nodeContract(f.userID, astral.GenerateIdentity())
			sc.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
			return sc
		}},
		{"node contract for an expelled subject", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			subject := astral.GenerateIdentity()
			if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, subject)); err != nil {
				panic(err)
			}
			return nodeContract(f.userID, subject)
		}},
		{"node contract without a subject", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return nodeContract(f.userID, nil)
		}},
		{"signed contract without a contract", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return &auth.SignedContract{IssuerSig: validSig(), SubjectSig: validSig()}
		}},
		{"contract from the user granting another action", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return contractWith(f.userID, astral.GenerateIdentity(), auth.SeeObjectsAction{}.ObjectType())
		}},
		{"relay-for contract with another permit", false, func(f *receiverFixture, sender *astral.Identity) *auth.SignedContract {
			return contractWith(astral.GenerateIdentity(), sender, nodes.RelayForAction{}.ObjectType(), auth.SeeObjectsAction{}.ObjectType())
		}},
		{"relay-for contract for a subject outside the swarm", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity())
		}},
		{"relay-for contract for an expelled subject", false, func(f *receiverFixture, _ *astral.Identity) *auth.SignedContract {
			return relayForContract(astral.GenerateIdentity(), f.expelSibling())
		}},
		{"relay-for contract with a forged signature", false, func(f *receiverFixture, sender *astral.Identity) *auth.SignedContract {
			sc := relayForContract(astral.GenerateIdentity(), sender)
			sc.IssuerSig = forgedSig()
			return sc
		}},
		{"expired relay-for contract", false, func(f *receiverFixture, sender *astral.Identity) *auth.SignedContract {
			sc := relayForContract(astral.GenerateIdentity(), sender)
			sc.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
			return sc
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var userID *astral.Identity
			if !tc.noUser {
				userID = astral.GenerateIdentity()
			}
			f := newReceiverFixture(t, userID)

			sender := astral.GenerateIdentity()
			if userID != nil {
				f.auth.seed(nodeContract(userID, sender))
			}

			drop := &recordingDrop{sender: sender, object: tc.contract(f, sender)}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			f.requireNoEffect(t, drop)
		})
	}
}

// TestReceiveRelayForContractRequiresSiblingSender covers the sender half of the
// relay-for rule: a contract for a member's app is refused while its sender is
// not in the local swarm.
func TestReceiveRelayForContractRequiresSiblingSender(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())

	stranger := astral.GenerateIdentity()
	drop := &recordingDrop{sender: stranger, object: relayForContract(astral.GenerateIdentity(), f.addSibling())}

	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	f.requireNoEffect(t, drop)
}

// TestReceiveNodeContractAdmitsUnknownSubject covers the valid node contract: its
// subject is in no swarm yet and a third node delivers it. The contract is indexed
// and saved, the subject's endpoints are synced through the sender, and the
// sibling linker starts maintaining a link to the subject.
func TestReceiveNodeContractAdmitsUnknownSubject(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)

	relay := astral.GenerateIdentity()
	subject := astral.GenerateIdentity()
	signed := nodeContract(userID, subject)

	drop := &recordingDrop{sender: relay, object: signed}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	if got := f.auth.received(); len(got) != 1 || got[0] != signed {
		t.Fatalf("indexed %d contracts; want the pushed node contract", len(got))
	}

	if a := drop.accepted(); len(a) != 1 || !a[0] {
		t.Fatalf("Accept calls %v; want one Accept(true)", a)
	}

	select {
	case s := <-f.nodes.syncs:
		if !s.resolver.IsEqual(relay) || !s.target.IsEqual(subject) {
			t.Fatalf("synced endpoints of %v through %v; want %v through %v", s.target, s.resolver, subject, relay)
		}
	case <-time.After(effectTimeout):
		t.Fatal("subject endpoints were never synced")
	}

	select {
	case task := <-f.scheduler.tasks:
		link, ok := task.(*MaintainLinkTask)
		if !ok || !link.Target.IsEqual(subject) {
			t.Fatalf("scheduled %v; want a link maintained to the subject %v", task, subject)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the sibling linker scheduled nothing for the new subject")
	}
}

// TestReceiveNodeContractFromSubjectRefreshesFirstHand (EP5 criterion 1, the
// user half): a node contract pushed by its own subject, as user.introduce does
// before the subject is a member, refreshes the subject's endpoints first-hand
// once, through RefreshMemberEndpoints, which forwards the notice to the linked
// siblings (mod/nodes TestRefreshMemberEndpointsForwardsToLinkedSiblings). The
// contract starts no pull through the sender.
func TestReceiveNodeContractFromSubjectRefreshesFirstHand(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)

	subject := astral.GenerateIdentity()
	drop := &recordingDrop{sender: subject, object: nodeContract(userID, subject)}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	select {
	case id := <-f.nodes.refreshes:
		if !id.IsEqual(subject) {
			t.Fatalf("refreshed endpoints of %v; want the subject %v", id, subject)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the subject's endpoints were never refreshed first-hand")
	}

	select {
	case task := <-f.scheduler.tasks:
		if link, ok := task.(*MaintainLinkTask); !ok || !link.Target.IsEqual(subject) {
			t.Fatalf("scheduled %v; want a link maintained to the subject %v", task, subject)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the sibling linker scheduled nothing for the new subject")
	}

	time.Sleep(absenceWindow)
	select {
	case s := <-f.nodes.syncs:
		t.Fatalf("synced endpoints of %v through %v; a pull from the subject is RefreshMemberEndpoints alone", s.target, s.resolver)
	case id := <-f.nodes.refreshes:
		t.Fatalf("refreshed endpoints of %v twice; want once", id)
	default:
	}
}

// TestReceiveNodeContractFromOtherSenderForwardsNothing (EP5 criterion 2): a
// node contract pushed by a sender that is not its subject pulls the subject's
// endpoints from the sender, as before, and refreshes nothing first-hand.
func TestReceiveNodeContractFromOtherSenderForwardsNothing(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)

	sender := f.addSibling()
	subject := astral.GenerateIdentity()
	drop := &recordingDrop{sender: sender, object: nodeContract(userID, subject)}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	select {
	case s := <-f.nodes.syncs:
		if !s.resolver.IsEqual(sender) || !s.target.IsEqual(subject) {
			t.Fatalf("synced endpoints of %v through %v; want %v through %v", s.target, s.resolver, subject, sender)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the subject's endpoints were never pulled from the sender")
	}

	time.Sleep(absenceWindow)
	select {
	case id := <-f.nodes.refreshes:
		t.Fatalf("refreshed endpoints of %v first-hand; a sender that is not the subject forwards nothing", id)
	default:
	}
}

// TestReceiveRelayForContractFromSibling covers the valid relay-for contract: the
// caller proof a sibling pushes for an app it hosts is indexed and saved, and
// starts no endpoint sync and no sibling link.
func TestReceiveRelayForContractFromSibling(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())

	sibling := f.addSibling()
	signed := relayForContract(astral.GenerateIdentity(), sibling)

	drop := &recordingDrop{sender: sibling, object: signed}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	if got := f.auth.received(); len(got) != 1 || got[0] != signed {
		t.Fatalf("indexed %d contracts; want the pushed relay-for contract", len(got))
	}

	if a := drop.accepted(); len(a) != 1 || !a[0] {
		t.Fatalf("Accept calls %v; want one Accept(true)", a)
	}

	select {
	case s := <-f.nodes.syncs:
		t.Fatalf("synced endpoints of %v; a relay-for contract admits no node", s.target)
	case task := <-f.scheduler.tasks:
		t.Fatalf("scheduled %v; a relay-for contract admits no node", task)
	default:
	}
}

// TestReceiveRelayForContractForwardedByMember covers a forwarded relay-for
// contract: a sibling delivers the contract of an app another sibling hosts, and
// it is indexed and saved like one its host pushed.
func TestReceiveRelayForContractForwardedByMember(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())

	forwarder, host := f.addSibling(), f.addSibling()
	signed := relayForContract(astral.GenerateIdentity(), host)

	drop := &recordingDrop{sender: forwarder, object: signed}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	if got := f.auth.received(); len(got) != 1 || got[0] != signed {
		t.Fatalf("indexed %d contracts; want the forwarded relay-for contract", len(got))
	}
	if a := drop.accepted(); len(a) != 1 || !a[0] {
		t.Fatalf("Accept calls %v; want one Accept(true)", a)
	}
}

// TestReceiveExpulsion keeps signed-expulsion receipt: a ban signed by the current
// user is stored, applied and saved, and a ban from anyone else changes nothing.
func TestReceiveExpulsion(t *testing.T) {
	t.Run("from the current user", func(t *testing.T) {
		userID := astral.GenerateIdentity()
		f := newReceiverFixture(t, userID)

		banned := astral.GenerateIdentity()
		signed := sampleSigned(userID, banned)
		signed.IssuerSig = validSig()

		drop := &recordingDrop{sender: astral.GenerateIdentity(), object: signed}
		if err := f.mod.ReceiveObject(drop); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}

		if !f.mod.isExpelled(userID, banned) {
			t.Fatal("the ban was not stored")
		}

		if a := drop.accepted(); len(a) != 1 || !a[0] {
			t.Fatalf("Accept calls %v; want one Accept(true)", a)
		}

		select {
		case id := <-f.nodes.closed:
			if !id.IsEqual(banned) {
				t.Fatalf("closed links to %v; want the banned node %v", id, banned)
			}
		default:
			t.Fatal("links to the banned node were not closed")
		}
	})

	t.Run("from another identity", func(t *testing.T) {
		userID := astral.GenerateIdentity()
		f := newReceiverFixture(t, userID)

		banned := astral.GenerateIdentity()
		signed := sampleSigned(astral.GenerateIdentity(), banned)
		signed.IssuerSig = validSig()

		drop := &recordingDrop{sender: astral.GenerateIdentity(), object: signed}
		if err := f.mod.ReceiveObject(drop); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}

		if f.mod.isExpelled(userID, banned) || f.mod.isExpelled(signed.Issuer, banned) {
			t.Fatal("a ban from another identity was stored")
		}

		f.requireNoEffect(t, drop)
	})
}

// TestReceiveLinkCreatedEventRequiresLocalSender covers the event branch: a link
// event to a sibling schedules a sibling sync only when this node sent it.
func TestReceiveLinkCreatedEventRequiresLocalSender(t *testing.T) {
	linkEvent := func(remote *astral.Identity) *events.Event {
		return &events.Event{
			ID:   astral.NewNonce(),
			Data: &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: 1},
		}
	}

	t.Run("from another node", func(t *testing.T) {
		f := newReceiverFixture(t, astral.GenerateIdentity())
		sibling := f.addSibling()

		event := linkEvent(sibling)
		// the claimed source is not the sender
		event.SourceID = f.nodeID

		drop := &recordingDrop{sender: sibling, object: event}
		if err := f.mod.ReceiveObject(drop); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}

		f.requireNoEffect(t, drop)
	})

	t.Run("from this node", func(t *testing.T) {
		f := newReceiverFixture(t, astral.GenerateIdentity())
		sibling := f.addSibling()

		drop := &recordingDrop{sender: f.nodeID, object: linkEvent(sibling)}
		if err := f.mod.ReceiveObject(drop); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}

		select {
		case task := <-f.scheduler.tasks:
			if _, ok := task.(*SyncNodesTask); !ok {
				t.Fatalf("scheduled %v; want a sibling sync", task)
			}
		default:
			t.Fatal("a local link event to a sibling scheduled no sync")
		}

		if a := drop.accepted(); len(a) != 1 || a[0] {
			t.Fatalf("Accept calls %v; want one Accept(false)", a)
		}
	})
}

// TestReceivePartialLinkCloseSyncsSibling covers the LinkClosedEvent branch: a
// local event that leaves links to a sibling schedules one sync with it; no
// links left, a node outside the swarm, or an event from another node schedule
// nothing.
func TestReceivePartialLinkCloseSyncsSibling(t *testing.T) {
	cases := []struct {
		name      string
		sibling   bool
		linkCount uint8
		local     bool
		sync      bool
	}{
		{"one link left to a sibling", true, 1, true, true},
		{"no link left to a sibling", true, 0, true, false},
		{"one link left to a non-member", false, 1, true, false},
		{"from another node", true, 1, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newReceiverFixture(t, astral.GenerateIdentity())
			remote := astral.GenerateIdentity()
			if tc.sibling {
				remote = f.addSibling()
			}
			sender := remote
			if tc.local {
				sender = f.nodeID
			}

			event := &events.Event{
				ID:       astral.NewNonce(),
				SourceID: f.nodeID,
				Data:     &nodes.LinkClosedEvent{RemoteIdentity: remote, LinkCount: astral.Uint8(tc.linkCount)},
			}
			drop := &recordingDrop{sender: sender, object: event}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			if !tc.sync {
				time.Sleep(absenceWindow)
				f.requireNoEffect(t, drop)
				return
			}

			select {
			case task := <-f.scheduler.tasks:
				sync, ok := task.(*SyncNodesTask)
				if !ok || !sync.remoteIdentity.IsEqual(remote) {
					t.Fatalf("scheduled %v; want a sync with the sibling", task)
				}
			case <-time.After(effectTimeout):
				t.Fatal("a partial link close of a sibling scheduled no sync")
			}
			if a := drop.accepted(); len(a) != 1 || a[0] {
				t.Fatalf("Accept calls %v; want one Accept(false)", a)
			}
		})
	}
}

// scheduledSyncs returns the subjects of the SyncNodesTasks scheduled so far and
// drops every other task.
// note: the hook schedules before ReceiveObject returns, so a non-blocking drain sees it.
func (f *receiverFixture) scheduledSyncs() (subjects []*astral.Identity) {
	for {
		select {
		case task := <-f.scheduler.tasks:
			if sync, ok := task.(*SyncNodesTask); ok {
				subjects = append(subjects, sync.remoteIdentity)
			}
		default:
			return
		}
	}
}

// TestReceiveNodeContractSyncsALinkedNewMember: a valid node contract that admits
// a node this node already has a link with schedules one sync with that node,
// as no LinkCreatedEvent sync ran for it. A third node delivers the contract,
// so the sync names the subject, not the sender.
func TestReceiveNodeContractSyncsALinkedNewMember(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)

	relay, subject := astral.GenerateIdentity(), astral.GenerateIdentity()
	f.nodes.linked = []*astral.Identity{subject}
	signed := nodeContract(userID, subject)

	drop := &recordingDrop{sender: relay, object: signed}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	if got := f.auth.received(); len(got) != 1 || got[0] != signed {
		t.Fatalf("indexed %d contracts; want the pushed node contract", len(got))
	}
	if a := drop.accepted(); len(a) != 1 || !a[0] {
		t.Fatalf("Accept calls %v; want one Accept(true)", a)
	}

	syncs := f.scheduledSyncs()
	if len(syncs) != 1 || !syncs[0].IsEqual(subject) {
		t.Fatalf("scheduled syncs with %v; want one sync with the new member %v", syncs, subject)
	}
}

// skippedSync is a pushed node contract that schedules no sync. subject names
// the contract's subject and reports whether it is linked; linkSender links the
// sender; noUser leaves the receiver without a current user.
type skippedSync struct {
	name       string
	noUser     bool
	linkSender bool
	subject    func(f *receiverFixture) (*astral.Identity, bool)
	modify     func(sc *auth.SignedContract)
}

func linkedNewNode(*receiverFixture) (*astral.Identity, bool) { return astral.GenerateIdentity(), true }

var skippedSyncs = []skippedSync{
	{name: "an existing member", subject: func(f *receiverFixture) (*astral.Identity, bool) { return f.addSibling(), true }},
	{name: "a node without a link", subject: func(*receiverFixture) (*astral.Identity, bool) { return astral.GenerateIdentity(), false }},
	{name: "a node without a link from a linked sender", linkSender: true, subject: func(*receiverFixture) (*astral.Identity, bool) {
		return astral.GenerateIdentity(), false
	}},
	{name: "this node", subject: func(f *receiverFixture) (*astral.Identity, bool) { return f.nodeID, true }},
	{name: "an expelled node", subject: func(f *receiverFixture) (*astral.Identity, bool) {
		subject := astral.GenerateIdentity()
		if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, subject)); err != nil {
			panic(err)
		}
		return subject, true
	}},
	{name: "an expired contract", subject: linkedNewNode, modify: func(sc *auth.SignedContract) {
		sc.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
	}},
	{name: "a receiver without a current user", noUser: true, subject: linkedNewNode},
}

// TestReceiveNodeContractSkipsTheSync: a pushed node contract schedules no sync
// for a node already in the swarm, a node without a link, or this node, and a
// refused contract schedules none for a linked node.
func TestReceiveNodeContractSkipsTheSync(t *testing.T) {
	for _, tc := range skippedSyncs {
		t.Run(tc.name, func(t *testing.T) {
			userID := astral.GenerateIdentity()
			var current *astral.Identity
			if !tc.noUser {
				current = userID
			}
			f := newReceiverFixture(t, current)

			sender := astral.GenerateIdentity()
			subject, linked := tc.subject(f)
			if linked {
				f.nodes.linked = append(f.nodes.linked, subject)
			}
			if tc.linkSender {
				f.nodes.linked = append(f.nodes.linked, sender)
			}

			signed := nodeContract(userID, subject)
			if tc.modify != nil {
				tc.modify(signed)
			}

			drop := &recordingDrop{sender: sender, object: signed}
			if err := f.mod.ReceiveObject(drop); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}

			if syncs := f.scheduledSyncs(); len(syncs) != 0 {
				t.Fatalf("scheduled syncs with %v; want none", syncs)
			}
		})
	}
}
