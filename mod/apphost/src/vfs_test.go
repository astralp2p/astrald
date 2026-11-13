package apphost

import (
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/vfs"
	vfssrc "github.com/astralp2p/astrald/mod/vfs/src"
	"github.com/astralp2p/astrald/resources"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// recordingVFS records every identity it is asked to create a VFS for.
type recordingVFS struct {
	vfs.Module
	mu  sync.Mutex
	ids []*astral.Identity
}

func (r *recordingVFS) CreateVFS(id *astral.Identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	return nil
}

// vfsAssets answers the module's database and no config file.
type vfsAssets struct {
	assets.Assets
	db *gorm.DB
}

func (a vfsAssets) Database() *gorm.DB { return a.db }

func (vfsAssets) LoadYAML(string, interface{}) error { return resources.ErrNotFound }

// realVFSRegistrar is a registrar over the real vfs module and an in-memory database of its own. It
// returns the database so a test reads what the module stored.
func realVFSRegistrar(t *testing.T) (*Module, *gorm.DB) {
	t.Helper()

	mod := serveAppsRegistrar(t)

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// note: every connection to :memory: opens its own database, so the pool holds one.
	sqlDB.SetMaxOpenConns(1)

	loaded, err := vfssrc.Loader{}.Load(mod.node, vfsAssets{db: gdb}, log.New(nil))
	if err != nil {
		t.Fatalf("load vfs: %v", err)
	}
	mod.VFS = loaded.(vfs.Module)

	return mod, gdb
}

// volume is the row of vfs__volumes that vfs.statfs reports Quota and InodeLimit from.
type volume struct {
	ByteLimit  int64
	InodeLimit int64
}

// findVolumes returns the volumes of vfs__volumes stored for id.
func findVolumes(t *testing.T, gdb *gorm.DB, id *astral.Identity) []volume {
	t.Helper()

	var rows []volume
	if err := gdb.Table(vfs.DBPrefix+"volumes").Where("identity = ?", id).Find(&rows).Error; err != nil {
		t.Fatalf("find volumes: %v", err)
	}
	return rows
}

func countVolumes(t *testing.T, gdb *gorm.DB) int64 {
	t.Helper()

	var n int64
	if err := gdb.Table(vfs.DBPrefix + "volumes").Count(&n).Error; err != nil {
		t.Fatalf("count volumes: %v", err)
	}
	return n
}

func registerApp(t *testing.T, mod *Module) {
	t.Helper()

	w := newRecordingWriter()
	if err := register(t, mod, &apphost.RegisterRequest{}, w); err != nil {
		t.Fatalf("apphost.register refused: %v", err)
	}
	awaitServeAppsClose(t, w)
}

// TestRegisterCreatesTheVFSWithTheDefaultQuotas: the new app has a VFS whose stored limits are the
// ones vfs.statfs reports, 1 GiB and 1,000,000.
func TestRegisterCreatesTheVFSWithTheDefaultQuotas(t *testing.T) {
	mod, gdb := realVFSRegistrar(t)

	registerApp(t, mod)

	tokens := issuedTokens(t, mod)
	if len(tokens) != 1 {
		t.Fatalf("issued %d tokens; want 1", len(tokens))
	}

	rows := findVolumes(t, gdb, tokens[0].Identity)
	if len(rows) != 1 {
		t.Fatalf("app %v has %d VFSes; want 1", tokens[0].Identity, len(rows))
	}
	if rows[0].ByteLimit != 1<<30 || rows[0].InodeLimit != 1_000_000 {
		t.Fatalf("limits %d bytes, %d entries; want %d and 1000000", rows[0].ByteLimit, rows[0].InodeLimit, 1<<30)
	}
}

// TestRegisterTwiceIsHarmless: a second registration of the same identity leaves its one VFS as it
// is, and a second app gets a VFS of its own.
func TestRegisterTwiceIsHarmless(t *testing.T) {
	mod, gdb := realVFSRegistrar(t)

	registerApp(t, mod)
	app := issuedTokens(t, mod)[0].Identity

	if err := mod.VFS.CreateVFS(app); err != nil {
		t.Fatalf("CreateVFS for an existing VFS: %v", err)
	}
	if n := len(findVolumes(t, gdb, app)); n != 1 {
		t.Fatalf("app %v has %d VFSes after a repeated create; want 1", app, n)
	}

	registerApp(t, mod)
	if n := countVolumes(t, gdb); n != 2 {
		t.Fatalf("two registrations left %d VFSes; want 2", n)
	}
}

// TestRegisterCreatesTheVFSForTheTokenIdentity: the call carries the identity the token names.
func TestRegisterCreatesTheVFSForTheTokenIdentity(t *testing.T) {
	mod := serveAppsRegistrar(t)
	rec := &recordingVFS{}
	mod.VFS = rec

	registerApp(t, mod)

	tokens := issuedTokens(t, mod)
	if len(tokens) != 1 || len(rec.ids) != 1 || !rec.ids[0].IsEqual(tokens[0].Identity) {
		t.Fatalf("created VFSes for %v; want the token identity of %v", rec.ids, tokens)
	}
}

// TestProvisionAppCreatesNoVFS: the manager app, provisioned through ProvisionApp, gets no VFS.
func TestProvisionAppCreatesNoVFS(t *testing.T) {
	mod, gdb := realVFSRegistrar(t)

	if _, err := mod.ProvisionApp(astral.NewContext(nil), nil, nil); err != nil {
		t.Fatalf("ProvisionApp: %v", err)
	}

	if n := countVolumes(t, gdb); n != 0 {
		t.Fatalf("ProvisionApp left %d VFSes; want none", n)
	}
}

func vfsPermit(action string, vfsIDs ...*astral.Identity) *auth.Permit {
	permit := &auth.Permit{Action: astral.String8(action)}

	if len(vfsIDs) > 0 {
		permit.Constraints = astral.NewBundle()
		for _, id := range vfsIDs {
			if err := permit.Constraints.Append(id); err != nil {
				panic(err)
			}
		}
	}

	return permit
}

// TestVFSGrantsAuthorizeTheGrantee: a node-local grant of each vfs action is accepted and
// authorizes its grantee for what it lists, through the shim of the action.
func TestVFSGrantsAuthorizeTheGrantee(t *testing.T) {
	mod := testGrantModule(t)
	app, target, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	permits := []*auth.Permit{
		vfsPermit(vfsapi.ReadAction{}.ObjectType(), target),
		vfsPermit(vfsapi.WriteAction{}.ObjectType(), target),
		vfsPermit(vfsapi.ManageAction{}.ObjectType()),
	}
	for _, p := range permits {
		if err := validatePermits([]*auth.Permit{p}); err != nil {
			t.Fatalf("permit %v refused: %v", p.Action, err)
		}
		if err := mod.Grant(app, p, nil); err != nil {
			t.Fatalf("grant %v: %v", p.Action, err)
		}
	}

	read := func(actor, vfsID *astral.Identity) bool {
		return mod.AuthorizeVFSRead(nil, &vfsapi.ReadAction{Action: auth.NewAction(actor), VFSID: vfsID})
	}
	write := func(actor, vfsID *astral.Identity) bool {
		return mod.AuthorizeVFSWrite(nil, &vfsapi.WriteAction{Action: auth.NewAction(actor), VFSID: vfsID})
	}
	manage := func(actor *astral.Identity) bool {
		return mod.AuthorizeVFSManage(nil, &vfsapi.ManageAction{Action: auth.NewAction(actor)})
	}

	switch {
	case !read(app, target):
		t.Error("a read grant was refused to its grantee for a listed VFS")
	case read(app, other):
		t.Error("a read grant was honoured for a VFS it does not list")
	case read(other, target):
		t.Error("a read grant was honoured for another identity")
	case !write(app, target):
		t.Error("a write grant was refused to its grantee for a listed VFS")
	case write(app, other):
		t.Error("a write grant was honoured for a VFS it does not list")
	case !manage(app):
		t.Error("a manage grant was refused to its grantee")
	case manage(other):
		t.Error("a manage grant was honoured for another identity")
	}
}

// TestVFSGrantWithoutConstraintsIsRefused: a read or write permit with no constraints is refused
// when the grant is made, and one that was stored anyway authorizes nothing. A constrained manage
// permit authorizes nothing.
func TestVFSGrantWithoutConstraintsIsRefused(t *testing.T) {
	mod := testGrantModule(t)
	app, target := astral.GenerateIdentity(), astral.GenerateIdentity()

	for _, action := range []string{vfsapi.ReadAction{}.ObjectType(), vfsapi.WriteAction{}.ObjectType()} {
		permit := vfsPermit(action)
		if err := validatePermits([]*auth.Permit{permit}); err == nil {
			t.Errorf("an unconstrained %v permit was accepted", action)
		}

		if err := mod.Grant(app, permit, nil); err != nil {
			t.Fatalf("store %v: %v", action, err)
		}
	}

	if mod.AuthorizeVFSRead(nil, &vfsapi.ReadAction{Action: auth.NewAction(app), VFSID: target}) ||
		mod.AuthorizeVFSWrite(nil, &vfsapi.WriteAction{Action: auth.NewAction(app), VFSID: target}) {
		t.Error("a stored unconstrained read or write grant authorized")
	}

	constrained := vfsPermit(vfsapi.ManageAction{}.ObjectType(), target)
	if err := mod.Grant(app, constrained, nil); err != nil {
		t.Fatalf("store manage: %v", err)
	}
	if mod.AuthorizeVFSManage(nil, &vfsapi.ManageAction{Action: auth.NewAction(app)}) {
		t.Error("a constrained manage grant authorized")
	}
}

// vfsOps are the ops of the vfs module that the gate must refuse without a token.
var vfsOps = []string{
	"vfs.ls", "vfs.stat", "vfs.statfs", "vfs.read", "vfs.write", "vfs.mkdir",
	"vfs.move", "vfs.remove", "vfs.touch", "vfs.volumes", "vfs.create", "vfs.set_quota",
}

// TestGateRefusesEveryVFSOpWithoutAToken: a local session without a token reaches no vfs op, in
// any node state, with the default gate.
func TestGateRefusesEveryVFSOpWithoutAToken(t *testing.T) {
	cfg := defaultConfig()

	for _, op := range vfsOps {
		if gateLists(cfg.Gate.Tokenless, op) {
			t.Errorf("the token-less list names %v", op)
		}
		if gateLists(cfg.Gate.States.Unclaimed, op) || gateLists(cfg.Gate.States.Fresh, op) {
			t.Errorf("an unclaimed or fresh list names %v", op)
		}
	}

	ctx := astral.NewContext(nil)
	for _, state := range []string{user.StateFresh, user.StateUnclaimed, user.StateClaimed} {
		mod := stateModule(t, state)
		for _, op := range vfsOps {
			q := &astral.Query{QueryString: astral.String32(op)}
			if mod.gateAdmits(ctx, q, true) {
				t.Errorf("the gate admitted %v to a token-less session in state %v", op, state)
			}
		}
	}
}
