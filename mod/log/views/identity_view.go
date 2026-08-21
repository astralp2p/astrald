package views

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/theme"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/dir"
)

var IdentityResolver sig.Value[dir.Resolver]

type IdentityView struct {
	Highlight bool
	*astral.Identity
}

func (v IdentityView) Render() string {
	s := theme.Identity
	if v.Highlight {
		s = s.Bri(theme.More)
	}

	if r := IdentityResolver.Get(); r != nil {
		return s.Render(r.DisplayName(v.Identity))
	}

	return s.Render(v.Identity.Fingerprint())
}

func UseIdentityView() {
	fmt.SetView(func(identity *astral.Identity) fmt.View {
		return IdentityView{Identity: identity}
	})
}
