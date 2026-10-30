package nearby

import (
	"time"

	"github.com/astralp2p/astral-go/api/ip"
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

const statusExpiration = 5 * time.Minute

type Module struct {
	Deps
	node   astral.Node
	config Config
	log    *log.Logger
	ctx    *astral.Context

	composers sig.Set[nearbymod.Composer]

	cache  sig.Map[string, *cache]
	mode   tree.Value[*nearby.Mode]
	router routing.OpRouter
}

type cache struct {
	Identity  *astral.Identity
	IP        ip.IP
	Timestamp time.Time
	Status    *nearby.StatusMessage
}

func (c *cache) GetIdentity() *astral.Identity {
	profile := astral.SelectByType[*nearby.PublicProfile](c.Status.Attachments.Objects())
	if len(profile) == 0 {
		return nil
	}

	return profile[0].NodeID
}

func (mod *Module) Run(ctx *astral.Context) (err error) {
	mod.ctx = ctx

	<-mod.User.Ready()

	go mod.periodicUpdater(ctx)

	go func() {
		mod.Scan()
	}()

	<-ctx.Done()
	return nil
}

func (mod *Module) syncConfig(ctx *astral.Context) error {
	if mod.config.Mode != nil {
		return mod.SetMode(ctx, *mod.config.Mode)
	}
	return nil
}

// Scan asks the nodes on the LAN for their status; no-ops in ModeSilent.
func (mod *Module) Scan() error {
	if mod.Mode() == nearby.ModeSilent {
		return nil
	}
	return mod.Ether.Push(&nearby.ScanMessage{})
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
	m := mod.mode.Get()
	if mod.User.Identity() == nil && (m == nil || *m != nearby.ModeSilent) {
		return nearby.ModeVisible
	}
	if m == nil {
		return nearby.ModeStealth
	}
	return *m
}

func (mod *Module) SetMode(ctx *astral.Context, m nearby.Mode) error {
	return mod.mode.Set(ctx, &m)
}

// Cache returns the live peer-status map after evicting entries older than statusExpiration.
func (mod *Module) Cache() *sig.Map[string, *cache] {
	mod.expireCache()
	return &mod.cache
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return nearbymod.ModuleName
}

func (mod *Module) myAlias() string {
	a, _ := mod.Dir.GetAlias(mod.node.Identity())
	return a
}

func (mod *Module) periodicUpdater(ctx *astral.Context) {
	modeUpdates := mod.mode.Follow(ctx)
	<-modeUpdates // discard initial value; initial broadcast is handled in Run

	// why: 255 is no defined mode, so the first pass logs on every node, a silent one included.
	last := nearby.Mode(255)
	for {
		if m := mod.Mode(); m != last {
			mod.log.Log("mode %v", m)
			last = m
		}

		if mod.Mode() != nearby.ModeSilent {
			if err := mod.Broadcast(); err != nil {
				mod.log.Error("push error: %v", err)
			} else {
				mod.log.Logv(3, "pushed status")
			}
		}

		select {
		case <-time.After(statusExpiration - 5*time.Second): // broadcast 5s early to avoid status timeout
		case _, ok := <-modeUpdates:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}

	}
}

// Broadcast pushes the current status to all reachable peers; no-ops silently in
// ModeSilent without returning an error.
func (mod *Module) Broadcast() error {
	if mod.Mode() == nearby.ModeSilent {
		return nil
	}

	return mod.pushStatus()
}

func (mod *Module) pushStatus() error {
	s := mod.Status(nil)
	if !mod.canBroadcast(s) {
		return nil
	}
	return mod.Ether.Push(s)
}

// canBroadcast returns false when the status should be suppressed: always in
// ModeSilent, and in ModeStealth when the status has no attachments (no active
// contract). It gates the periodic status, the address-change status and the
// scan reply alike.
func (mod *Module) canBroadcast(s *nearby.StatusMessage) bool {
	switch mod.Mode() {
	case nearby.ModeSilent:
		return false
	case nearby.ModeStealth:
		return len(s.Attachments.Objects()) > 0
	}
	return true
}

// Status assembles a StatusMessage by invoking every registered Composer; pass nil
// to address the message to astral.Anyone (broadcast semantics).
func (mod *Module) Status(receiver *astral.Identity) *nearby.StatusMessage {
	s := &nearby.StatusMessage{
		Attachments: astral.NewBundle(),
	}

	if receiver == nil {
		receiver = astral.Anyone
	}

	for _, a := range mod.composers.Clone() {
		a.ComposeStatus(&Composition{
			receiver: receiver,
			s:        s,
		})
	}

	return s
}

func (mod *Module) expireCache() {
	for k, v := range mod.cache.Clone() {
		if time.Since(v.Timestamp) > statusExpiration {
			mod.cache.Delete(k)
		}
	}
}
