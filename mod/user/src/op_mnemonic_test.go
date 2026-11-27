package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// memDB is the user module database in memory.
func memDB() *DB {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbExpulsion{}, &dbMnemonic{}); err != nil {
		panic(err)
	}
	return db
}

func (o *memObjects) ReadDefault() objectsmod.Repository { return nil }

func (o *memObjects) Load(_ *astral.Context, _ objectsmod.Repository, id *astral.ObjectID) (astral.Object, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, obj := range o.stored {
		if oid, _ := astral.ResolveObjectID(obj); oid.IsEqual(id) {
			return obj, nil
		}
	}
	return nil, objectsmod.ErrNotFound
}

func (o *memObjects) DeleteUnheld(_ *astral.Context, _ objectsmod.Repository, id *astral.ObjectID, _ time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, obj := range o.stored {
		if oid, _ := astral.ResolveObjectID(obj); oid.IsEqual(id) {
			o.stored = append(o.stored[:i], o.stored[i+1:]...)
			return nil
		}
	}
	return objectsmod.ErrNotFound
}

// bootstrapped is a node claimed with input under the query string qs, and
// the words the claim used.
func bootstrapped(t *testing.T, qs string, input astral.Object) (*bootstrapFixture, *user.BootstrapResult, string) {
	t.Helper()
	f := newBootstrapFixture()
	objs, err := routeBootstrap(t, f, f.nodeID, "", qs, input)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	result := onlyResult(t, objs)
	words := bootstrapWords
	if result.Mnemonic != "" {
		words = string(result.Mnemonic)
	}
	return f, result, words
}

func routeMnemonic(t *testing.T, f *bootstrapFixture, fn any, op string, caller *astral.Identity, origin string) astral.Object {
	t.Helper()
	objs, err := routeOp(t, fn, query.New(caller, f.nodeID, op, nil), origin, nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("the op answered %d objects (%v); want one", len(objs), objs)
	}
	return objs[0]
}

func wantWords(t *testing.T, obj astral.Object, words string) {
	t.Helper()
	m, ok := obj.(*user.Mnemonic)
	if !ok {
		t.Fatalf("the op answered %v; want a mod.user.mnemonic", obj)
	}
	if string(m.Words) != words {
		t.Fatalf("words %q; want %q", m.Words, words)
	}
}

func wantError(t *testing.T, obj astral.Object, want string) {
	t.Helper()
	e, ok := obj.(astral.Error)
	if !ok || e.Error() != want {
		t.Fatalf("the op answered %v; want error %q", obj, want)
	}
}

// TestMnemonicKeptForSuppliedAndGeneratedWords: user.bootstrap keeps the words
// it claimed with, given or generated, and the user reads them back.
func TestMnemonicKeptForSuppliedAndGeneratedWords(t *testing.T) {
	cases := map[string]struct {
		qs    string
		input astral.Object
	}{
		"supplied":  {"user.bootstrap", astral.NewString16(bootstrapWords)},
		"generated": {"user.bootstrap?generate=true", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, result, words := bootstrapped(t, c.qs, c.input)
			got := routeMnemonic(t, f, f.mod.OpMnemonic, user.OpMnemonic, result.Identity, astral.OriginLocal)
			wantWords(t, got, words)
		})
	}
}

// TestMnemonicManagerReads: the manager reads the user's words.
func TestMnemonicManagerReads(t *testing.T) {
	f, _, words := bootstrapped(t, "user.bootstrap", astral.NewString16(bootstrapWords))
	f.apphost.manager = astral.GenerateIdentity()
	got := routeMnemonic(t, f, f.mod.OpMnemonic, user.OpMnemonic, f.apphost.manager, "")
	wantWords(t, got, words)
}

// TestMnemonicRefuses: another caller and a network origin are refused, by
// both ops, and the words stay.
func TestMnemonicRefuses(t *testing.T) {
	f, result, words := bootstrapped(t, "user.bootstrap", astral.NewString16(bootstrapWords))
	ops := map[string]any{user.OpMnemonic: f.mod.OpMnemonic, user.OpForgetMnemonic: f.mod.OpForgetMnemonic}
	for op, fn := range ops {
		got := routeMnemonic(t, f, fn, op, astral.GenerateIdentity(), "")
		wantError(t, got, "not permitted")
		got = routeMnemonic(t, f, fn, op, result.Identity, astral.OriginNetwork)
		if _, ok := got.(astral.Error); !ok {
			t.Fatalf("%s from the network answered %v; want an error", op, got)
		}
	}
	wantWords(t, routeMnemonic(t, f, f.mod.OpMnemonic, user.OpMnemonic, result.Identity, ""), words)
}

// TestForgetMnemonic: after user.forget_mnemonic the words are gone from the
// repository and user.mnemonic answers ErrNoMnemonic.
func TestForgetMnemonic(t *testing.T) {
	f, result, _ := bootstrapped(t, "user.bootstrap", astral.NewString16(bootstrapWords))

	got := routeMnemonic(t, f, f.mod.OpForgetMnemonic, user.OpForgetMnemonic, result.Identity, "")
	if _, ok := got.(*astral.Ack); !ok {
		t.Fatalf("forget answered %v; want ack", got)
	}
	for _, obj := range f.objects.objects() {
		if _, ok := obj.(*user.Mnemonic); ok {
			t.Fatal("the mnemonic is still stored")
		}
	}
	got = routeMnemonic(t, f, f.mod.OpMnemonic, user.OpMnemonic, result.Identity, "")
	wantError(t, got, user.ErrNoMnemonic.Error())
}
