package services

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

var _ astral.Object = &Update{}

type Update struct {
	Available  bool
	Name       astral.String8
	ProviderID *astral.Identity
	Info       *astral.Bundle
}

func (s Update) ObjectType() string {
	return "services.update"
}

func (s Update) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *Update) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(s).ReadFrom(r)
}

func init() {
	_ = astral.Add(&Update{})
}
