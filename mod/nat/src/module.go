package nat

import (
	ipmod "github.com/astralp2p/astrald/mod/ip"
	natmod "github.com/astralp2p/astrald/mod/nat"
	treemod "github.com/astralp2p/astrald/mod/tree"
	"net"
	"sync"
	"sync/atomic"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/resources"
)

// Ensure Module struct implements the public nat.Module interface
var _ natmod.Module = &Module{}

// Deps are injected by the core injector.
type Deps struct {
	Dir     dir.Module
	Objects objects.Module
	IP      ipmod.Module
	Tree    treemod.Module
	Events  events.Module
}

type Settings struct {
	Enabled *tree.Value[*astral.Bool] `tree:"enabled"`
}

// Module is the concrete implementation of the NAT module.
type Module struct {
	Deps

	ctx      *astral.Context
	node     astral.Node
	log      *log.Logger
	assets   resources.Resources
	settings Settings

	pool   *HolePool
	router routing.OpRouter

	enabled atomic.Bool
	cond    *sync.Cond
}

func (mod *Module) Run(ctx *astral.Context) error {
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	go func() {
		for range mod.settings.Enabled.Follow(ctx) {
			mod.evaluateEnabled()
		}
	}()

	<-ctx.Done()

	return nil
}

func (mod *Module) evaluateEnabled() {
	setting := mod.settings.Enabled.Get()
	settingEnabled := setting == nil || bool(*setting)
	hasPublicIPs := len(mod.IP.PublicIPCandidates()) > 0

	mod.SetEnabled(settingEnabled && hasPublicIPs)
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

// SetEnabled updates the enabled flag and broadcasts to all waiters if the value changed.
func (mod *Module) SetEnabled(enabled bool) {
	if mod.enabled.Swap(enabled) != enabled {
		mod.cond.Broadcast()
	}
}

func (mod *Module) String() string {
	return natmod.ModuleName
}

func (mod *Module) addHole(hole nat.Hole, active bool) {
	mod.log.Info("added hole: %v (%v) <-> %v (%v) nonce=%v",
		hole.ActiveIdentity,
		hole.ActiveEndpoint,
		hole.PassiveIdentity,
		hole.PassiveEndpoint,
		hole.Nonce,
	)

	h, err := NewHole(hole, mod.ctx.Identity(), active, WithOnHoleExpire(func(h *Hole) {
		mod.log.Info("expired hole: %v (%v) <-> %v (%v) nonce=%v",
			h.ActiveIdentity,
			h.ActiveEndpoint,
			h.PassiveIdentity,
			h.PassiveEndpoint,
			h.Nonce,
		)

		mod.pool.Remove(h.Nonce)
	}))
	if err != nil {
		mod.log.Error("error while creating hole: %v", err)
		return
	}

	err = h.StartKeepAlive(mod.ctx)
	if err != nil {
		mod.log.Error("error starting hole keep-alive: %v", err)
	}

	err = mod.pool.Add(h)
	if err != nil {
		mod.log.Error("error while adding hole to pool: %v", err)
	}
}

func (mod *Module) newPuncher(session []byte) (nat.Puncher, error) {
	cb := &ConePuncherCallbacks{
		OnAttempt:       func(peer ip.IP, port int, _ []*net.UDPAddr) { mod.log.Log("punching → %v:%v", peer, port) },
		OnProbeReceived: func(from *net.UDPAddr) { mod.log.Log("probe ← %v", from) },
	}
	p, err := newConePuncher(session, cb)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (mod *Module) getLocalIPv4() (ip.IP, error) {
	for _, addr := range mod.IP.PublicIPCandidates() {
		if addr.IsIPv4() {
			return addr, nil
		}
	}
	return nil, natmod.ErrNoSuitableIP
}
