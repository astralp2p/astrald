package apps

import (
	"errors"
	"log"
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	objectsClient "github.com/cryptopunkscc/astrald/mod/objects/client"
)

var (
	blueprintCacheOnce sync.Once
	blueprintCache     []*astral.Blueprint
)

// SyncBlueprints pushes every local runtime Blueprint (struct kind + alias kind) to the
// node, skipping any already present. The cache is built once per process (reflection cost
// paid at first call); the remote list and diff/push run every invocation since the node
// may have lost state across reconnects.
//
// AllBlueprints yields alias-kind Blueprints first, then struct-kind ones, so a Blueprint
// with a RefSpec to an alias type passes the remote validateReferences check during replay.
func SyncBlueprints(ctx *astral.Context) error {
	blueprintCacheOnce.Do(func() {
		// why: AllBlueprints aggregates per-entry derivation failures (PrimitiveAlias returning a
		// non-allowlisted underlying, prototypes with non-Object fields). Every such entry
		// is either already on the node as a compile-time built-in or part of a module both
		// sides link, so the skip is harmless and the error is intentionally dropped here.
		blueprintCache, _ = astral.DefaultBlueprints().AllBlueprints()
	})

	remote, err := objectsClient.Blueprints(ctx)
	if err != nil {
		return err
	}
	have := make(map[string]struct{}, len(remote))
	for _, n := range remote {
		have[n] = struct{}{}
	}

	for _, b := range blueprintCache {
		name := b.Type.String()
		if _, ok := have[name]; ok {
			continue
		}
		_, regErr := objectsClient.Register(ctx, b)
		if regErr == nil || errors.Is(regErr, astral.ErrAlreadyRegistered) {
			continue
		}
		log.Printf("blueprint sync: %s: %v", name, regErr)
	}
	return nil
}

// WithBlueprintSync installs SyncBlueprints as a registration hook so the
// app's local Blueprints are pushed on every (re)connect.
func WithBlueprintSync() ServeOption {
	return WithRegistrationHook(SyncBlueprints)
}
