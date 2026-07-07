package nearby

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	_ "github.com/cryptopunkscc/astrald/mod/nearby/views"
)

var _ nearby.Module = &Module{}

const statusExpiration = 5 * time.Minute

type Module struct {
	Deps
	node   astral.Node
	config Config
	log    *log.Logger
	ctx    *astral.Context

	composers sig.Set[nearby.Composer]

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

	err = mod.syncConfig(ctx)
	if err != nil {
		return
	}

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

func (mod *Module) Scan() error {
	return mod.Ether.Push(&nearby.ScanMessage{}, nil)
}

func (mod *Module) AddStatusComposer(composer nearby.Composer) {
	mod.composers.Add(composer)
}

// Mode returns the current broadcast mode; falls back to ModeVisible when no user
// identity is set (node is unowned), and to ModeStealth when the mode is unset.
func (mod *Module) Mode() nearby.Mode {
	if mod.User.Identity() == nil {
		return nearby.ModeVisible
	}

	m := mod.mode.Get()
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
	return nearby.ModuleName
}

func (mod *Module) myAlias() string {
	a, _ := mod.Dir.GetAlias(mod.node.Identity())
	return a
}

func (mod *Module) periodicUpdater(ctx *astral.Context) {
	modeUpdates := mod.mode.Follow(ctx)
	<-modeUpdates // discard initial value; initial broadcast is handled in Run

	for {
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
	return mod.Ether.Push(s, nil)
}

// canBroadcast returns false when the status should be suppressed.
// Stealth mode with no attachments (no active contract) falls back to silent.
func (mod *Module) canBroadcast(s *nearby.StatusMessage) bool {
	return mod.Mode() != nearby.ModeStealth || len(s.Attachments.Objects()) > 0
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
