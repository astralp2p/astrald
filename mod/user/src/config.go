package user

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/tree"
	"github.com/cryptopunkscc/astrald/mod/auth"
)

const (
	minimalContractLength   = time.Hour
	defaultContractValidity = 365 * 24 * time.Hour
)

type Config struct {
	ActiveContract tree.Value[*auth.SignedContract]
}
