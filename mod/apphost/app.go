package apphost

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type App struct {
	AppID       *astral.Identity
	HostID      *astral.Identity
	InstalledAt astral.Time
}

var _ astral.Object = &App{}

func (App) ObjectType() string { return "mod.apphost.app" }

func (a App) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&a).WriteTo(w)
}

func (a *App) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(a).ReadFrom(r)
}

func init() {
	_ = astral.Add(&App{})
}
