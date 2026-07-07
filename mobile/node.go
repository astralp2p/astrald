package mobile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/ether"
	ipsrc "github.com/cryptopunkscc/astrald/mod/ip/src"
	"github.com/cryptopunkscc/astrald/mod/secp256k1"
	"github.com/cryptopunkscc/astrald/resources"

	_ "github.com/cryptopunkscc/astrald/mod/all"
	_ "github.com/cryptopunkscc/astrald/mod/all/views"
)

// Default WebSocket bind address — matches mod/apphost/src/config.go.
// The mobile package returns the loopback form from ApphostWSURL since
// Android apps connect locally; on-host clients only.
const defaultApphostWSURL = "ws://127.0.0.1:8624/.ws"

// ErrAlreadyRunning is returned by Start when the node is already up.
var ErrAlreadyRunning = errors.New("node already running")

// ErrNotConfigured is returned by Start when SetConfigDir was never called.
var ErrNotConfigured = errors.New("config dir not set")

// Node is a single astrald instance driven from a platform wrapper.
// One Node per process; see package doc.
type Node struct {
	mu        sync.Mutex
	configDir string
	dataDir   string
	host      Host

	running  atomic.Bool
	identity string
	cancel   context.CancelFunc
	done     chan struct{}
	runErr   error
	cnode    *core.Node

	queryHandlers map[string]QueryHandler
}

// NewNode constructs an unstarted Node. Configure it with SetConfigDir/
// SetDataDir (and optionally SetHost), then call Start.
func NewNode() *Node {
	return &Node{}
}

// SetConfigDir sets the on-disk config root. Required before Start.
// On Android, pass a path under filesDir(), e.g. "<filesDir>/astrald/config".
func (n *Node) SetConfigDir(path string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.configDir = path
}

// SetDataDir sets the on-disk data root. If left empty, defaults to the
// config dir (matching resources.FileResources behavior). On Android,
// pass a path under filesDir() or noBackupFilesDir().
func (n *Node) SetDataDir(path string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dataDir = path
}

// SetHost installs the optional platform integration hooks. Must be called
// before Start to take effect for this run.
func (n *Node) SetHost(h Host) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.host = h
}

// Identity returns the node's public identity as a hex-encoded string,
// or "" if the node hasn't been started yet (identity is loaded/generated
// on first Start).
func (n *Node) Identity() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.identity
}

// Alias returns the local node's directory alias — the human-friendly name
// the dir module assigns on first run (e.g. "snazzy-spark"). Returns "" if
// the node isn't running yet or the alias hasn't been assigned; callers
// should fall back to the identity in that case.
func (n *Node) Alias() string {
	n.mu.Lock()
	cnode := n.cnode
	n.mu.Unlock()

	if cnode == nil || !n.running.Load() {
		return ""
	}

	dirMod, err := core.Load[dir.Module](cnode, dir.ModuleName)
	if err != nil {
		return ""
	}

	alias, err := dirMod.GetAlias(cnode.Identity())
	if err != nil {
		return ""
	}
	return alias
}

// ApphostWSURL returns the loopback WebSocket URL that local apps connect
// to in order to speak the apphost protocol. The default assumes the
// stock apphost.yaml; if the user overrides BindHTTP, they must use the
// matching URL.
func (n *Node) ApphostWSURL() string {
	return defaultApphostWSURL
}

// Running reports whether the node's Run loop is active.
func (n *Node) Running() bool {
	return n.running.Load()
}

// Start brings the node up. It returns when the node goroutine has been
// spawned and identity/resource setup has succeeded — or with an error if
// any of those steps failed. The node continues running in the background
// until Stop is called; use Join to block for shutdown.
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.running.Load() {
		return ErrAlreadyRunning
	}
	if n.configDir == "" {
		return ErrNotConfigured
	}

	res, err := setupResources(n.configDir, n.dataDir)
	if err != nil {
		return fmt.Errorf("resources: %w", err)
	}

	nodeID, err := loadNodeIdentity(res)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}

	cnode, err := core.NewNode(nodeID, res)
	if err != nil {
		return fmt.Errorf("new node: %w", err)
	}

	// let the platform wrapper serve queries at the node identity
	// (see AddQueryHandler)
	cnode.Add(&handlerRouter{node: n, cnode: cnode}, astral.RoutingPriorityNormal)

	if n.host != nil {
		host := n.host
		ether.LANDiscoveryHook = func(active bool) {
			host.LANDiscoveryActive(active)
		}
		// Bypass Go's netlink-based net.InterfaceAddrs (blocked in the
		// Android app sandbox) by routing through the platform Host.
		ipsrc.InterfaceAddrs = func() ([]net.Addr, error) {
			return parseCIDRList(host.LocalInterfaceAddrs()), nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	n.done = make(chan struct{})
	n.identity = nodeID.String()
	n.runErr = nil
	n.cnode = cnode
	n.running.Store(true)

	go func() {
		defer close(n.done)
		defer n.running.Store(false)
		if err := cnode.Run(ctx); err != nil {
			n.mu.Lock()
			n.runErr = err
			n.mu.Unlock()
		}
		ether.LANDiscoveryHook = nil
		ipsrc.InterfaceAddrs = net.InterfaceAddrs
	}()

	return nil
}

// Stop signals the node to shut down. Safe to call multiple times.
// Returns immediately; use Join to block until shutdown completes.
func (n *Node) Stop() {
	n.mu.Lock()
	cancel := n.cancel
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Join blocks until the node has fully stopped. Returns the run error,
// if any, or nil on clean shutdown. Safe to call before Start (returns
// immediately with nil).
//
// Named Join rather than Wait so the gomobile-generated Java method
// doesn't collide with Object.wait().
func (n *Node) Join() error {
	n.mu.Lock()
	done := n.done
	n.mu.Unlock()
	if done == nil {
		return nil
	}
	<-done

	n.mu.Lock()
	defer n.mu.Unlock()
	return n.runErr
}

// setupResources mirrors cmd/astrald/run.go's resource setup but takes
// explicit paths supplied by the platform wrapper instead of reading
// from XDG/UserConfigDir.
func setupResources(configDir, dataDir string) (resources.Resources, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("mkdir config: %w", err)
	}

	res, err := resources.NewFileResources(configDir, true)
	if err != nil {
		return nil, err
	}

	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0700); err != nil {
			return nil, fmt.Errorf("mkdir data: %w", err)
		}
		res.SetDataRoot(dataDir)
	}

	return res, nil
}

// loadNodeIdentity duplicates cmd/astrald/identity.go.loadNodeIdentity to
// avoid pulling in package main. If this logic grows, extract to a shared
// package (e.g. core/identity).
func loadNodeIdentity(res resources.Resources) (*astral.Identity, error) {
	const resNodeKey = "node_key"

	var nodeKey *crypto.PrivateKey

	data, err := res.Read(resNodeKey)
	if err == nil {
		object, _, _ := astral.Decode(bytes.NewReader(data), astral.Canonical())
		var ok bool
		nodeKey, ok = object.(*crypto.PrivateKey)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(object)
		}
	} else {
		nodeKey = secp256k1.New()

		var keyBytes = &bytes.Buffer{}
		if _, err = astral.Encode(keyBytes, nodeKey, astral.Canonical()); err != nil {
			return nil, err
		}
		if err = res.Write(resNodeKey, keyBytes.Bytes()); err != nil {
			return nil, err
		}
	}

	return secp256k1.Identity(secp256k1.PublicKey(nodeKey)), nil
}

// parseCIDRList converts a newline-separated CIDR list from Host into the
// []*net.IPNet shape expected by mod/ip's consumer at module.go:52. IPv6
// zone suffixes ("fe80::1%wlan0/64") are stripped before parsing since
// net.ParseCIDR doesn't accept them. Bad entries are silently dropped.
func parseCIDRList(s string) []net.Addr {
	out := make([]net.Addr, 0)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.IndexByte(line, '%'); i >= 0 {
			if j := strings.IndexByte(line[i:], '/'); j >= 0 {
				line = line[:i] + line[i+j:]
			} else {
				line = line[:i]
			}
		}
		ip, ipnet, err := net.ParseCIDR(line)
		if err != nil {
			continue
		}
		ipnet.IP = ip
		out = append(out, ipnet)
	}
	return out
}
