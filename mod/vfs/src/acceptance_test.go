package vfs_test

import (
	"bytes"
	"testing"
	"time"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	cryptoapi "github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// expectError fails unless a call answered the error want.
func expectError(t *testing.T, what string, answer astral.Object, err error, want error) {
	t.Helper()

	if err != nil {
		t.Errorf("%v: %v; want the answer %q", what, err, want)
		return
	}
	got, ok := answer.(astral.Error)
	if !ok || got.Error() != want.Error() {
		t.Errorf("%v answered %#v; want %q", what, answer, want)
	}
}

// expectEntry fails unless a call answered an entry.
func expectEntry(t *testing.T, what string, answer astral.Object, err error) *vfsapi.Entry {
	t.Helper()

	entry, ok := answer.(*vfsapi.Entry)
	if err != nil || !ok {
		t.Fatalf("%v answered %#v, %v; want an entry", what, answer, err)
	}

	return entry
}

// TestAcceptanceAppUsesItsOwnVFSOnly (acceptance criterion 2): an app registered with no permit writes, lists,
// stats, reads, moves, touches and removes files in its own VFS through apphost, and each of those is refused on
// another app's VFS.
func TestAcceptanceAppUsesItsOwnVFSOnly(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app, other := a.registerApp(), a.registerApp()

	a.mustWrite(other.client, "/theirs.txt", []byte("theirs"))
	a.mustWrite(app.client, "/docs/a.txt", []byte("hello"))

	ls := a.mustChannel(app.client, "vfs.ls", query.Args{"path": "/docs"})
	listed, err := ls.Receive()
	if e := expectEntry(t, "vfs.ls", listed, err); e.Name != "a.txt" {
		t.Errorf("vfs.ls listed %q", e.Name)
	}
	if eos, err := ls.Receive(); err != nil {
		t.Errorf("vfs.ls: %v", err)
	} else if _, ok := eos.(*astral.EOS); !ok {
		t.Errorf("vfs.ls answered %#v after the entry; want eos", eos)
	}

	stat, err := a.call(app.client, "vfs.stat", query.Args{"path": "/docs/a.txt"})
	expectEntry(t, "vfs.stat", stat, err)

	content, err := a.readRaw(app.client, "vfs.read", query.Args{"path": "/docs/a.txt"})
	if err != nil || string(content) != "hello" {
		t.Errorf("vfs.read = %q, %v; want hello", content, err)
	}

	moved, err := a.call(app.client, "vfs.move", query.Args{"from": "/docs/a.txt", "to": "/docs/b.txt"})
	expectEntry(t, "vfs.move", moved, err)

	touched, err := a.call(app.client, "vfs.touch", query.Args{"path": "/docs/b.txt"})
	expectEntry(t, "vfs.touch", touched, err)

	removed, err := a.call(app.client, "vfs.remove", query.Args{"path": "/docs/b.txt"})
	if _, ok := removed.(*astral.Ack); err != nil || !ok {
		t.Errorf("vfs.remove answered %#v, %v; want ack", removed, err)
	}

	expectRefusedOnOtherVFS(t, a, app, other)
}

// expectRefusedOnOtherVFS checks that each of the seven operations of app on the VFS of other is refused and
// leaves the other VFS as it was.
func expectRefusedOnOtherVFS(t *testing.T, a *acceptanceNode, app, other *acceptanceApp) {
	t.Helper()

	theirs := other.id
	answer, err := a.write(app.client, query.Args{"vfs": theirs, "path": "/mine.txt"}, []byte("x"))
	expectError(t, "vfs.write on another VFS", answer, err, vfsapi.ErrNotPermitted)

	refusals := map[string]query.Args{
		"vfs.ls":     {"vfs": theirs, "path": "/"},
		"vfs.stat":   {"vfs": theirs, "path": "/theirs.txt"},
		"vfs.move":   {"vfs": theirs, "from": "/theirs.txt", "to": "/moved.txt"},
		"vfs.touch":  {"vfs": theirs, "path": "/theirs.txt"},
		"vfs.remove": {"vfs": theirs, "path": "/theirs.txt"},
	}
	for method, args := range refusals {
		answer, err := a.call(app.client, method, args)
		expectError(t, method+" on another VFS", answer, err, vfsapi.ErrNotPermitted)
	}

	// note: vfs.read answers raw bytes, so it refuses by rejecting the query.
	if content, err := a.readRaw(app.client, "vfs.read", query.Args{"vfs": theirs, "path": "/theirs.txt"}); err == nil {
		t.Errorf("vfs.read on another VFS answered %q; want a rejection", content)
	}

	content, err := a.readRaw(other.client, "vfs.read", query.Args{"path": "/theirs.txt"})
	if err != nil || !bytes.Equal(content, []byte("theirs")) {
		t.Errorf("the other VFS's file reads %q, %v after the refusals; want theirs", content, err)
	}
	if answer, err := a.call(other.client, "vfs.stat", query.Args{"path": "/mine.txt"}); err == nil {
		expectError(t, "vfs.stat of a refused write", answer, nil, vfsapi.ErrNotFound)
	}
}

// TestAcceptanceReadByObjectIDFollowsTheVFS (acceptance criterion 3): an app reads its own file with objects.read
// by ObjectID and is refused for a file of another VFS; objects.describe and objects.find are refused for its own
// file.
func TestAcceptanceReadByObjectIDFollowsTheVFS(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app, other := a.registerApp(), a.registerApp()

	mine := a.mustWrite(app.client, "/mine.txt", []byte("mine"))
	theirs := a.mustWrite(other.client, "/theirs.txt", []byte("theirs"))

	content, err := a.readRaw(app.client, "objects.read", query.Args{"id": &mine.ID})
	if err != nil || string(content) != "mine" {
		t.Errorf("objects.read of the own file = %q, %v; want mine", content, err)
	}

	if content, err := a.readRaw(app.client, "objects.read", query.Args{"id": &theirs.ID}); err == nil {
		t.Errorf("objects.read of another VFS's file answered %q; want a rejection", content)
	}

	for _, method := range []string{"objects.describe", "objects.find"} {
		answer, err := a.call(app.client, method, query.Args{"id": &mine.ID})
		expectError(t, method+" of the own file", answer, err, vfsapi.ErrNotPermitted)
	}
}

// TestAcceptanceManageOperations (acceptance criterion 4): vfs.volumes, vfs.create and vfs.set_quota are refused
// for a query from the network and for a caller without the manage action; the node's user and the node are
// allowed without a permit.
func TestAcceptanceManageOperations(t *testing.T) {
	a := startAcceptanceNode(t)
	user := a.claim()
	app := a.registerApp()
	target := astral.GenerateIdentity()

	ops := map[string]query.Args{
		"vfs.volumes":   nil,
		"vfs.create":    {"vfs": target},
		"vfs.set_quota": {"vfs": app.id, "bytes": 5, "inodes": 6},
	}

	for method, args := range ops {
		answer, err := a.call(app.client, method, args)
		expectError(t, method+" by an app without the manage action", answer, err, vfsapi.ErrNotPermitted)

		for name, caller := range map[string]*astral.Identity{"node": a.nodeID(), "user": user} {
			// note: a query the router refuses outright is refused too.
			ch, err := a.fromNetwork(caller, method, args)
			if err != nil {
				t.Logf("%v from the network as the %v: refused by the router: %v", method, name, err)
				continue
			}
			answer, err := ch.Receive()
			expectError(t, method+" from the network as the "+name, answer, err, vfsapi.ErrNotPermitted)
		}
	}
	expectStatfs(t, a, app.client, app.id, 1<<30, 1_000_000)

	for name, caller := range map[string]*astral.Identity{"node": a.nodeID(), "user": user} {
		vfsID := astral.GenerateIdentity()
		c := a.as(caller)

		answer, err := a.call(c, "vfs.volumes", nil)
		if _, ok := answer.(*vfsapi.Info); err != nil || !ok {
			t.Errorf("vfs.volumes as the %v answered %#v, %v; want info", name, answer, err)
		}

		answer, err = a.call(c, "vfs.create", query.Args{"vfs": vfsID})
		if _, ok := answer.(*astral.Ack); err != nil || !ok {
			t.Errorf("vfs.create as the %v answered %#v, %v; want ack", name, answer, err)
		}

		answer, err = a.call(c, "vfs.set_quota", query.Args{"vfs": vfsID, "bytes": 7, "inodes": 8})
		if _, ok := answer.(*astral.Ack); err != nil || !ok {
			t.Errorf("vfs.set_quota as the %v answered %#v, %v; want ack", name, answer, err)
		}
		expectStatfs(t, a, c, vfsID, 7, 8)
	}
}

// expectStatfs fails unless vfs.statfs of vfsID through c reports the limits.
func expectStatfs(t *testing.T, a *acceptanceNode, c *astrald.Client, vfsID *astral.Identity, quota, inodes uint64) {
	t.Helper()

	answer, err := a.call(c, "vfs.statfs", query.Args{"vfs": vfsID})
	info, ok := answer.(*vfsapi.Info)
	if err != nil || !ok {
		t.Fatalf("vfs.statfs of %v answered %#v, %v; want info", vfsID, answer, err)
	}
	if uint64(info.Quota) != quota || uint64(info.InodeLimit) != inodes {
		t.Errorf("vfs.statfs of %v: quota %v, inode limit %v; want %v and %v", vfsID, info.Quota, info.InodeLimit, quota, inodes)
	}
}

// acceptanceOps are the twelve operations of the vfs module, each with arguments that name the node's own VFS.
var acceptanceOps = []string{
	"vfs.ls", "vfs.stat", "vfs.statfs", "vfs.read", "vfs.write", "vfs.mkdir",
	"vfs.move", "vfs.remove", "vfs.touch", "vfs.volumes", "vfs.create", "vfs.set_quota",
}

// TestAcceptanceTokenlessSessionReachesNoOperation (acceptance criterion 5): a local apphost session without a
// token is refused for each of the twelve operations, on a claimed node.
//
// note: a session without a token calls as the node, a root caller, so each refusal is the gate's: the same
// operations called by the node inside the node succeed.
func TestAcceptanceTokenlessSessionReachesNoOperation(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	nodeVFS := a.nodeID()
	if answer, err := a.call(a.as(a.nodeID()), "vfs.create", query.Args{"vfs": nodeVFS}); err != nil {
		t.Fatalf("vfs.create of the node's VFS: %#v, %v", answer, err)
	}

	args := map[string]query.Args{
		"vfs.ls":        {"vfs": nodeVFS, "path": "/"},
		"vfs.stat":      {"vfs": nodeVFS, "path": "/"},
		"vfs.statfs":    {"vfs": nodeVFS},
		"vfs.read":      {"vfs": nodeVFS, "path": "/a"},
		"vfs.write":     {"vfs": nodeVFS, "path": "/a"},
		"vfs.mkdir":     {"vfs": nodeVFS, "path": "/d"},
		"vfs.move":      {"vfs": nodeVFS, "from": "/d", "to": "/e"},
		"vfs.remove":    {"vfs": nodeVFS, "path": "/d"},
		"vfs.touch":     {"vfs": nodeVFS, "path": "/"},
		"vfs.volumes":   nil,
		"vfs.create":    {"vfs": astral.GenerateIdentity()},
		"vfs.set_quota": {"vfs": nodeVFS, "bytes": 1, "inodes": 1},
	}

	for _, method := range acceptanceOps {
		ctx, cancel := a.ctx.WithTimeout(acceptanceWait)
		conn, err := a.tokenless().Query(ctx, method, args[method])
		cancel()
		if err == nil {
			conn.Close()
			t.Errorf("a session without a token reached %v", method)
		}
	}

	answer, err := a.call(a.as(a.nodeID()), "vfs.statfs", args["vfs.statfs"])
	if _, ok := answer.(*vfsapi.Info); err != nil || !ok {
		t.Errorf("vfs.statfs as the node inside the node answered %#v, %v; want info", answer, err)
	}
}

// TestAcceptanceEmptyFile (acceptance criterion 6): a write of zero bytes answers empty file and changes nothing.
// No entry and no missing parent folder is created, and a file already at the path keeps its ObjectID and
// modification time.
func TestAcceptanceEmptyFile(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerApp()
	emptyFile := astral.NewError("empty file")

	answer, err := a.write(app.client, query.Args{"path": "/new/empty"}, nil)
	expectError(t, "vfs.write of zero bytes", answer, err, emptyFile)
	for _, path := range []string{"/new/empty", "/new"} {
		answer, err = a.call(app.client, "vfs.stat", query.Args{"path": path})
		expectError(t, "vfs.stat of "+path, answer, err, vfsapi.ErrNotFound)
	}

	before := a.mustWrite(app.client, "/kept", []byte("content"))
	answer, err = a.write(app.client, query.Args{"path": "/kept"}, nil)
	expectError(t, "vfs.write of zero bytes over a file", answer, err, emptyFile)

	answer, err = a.call(app.client, "vfs.stat", query.Args{"path": "/kept"})
	after, ok := answer.(*vfsapi.Entry)
	if err != nil || !ok {
		t.Fatalf("vfs.stat of /kept answered %#v, %v; want an entry", answer, err)
	}
	if after.ID != before.ID || !after.ModTime.Time().Equal(before.ModTime.Time()) {
		t.Errorf("/kept is %v at %v after the empty write; want %v at %v", after.ID, after.ModTime, before.ID, before.ModTime)
	}
}

// TestAcceptanceQuotasAndInterruptedWrites (acceptance criterion 7): a write over the byte quota or the entry quota
// is refused and leaves no entry; a write whose stream ends before the commit leaves no entry.
func TestAcceptanceQuotasAndInterruptedWrites(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerApp()

	answer, err := a.call(a.as(a.nodeID()), "vfs.set_quota", query.Args{"vfs": app.id, "bytes": 10, "inodes": 2})
	if _, ok := answer.(*astral.Ack); err != nil || !ok {
		t.Fatalf("vfs.set_quota answered %#v, %v", answer, err)
	}

	answer, err = a.write(app.client, query.Args{"path": "/big"}, []byte("eleven byte"))
	expectError(t, "a write over the byte quota", answer, err, vfsapi.ErrQuotaExceeded)
	expectNoEntry(t, a, app, "/big")

	a.mustWrite(app.client, "/one", []byte("1"))
	a.mustWrite(app.client, "/two", []byte("2"))
	answer, err = a.write(app.client, query.Args{"path": "/three"}, []byte("3"))
	expectError(t, "a write over the entry quota", answer, err, vfsapi.ErrNoInodesLeft)
	expectNoEntry(t, a, app, "/three")

	a.mustWrite(app.client, "/two", []byte("22")) // note: replacing a file adds no entry
	expectInterruptedWriteLeavesNoEntry(t, a, app)
}

// expectInterruptedWriteLeavesNoEntry writes part of a file and closes the stream before the commit.
func expectInterruptedWriteLeavesNoEntry(t *testing.T, a *acceptanceNode, app *acceptanceApp) {
	t.Helper()

	if answer, err := a.call(a.as(a.nodeID()), "vfs.set_quota", query.Args{"vfs": app.id, "bytes": 0, "inodes": 0}); err != nil {
		t.Fatalf("vfs.set_quota answered %#v, %v", answer, err)
	}

	ch := a.mustChannel(app.client, "vfs.write", query.Args{"path": "/cut"})
	if ack, err := ch.Receive(); err != nil {
		t.Fatalf("vfs.write: %v", err)
	} else if _, ok := ack.(*astral.Ack); !ok {
		t.Fatalf("vfs.write answered %#v; want ack", ack)
	}
	blob := astral.Blob("partial")
	if err := ch.Send(&blob); err != nil {
		t.Fatalf("send: %v", err)
	}
	ch.Close()

	// note: the op ends after the stream does; a stat after it sees what the write left.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		expectNoEntry(t, a, app, "/cut")
		time.Sleep(50 * time.Millisecond)
	}
}

// expectNoEntry fails unless vfs.stat of the path answers not found.
func expectNoEntry(t *testing.T, a *acceptanceNode, app *acceptanceApp, path string) {
	t.Helper()

	answer, err := a.call(app.client, "vfs.stat", query.Args{"path": path})
	expectError(t, "vfs.stat "+path, answer, err, vfsapi.ErrNotFound)
}

// TestAcceptanceKeypairChain (acceptance criterion 15, as a Go test): a VFS with a new keypair is created by the
// chain secp256k1.new, objects.store, vfs.create, called by the node's user; vfs.statfs then reports it.
func TestAcceptanceKeypairChain(t *testing.T) {
	a := startAcceptanceNode(t)
	user := a.as(a.claim())

	key, err := a.call(user, "secp256k1.new", nil)
	if err != nil {
		t.Fatalf("secp256k1.new: %v", err)
	}

	ch := a.mustChannel(user, "objects.store", nil)
	if err = ch.Send(key); err != nil {
		t.Fatalf("send the key: %v", err)
	}
	stored, err := ch.Receive()
	if _, ok := stored.(*astral.ObjectID); err != nil || !ok {
		t.Fatalf("objects.store answered %#v, %v; want an ObjectID", stored, err)
	}

	pub := a.mustChannel(user, "crypto.public_key", nil)
	if err = pub.Send(key); err != nil {
		t.Fatalf("send the key: %v", err)
	}
	publicKey, err := pub.Receive()
	pk, ok := publicKey.(*cryptoapi.PublicKey)
	if err != nil || !ok {
		t.Fatalf("crypto.public_key answered %#v, %v", publicKey, err)
	}
	vfsID := secp256k1.Identity(pk)

	answer, err := a.call(user, "vfs.create", query.Args{"vfs": vfsID})
	if _, ok := answer.(*astral.Ack); err != nil || !ok {
		t.Fatalf("vfs.create answered %#v, %v; want ack", answer, err)
	}
	expectStatfs(t, a, user, vfsID, 1<<30, 1_000_000)

	if _, err := a.crypto().PrivateKeyID(pk); err != nil {
		t.Errorf("the stored key of the new VFS is not indexed: %v", err)
	}
}

// TestAcceptanceRegisterCreatesTheVFS (acceptance criterion 16, as a Go test): apphost.register leaves the new
// app with a VFS that vfs.statfs, called by the app, reports with the default quotas, 1 GiB and 1,000,000.
func TestAcceptanceRegisterCreatesTheVFS(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerApp()

	answer, err := a.call(app.client, "vfs.statfs", nil)
	info, ok := answer.(*vfsapi.Info)
	if err != nil || !ok {
		t.Fatalf("vfs.statfs answered %#v, %v; want info", answer, err)
	}
	if !info.VFS.IsEqual(app.id) || info.Quota != 1<<30 || info.InodeLimit != 1_000_000 {
		t.Errorf("vfs.statfs = %v, quota %v, inode limit %v; want %v, 1 GiB, 1,000,000", info.VFS, info.Quota, info.InodeLimit, app.id)
	}
}

// TestAcceptanceReadContractIsHonouredOnceIndexed (validation check 17): a contract for mod.vfs.read_action signed
// by the VFS identity lets another app read the VFS only after the node has indexed it.
func TestAcceptanceReadContractIsHonouredOnceIndexed(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner, reader := a.registerApp(), a.registerApp()
	a.mustWrite(owner.client, "/shared.txt", []byte("shared"))
	args := query.Args{"vfs": owner.id, "path": "/shared.txt"}

	constraints := astral.NewBundle()
	if err := constraints.Append(owner.id); err != nil {
		t.Fatalf("constraint: %v", err)
	}
	signed := &apiauth.SignedContract{Contract: &apiauth.Contract{
		Issuer:    owner.id,
		Subject:   reader.id,
		Permits:   []*apiauth.Permit{{Action: astral.String8((&vfsapi.ReadAction{}).ObjectType()), Constraints: constraints}},
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}}
	if err := a.auth().SignContract(a.ctx, signed); err != nil {
		t.Fatalf("sign the contract: %v", err)
	}

	if content, err := a.readRaw(reader.client, "vfs.read", args); err == nil {
		t.Errorf("vfs.read before the contract is indexed answered %q; want a rejection", content)
	}

	if err := a.auth().IndexContract(a.ctx, signed); err != nil {
		t.Fatalf("index the contract: %v", err)
	}

	content, err := a.readRaw(reader.client, "vfs.read", args)
	if err != nil || string(content) != "shared" {
		t.Errorf("vfs.read after the contract is indexed = %q, %v; want shared", content, err)
	}
	answer, err := a.write(reader.client, query.Args{"vfs": owner.id, "path": "/x"}, []byte("x"))
	expectError(t, "vfs.write with a read contract", answer, err, vfsapi.ErrNotPermitted)
}
