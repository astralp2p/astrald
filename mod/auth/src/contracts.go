package auth

import (
	"bytes"
	"errors"
	"fmt"
	authmod "github.com/astralp2p/astrald/mod/auth"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"slices"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
)

// IndexContract admits, verifies and stores sc in the local DB; skips silently if already indexed.
// Callers must hold no locks — the method acquires indexMu internally.
// A newly stored contract runs every OnContractIndexed callback after indexMu is released.
func (mod *Module) IndexContract(ctx *astral.Context, sc *auth.SignedContract) error {
	if err := mod.admit(sc); err != nil {
		return err
	}

	stored, err := mod.storeContract(sc)
	if err != nil || !stored {
		return err
	}
	mod.onIndexedMu.Lock()
	callbacks := mod.onIndexed
	mod.onIndexedMu.Unlock()
	for _, fn := range callbacks {
		fn(sc)
	}
	return nil
}

// OnContractIndexed registers fn to run after each newly indexed contract.
func (mod *Module) OnContractIndexed(fn func(*auth.SignedContract)) {
	mod.onIndexedMu.Lock()
	defer mod.onIndexedMu.Unlock()
	// why: a new slice, so a reader iterating the old one never sees it change.
	mod.onIndexed = append(slices.Clip(mod.onIndexed), fn)
}

// AddAdmitter registers fn to run on each contract new to the index, before it is stored.
func (mod *Module) AddAdmitter(fn func(*auth.SignedContract) error) {
	mod.admittersMu.Lock()
	defer mod.admittersMu.Unlock()
	// why: a new slice, so a reader iterating the old one never sees it change.
	mod.admitters = append(slices.Clip(mod.admitters), fn)
}

// admit runs every admitter on sc and wraps the first refusal in ErrNotAdmitted.
// why before storeContract: an admitter reads other modules, which read the
// index in turn, so holding indexMu across it would invert the lock order.
func (mod *Module) admit(sc *auth.SignedContract) error {
	mod.admittersMu.Lock()
	admitters := mod.admitters
	mod.admittersMu.Unlock()

	if len(admitters) == 0 {
		return nil
	}

	// why: the indexer meets every stored contract on each start; one the index
	// holds already needs no admitter read.
	if objectID, err := astral.ResolveObjectID(sc); err == nil && mod.db.contractExists(objectID) {
		return nil
	}

	for _, fn := range admitters {
		if err := fn(sc); err != nil {
			return fmt.Errorf("%w: %w", authmod.ErrNotAdmitted, err)
		}
	}
	return nil
}

// storeContract verifies and stores sc under indexMu, and reports whether sc
// was new to the index.
func (mod *Module) storeContract(sc *auth.SignedContract) (bool, error) {
	mod.indexMu.Lock()
	defer mod.indexMu.Unlock()

	objectID, err := astral.ResolveObjectID(sc)
	if err != nil {
		return false, fmt.Errorf("resolve object id: %w", err)
	}

	// check if already indexed
	if mod.db.contractExists(objectID) {
		return false, nil
	}

	err = mod.VerifyContract(sc)
	if err != nil {
		return false, fmt.Errorf("verify: %w", err)
	}

	return true, mod.db.storeSignedContract(sc)
}

// maxContractSize is the largest object the indexer reads to find a contract.
// note: a signed contract above it in local is not found by the indexer; one issued on this node is indexed when it is issued.
const maxContractSize = 64 * 1024

func (mod *Module) indexer(ctx *astral.Context) {
	// why: the follower reads every object in local. Housekeeping reads leave last-touched times unchanged (VFS 6, §11 "objects: last touched").
	ctx = objectsmod.WithHousekeeping(ctx.ExcludeZone(astral.ZoneNetwork))

	repo := mod.Objects.GetRepository(objects.RepoLocal)
	if repo == nil {
		mod.log.Error("cannot scan objects: repository %v not found", objects.RepoLocal)
		return
	}

	ch, err := repo.Scan(ctx, true)
	if err != nil {
		mod.log.Error("cannot scan objects: %v", err)
		return
	}

	// deferred holds the snapshot's contracts an admitter refused.
	// why: the scan order is the repository's, so a relay contract can come
	// before the node contract that makes its subject a member.
	var deferred []*auth.SignedContract

	for objectID := range ch {
		// note: Scan in follow mode sends one nil between the snapshot and live updates.
		if objectID == nil {
			mod.retryDeferred(ctx, deferred)
			deferred = nil
			continue
		}

		// todo: rethink this indexing mechanism in terms of performance. It follows
		// every object in the repository and reads each one to find contracts; the
		// size cap and the probe below only make each miss cheaper.
		if objectID.Size > maxContractSize {
			continue
		}

		if sc := mod.indexIfContract(ctx, repo, objectID); sc != nil {
			deferred = append(deferred, sc)
		}
	}

	mod.log.Logv(1, "auth indexer finished")
}

// retryDeferred indexes each refused contract once more, after the snapshot.
func (mod *Module) retryDeferred(ctx *astral.Context, deferred []*auth.SignedContract) {
	for _, sc := range deferred {
		if err := mod.IndexContract(ctx, sc); err != nil {
			mod.log.Logv(1, "auth indexer: deferred contract: %v", err)
		}
	}
}

// indexIfContract indexes the object when its probed type is a signed contract.
// Any other object is not loaded. It returns the contract when an admitter refused it.
func (mod *Module) indexIfContract(ctx *astral.Context, repo objectsmod.Repository, objectID *astral.ObjectID) *auth.SignedContract {
	probe, err := mod.Objects.Probe(ctx, repo, objectID)
	if err != nil || string(probe.Type) != (&auth.SignedContract{}).ObjectType() {
		return nil
	}

	object, err := mod.Objects.Load(ctx, mod.Objects.ReadDefault(), objectID)
	if err != nil {
		return nil
	}

	sc, ok := object.(*auth.SignedContract)
	if !ok {
		return nil
	}

	if err = mod.IndexContract(ctx, sc); errors.Is(err, authmod.ErrNotAdmitted) {
		return sc
	}
	return nil
}

// WithdrawContracts takes back, on this node, every unexpired contract issued by
// issuer that carries a permit for action. The next authorization no longer
// finds the contract, and indexing it again changes nothing.
func (mod *Module) WithdrawContracts(issuer *astral.Identity, action astral.Object) (int, error) {
	if issuer == nil || issuer.IsZero() {
		return 0, fmt.Errorf("missing issuer")
	}

	n, err := mod.db.withdrawContracts(issuer, action.ObjectType())
	return int(n), err
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
