package user

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// bridgePusher is the objects module of a node that reaches one peer only: a
// push to peer is delivered to the peer's user module as a drop from sender,
// and a push to any other node fails as unreachable.
type bridgePusher struct {
	objectsmod.Module
	sender *astral.Identity
	peer   *astral.Identity
	to     *Module

	mu      sync.Mutex
	refused []*astral.Identity
}

func (p *bridgePusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	if !target.IsEqual(p.peer) {
		p.refuse(target)
		return errors.New("unreachable")
	}
	return p.to.ReceiveObject(&recordingDrop{sender: p.sender, object: obj})
}

func (p *bridgePusher) refuse(target *astral.Identity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refused = append(p.refused, target)
}

func (p *bridgePusher) unreachable() []*astral.Identity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*astral.Identity(nil), p.refused...)
}

// Member A adopts joiner D while B, the host of app Y, is unreachable from D.
// One A–D sync (SyncNodesTask on A, every push delivered to D's user module)
// leaves D's index naming B as the relay for Y: D admits the contract that A
// forwards, because step 3 of the same sync made B a member on D. The sync
// pushes to D alone.
// Covers f0f7HKYTge acceptance criterion 6 in process; D's routing from that
// index is TestPreprocessQueryRelaysOnlyThroughSwarmMembers (apphost) and
// TestRouteQueryAfterADroppedPushRoutesThroughTheHost (nodes).
func TestAdoptionSyncLetsTheJoinerRouteToAMembersAppWithoutLinkingIt(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	userID := astral.GenerateIdentity()
	joiner := newReceiverFixture(t, userID)
	adopterID, hostID, appID := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	appContract := relayForContract(appID, hostID)

	adopterIndex := &indexAuth{}
	for _, sc := range []*auth.SignedContract{
		nodeContract(userID, adopterID),
		nodeContract(userID, hostID),
		nodeContract(userID, joiner.nodeID),
		appContract,
	} {
		adopterIndex.seed(sc)
	}
	bridge := &bridgePusher{sender: adopterID, peer: joiner.nodeID, to: joiner.mod}
	adopter := &Module{
		Deps: Deps{Auth: adopterIndex, Objects: bridge},
		ctx:  ctx,
		node: &stepNode{id: adopterID, steps: &syncSteps{}},
		log:  log.New(adopterID),
		db:   testDB(t),
	}
	if err := adopter.config.ActiveContract.Set(nil, nodeContract(userID, adopterID)); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	if err := adopter.NewSyncNodesTask(joiner.nodeID).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	relays, err := joiner.auth.SignedContracts().WithIssuer(appID).WithAction(&nodes.RelayForAction{}).Find(ctx)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(relays) != 1 || relays[0] != appContract {
		t.Fatalf("the joiner holds %d relay contracts for the app; want the host's, forwarded by the adopter", len(relays))
	}
	if !relays[0].Subject.IsEqual(hostID) {
		t.Fatalf("the joiner's relay for the app is %v; want the host %v", relays[0].Subject, hostID)
	}
	if got := bridge.unreachable(); len(got) != 0 {
		t.Fatalf("the adopter pushed to %v; want the joiner only", got)
	}
}
