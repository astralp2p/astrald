package user

import (
	"errors"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/user"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	mod *Module
	*gorm.DB
}

func (db *DB) assetExists(objectID *astral.ObjectID) (exists bool) {
	db.Model(&dbAsset{}).
		Select("1").
		Where("object_id = ? and removed = false", objectID).
		Limit(1).
		Scan(&exists)
	return
}

// AssetHeight returns the maximum height across all asset rows, or -1 on query error.
func (db *DB) AssetHeight() int {
	var height uint64

	err := db.Model(&dbAsset{}).
		Select("MAX(height) AS height").
		Scan(&height).
		Error

	if err != nil {
		return -1
	}

	return int(height)
}

func (db *DB) NonceExists(nonce astral.Nonce) (exists bool) {
	db.Model(&dbAsset{}).
		Select("1").
		Where("nonce", nonce).
		Limit(1).
		Scan(&exists)
	return
}

// AddAsset inserts a new asset row with a fresh nonce; returns an error if the object already exists unless force is true.
func (db *DB) AddAsset(objectID *astral.ObjectID, force bool) (nonce astral.Nonce, err error) {
	if !force && db.assetExists(objectID) {
		return nonce, errors.New("asset already exists")
	}

	nonce = astral.NewNonce()

	return nonce, db.Save(&dbAsset{
		Nonce:    nonce,
		ObjectID: objectID,
		Height:   uint64(db.AssetHeight() + 1),
	}).Error
}

// AddAssetWithNonce is idempotent: if the nonce already exists it is a no-op, enabling replay-safe replication.
func (db *DB) AddAssetWithNonce(objectID *astral.ObjectID, nonce astral.Nonce) error {
	if db.NonceExists(nonce) {
		return nil
	}

	return db.Create(&dbAsset{
		Nonce:    nonce,
		ObjectID: objectID,
		Height:   uint64(db.AssetHeight() + 1),
	}).Error
}

// RemoveAsset marks all non-removed rows for objectID as removed and advances their height; rows are never deleted.
func (db *DB) RemoveAsset(objectID *astral.ObjectID) (err error) {
	var rows []dbAsset

	err = db.Where("object_id = ? and removed = false", objectID).Find(&rows).Error
	if err != nil {
		return
	}

	for _, row := range rows {
		row.Removed = true
		row.Height = uint64(db.AssetHeight() + 1)
		err = db.Save(row).Error
		if err != nil {
			return
		}
	}

	return
}

// RemoveAssetByNonce marks the row with the given nonce as removed; if the nonce is unknown it upserts a tombstone so that late-arriving adds are suppressed.
func (db *DB) RemoveAssetByNonce(nonce astral.Nonce, objectID *astral.ObjectID) (err error) {
	var row dbAsset

	err = db.Where("nonce = ?", nonce).First(&row).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return db.Create(&dbAsset{
			Nonce:    nonce,
			Removed:  true,
			ObjectID: objectID,
			Height:   uint64(db.AssetHeight() + 1),
		}).Error

	case err != nil:
		return
	}

	if row.Removed {
		return nil
	}

	row.Removed = true
	row.Height = uint64(db.AssetHeight() + 1)

	return db.Save(row).Error
}

// Assets returns the distinct set of object IDs that have at least one non-removed row.
func (db *DB) Assets() (assets []*astral.ObjectID, err error) {
	err = db.
		Model(&dbAsset{}).
		Where("removed = false").
		Distinct("object_id").
		Find(&assets).
		Error

	return
}

// StoreExpulsion records a ban for (issuer, subject). Bans are append-only and
// irreversible: an existing row for the pair is left untouched.
func (db *DB) StoreExpulsion(signed *user.SignedExpulsion) error {
	sig, err := astral.EncodeBytes(signed.IssuerSig)
	if err != nil {
		return err
	}

	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&dbExpulsion{
		IssuerID:   signed.Issuer,
		SubjectID:  signed.Subject,
		ExpelledAt: signed.ExpelledAt.Time(),
		IssuerSig:  sig,
	}).Error
}

// ExpelledSubjects returns the subjects banned by issuer.
func (db *DB) ExpelledSubjects(issuer *astral.Identity) (subjects []*astral.Identity, err error) {
	var rows []dbExpulsion
	err = db.Select("subject_id").Where("issuer_id = ?", issuer).Find(&rows).Error
	if err != nil {
		return
	}

	for _, row := range rows {
		subjects = append(subjects, row.SubjectID)
	}
	return
}

// Expulsions rebuilds the signed bans issued by issuer from their stored columns.
func (db *DB) Expulsions(issuer *astral.Identity) (list []*user.SignedExpulsion, err error) {
	var rows []dbExpulsion
	err = db.Where("issuer_id = ?", issuer).Find(&rows).Error
	if err != nil {
		return
	}

	for _, row := range rows {
		sig, decodeErr := astral.DecodeAs[*crypto.Signature](row.IssuerSig)
		if decodeErr != nil {
			continue
		}

		list = append(list, &user.SignedExpulsion{
			Expulsion: &user.Expulsion{
				Issuer:     row.IssuerID,
				Subject:    row.SubjectID,
				ExpelledAt: astral.Time(row.ExpelledAt),
			},
			IssuerSig: sig,
		})
	}
	return
}
