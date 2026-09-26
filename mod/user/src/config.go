package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
)

type Config struct {
	ActiveContract tree.Value[*auth.SignedContract]
}
