package nodes

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/nodes/src/noise"
)

// errRefusedPeer is the refusal of refusingAdmitter.
var errRefusedPeer = errors.New("refused peer")

// refusingAdmitter refuses the identities in its set and admits every other one.
type refusingAdmitter struct {
	mu      sync.Mutex
	refused map[string]bool
}

func refusing(ids ...*astral.Identity) *refusingAdmitter {
	a := &refusingAdmitter{refused: map[string]bool{}}
	for _, id := range ids {
		a.refuse(id)
	}
	return a
}

func (a *refusingAdmitter) refuse(id *astral.Identity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refused[id.String()] = true
}

func (a *refusingAdmitter) AdmitLink(remote *astral.Identity) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.refused[remote.String()] {
		return errRefusedPeer
	}
	return nil
}

// newAdmissionModule returns a module with a pool, recorded events and admitter.
func newAdmissionModule(admitter nodesmod.LinkAdmitter) (*Module, *recordingEvents) {
	localID := astral.GenerateIdentity()
	events := &recordingEvents{}
	mod := &Module{Deps: Deps{Events: events}, node: &identityNode{id: localID}, log: log.New(localID)}
	mod.linkPool = NewLinkPool(mod)
	mod.AddLinkAdmitter(admitter)
	return mod, events
}

// idleLink returns an inbound link to remote whose keepalive stays silent for the test.
func idleLink(t *testing.T, mod *Module, remote *astral.Identity) *Link {
	t.Helper()
	link, _ := newPeerLink(t, mod, remote, time.Minute, time.Minute)
	return link
}

func requireDenied(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, nodesmod.ErrLinkDenied) || !errors.Is(err, errRefusedPeer) {
		t.Fatalf("err = %v; want ErrLinkDenied wrapping the admitter's refusal", err)
	}
}

func requireClosed(t *testing.T, link *Link) {
	t.Helper()
	select {
	case <-link.Done():
	case <-time.After(effectTimeout):
		t.Fatal("the refused link is still open")
	}
}

// TestRetrieveLinkToARefusedPeerIsDeniedWithoutDialing: RetrieveLink to a
// refused peer answers ErrLinkDenied at once and creates no NodeLinker, so no
// strategy dials; the check runs before the pooled links, so a pooled link to
// the peer is not handed out either.
func TestRetrieveLinkToARefusedPeerIsDeniedWithoutDialing(t *testing.T) {
	refused := astral.GenerateIdentity()
	mod, _ := newAdmissionModule(refusing(refused))
	_ = mod.linkPool.links.Add(idleLink(t, mod, refused))

	for _, opts := range [][]RetrieveLinkOption{nil, {WithForceNew()}} {
		select {
		case result := <-mod.linkPool.RetrieveLink(astral.NewContext(context.Background()), refused, opts...):
			requireDenied(t, result.Err)
			if result.Link != nil {
				t.Fatal("RetrieveLink returned a link to a refused peer")
			}
		case <-time.After(effectTimeout):
			t.Fatal("RetrieveLink to a refused peer did not answer at once")
		}
	}
	if _, ok := mod.linkPool.linkers.Get(refused.String()); ok {
		t.Fatal("RetrieveLink created a NodeLinker for a refused peer")
	}
}

// TestAddLinkOfARefusedPeerClosesIt: AddLink of an inbound link from a
// refused peer closes the link, returns the refusal, keeps it out of the pool
// and announces nothing; a link from another peer is added as before.
func TestAddLinkOfARefusedPeerClosesIt(t *testing.T) {
	refused, admitted := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod, events := newAdmissionModule(refusing(refused))

	link := idleLink(t, mod, refused)
	requireDenied(t, mod.linkPool.AddLink(link))
	requireClosed(t, link)
	if !errors.Is(link.Err(), nodesmod.ErrLinkDenied) {
		t.Fatalf("the refused link closed with %v; want ErrLinkDenied", link.Err())
	}
	if mod.IsLinked(refused) {
		t.Fatal("the refused link is pooled")
	}
	for _, e := range events.recorded() {
		if created, ok := e.(*nodes.LinkCreatedEvent); ok && created.RemoteIdentity.IsEqual(refused) {
			t.Fatal("the pool announced a refused link")
		}
	}

	if err := mod.linkPool.AddLink(idleLink(t, mod, admitted)); err != nil {
		t.Fatalf("AddLink of an admitted peer: %v", err)
	}
	if !mod.IsLinked(admitted) {
		t.Fatal("the admitted link is not pooled")
	}
}

// TestInboundHandshakeFromARefusedPeerClosesBeforeNegotiation: after the noise
// handshake with a refused peer, EstablishInboundLink returns the refusal and
// closes the connection before it sends the first negotiation message, so the
// peer reads end of stream where the feature count would be.
func TestInboundHandshakeFromARefusedPeerClosesBeforeNegotiation(t *testing.T) {
	localKey, peerKey := newNegotiationKey(t), newNegotiationKey(t)
	peerID := astral.IdentityFromPubKey(peerKey.PubKey())
	mod, _ := newAdmissionModule(refusing(peerID))
	mod.privateKey = &crypto.PrivateKey{Key: localKey.Serialize()}

	a, b := net.Pipe()
	deadline := time.Now().Add(negotiationTimeout)
	_ = a.SetDeadline(deadline)
	_ = b.SetDeadline(deadline)
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })

	peerRead := make(chan error, 1)
	go func() {
		aconn, err := noise.HandshakeOutbound(context.Background(), &negotiationPipeConn{Conn: b, outbound: true}, localKey.PubKey(), peerKey)
		if err != nil {
			peerRead <- err
			return
		}
		_, err = channel.New(aconn).Receive()
		peerRead <- err
	}()

	requireDenied(t, mod.EstablishInboundLink(context.Background(), &negotiationPipeConn{Conn: a}))
	if err := <-peerRead; !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("the refused peer read %v after the handshake; want end of stream", err)
	}
	if mod.IsLinked(peerID) {
		t.Fatal("the refused peer is linked")
	}
}

// TestEnforceLinkPolicyClosesOnlyRefusedLinks: once the admitter refuses one of
// two linked peers, EnforceLinkPolicy closes that peer's links with the
// refusal and leaves the other peer's link up.
func TestEnforceLinkPolicyClosesOnlyRefusedLinks(t *testing.T) {
	refused, kept := astral.GenerateIdentity(), astral.GenerateIdentity()
	admitter := refusing()
	mod, _ := newAdmissionModule(admitter)

	refusedLinks := []*Link{idleLink(t, mod, refused), idleLink(t, mod, refused)}
	keptLink := idleLink(t, mod, kept)
	for _, l := range append(refusedLinks, keptLink) {
		if err := mod.linkPool.AddLink(l); err != nil {
			t.Fatalf("AddLink: %v", err)
		}
	}

	mod.EnforceLinkPolicy()
	if keptLink.Err() != nil || refusedLinks[0].Err() != nil {
		t.Fatal("EnforceLinkPolicy closed a link no admitter refuses")
	}

	admitter.refuse(refused)
	mod.EnforceLinkPolicy()

	for _, l := range refusedLinks {
		requireClosed(t, l)
		requireDenied(t, l.Err())
	}
	if keptLink.Err() != nil {
		t.Fatalf("EnforceLinkPolicy closed the admitted peer's link: %v", keptLink.Err())
	}
	if !mod.IsLinked(kept) {
		t.Fatal("the admitted peer is no longer linked")
	}
}

// TestARefusedPeerGetsNoLinkFromEitherSide: once a node refuses a peer, the
// peer's dial fails and neither pool holds a link, and the node's own request
// for a link to the peer is denied without a dial; a link exists in neither
// direction.
func TestARefusedPeerGetsNoLinkFromEitherSide(t *testing.T) {
	member, expelled := newLinkEnd(t), newLinkEnd(t)
	member.mod.AddLinkAdmitter(refusing(expelled.id))

	if _, err := expelled.mod.EstablishOutboundLink(context.Background(), member.id, member.pipeTo(t)); err == nil {
		t.Fatal("the refused peer's dial produced a link")
	}
	result := <-member.mod.linkPool.RetrieveLink(astral.NewContext(context.Background()), expelled.id)
	requireDenied(t, result.Err)

	time.Sleep(100 * time.Millisecond)
	if member.mod.IsLinked(expelled.id) || expelled.mod.IsLinked(member.id) {
		t.Fatal("a link between the node and the refused peer exists")
	}
}
