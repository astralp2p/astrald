package dir

import (
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// TestSetAliasResolvesItsIdentity: -identity takes a name the directory
// resolves, so an existing alias renames the identity it points at.
func TestSetAliasResolvesItsIdentity(t *testing.T) {
	mod := newConfigureNodeStateDir(t, &recordingAuth{verdict: true})
	subject := astral.GenerateIdentity()
	if err := mod.SetAlias(subject, "alice"); err != nil {
		t.Fatalf("set alias: %v", err)
	}

	answerConfigureNodeState(t, mod, astral.GenerateIdentity(), configureNodeStateOp{"dir.set_alias by alias", "dir.set_alias?identity=alice&alias=bob"})

	if alias, err := mod.GetAlias(subject); err != nil || alias != "bob" {
		t.Fatalf("alias after set is %q (err %v); want bob", alias, err)
	}
}

// TestSetAliasRefusesTheZeroIdentity: the empty name and "anyone" resolve to the
// zero identity, which holds no alias.
func TestSetAliasRefusesTheZeroIdentity(t *testing.T) {
	for _, name := range []string{"", "anyone"} {
		t.Run("identity="+name, func(t *testing.T) {
			mod := newConfigureNodeStateDir(t, &recordingAuth{verdict: true})

			answerConfigureNodeState(t, mod, astral.GenerateIdentity(), configureNodeStateOp{"dir.set_alias zero", "dir.set_alias?identity=" + name + "&alias=ghost"})

			var n int64
			if err := mod.db.Model(&dbAlias{}).Count(&n).Error; err != nil {
				t.Fatalf("count aliases: %v", err)
			}
			if n != 0 {
				t.Fatalf("the alias table holds %d rows; want 0", n)
			}
		})
	}
}

// TestGetAliasResolvesItsIdentity: dir.get_alias answers the alias for a hex
// identity and for a name, and answers the zero identity with an error.
func TestGetAliasResolvesItsIdentity(t *testing.T) {
	mod := newConfigureNodeStateDir(t, &recordingAuth{verdict: true})
	subject := astral.GenerateIdentity()
	if err := mod.SetAlias(subject, "alice"); err != nil {
		t.Fatalf("set alias: %v", err)
	}

	cases := []struct {
		name string
		want string
	}{
		{subject.String(), "alice"},
		{"alice", "alice"},
		{"anyone", ""},
	}

	for _, c := range cases {
		t.Run("identity="+c.name, func(t *testing.T) {
			answer := receiveFirst(t, mod.OpGetAlias, "dir.get_alias?identity="+c.name)

			switch answer := answer.(type) {
			case *astral.String8:
				if c.want == "" || string(*answer) != c.want {
					t.Fatalf("dir.get_alias answered %q; want %q", *answer, c.want)
				}
			case *astral.ErrorMessage:
				if c.want != "" {
					t.Fatalf("dir.get_alias answered error %v; want %q", answer, c.want)
				}
			default:
				t.Fatalf("dir.get_alias answered %T", answer)
			}
		})
	}
}

// receiveFirst routes one query and decodes the first object the op answers.
func receiveFirst(t *testing.T, fn any, queryString string) astral.Object {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	r, w := io.Pipe()
	defer r.Close()

	caller := astral.GenerateIdentity()
	if _, err = op.RouteQuery(astral.NewContext(nil), astral.Launch(query.New(caller, caller, queryString, nil)), w); err != nil {
		t.Fatalf("%s rejected: %v", queryString, err)
	}

	answers := make(chan astral.Object, 1)
	go func() {
		object, _ := channel.NewReceiver(r).Receive()
		answers <- object
	}()

	select {
	case object := <-answers:
		return object
	case <-time.After(10 * time.Second):
		t.Fatalf("%s answered nothing", queryString)
		return nil
	}
}
