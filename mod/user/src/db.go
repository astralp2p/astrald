package user

import (
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	mod *Module
	*gorm.DB
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
// A row whose signature does not decode is skipped.
func (db *DB) Expulsions(issuer *astral.Identity) (list []*user.SignedExpulsion, err error) {
	var rows []dbExpulsion
	err = db.Where("issuer_id = ?", issuer).Find(&rows).Error
	if err != nil {
		return
	}

	for _, row := range rows {
		signed, decodeErr := row.signed()
		if decodeErr != nil {
			continue
		}
		list = append(list, signed)
	}
	return
}

// ExpulsionFor rebuilds the ban issuer issued on subject. It returns
// gorm.ErrRecordNotFound when issuer has not banned subject.
func (db *DB) ExpulsionFor(issuer, subject *astral.Identity) (*user.SignedExpulsion, error) {
	var row dbExpulsion
	err := db.Where("issuer_id = ? AND subject_id = ?", issuer, subject).First(&row).Error
	if err != nil {
		return nil, err
	}

	return row.signed()
}

// signed rebuilds the SignedExpulsion the row stores.
func (row dbExpulsion) signed() (*user.SignedExpulsion, error) {
	sig, err := astral.DecodeAs[*crypto.Signature](row.IssuerSig)
	if err != nil {
		return nil, err
	}

	return &user.SignedExpulsion{
		Expulsion: &user.Expulsion{
			Issuer:     row.IssuerID,
			Subject:    row.SubjectID,
			ExpelledAt: astral.Time(row.ExpelledAt),
		},
		IssuerSig: sig,
	}, nil
}
