package tree

import (
	"context"
	"errors"
	"fmt"
	treemod "github.com/astralp2p/astrald/mod/tree"
	"strings"
	"sync"

	"github.com/astralp2p/astral-go/api/tree"
	treecli "github.com/astralp2p/astral-go/api/tree/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/resources"
)

type Deps struct {
	Dir dir.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	router routing.OpRouter
	db     *DB
	ctx    *astral.Context

	// mounted nodes
	mounts sig.Map[string, tree.Node]

	// node value cache
	nodeValue   map[int]*sig.Queue[astral.Object]
	nodeValueMu sync.Mutex
}

var _ treemod.Module = &Module{}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.WithZone(astral.ZoneNetwork)

	<-ctx.Done()
	return nil
}

func (mod *Module) Get(ctx *astral.Context, path string) (astral.Object, error) {
	node, err := tree.Query(ctx, mod.Root(), path, false)
	if err != nil {
		return nil, err
	}

	get, err := node.Get(ctx, false)
	if err != nil {
		return nil, err
	}

	return <-get, nil
}

func (mod *Module) Set(ctx *astral.Context, path string, object astral.Object) error {
	node, err := tree.Query(ctx, mod.Root(), path, true)
	if err != nil {
		return err
	}

	return node.Set(ctx, object)
}

func (mod *Module) Delete(ctx *astral.Context, path string) error {
	node, err := tree.Query(ctx, mod.Root(), path, false)
	if err != nil {
		return err
	}

	if err := node.Delete(ctx); err != nil {
		return err
	}

	return nil
}

// Mount registers node at an absolute path; fails if the path is not absolute or is already mounted.
func (mod *Module) Mount(path string, node tree.Node) error {
	// check and normalize the path
	if !strings.HasPrefix(path, "/") {
		return errors.New("path must be absolute")
	}
	path = strings.TrimSuffix(path, "/")

	// set the mount point
	_, ok := mod.mounts.Set(path, node)
	if !ok {
		return errors.New("mount point already exists")
	}

	return nil
}

// Unmount removes the mount at an absolute path; fails if the path is not absolute or has no mount.
func (mod *Module) Unmount(path string) error {
	// check and normalize the path
	if !strings.HasPrefix(path, "/") {
		return errors.New("path must be absolute")
	}
	path = strings.TrimSuffix(path, "/")

	// delete the mount point
	_, ok := mod.mounts.Delete(path)
	if !ok {
		return errors.New("mount point does not exist")
	}

	return nil
}

// MountRemote resolves remotePath on targetID's tree (empty remotePath means the root) and mounts it locally at path.
func (mod *Module) MountRemote(ctx *astral.Context, path string, targetID *astral.Identity, remotePath string) (err error) {
	var remoteNode = treecli.New(targetID, nil).Root()

	if len(remotePath) > 0 {
		remoteNode, err = tree.Query(ctx, remoteNode, remotePath, false)
		if err != nil {
			return fmt.Errorf("failed to query remote path %s: %w", remotePath, err)
		}
	}

	return mod.Mount(path, remoteNode)
}

// Root returns the "/" mount wrapped so that subnodes at mounted paths are transparently overlaid.
func (mod *Module) Root() tree.Node {
	root, _ := mod.mounts.Get("/")
	if root == nil {
		return nil
	}

	return &NodeWrapper{
		Node: root,
		mod:  mod,
	}
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return treemod.ModuleName
}

// getMount returns the mounted node for the given path or nil if nothing is mounted.
func (mod *Module) getMount(path string) tree.Node {
	// check and normalize the path
	if !strings.HasPrefix(path, "/") {
		return nil
	}
	path = strings.TrimSuffix(path, "/")

	node, _ := mod.mounts.Get(path)

	return node
}

// pushNodeValue sets node's value cache and notifies all subscribers.
func (mod *Module) pushNodeValue(nodeID int, value astral.Object) {
	mod.nodeValueMu.Lock()
	defer mod.nodeValueMu.Unlock()

	queue, found := mod.nodeValue[nodeID]
	if !found {
		return // do nothing if there are no observers for this node
	}

	mod.nodeValue[nodeID] = queue.Push(value)
}

// subscribeNodeValue returns a channel that receives node's value changes until the context is canceled.
func (mod *Module) subscribeNodeValue(ctx context.Context, nodeID int) <-chan astral.Object {
	mod.nodeValueMu.Lock()
	queue, found := mod.nodeValue[nodeID]
	if !found {
		queue = &sig.Queue[astral.Object]{}
		mod.nodeValue[nodeID] = queue
	}
	mod.nodeValueMu.Unlock()

	var out = make(chan astral.Object)

	go func() {
		defer close(out)
		for v := range sig.Subscribe(ctx, queue) {
			select {
			case <-ctx.Done():
				return
			case out <- v:
			}
		}
	}()

	return out
}

func (mod *Module) deleteNode(nodeID int) error {
	sub, err := mod.db.getSubNodes(nodeID)
	if err != nil {
		return err
	}
	if len(sub) > 0 {
		return tree.ErrNodeHasSubnodes
	}

	mod.nodeValueMu.Lock()
	queue, found := mod.nodeValue[nodeID]
	if found {
		queue.Close()
		delete(mod.nodeValue, nodeID)
	}
	mod.nodeValueMu.Unlock()

	return mod.db.deleteNode(nodeID)
}
