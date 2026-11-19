package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	dirmod "github.com/astralp2p/astrald/mod/dir"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

// modeNearby is a nearby module in mode.
type modeNearby struct {
	nearbymod.Module
	mode nearby.Mode
}

func (n modeNearby) Mode() nearby.Mode { return n.mode }

// aliasDir is a dir module that names every identity alias, and has no alias
// for any identity when alias is empty.
type aliasDir struct {
	dirmod.Module
	alias string
}

func (d aliasDir) GetAlias(*astral.Identity) (string, error) {
	if d.alias == "" {
		return "", errors.New("no alias")
	}
	return d.alias, nil
}

// recordingComposition records what a composer attaches.
type recordingComposition struct {
	attached []astral.Object
}

func (c *recordingComposition) Receiver() *astral.Identity { return nil }

func (c *recordingComposition) Attach(obj astral.Object) error {
	c.attached = append(c.attached, obj)
	return nil
}

// portModule is a tcp or kcp module listening on its value.
type portModule int

func (p portModule) ListenPort() int { return int(p) }

// defaultTransports is a node's tcp and kcp on their default listen ports.
func defaultTransports() Transports {
	return Transports{TCP: portModule(1791), KCP: portModule(1792)}
}

// composerModule is a node in mode named kitchen, on the default listen ports, owned by
// userID unless userID is nil.
func composerModule(t *testing.T, userID *astral.Identity, mode nearby.Mode) *Module {
	t.Helper()

	nodeID := astral.GenerateIdentity()
	mod := &Module{
		Deps:       Deps{Nearby: modeNearby{mode: mode}, Dir: aliasDir{alias: "kitchen"}},
		Transports: defaultTransports(),
		node:       &identityNode{id: nodeID},
		log:        log.New(nodeID),
	}
	if userID != nil {
		signed := &auth.SignedContract{Contract: &auth.Contract{Issuer: userID, Subject: nodeID}}
		if err := mod.config.ActiveContract.Set(nil, signed); err != nil {
			t.Fatalf("seed active contract: %v", err)
		}
	}
	return mod
}

func compose(mod *Module) []astral.Object {
	c := &recordingComposition{}
	mod.ComposeStatus(c)
	return c.attached
}

// requireProfile fails unless obj is a PublicProfile equal to want.
func requireProfile(t *testing.T, obj astral.Object, want *nearby.PublicProfile) {
	t.Helper()

	p, ok := obj.(*nearby.PublicProfile)
	if !ok {
		t.Fatalf("attached %v; want a public profile", obj.ObjectType())
	}
	if !p.NodeID.IsEqual(want.NodeID) || p.NodeAlias != want.NodeAlias {
		t.Fatalf("profile of %v named %q; want %v named %q", p.NodeID, p.NodeAlias, want.NodeID, want.NodeAlias)
	}
	if (want.UserID == nil) != (p.UserID == nil) || !p.UserID.IsEqual(want.UserID) {
		t.Fatalf("profile user %v; want %v", p.UserID, want.UserID)
	}
}

// TestComposeVisible: an unowned node attaches the adoptable flag and its
// profile with no user; an owned node attaches its profile with its user. No
// contract, endpoint or dir alias is attached.
func TestComposeVisible(t *testing.T) {
	t.Run("unowned", func(t *testing.T) {
		mod := composerModule(t, nil, nearby.ModeVisible)
		got := compose(mod)
		if len(got) != 2 {
			t.Fatalf("attached %v objects; want the flag and the profile", len(got))
		}
		if f, ok := got[0].(*nearby.Flag); !ok || f.String() != "adoptable" {
			t.Fatalf("attached %v first; want the adoptable flag", got[0])
		}
		requireProfile(t, got[1], &nearby.PublicProfile{NodeID: mod.node.Identity(), NodeAlias: "kitchen"})
	})

	t.Run("owned", func(t *testing.T) {
		userID := astral.GenerateIdentity()
		mod := composerModule(t, userID, nearby.ModeVisible)
		got := compose(mod)
		if len(got) != 1 {
			t.Fatalf("attached %v objects; want the profile alone", len(got))
		}
		requireProfile(t, got[0], &nearby.PublicProfile{NodeID: mod.node.Identity(), UserID: userID, NodeAlias: "kitchen"})
	})
}

// TestComposeProfileAlias: the profile names an alias that fits a string8, and
// names no alias for a longer one or for a node without one.
func TestComposeProfileAlias(t *testing.T) {
	for _, tt := range []struct {
		name, alias, want string
	}{
		{"255 bytes", strings.Repeat("a", 255), strings.Repeat("a", 255)},
		{"256 bytes", strings.Repeat("a", 256), ""},
		{"no alias", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mod := composerModule(t, astral.GenerateIdentity(), nearby.ModeVisible)
			mod.Dir = aliasDir{alias: tt.alias}
			got := compose(mod)
			if len(got) != 1 {
				t.Fatalf("attached %v objects; want the profile alone", len(got))
			}
			p := got[0].(*nearby.PublicProfile)
			if string(p.NodeAlias) != tt.want {
				t.Fatalf("profile alias of %v bytes; want %v bytes", len(p.NodeAlias), len(tt.want))
			}
		})
	}
}

// TestComposeStealth: an owned node attaches one fresh stealth hint that opens
// with its user to the node, and with the node to the user; an unowned node
// attaches nothing.
func TestComposeStealth(t *testing.T) {
	t.Run("owned", func(t *testing.T) {
		userID := astral.GenerateIdentity()
		mod := composerModule(t, userID, nearby.ModeStealth)
		nodeID := mod.node.Identity()
		first, second := compose(mod), compose(mod)
		if len(first) != 1 || len(second) != 1 {
			t.Fatalf("attached %v and %v objects; want one hint each", len(first), len(second))
		}
		hint, ok := first[0].(*nearby.StealthHint)
		if !ok {
			t.Fatalf("attached %v; want a stealth hint", first[0].ObjectType())
		}
		if node, ok := hint.Open(userID); !ok || !node.IsEqual(nodeID) {
			t.Fatalf("the hint opens with the user to %v (%v); want %v", node, ok, nodeID)
		}
		if user, ok := hint.OpenWithNode(nodeID); !ok || !user.IsEqual(userID) {
			t.Fatalf("the hint opens with the node to %v (%v); want %v", user, ok, userID)
		}
		if second[0].(*nearby.StealthHint).Nonce == hint.Nonce {
			t.Fatal("two hints share a nonce; want a fresh nonce per hint")
		}
	})

	t.Run("unowned", func(t *testing.T) {
		if got := compose(composerModule(t, nil, nearby.ModeStealth)); len(got) != 0 {
			t.Fatalf("attached %v objects; want none", len(got))
		}
	})
}

// TestComposeSilent: a Silent node attaches nothing, owned or not.
func TestComposeSilent(t *testing.T) {
	for _, userID := range []*astral.Identity{nil, astral.GenerateIdentity()} {
		if got := compose(composerModule(t, userID, nearby.ModeSilent)); len(got) != 0 {
			t.Fatalf("owned %v: attached %v objects; want none", userID != nil, len(got))
		}
	}
}

// TestComposeVisibleListenPorts: a Visible node off the default listen ports
// attaches its ports after the profile, a missing or out-of-range transport
// reading 0; a node on both defaults attaches none (TestComposeVisible).
func TestComposeVisibleListenPorts(t *testing.T) {
	for _, tt := range []struct {
		name       string
		transports Transports
		want       nearby.ListenPorts
	}{
		{"tcp 1800", Transports{TCP: portModule(1800), KCP: portModule(1792)}, nearby.ListenPorts{TCP: 1800, KCP: 1792}},
		{"kcp 1801", Transports{TCP: portModule(1791), KCP: portModule(1801)}, nearby.ListenPorts{TCP: 1791, KCP: 1801}},
		{"no kcp", Transports{TCP: portModule(1791)}, nearby.ListenPorts{TCP: 1791}},
		{"tcp out of range", Transports{TCP: portModule(70000), KCP: portModule(1792)}, nearby.ListenPorts{KCP: 1792}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mod := composerModule(t, astral.GenerateIdentity(), nearby.ModeVisible)
			mod.Transports = tt.transports
			got := compose(mod)
			if len(got) != 2 {
				t.Fatalf("attached %v objects; want the profile and the ports", len(got))
			}
			ports, ok := got[1].(*nearby.ListenPorts)
			if !ok || *ports != tt.want {
				t.Fatalf("attached %v second; want ListenPorts %+v", got[1], tt.want)
			}
		})
	}
}

// TestComposeStealthSealedPorts: a Stealth node off the default listen ports
// attaches its ports after the hint, sealed under the node and the hint's nonce;
// a node on both defaults attaches none (TestComposeStealth).
func TestComposeStealthSealedPorts(t *testing.T) {
	mod := composerModule(t, astral.GenerateIdentity(), nearby.ModeStealth)
	mod.Transports = Transports{TCP: portModule(1800), KCP: portModule(1792)}

	got := compose(mod)
	if len(got) != 2 {
		t.Fatalf("attached %v objects; want the hint and the sealed ports", len(got))
	}
	hint, ok := got[0].(*nearby.StealthHint)
	if !ok {
		t.Fatalf("attached %v first; want a stealth hint", got[0].ObjectType())
	}
	sealed, ok := got[1].(*nearby.SealedPorts)
	if !ok {
		t.Fatalf("attached %v second; want sealed ports", got[1].ObjectType())
	}
	want := nearby.ListenPorts{TCP: 1800, KCP: 1792}
	if ports, ok := sealed.Open(mod.node.Identity(), hint.Nonce); !ok || *ports != want {
		t.Fatalf("sealed ports open to %+v (%v); want %+v", ports, ok, want)
	}
}
