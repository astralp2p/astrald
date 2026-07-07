package auth

import (
	"fmt"
	authmod "github.com/cryptopunkscc/astrald/mod/auth"
	"time"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct{ *gorm.DB }

func (db *DB) findActiveContracts(q *contractQuery) ([]*dbContract, error) {
	now := time.Now().UTC()
	gq := db.DB.
		Where("starts_at <= ?", now).
		Where("expires_at > ?", now)

	if q.issuer != nil {
		gq = gq.Where("issuer_id = ?", q.issuer)
	}

	if q.subject != nil {
		gq = gq.Where("subject_id = ?", q.subject)
	}

	if len(q.actions) > 0 {
		gq = gq.
			Joins("JOIN "+authmod.DBPrefix+"contract_permits ON "+authmod.DBPrefix+"contract_permits.object_id = "+authmod.DBPrefix+"contracts.object_id").
			Where(authmod.DBPrefix+"contract_permits.name IN ?", q.actions).
			Distinct(authmod.DBPrefix + "contracts.*")
	}

	var rows []*dbContract
	return rows, gq.Find(&rows).Error
}

func (db *DB) findContractPermits(objectID *astral.ObjectID) ([]*dbContractPermit, error) {
	var rows []*dbContractPermit
	return rows, db.Where("object_id = ?", objectID).Order("id").Find(&rows).Error
}

func (db *DB) contractExists(objectID *astral.ObjectID) bool {
	var row dbContract
	err := db.Where("object_id = ?", objectID).Take(&row).Error
	return err == nil && len(row.IssuerSig) > 0 && len(row.SubjectSig) > 0
}

func (db *DB) activeContractExists(objectID *astral.ObjectID) (exists bool, err error) {
	now := time.Now().UTC()
	err = db.
		Model(&dbContract{}).
		Where("object_id = ?", objectID).
		Where("starts_at <= ?", now).
		Where("expires_at > ?", now).
		Select("count(*) > 0").
		First(&exists).
		Error
	return
}

func (db *DB) storeSignedContract(sc *auth.SignedContract) error {
	objectID, err := astral.ResolveObjectID(sc)
	if err != nil {
		return fmt.Errorf("resolve object id: %w", err)
	}

	issuerSig, err := encodeSignature(sc.IssuerSig)
	if err != nil {
		return fmt.Errorf("encode issuer signature: %w", err)
	}

	subjectSig, err := encodeSignature(sc.SubjectSig)
	if err != nil {
		return fmt.Errorf("encode subject signature: %w", err)
	}

	row := &dbContract{
		ObjectID:   objectID,
		IssuerID:   sc.Issuer,
		SubjectID:  sc.Subject,
		IssuerSig:  issuerSig,
		SubjectSig: subjectSig,
		ExpiresAt:  sc.ExpiresAt.Time(),
	}
	return db.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "object_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"issuer_id",
				"subject_id",
				"issuer_sig",
				"subject_sig",
				"expires_at",
			}),
		}).Create(row).Error
		if err != nil {
			return err
		}

		if len(sc.Permits) == 0 {
			return nil
		}

		var count int64
		err = tx.Model(&dbContractPermit{}).Where("object_id = ?", row.ObjectID).Count(&count).Error
		if err != nil {
			return err
		}
		if count > 0 {
			return nil
		}

		for _, p := range sc.Permits {
			permit, err := fromPermit(row.ObjectID, p)
			if err != nil {
				return err
			}
			err = tx.Create(permit).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
}
