package auth

import (
	"fmt"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/crypto"
)

type contractQuery struct {
	*DB
	issuer  *astral.Identity
	subject *astral.Identity
	actions []string
}

func (q *contractQuery) WithIssuer(id *astral.Identity) auth.ContractQueryBuilder {
	q.issuer = id
	return q
}

func (q *contractQuery) WithSubject(id *astral.Identity) auth.ContractQueryBuilder {
	q.subject = id
	return q
}

func (q *contractQuery) WithAction(actions ...astral.Object) auth.ContractQueryBuilder {
	for _, a := range actions {
		q.actions = append(q.actions, a.ObjectType())
	}

	return q
}

func (q *contractQuery) Find(ctx *astral.Context) ([]*auth.SignedContract, error) {
	rows, err := q.DB.findActiveContracts(q)
	if err != nil {
		return nil, err
	}

	var result []*auth.SignedContract
	for _, row := range rows {
		dbPermits, err := q.DB.findContractPermits(row.ObjectID)
		if err != nil {
			return nil, err
		}

		var permits []*auth.Permit
		for _, permit := range dbPermits {
			permit, err := toPermit(permit)
			if err != nil {
				return nil, err
			}
			permits = append(permits, permit)
		}

		issuerSig, err := astral.DecodeAs[*crypto.Signature](row.IssuerSig)
		if err != nil {
			return nil, fmt.Errorf("decode issuer signature: %w", err)
		}

		subjectSig, err := astral.DecodeAs[*crypto.Signature](row.SubjectSig)
		if err != nil {
			return nil, fmt.Errorf("decode subject signature: %w", err)
		}

		result = append(result, &auth.SignedContract{
			Contract: &auth.Contract{
				Issuer:    row.IssuerID,
				Subject:   row.SubjectID,
				ExpiresAt: astral.Time(row.ExpiresAt),
				Permits:   permits,
			},
			IssuerSig:  issuerSig,
			SubjectSig: subjectSig,
		})
	}

	return result, nil
}
