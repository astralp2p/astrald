package user

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/objects"
)

// assetModule returns a Module with an in-memory asset table and an authority that allows every call.
// note: there is no active contract, so a stored asset notifies no sibling.
func assetModule(t *testing.T) (*Module, *recordingAuth) {
	t.Helper()

	authority := &recordingAuth{verdict: true}
	return &Module{Deps: Deps{Auth: authority}, db: testDB(t), log: log.New(nil)}, authority
}

// checkPartialRejected fails the test unless the op authorized the caller once and then
// rejected the query with the invalid-query code without answering.
// note: the authorization refusal and a database failure both answer the internal-error code.
func checkPartialRejected(t *testing.T, err error, authority *recordingAuth, w *recordingWriter) {
	t.Helper()

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("got err %v; want a rejection", err)
	}
	if rejected.Code != astral.CodeInvalidQuery {
		t.Fatalf("rejected with code %d; want the invalid-query code %d", rejected.Code, astral.CodeInvalidQuery)
	}
	if n := w.written(); n != 0 {
		t.Fatalf("the op wrote %d bytes; want none", n)
	}
	if n := len(authority.recorded()); n != 1 {
		t.Fatalf("the op made %d authorization calls; want 1 before the refusal", n)
	}
}

// TestAddAssetRefusesPartialID: an asset keyed by a partial ID never matches the full
// ID purge asks about and would replicate to siblings, so user.add_asset stores nothing.
func TestAddAssetRefusesPartialID(t *testing.T) {
	mod, authority := assetModule(t)
	partial := &astral.ObjectID{Hash: testObjectID().Hash}

	w := newRecordingWriter()
	err := route(t, mod.OpAddAsset, astral.GenerateIdentity(), "user.add_asset?id="+partial.PartialString(), w)
	checkPartialRejected(t, err, authority, w)

	assets, err := mod.db.Assets()
	if err != nil {
		t.Fatalf("read assets: %v", err)
	}
	if len(assets) != 0 {
		t.Fatalf("the asset table holds %d rows after a refused add; want 0", len(assets))
	}
}

// TestRemoveAssetRefusesPartialID: user.remove_asset refuses a partial ID and removes
// nothing, so the asset keyed by the full ID still protects the object from purge.
func TestRemoveAssetRefusesPartialID(t *testing.T) {
	mod, authority := assetModule(t)
	full := testObjectID()
	partial := &astral.ObjectID{Hash: full.Hash}

	if err := mod.AddAsset(full); err != nil {
		t.Fatalf("add asset by full ID: %v", err)
	}

	w := newRecordingWriter()
	err := route(t, mod.OpRemoveAsset, astral.GenerateIdentity(), "user.remove_asset?id="+partial.PartialString(), w)
	checkPartialRejected(t, err, authority, w)

	if !mod.AssetsContain(full) {
		t.Fatal("the asset keyed by the full ID was removed")
	}
	if !mod.HoldObject(full) {
		t.Fatal("the asset keyed by the full ID no longer protects the object")
	}
}

// TestAssetMethodsRefuseSizeZeroIDs pins the error both ops reject with. The empty
// object's full ID has Size 0 and is refused like a partial ID.
func TestAssetMethodsRefuseSizeZeroIDs(t *testing.T) {
	ids := map[string]*astral.ObjectID{
		"partial ID":   {Hash: testObjectID().Hash},
		"empty object": {Hash: sha256.Sum256(nil)},
	}

	for name, id := range ids {
		t.Run(name, func(t *testing.T) {
			mod, _ := assetModule(t)

			if err := mod.AddAsset(id); !errors.Is(err, objects.ErrPartialObjectID) {
				t.Fatalf("AddAsset: %v; want %v", err, objects.ErrPartialObjectID)
			}
			if err := mod.RemoveAsset(id); !errors.Is(err, objects.ErrPartialObjectID) {
				t.Fatalf("RemoveAsset: %v; want %v", err, objects.ErrPartialObjectID)
			}
			if mod.AssetsContain(id) {
				t.Fatal("a refused ID is an asset")
			}
		})
	}
}
