package auth

import (
	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
	"github.com/cryptopunkscc/astral-go/astral/log/theme"
	"github.com/cryptopunkscc/astrald/mod/log/views"
)

type SignedContractView struct {
	*auth.SignedContract
}

func (view SignedContractView) Render() string {
	return fmt.Sprintf(
		"Signed contract (%v -> %v) until %v",
		view.Issuer,
		view.Subject,
		views.NewTimeViewColor(&view.ExpiresAt, "2006-01-02 15:04:05.000", styles.Green.Bri(theme.Less)),
	)
}

func init() {
	fmt.SetView(func(o *auth.SignedContract) fmt.View {
		return &SignedContractView{SignedContract: o}
	})
}
