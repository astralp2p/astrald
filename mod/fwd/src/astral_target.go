package fwd

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
)

type AstralTarget struct {
	template *astral.Query
	router   astral.Router
	label    string
}

func NewAstralTarget(template *astral.Query, router astral.Router, label string) (*AstralTarget, error) {
	return &AstralTarget{
		template: template,
		router:   router,
		label:    label,
	}, nil
}

// RouteQuery clones the query template and assigns a fresh nonce before forwarding.
// why: the template nonce must not be reused across invocations.
func (t *AstralTarget) RouteQuery(ctx *astral.Context, _ *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	var query = *t.template
	query.Nonce = astral.NewNonce()
	return t.router.RouteQuery(ctx, astral.Launch(&query), w)
}

func (t *AstralTarget) String() string {
	return "astral://" + t.label
}
