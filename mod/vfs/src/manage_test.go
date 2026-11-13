package vfs

import (
	"io"
	"testing"
	"time"

	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

const manageWait = 2 * time.Second

// manageAuth holds the permits of the manage action, as the auth module evaluates them: the type of the action
// must match and the constraints of the permit pass through the action.
type manageAuth struct {
	authmod.Module
	permits map[string][]*apiauth.Permit
}

func newManageAuth() *manageAuth { return &manageAuth{permits: map[string][]*apiauth.Permit{}} }

func (a *manageAuth) grant(actor *astral.Identity, p *apiauth.Permit) {
	a.permits[actor.String()] = append(a.permits[actor.String()], p)
}

func (a *manageAuth) Authorize(_ *astral.Context, action apiauth.ActionObject) bool {
	for _, p := range a.permits[action.Actor().String()] {
		if p.Allows(action) {
			return true
		}
	}

	return false
}

func managePermit(constraints ...astral.Object) *apiauth.Permit {
	p := &apiauth.Permit{Action: astral.String8((&vfsapi.ManageAction{}).ObjectType())}
	if len(constraints) > 0 {
		p.Constraints = astral.NewBundle()
		if err := p.Constraints.Append(constraints...); err != nil {
			panic(err)
		}
	}

	return p
}

// manageFixture is a module with one user, one permit holder and a manage-aware auth module.
type manageFixture struct {
	mod    *Module
	auth   *manageAuth
	user   *astral.Identity
	holder *astral.Identity
}

func newManageFixture(t *testing.T) *manageFixture {
	t.Helper()

	f := &manageFixture{
		mod:    testModule(t),
		auth:   newManageAuth(),
		user:   astral.GenerateIdentity(),
		holder: astral.GenerateIdentity(),
	}
	f.mod.Auth = f.auth
	f.mod.User = fakeUser{id: f.user}
	f.auth.grant(f.holder, managePermit())

	return f
}

// manageCaller is the caller's end of one query.
type manageCaller struct {
	ch   *channel.Channel
	conn io.WriteCloser
	in   *io.PipeReader
}

type manageConn struct {
	io.Reader
	io.Writer
}

// manageOpen runs an operation as caller with the origin of the query and returns the caller's end.
func manageOpen(t *testing.T, fn any, caller *astral.Identity, queryString string, origin string) *manageCaller {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	q := astral.Launch(query.New(caller, caller, queryString, nil))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	in, out := io.Pipe()
	conn, err := op.RouteQuery(ctx, q, out)
	if err != nil {
		t.Fatalf("%s: %v", queryString, err)
	}

	c := &manageCaller{ch: channel.New(manageConn{in, conn}, channel.WithLockedWrites()), conn: conn, in: in}
	t.Cleanup(func() {
		c.conn.Close()
		c.in.Close()
	})

	return c
}

// next returns the next object of the answer, or the read error.
func (c *manageCaller) next(t *testing.T) (astral.Object, error) {
	t.Helper()

	type received struct {
		obj astral.Object
		err error
	}
	got := make(chan received, 1)
	go func() {
		obj, err := c.ch.Receive()
		got <- received{obj, err}
	}()

	select {
	case r := <-got:
		return r.obj, r.err
	case <-time.After(manageWait):
		t.Fatal("nothing received")
		return nil, nil
	}
}

func manageExpect[T astral.Object](t *testing.T, c *manageCaller) T {
	t.Helper()

	obj, err := c.next(t)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}

	v, ok := obj.(T)
	if !ok {
		var zero T
		t.Fatalf("received %s %v, want %T", obj.ObjectType(), obj, zero)
	}

	return v
}

// manageExpectError reads one answer and requires it to be the error.
func manageExpectError(t *testing.T, c *manageCaller, want astral.Error) {
	t.Helper()

	e := manageExpect[astral.Error](t, c)
	if e.Error() != want.Error() {
		t.Fatalf("answered error %q, want %q", e.Error(), want.Error())
	}
}

// manageOps runs the three manage operations as caller and returns one caller end for each.
func (f *manageFixture) manageOps(t *testing.T, caller *astral.Identity, origin string, target *astral.Identity) map[string]*manageCaller {
	t.Helper()

	vfs := target.String()

	return map[string]*manageCaller{
		"volumes":   manageOpen(t, f.mod.OpVolumes, caller, "vfs.volumes", origin),
		"create":    manageOpen(t, f.mod.OpCreate, caller, "vfs.create?vfs="+vfs, origin),
		"set_quota": manageOpen(t, f.mod.OpSetQuota, caller, "vfs.set_quota?vfs="+vfs+"&bytes=5&entries=6", origin),
	}
}

func TestManageOperationsAllowRootCallersWithoutAPermit(t *testing.T) {
	f := newManageFixture(t)
	target := astral.GenerateIdentity()
	createVFS(t, f.mod, target)

	for name, caller := range map[string]*astral.Identity{"node": f.mod.identity, "user": f.user} {
		for op, c := range f.manageOps(t, caller, "", target) {
			obj, err := c.next(t)
			if err != nil {
				t.Fatalf("%s %s: %v", name, op, err)
			}
			if e, isErr := obj.(astral.Error); isErr {
				t.Errorf("%s %s answered %v", name, op, e)
			}
		}
	}
}

func TestManageOperationsAllowAPermitHolder(t *testing.T) {
	f := newManageFixture(t)
	target := astral.GenerateIdentity()
	createVFS(t, f.mod, target)

	for op, c := range f.manageOps(t, f.holder, "", target) {
		obj, err := c.next(t)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if e, isErr := obj.(astral.Error); isErr {
			t.Errorf("%s answered %v", op, e)
		}
	}
}

func TestManageOperationsRefuseACallerWithoutAPermit(t *testing.T) {
	f := newManageFixture(t)
	stranger := astral.GenerateIdentity()
	target := astral.GenerateIdentity()
	createVFS(t, f.mod, target)

	for op, c := range f.manageOps(t, stranger, "", target) {
		t.Run(op, func(t *testing.T) { manageExpectError(t, c, vfsapi.ErrNotPermitted) })
	}
}

func TestManageOperationsRefuseAConstrainedPermit(t *testing.T) {
	f := newManageFixture(t)
	target := astral.GenerateIdentity()
	createVFS(t, f.mod, target)

	constrained := astral.GenerateIdentity()
	f.auth.grant(constrained, managePermit(target))

	for op, c := range f.manageOps(t, constrained, "", target) {
		t.Run(op, func(t *testing.T) { manageExpectError(t, c, vfsapi.ErrNotPermitted) })
	}
}

func TestManageOperationsRefuseAPermitForAnotherAction(t *testing.T) {
	f := newManageFixture(t)
	target := astral.GenerateIdentity()
	createVFS(t, f.mod, target)

	writer := astral.GenerateIdentity()
	f.auth.grant(writer, &apiauth.Permit{Action: astral.String8((&vfsapi.WriteAction{}).ObjectType())})

	for op, c := range f.manageOps(t, writer, "", target) {
		t.Run(op, func(t *testing.T) { manageExpectError(t, c, vfsapi.ErrNotPermitted) })
	}
}

// A query from the network is refused for every caller, a root caller and a permit holder included.
func TestManageOperationsRefuseTheNetwork(t *testing.T) {
	f := newManageFixture(t)
	target := astral.GenerateIdentity()
	existing := astral.GenerateIdentity()
	createVFS(t, f.mod, existing)

	for name, caller := range map[string]*astral.Identity{"node": f.mod.identity, "user": f.user, "holder": f.holder} {
		callers := map[string]*manageCaller{
			"volumes":   manageOpen(t, f.mod.OpVolumes, caller, "vfs.volumes", astral.OriginNetwork),
			"create":    manageOpen(t, f.mod.OpCreate, caller, "vfs.create?vfs="+target.String(), astral.OriginNetwork),
			"set_quota": manageOpen(t, f.mod.OpSetQuota, caller, "vfs.set_quota?vfs="+existing.String()+"&bytes=5&entries=6", astral.OriginNetwork),
		}

		for op, c := range callers {
			t.Run(name+" "+op, func(t *testing.T) { manageExpectError(t, c, vfsapi.ErrNotPermitted) })
		}
	}

	if _, err := f.mod.db.findVolume(target); err != vfsapi.ErrNoSuchVFS {
		t.Errorf("a refused vfs.create created the VFS: %v", err)
	}

	vol, err := f.mod.db.findVolume(existing)
	if err != nil {
		t.Fatalf("findVolume: %v", err)
	}
	if vol.byteLimit() != defaultByteLimit || vol.entryLimit() != defaultEntryLimit {
		t.Errorf("a refused vfs.set_quota changed the limits to %d and %d", vol.byteLimit(), vol.entryLimit())
	}
}

func TestVolumesStreamsInfoThenEOS(t *testing.T) {
	f := newManageFixture(t)

	// no VFS: eos alone
	c := manageOpen(t, f.mod.OpVolumes, f.mod.identity, "vfs.volumes", "")
	manageExpect[*astral.EOS](t, c)

	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	volA := createVFS(t, f.mod, a)
	volB := createVFS(t, f.mod, b)

	// a has the file "x" and the file "y" with the same content, and a folder; b is empty
	addFile(t, f.mod, volA, volA.RootID, "x", "hello\n")
	addFile(t, f.mod, volA, volA.RootID, "y", "hello\n")
	folder := addFolder(t, f.mod, volA, volA.RootID, "d")
	addFile(t, f.mod, volA, folder.ID, "z", "abc")

	c = manageOpen(t, f.mod.OpVolumes, f.user, "vfs.volumes", "")

	infoA := manageExpect[*vfsapi.Info](t, c)
	infoB := manageExpect[*vfsapi.Info](t, c)
	manageExpect[*astral.EOS](t, c)

	if !infoA.VFS.IsEqual(a) || !infoB.VFS.IsEqual(b) {
		t.Fatalf("volumes in the order %v, %v; want %v, %v", infoA.VFS, infoB.VFS, a, b)
	}

	rootA, err := f.mod.folderObjectID(a, volA.RootID)
	if err != nil {
		t.Fatalf("root: %v", err)
	}

	want := vfsapi.Info{
		VFS: a, Root: rootA,
		Usage: astral.Uint64(len("hello\n") + len("abc")), Quota: astral.Uint64(defaultByteLimit),
		Entries: 4, EntryLimit: astral.Uint64(defaultEntryLimit),
	}
	if infoA.Root != want.Root || infoA.Usage != want.Usage || infoA.Quota != want.Quota ||
		infoA.Entries != want.Entries || infoA.EntryLimit != want.EntryLimit {
		t.Errorf("info of a = %+v, want %+v", *infoA, want)
	}

	rootB, _ := f.mod.folderObjectID(b, volB.RootID)
	if infoB.Root != rootB || infoB.Usage != 0 || infoB.Entries != 0 {
		t.Errorf("info of b = %+v, want root %v, no usage, no entries", *infoB, rootB)
	}
}

func TestCreateIsIdempotentWithDefaultQuotas(t *testing.T) {
	f := newManageFixture(t)
	f.mod.config = Config{ByteLimit: 123, EntryLimit: 45}
	id := astral.GenerateIdentity()

	manageExpect[*astral.Ack](t, manageOpen(t, f.mod.OpCreate, f.user, "vfs.create?vfs="+id.String(), ""))

	vol, err := f.mod.db.findVolume(id)
	if err != nil {
		t.Fatalf("findVolume: %v", err)
	}
	if vol.byteLimit() != 123 || vol.entryLimit() != 45 {
		t.Errorf("limits %d and %d, want the configured 123 and 45", vol.byteLimit(), vol.entryLimit())
	}

	// a second create changes nothing: the file, the root and the limits stay
	addFile(t, f.mod, vol, vol.RootID, "keep", "content")
	f.mod.config = Config{ByteLimit: 999, EntryLimit: 99}
	manageExpect[*astral.Ack](t, manageOpen(t, f.mod.OpCreate, f.holder, "vfs.create?vfs="+id.String(), ""))

	again, err := f.mod.db.findVolume(id)
	if err != nil {
		t.Fatalf("findVolume: %v", err)
	}
	if again.ID != vol.ID || again.RootID != vol.RootID || again.byteLimit() != 123 || again.entryLimit() != 45 {
		t.Errorf("a second create changed the VFS: %+v, was %+v", again, vol)
	}
	if _, err := f.mod.db.findChild(vol.RootID, "keep"); err != nil {
		t.Errorf("a second create lost an entry: %v", err)
	}
}

func TestCreateRefusesTheZeroIdentity(t *testing.T) {
	f := newManageFixture(t)

	for _, caller := range []*astral.Identity{f.mod.identity, f.user} {
		c := manageOpen(t, f.mod.OpCreate, caller, "vfs.create?vfs="+(&astral.Identity{}).String(), "")
		manageExpectError(t, c, vfsapi.ErrNotPermitted)
	}

	if volumes, err := f.mod.db.volumesList(); err != nil || len(volumes) != 0 {
		t.Errorf("volumes %v, %v; want none", volumes, err)
	}
}

func TestSetQuotaSetsBothLimits(t *testing.T) {
	f := newManageFixture(t)
	id := astral.GenerateIdentity()
	createVFS(t, f.mod, id)

	manageExpect[*astral.Ack](t, manageOpen(t, f.mod.OpSetQuota, f.user, "vfs.set_quota?vfs="+id.String()+"&bytes=100&entries=7", ""))

	vol, _ := f.mod.db.findVolume(id)
	if vol.byteLimit() != 100 || vol.entryLimit() != 7 {
		t.Errorf("limits %d and %d, want 100 and 7", vol.byteLimit(), vol.entryLimit())
	}

	// a limit left out is 0, no limit
	manageExpect[*astral.Ack](t, manageOpen(t, f.mod.OpSetQuota, f.user, "vfs.set_quota?vfs="+id.String(), ""))

	vol, _ = f.mod.db.findVolume(id)
	if vol.byteLimit() != 0 || vol.entryLimit() != 0 {
		t.Errorf("limits %d and %d, want 0 and 0", vol.byteLimit(), vol.entryLimit())
	}
}

// A limit below usage is accepted. It removes nothing.
func TestSetQuotaBelowUsageRemovesNothing(t *testing.T) {
	f := newManageFixture(t)
	id := astral.GenerateIdentity()
	vol := createVFS(t, f.mod, id)
	addFile(t, f.mod, vol, vol.RootID, "a", "0123456789")
	addFolder(t, f.mod, vol, vol.RootID, "d")
	addFile(t, f.mod, vol, vol.RootID, "b", "abcdef")

	manageExpect[*astral.Ack](t, manageOpen(t, f.mod.OpSetQuota, f.user, "vfs.set_quota?vfs="+id.String()+"&bytes=1&entries=1", ""))

	vol, _ = f.mod.db.findVolume(id)
	if vol.byteLimit() != 1 || vol.entryLimit() != 1 {
		t.Errorf("limits %d and %d, want 1 and 1", vol.byteLimit(), vol.entryLimit())
	}

	if n, _ := f.mod.db.entryCount(id); n != 3 {
		t.Errorf("entries %d, want 3", n)
	}
	if u, _ := f.mod.db.usage(id); u != 16 {
		t.Errorf("usage %d, want 16", u)
	}

	c := manageOpen(t, f.mod.OpVolumes, f.user, "vfs.volumes", "")
	info := manageExpect[*vfsapi.Info](t, c)
	if info.Quota != 1 || info.EntryLimit != 1 || info.Usage != 16 || info.Entries != 3 {
		t.Errorf("info %+v, want quota 1, entry limit 1, usage 16, entries 3", *info)
	}
}

func TestSetQuotaAnswersNoSuchVFSForAnUnknownIdentity(t *testing.T) {
	f := newManageFixture(t)

	c := manageOpen(t, f.mod.OpSetQuota, f.user, "vfs.set_quota?vfs="+astral.GenerateIdentity().String()+"&bytes=1", "")
	manageExpectError(t, c, vfsapi.ErrNoSuchVFS)
}

// Authorization comes before the existence of the VFS.
func TestSetQuotaChecksAuthorizationBeforeExistence(t *testing.T) {
	f := newManageFixture(t)

	c := manageOpen(t, f.mod.OpSetQuota, astral.GenerateIdentity(), "vfs.set_quota?vfs="+astral.GenerateIdentity().String(), "")
	manageExpectError(t, c, vfsapi.ErrNotPermitted)
}

// TestSetQuotaUnknownVFSLeavesOtherVFSesUnchanged: vfs.set_quota on an unknown identity answers ErrNoSuchVFS and
// leaves the limits of an existing VFS as they were.
func TestSetQuotaUnknownVFSLeavesOtherVFSesUnchanged(t *testing.T) {
	f := newManageFixture(t)
	id := astral.GenerateIdentity()
	createVFS(t, f.mod, id)
	manageExpect[*astral.Ack](t, manageOpen(t, f.mod.OpSetQuota, f.user, "vfs.set_quota?vfs="+id.String()+"&bytes=100&entries=7", ""))

	c := manageOpen(t, f.mod.OpSetQuota, f.user, "vfs.set_quota?vfs="+astral.GenerateIdentity().String()+"&bytes=1&entries=1", "")
	manageExpectError(t, c, vfsapi.ErrNoSuchVFS)

	vol, err := f.mod.db.findVolume(id)
	if err != nil {
		t.Fatalf("findVolume: %v", err)
	}
	if vol.byteLimit() != 100 || vol.entryLimit() != 7 {
		t.Errorf("limits %d and %d, want 100 and 7", vol.byteLimit(), vol.entryLimit())
	}
}
