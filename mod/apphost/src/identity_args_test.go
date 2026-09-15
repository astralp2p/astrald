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

// TestGrantOpResolvesAnAlias: -identity takes a name, and the grant lands on
// the identity the name resolves to.
func TestGrantOpResolvesAnAlias(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()
	mod.Dir = &namingDir{aliases: map[string]*astral.Identity{"scout": app}}
	action := auth.ServeObjectsAction{}.ObjectType()

	w := newRecordingWriter()
	err := route(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant?identity=scout&action="+action, w)
	if err != nil {
		t.Fatalf("apphost.grant refused an authorized caller: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("the aliased identity holds %d grants; want 1", len(grants))
	}
}

// TestGrantOpRecordsNothingForAnUnusableIdentity: the zero identity and an
// unresolved name are answered with an error, and no grant is written.
func TestGrantOpRecordsNothingForAnUnusableIdentity(t *testing.T) {
	action := auth.ServeObjectsAction{}.ObjectType()

	for _, name := range []string{"", "anyone", "nobody"} {
		t.Run("identity="+name, func(t *testing.T) {
			mod := testGrantModule(t)
			mod.Auth = &recordingAuth{verdict: true}

			w := newRecordingWriter()
			err := route(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant?identity="+name+"&action="+action, w)
			if err != nil {
				t.Fatalf("apphost.grant rejected instead of answering: %v", err)
			}
			waitAdminManageAppsAnswer(t, w)

			if w.written() == 0 {
				t.Fatal("apphost.grant answered nothing")
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

// TestGrantOpRefusesTheOldArgumentName: -id is not an alias of -identity, so a
// query naming only -id lacks the required argument.
func TestGrantOpRefusesTheOldArgumentName(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()
	action := auth.ServeObjectsAction{}.ObjectType()

	err := route(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant?id="+app.String()+"&action="+action, newRecordingWriter())

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("apphost.grant answered a query without -identity: got err %v, want a rejection", err)
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
		if _, err := mod.CreateAccessToken(id, DefaultTokenDuration); err != nil {
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
