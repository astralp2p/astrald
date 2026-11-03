package apphost

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	dirmod "github.com/astralp2p/astrald/mod/dir"
)

// namingDir resolves names in the order mod/dir/src/module.go does: the empty
// name and "anyone" to the zero identity, then a hex identity, then an alias.
// Every other method panics.
type namingDir struct {
	dirmod.Module
	aliases map[string]*astral.Identity
}

func (d *namingDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if name == "" || name == "anyone" {
		return &astral.Identity{}, nil
	}
	if id, err := astral.ParseIdentity(name); err == nil {
		return id, nil
	}
	if id, ok := d.aliases[name]; ok {
		return id, nil
	}
	return nil, errors.New("unknown identity: " + name)
}

// TestGrantOpRecordsNothingForAnUnusableIdentity: a grant_request with no
// identity or the zero identity is answered with an error, and no grant is
// written.
func TestGrantOpRecordsNothingForAnUnusableIdentity(t *testing.T) {
	action := auth.ServeObjectsAction{}.ObjectType()

	for name, identity := range map[string]*astral.Identity{"absent": nil, "zero": {}} {
		t.Run("identity "+name, func(t *testing.T) {
			mod := testGrantModule(t)
			mod.Auth = &recordingAuth{verdict: true}

			w := newRecordingWriter()
			req := &apphost.GrantRequest{Identity: identity, Permits: permits(action)}
			if err := routeSending(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant", req, w); err != nil {
				t.Fatalf("apphost.grant rejected instead of answering: %v", err)
			}
			if got, want := readReply(t, w), "missing identity"; got != want {
				t.Fatalf("answered %q; want %q", got, want)
			}

			var n int64
			if err := mod.db.Model(&dbGrant{}).Count(&n).Error; err != nil {
				t.Fatalf("count grants: %v", err)
			}
			if n != 0 {
				t.Fatalf("the node holds %d grants; want 0", n)
			}
		})
	}
}

// TestListTokensFiltersByIdentity: an omitted identity lists every token, a
// named identity lists its own, and the zero identity is refused.
func TestListTokensFiltersByIdentity(t *testing.T) {
	mod := testTokenModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	holder, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod.Dir = &namingDir{aliases: map[string]*astral.Identity{"holder": holder}}

	for _, id := range []*astral.Identity{holder, other} {
		if _, err := mod.CreateAccessToken(id, apphostmod.DefaultTokenDuration); err != nil {
			t.Fatalf("create token: %v", err)
		}
	}

	cases := []struct {
		query string
		want  int
	}{
		{"apphost.list_tokens", 2},
		{"apphost.list_tokens?identity=holder", 1},
		{"apphost.list_tokens?identity=" + other.String(), 1},
		{"apphost.list_tokens?identity=anyone", 0},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			tokens, errs := collectAnswers(t, mod.OpListTokens, c.query)
			if len(tokens) != c.want {
				t.Fatalf("%s listed %d tokens; want %d", c.query, len(tokens), c.want)
			}
			if c.want == 0 && errs == 0 {
				t.Fatalf("%s answered no error for the zero identity", c.query)
			}
		})
	}
}

// collectAnswers routes one query from an authorized caller and reads the
// stream to its end: the access tokens it lists and the errors it answers.
func collectAnswers(t *testing.T, fn any, queryString string) (tokens []*apphost.AccessToken, errs int) {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	r, w := io.Pipe()
	defer r.Close()

	caller := astral.GenerateIdentity()
	if _, err = op.RouteQuery(astral.NewContext(nil), astral.Launch(query.New(caller, caller, queryString, nil)), w); err != nil {
		t.Fatalf("%s refused an authorized caller: %v", queryString, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rcv := channel.NewReceiver(r)
		for {
			object, err := rcv.Receive()
			if err != nil {
				return
			}
			switch object := object.(type) {
			case *apphost.AccessToken:
				tokens = append(tokens, object)
			case *astral.ErrorMessage:
				errs++
				return
			case *astral.EOS:
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never ended its stream", queryString)
	}
	return tokens, errs
}
