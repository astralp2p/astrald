package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func assetID(n byte) *astral.ObjectID {
	return &astral.ObjectID{Size: 1, Hash: [32]byte{n}}
}

func assetRow(t *testing.T, db *DB, nonce astral.Nonce) dbAsset {
	t.Helper()

	var row dbAsset
	if err := db.Where("nonce = ?", nonce).First(&row).Error; err != nil {
		t.Fatalf("read asset row %v: %v", nonce, err)
	}
	return row
}

func assetRowCount(t *testing.T, db *DB) int64 {
	t.Helper()

	var n int64
	if err := db.Model(&dbAsset{}).Count(&n).Error; err != nil {
		t.Fatalf("count asset rows: %v", err)
	}
	return n
}

func addAsset(t *testing.T, db *DB, id *astral.ObjectID) astral.Nonce {
	t.Helper()

	nonce, err := db.AddAsset(id, false)
	if err != nil {
		t.Fatalf("AddAsset(%v): %v", id, err)
	}
	return nonce
}

func TestAddAssetHeights(t *testing.T) {
	db := testDB(t)

	if h := db.AssetHeight(); h != -1 {
		t.Fatalf("AssetHeight on an empty table = %d, want -1", h)
	}

	for want, id := range []*astral.ObjectID{assetID(1), assetID(2)} {
		row := assetRow(t, db, addAsset(t, db, id))
		if row.Height != uint64(want) {
			t.Fatalf("asset #%d height = %d, want %d", want+1, row.Height, want)
		}
	}
}

func TestAddAssetRefusesADuplicateUnlessForced(t *testing.T) {
	db := testDB(t)
	a := assetID(1)
	first := addAsset(t, db, a)

	if _, err := db.AddAsset(a, false); err == nil || err.Error() != "asset already exists" {
		t.Fatalf("second AddAsset err = %v, want %q", err, "asset already exists")
	}

	forced, err := db.AddAsset(a, true)
	if err != nil {
		t.Fatalf("forced AddAsset: %v", err)
	}
	if forced == first {
		t.Fatalf("forced AddAsset reused nonce %v, want a new one", first)
	}
}

func TestRemoveAssetKeepsATombstone(t *testing.T) {
	db := testDB(t)
	a, b := assetID(1), assetID(2)
	nonceA := addAsset(t, db, a)
	addAsset(t, db, b)
	maxHeight := db.AssetHeight()

	if err := db.RemoveAsset(a); err != nil {
		t.Fatalf("RemoveAsset: %v", err)
	}

	if db.assetExists(a) {
		t.Fatal("assetExists = true after RemoveAsset, want false")
	}
	row := assetRow(t, db, nonceA)
	if !row.Removed {
		t.Fatal("removed asset row has Removed = false, want true")
	}
	if want := uint64(maxHeight + 1); row.Height != want {
		t.Fatalf("removed asset row height = %d, want %d", row.Height, want)
	}

	assets, err := db.Assets()
	if err != nil {
		t.Fatalf("Assets: %v", err)
	}
	if len(assets) != 1 || !assets[0].IsEqual(b) {
		t.Fatalf("Assets = %v, want only %v", assets, b)
	}
}

func TestRemoveAssetByUnknownNonceSuppressesALateAdd(t *testing.T) {
	db := testDB(t)
	b := assetID(2)
	nonce := astral.NewNonce()

	if err := db.RemoveAssetByNonce(nonce, b); err != nil {
		t.Fatalf("RemoveAssetByNonce: %v", err)
	}
	if row := assetRow(t, db, nonce); !row.Removed || !row.ObjectID.IsEqual(b) {
		t.Fatalf("tombstone row = {Removed: %v, ObjectID: %v}, want {true, %v}", row.Removed, row.ObjectID, b)
	}
	rows := assetRowCount(t, db)

	if err := db.AddAssetWithNonce(b, nonce); err != nil {
		t.Fatalf("AddAssetWithNonce over a tombstone: %v, want nil", err)
	}
	if got := assetRowCount(t, db); got != rows {
		t.Fatalf("asset rows = %d after a late add, want %d", got, rows)
	}
	if db.assetExists(b) {
		t.Fatal("a late add over a tombstone made the asset exist")
	}
}

func TestRemoveAssetByNonceTwiceKeepsTheHeight(t *testing.T) {
	db := testDB(t)
	a := assetID(1)
	nonce := addAsset(t, db, a)
	addAsset(t, db, assetID(2))

	if err := db.RemoveAssetByNonce(nonce, a); err != nil {
		t.Fatalf("first RemoveAssetByNonce: %v", err)
	}
	height := assetRow(t, db, nonce).Height

	if err := db.RemoveAssetByNonce(nonce, a); err != nil {
		t.Fatalf("second RemoveAssetByNonce: %v, want nil", err)
	}
	if got := assetRow(t, db, nonce).Height; got != height {
		t.Fatalf("height = %d after a repeated removal, want %d", got, height)
	}
}

func TestAddAssetWithNonceTwiceKeepsOneRow(t *testing.T) {
	db := testDB(t)
	nonce := astral.NewNonce()

	for i := 1; i <= 2; i++ {
		if err := db.AddAssetWithNonce(assetID(1), nonce); err != nil {
			t.Fatalf("AddAssetWithNonce #%d: %v", i, err)
		}
	}

	if n := assetRowCount(t, db); n != 1 {
		t.Fatalf("asset rows = %d, want 1", n)
	}
}
