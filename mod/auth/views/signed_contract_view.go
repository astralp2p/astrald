package auth

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral/fmt"
	"github.com/astralp2p/astral-go/astral/log/styles"
	"github.com/astralp2p/astral-go/astral/log/theme"
	"github.com/astralp2p/astrald/mod/log/views"
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
