package auth

import (
	"bytes"
	"fmt"
	authmod "github.com/cryptopunkscc/astrald/mod/auth"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/api/objects"
	"github.com/cryptopunkscc/astral-go/astral"
)

// IndexContract verifies and stores sc in the local DB; skips silently if already indexed.
// Callers must hold no locks — the method acquires indexMu internally.
func (mod *Module) IndexContract(ctx *astral.Context, sc *auth.SignedContract) error {
	mod.indexMu.Lock()
	defer mod.indexMu.Unlock()

	objectID, err := astral.ResolveObjectID(sc)
	if err != nil {
		return fmt.Errorf("resolve object id: %w", err)
	}

	// check if already indexed
	if mod.db.contractExists(objectID) {
		return nil
	}

	err = mod.VerifyContract(sc)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	return mod.db.storeSignedContract(sc)
}

func (mod *Module) indexer(ctx *astral.Context) {
	ctx = ctx.ExcludeZone(astral.ZoneNetwork)

	ch, err := mod.Objects.GetRepository(objects.RepoLocal).Scan(ctx, true)
	if err != nil {
		mod.log.Error("cannot scan objects: %v", err)
		return
	}

	for objectID := range ch {
		if objectID == nil {
			continue
		}

		object, err := mod.Objects.Load(ctx, mod.Objects.ReadDefault(), objectID)

		switch {
		case err != nil:
			continue
		}

		sc, ok := object.(*auth.SignedContract)
		if !ok {
			continue
		}

		_ = mod.IndexContract(ctx, sc)
	}

	mod.log.Logv(1, "auth indexer finished")
}

func (mod *Module) SignedContracts() authmod.ContractQueryBuilder {
	return &contractQuery{DB: mod.db}
}

func encodeSignature(sig astral.Object) ([]byte, error) {
	var buf bytes.Buffer
	_, err := astral.Encode(&buf, sig)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
