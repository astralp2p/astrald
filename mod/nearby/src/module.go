package nearby

import (
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
	_ "github.com/astralp2p/astrald/mod/nearby/views"
)

var _ nearbymod.Module = &Module{}

// statusTTL is how long a received status stays in the cache, and how far a status
// timestamp may lie from the receiver's clock.
const statusTTL = 5 * time.Minute

type Module struct {
	Deps
	node   astral.Node
	config Config
	log    *log.Logger

	// why: injectable time, so the tests of every time rule run fast and deterministic.
	now   func() time.Time
	after func(time.Duration) <-chan time.Time

	composers   sig.Set[nearbymod.Composer]
	recognizers sig.Set[nearbymod.Recognizer]

	cache   peerCache
	nonces  nonceMemory
	replies scanReplies

	mode          tree.Value[*nearby.Mode]
	silentListen  tree.Value[*astral.Bool]
	badModeLogged atomic.Bool
	router        routing.OpRouter
}

func (mod *Module) Run(ctx *astral.Context) error {
	// why: the mode depends on the owner, which the user module knows once it is ready.
	select {
	case <-mod.User.Ready():
	case <-ctx.Done():
		return nil
	}

	go mod.runExpiry(ctx)
	go func() {
		if err := mod.Scan(); err != nil {
			mod.log.Error("scan: %v", err)
		}
	}()

	mod.runPeriodic(ctx)
	return nil
}

func (mod *Module) AddStatusComposer(composer nearbymod.Composer) {
	mod.composers.Add(composer)
}

// Mode returns the current broadcast mode. While no user identity is set (node
// is unowned) it is ModeVisible unless the stored mode is ModeSilent; an unset
// mode on an owned node is ModeStealth.
//
// why Visible while unowned: the adoptable status lets a LAN device claim a
// fresh node with no setup step.
// why Silent is kept: an operator who configures silence gets it before the
// claim as well as after.
func (mod *Module) Mode() nearby.Mode {
	m := mod.storedMode()
	if mod.User.Identity() == nil && (m == nil || *m != nearby.ModeSilent) {
		return nearby.ModeVisible
	}
	if m == nil {
		return nearby.ModeStealth
	}
	return *m
}

// storedMode returns the mode stored in the tree, or nil when it is unset or invalid.
//
// why: tree.Value has no validation hook, so a value above ModeStealth written straight
// into the tree reads as unset here instead of sending in an undefined mode.
func (mod *Module) storedMode() *nearby.Mode {
	m := mod.mode.Get()
	if m == nil || *m <= nearby.ModeStealth {
		return m
	}
	if mod.badModeLogged.CompareAndSwap(false, true) {
		mod.log.Logv(1, "stored mode %v is above %v, read as unset", *m, nearby.ModeStealth)
	}
	return nil
}

// SetMode stores m; it refuses a mode above ModeStealth.
func (mod *Module) SetMode(ctx *astral.Context, m nearby.Mode) error {
	if m > nearby.ModeStealth {
		return nearbymod.ErrInvalidMode
	}
	return mod.mode.Set(ctx, &m)
}

// listensInSilent reports the silent_listen setting; unset reads true.
func (mod *Module) listensInSilent() bool {
	v := mod.silentListen.Get()
	return v == nil || bool(*v)
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return nearbymod.ModuleName
}
