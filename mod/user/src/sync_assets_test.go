package user

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	apitree "github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// memNode is an in-memory tree.Node holding one value and named subnodes, which
// is all the asset height cursor uses.
type memNode struct {
	mu    sync.Mutex
	value astral.Object
	subs  map[string]apitree.Node
}

var _ apitree.Node = &memNode{}

func newMemNode() *memNode {
	return &memNode{value: &astral.Nil{}, subs: map[string]apitree.Node{}}
}

// note: follow is ignored; the channel carries the current value and closes.
func (n *memNode) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ch := make(chan astral.Object, 1)
	ch <- n.value
	close(ch)
	return ch, nil
}

func (n *memNode) Set(_ *astral.Context, object astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.value = object
	return nil
}

func (n *memNode) Delete(*astral.Context) error { return nil }

func (n *memNode) Sub(*astral.Context) (map[string]apitree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	subs := make(map[string]apitree.Node, len(n.subs))
	for name, sub := range n.subs {
		subs[name] = sub
	}
	return subs, nil
}

func (n *memNode) Create(_ *astral.Context, name string) (apitree.Node, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, ok := n.subs[name]; ok {
		return nil, apitree.ErrAlreadyExists
	}
	sub := newMemNode()
	n.subs[name] = sub
	return sub, nil
}

// memTree serves one in-memory root, which is the only tree method syncAssets calls.
type memTree struct {
	treemod.Module
	root apitree.Node
}

func (t memTree) Root() apitree.Node { return t.root }

// streamNode accepts every query and streams a fixed object sequence back. It
// stands in for the peer answering user.op_sync_assets.
type streamNode struct {
	id   *astral.Identity
	objs []astral.Object
}

var _ astral.Node = &streamNode{}

func (n *streamNode) Identity() *astral.Identity { return n.id }

func (n *streamNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return query.Accept(q, w, func(conn astral.Conn) {
		ch := channel.New(conn)
		defer ch.Close()

		for _, obj := range n.objs {
			if err := ch.Send(obj); err != nil {
				return
			}
		}
	})
}

// TestSyncAssetsAppliesAnUpdate covers the asset delta an established peer
// streams: the update row reaches the asset table and the trailing height
// advances the node's cursor.
//
// why: the wire type mod.user.op_update is declared in astral-go's api/user, so
// every frame decodes as *user.OpUpdate. A second Go declaration of that wire
// type in this package loses the blueprint registry to api/user and never
// matches the receive switch, which routes every update to the protocol-error
// branch and stops the sync.
func TestSyncAssetsAppliesAnUpdate(t *testing.T) {
	issuer, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	objectID := &astral.ObjectID{Size: 40}
	nonce := astral.NewNonce()
	nextHeight := astral.Uint64(8)

	ctx, cancel := astral.NewContext(nil).WithTimeout(30 * time.Second)
	defer cancel()

	mod := &Module{db: testDB(t), log: log.New(nodeID)}
	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: issuer}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	root := newMemNode()
	mod.Deps.Tree = memTree{root: root}
	mod.node = &streamNode{
		id: nodeID,
		objs: []astral.Object{
			&user.OpUpdate{Nonce: nonce, ObjectID: objectID},
			&nextHeight,
		},
	}

	if err = mod.syncAssets(ctx, nodeID); err != nil {
		t.Fatalf("syncAssets: %v", err)
	}

	assets, err := mod.db.Assets()
	if err != nil {
		t.Fatalf("read assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("asset table holds %d rows after one update, want 1", len(assets))
	}
	if !assets[0].IsEqual(objectID) {
		t.Errorf("asset is %v, want %v", assets[0], objectID)
	}

	cursor, err := apitree.Query(ctx, root, "/mod/user/assets/"+nodeID.String()+"/next_height", false)
	if err != nil {
		t.Fatalf("read height node: %v", err)
	}
	height, err := apitree.Get[*astral.Uint64](ctx, cursor)
	if err != nil {
		t.Fatalf("read height: %v", err)
	}
	if *height != nextHeight {
		t.Errorf("cursor is %v, want %v", *height, nextHeight)
	}
}
