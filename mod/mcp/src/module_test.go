package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	dirmod "github.com/astralp2p/astrald/mod/dir"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testLogger emits nothing: a test asserting on an answer has no use for the
// module's log lines.
func testLogger() *log.Logger {
	l := log.New(astral.GenerateIdentity())
	l.SetFilter(func(*log.Entry) bool { return false })
	return l
}

// testQueryModule is the module as a declared tool sees it: the caps and the
// window, and no dependency.
func testQueryModule(t *testing.T) *Module {
	t.Helper()
	return &Module{
		ctx: astral.NewContext(nil),
		config: Config{
			QueryTimeout:       time.Second,
			MaxResponseBytes:   64 << 10,
			MaxResponseObjects: 64,
		},
	}
}

// testAgentModule is the module over an agent store and a messaging module
// that answers as the test says.
func testAgentModule(t *testing.T) (*Module, *fakeMessaging) {
	t.Helper()

	msg := &fakeMessaging{}
	mod := &Module{
		ctx:    astral.NewContext(nil),
		db:     testDB(t),
		config: defaultConfig,
		log:    testLogger(),
	}
	mod.Messaging = msg
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{}}

	return mod, msg
}

// testDB opens an empty agent store.
func testDB(t *testing.T) *DB {
	t.Helper()

	db := &DB{DB: emptyStore(t)}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// emptyStore opens an in-memory store with no table in it.
//
// why the pool is capped at one connection: an in-memory sqlite database
// belongs to the connection that opened it, so a second pooled connection is a
// second, empty database.
func emptyStore(t *testing.T) *gorm.DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	pool, err := gdb.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	pool.SetMaxOpenConns(1)

	return gdb
}

// fakeMessaging records what the module asks of messaging and answers from its
// fields.
//
// why the interface is embedded rather than implemented: a method this reaches
// without a stub panics, which is the report that mcp's use of messaging grew.
type fakeMessaging struct {
	messagingmod.Module

	mu sync.Mutex

	// callers are the identities every mail call was made as, in order.
	callers []*astral.Identity

	createAlias    string
	createDuration astral.Duration
	sendReq        *messaging.SendMessageRequest
	listReq        messaging.ListMessagesRequest
	readReq        *messaging.ReadMessagesRequest
	waitReq        messaging.WaitRequest
	archiveRef     messaging.MessageRef
	archiveUndo    bool
	deleted        []*astral.Identity

	sent    messaging.MessageID
	listed  []*messaging.Envelope
	read    *messaging.ReadMessagesResult
	waited  *messaging.WaitResult
	changed bool
	cred    *messaging.IdentityCredential

	// deleteErr is what DeleteIdentity answers.
	deleteErr error
}

// called records one mail call as id and runs record under the same lock.
func (f *fakeMessaging) called(id *astral.Identity, record func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callers = append(f.callers, id)
	record()
}

func (f *fakeMessaging) CreateIdentity(_ *astral.Context, alias string, duration astral.Duration) (*messaging.IdentityCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createAlias, f.createDuration = alias, duration

	if f.cred == nil {
		return nil, errors.New("no credential")
	}
	return f.cred, nil
}

func (f *fakeMessaging) DeleteIdentity(_ *astral.Context, id *astral.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return f.deleteErr
}

func (f *fakeMessaging) SendMessage(_ context.Context, sender *astral.Identity, req *messaging.SendMessageRequest) (messaging.MessageID, error) {
	f.called(sender, func() { f.sendReq = req })
	return f.sent, nil
}

func (f *fakeMessaging) ListMessages(_ context.Context, owner *astral.Identity, req messaging.ListMessagesRequest) ([]*messaging.Envelope, error) {
	f.called(owner, func() { f.listReq = req })
	return f.listed, nil
}

func (f *fakeMessaging) ReadMessages(_ context.Context, owner *astral.Identity, req *messaging.ReadMessagesRequest) (*messaging.ReadMessagesResult, error) {
	f.called(owner, func() { f.readReq = req })
	if f.read == nil {
		return &messaging.ReadMessagesResult{}, nil
	}
	return f.read, nil
}

func (f *fakeMessaging) Wait(_ context.Context, owner *astral.Identity, req messaging.WaitRequest, _ messagingmod.ProgressFunc) (*messaging.WaitResult, error) {
	f.called(owner, func() { f.waitReq = req })
	if f.waited == nil {
		return &messaging.WaitResult{}, nil
	}
	return f.waited, nil
}

func (f *fakeMessaging) Archive(_ context.Context, owner *astral.Identity, ref messaging.MessageRef, undo bool) (bool, error) {
	f.called(owner, func() { f.archiveRef, f.archiveUndo = ref, undo })
	return f.changed, nil
}

// checkCalledAs asserts every mail call was made as the identity the tool is
// bound to.
func (f *fakeMessaging) checkCalledAs(t *testing.T, want *astral.Identity) {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.callers) == 0 {
		t.Fatal("messaging was never called")
	}
	for _, got := range f.callers {
		if !got.IsEqual(want) {
			t.Fatalf("messaging was called as %v, want the authenticated agent %v", got, want)
		}
	}
}

// stubApphost authenticates the tokens it holds and nothing else.
type stubApphost struct {
	apphostmod.Module
	tokens map[string]*astral.Identity
}

func (s *stubApphost) AuthenticateToken(token string) (*astral.Identity, error) {
	if id, ok := s.tokens[token]; ok {
		return id, nil
	}
	return nil, errors.New("invalid token")
}

type stubDir struct {
	dirmod.Module
	aliases map[string]*astral.Identity
}

// why the raw form is tried first: the real directory parses an identity
// before it looks in the alias table (mod/dir/src/module.go), so a stub that
// only knew aliases would pass a caller the node would refuse — and fail one
// the node would serve.
func (s *stubDir) ResolveIdentity(name string) (*astral.Identity, error) {
	if id, err := astral.ParseIdentity(name); err == nil {
		return id, nil
	}
	if id, ok := s.aliases[name]; ok {
		return id, nil
	}
	return nil, errors.New("not found")
}
