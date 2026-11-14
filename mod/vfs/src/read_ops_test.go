package vfs

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// readOpsFixture is a module with a fake auth module, a fake repository group "local" and one VFS.
type readOpsFixture struct {
	mod   *Module
	auth  *fakeAuth
	repo  *readOpsRepo
	vfs   *astral.Identity
	vol   *dbVolume
	other *astral.Identity

	dirObjects *dirTestObjects // where the directories are stored
}

func newReadOpsFixture(t *testing.T) *readOpsFixture {
	t.Helper()

	f := &readOpsFixture{
		mod:   testModule(t),
		auth:  newFakeAuth(),
		repo:  &readOpsRepo{content: map[astral.ObjectID][]byte{}},
		vfs:   astral.GenerateIdentity(),
		other: astral.GenerateIdentity(),
	}
	f.mod.Auth = f.auth
	f.dirObjects = f.mod.Objects.(*dirTestObjects)
	f.mod.Objects = &readOpsObjects{dirTestObjects: f.dirObjects, repos: map[string]objectsmod.Repository{readRepository: f.repo}}
	f.vol = createVFS(t, f.mod, f.vfs)

	return f
}

// putFile adds a file under a parent and stores its content in the fake repository.
func (f *readOpsFixture) putFile(t *testing.T, parent int64, name, content string) *dbEntry {
	t.Helper()

	f.repo.content[contentID(content)] = []byte(content)

	return addFile(t, f.mod, f.vol, parent, name, content)
}

// readOpsObjects answers the repositories by name, and stores directories in the test repository.
type readOpsObjects struct {
	*dirTestObjects
	repos map[string]objectsmod.Repository
}

func (o *readOpsObjects) GetRepository(name string) objectsmod.Repository { return o.repos[name] }

// readOpsRepo serves content and records every read.
type readOpsRepo struct {
	objectsmod.Repository
	mu      sync.Mutex
	content map[astral.ObjectID][]byte
	reads   []readOpsCall
}

type readOpsCall struct {
	zone          astral.Zone
	offset, limit int64
}

func (r *readOpsRepo) Read(ctx *astral.Context, id *astral.ObjectID, offset, limit int64) (objectsmod.Reader, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reads = append(r.reads, readOpsCall{zone: ctx.Zone(), offset: offset, limit: limit})

	data, ok := r.content[*id]
	if !ok {
		return nil, errors.New("not found")
	}
	if offset > int64(len(data)) {
		return nil, errors.New("offset beyond the end")
	}

	data = data[offset:]
	if limit > 0 && limit < int64(len(data)) {
		data = data[:limit]
	}

	return &readOpsReader{Reader: bytes.NewReader(data), id: *id}, nil
}

type readOpsReader struct {
	*bytes.Reader
	id astral.ObjectID
}

func (r *readOpsReader) Close() error                { return nil }
func (r *readOpsReader) Repo() objectsmod.Repository { return nil }
func (r *readOpsReader) ID() *astral.ObjectID        { id := r.id; return &id }

// readOpsWriter is the caller's end of the connection; it keeps every byte the op wrote.
type readOpsWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *readOpsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

func (w *readOpsWriter) Close() error { return nil }

// readOpsResult is what one routed query answered. Err is the rejection of the query.
type readOpsResult struct {
	Err     error
	Raw     []byte
	Objects []astral.Object
}

// route sends one query to one op and waits until the op returns.
func readOpsRoute(t *testing.T, fn any, caller *astral.Identity, queryString string) readOpsResult {
	t.Helper()

	return readOpsRouteIn(t, astral.NewContext(nil), fn, caller, queryString)
}

// readOpsRouteIn routes like readOpsRoute, from a context the test chooses.
func readOpsRouteIn(t *testing.T, parent *astral.Context, fn any, caller *astral.Identity, queryString string) readOpsResult {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	reports := make(chan *routing.Report, 1)
	op.LogFunc = func(r *routing.Report) { reports <- r }

	ctx, cancel := parent.WithTimeout(10 * time.Second)
	defer cancel()

	w := &readOpsWriter{}
	requester, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	if err != nil {
		return readOpsResult{Err: err}
	}
	defer requester.Close()

	select {
	case <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never returned")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return readOpsResult{Raw: bytes.Clone(w.buf.Bytes()), Objects: readOpsDecode(t, w.buf.Bytes())}
}

func readOpsDecode(t *testing.T, raw []byte) (objs []astral.Object) {
	t.Helper()

	recv := channel.NewReceiver(bytes.NewReader(raw))
	for {
		obj, err := recv.Receive()
		if errors.Is(err, io.EOF) {
			return objs
		}
		if err != nil {
			return append(objs, nil)
		}
		objs = append(objs, obj)
	}
}

// queryString builds "op?k=v&..." from key and value pairs.
func readOpsQuery(op string, kv ...string) string {
	values := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		values.Set(kv[i], kv[i+1])
	}

	return op + "?" + values.Encode()
}

// wantError checks that the answer is exactly one error_message with the text of want.
func (r readOpsResult) wantError(t *testing.T, want error) {
	t.Helper()

	if r.Err != nil {
		t.Fatalf("the query was rejected: %v; want the answer %q", r.Err, want)
	}
	if len(r.Objects) != 1 {
		t.Fatalf("the op answered %d objects, want one error %q", len(r.Objects), want)
	}
	e, ok := r.Objects[0].(astral.Error)
	if !ok {
		t.Fatalf("the op answered %T, want an error", r.Objects[0])
	}
	if e.Error() != want.Error() {
		t.Fatalf("the op answered %q, want %q", e.Error(), want)
	}
}

// wantRejected checks that the query was rejected and nothing was answered.
func (r readOpsResult) wantRejected(t *testing.T) {
	t.Helper()

	if r.Err == nil {
		t.Fatalf("the query was accepted and answered %d raw bytes, want a rejection", len(r.Raw))
	}
	var rejected *astral.ErrRejected
	if !errors.As(r.Err, &rejected) {
		t.Fatalf("the query failed with %v, want a rejection", r.Err)
	}
}

func readOpsHash(id astral.ObjectID) string { return hex.EncodeToString(id.Hash[:]) }

func (r readOpsResult) entries(t *testing.T) (list []*vfsapi.Entry) {
	t.Helper()

	if r.Err != nil {
		t.Fatalf("the query was rejected: %v", r.Err)
	}
	if len(r.Objects) == 0 {
		t.Fatal("the op answered nothing")
	}
	if _, ok := r.Objects[len(r.Objects)-1].(*astral.EOS); !ok {
		t.Fatalf("the stream ends with %T, want eos", r.Objects[len(r.Objects)-1])
	}
	for _, obj := range r.Objects[:len(r.Objects)-1] {
		e, ok := obj.(*vfsapi.Entry)
		if !ok {
			t.Fatalf("the stream holds %T, want an entry", obj)
		}
		list = append(list, e)
	}

	return list
}

func (r readOpsResult) entry(t *testing.T) *vfsapi.Entry {
	t.Helper()

	if r.Err != nil {
		t.Fatalf("the query was rejected: %v", r.Err)
	}
	if len(r.Objects) != 1 {
		t.Fatalf("the op answered %d objects, want one entry", len(r.Objects))
	}
	e, ok := r.Objects[0].(*vfsapi.Entry)
	if !ok {
		t.Fatalf("the op answered %T, want an entry", r.Objects[0])
	}

	return e
}

func (r readOpsResult) info(t *testing.T) *vfsapi.Info {
	t.Helper()

	if r.Err != nil {
		t.Fatalf("the query was rejected: %v", r.Err)
	}
	if len(r.Objects) != 1 {
		t.Fatalf("the op answered %d objects, want one info", len(r.Objects))
	}
	i, ok := r.Objects[0].(*vfsapi.Info)
	if !ok {
		t.Fatalf("the op answered %T, want an info", r.Objects[0])
	}

	return i
}

// run routes a query to the read operation of that name.
func (f *readOpsFixture) run(t *testing.T, op string, caller *astral.Identity, kv ...string) readOpsResult {
	t.Helper()

	var fn any
	switch op {
	case "ls":
		fn = f.mod.OpLs
	case "stat":
		fn = f.mod.OpStat
	case "statfs":
		fn = f.mod.OpStatfs
	case "read":
		fn = f.mod.OpRead
	default:
		t.Fatalf("unknown op %q", op)
	}

	return readOpsRoute(t, fn, caller, readOpsQuery(op, kv...))
}

// wantFailure checks a failure the way the operation answers it: a rejection for vfs.read, an error_message for
// every other operation.
func (r readOpsResult) wantFailure(t *testing.T, op string, want error) {
	t.Helper()

	if op == "read" {
		r.wantRejected(t)
	} else {
		r.wantError(t, want)
	}
}

// The four read operations share their authorization and its order.

func TestReadOpsAuthorizationOrder(t *testing.T) {
	f := newReadOpsFixture(t)
	holder := astral.GenerateIdentity()
	missing := astral.GenerateIdentity()
	f.auth.permit(&vfsapi.ReadAction{}, holder, missing)

	for _, op := range []string{"ls", "stat", "statfs", "read"} {
		t.Run(op, func(t *testing.T) {
			bad := []string{"path", "no-slash"}

			// not permitted comes before no such vfs and before an invalid path
			for _, id := range []*astral.Identity{f.vfs, missing} {
				f.run(t, op, f.other, append([]string{"vfs", id.String()}, bad...)...).wantFailure(t, op, vfsapi.ErrNotPermitted)
			}

			// no such vfs comes before an invalid path
			f.run(t, op, holder, append([]string{"vfs", missing.String()}, bad...)...).wantFailure(t, op, vfsapi.ErrNoSuchVFS)

			// an invalid path comes last
			if op != "statfs" {
				f.run(t, op, f.vfs, bad...).wantFailure(t, op, vfsapi.ErrInvalidPath)
			}
		})
	}
}

func TestReadOpsRefuseTheZeroCaller(t *testing.T) {
	f := newReadOpsFixture(t)

	for _, op := range []string{"ls", "stat", "statfs", "read"} {
		f.run(t, op, astral.Anyone, "vfs", f.vfs.String(), "path", "/").wantFailure(t, op, vfsapi.ErrNotPermitted)
		f.run(t, op, astral.Anyone, "path", "/").wantFailure(t, op, vfsapi.ErrNotPermitted)
	}
}

func TestReadOpsAllowAPermitHolderAndRootCaller(t *testing.T) {
	f := newReadOpsFixture(t)
	holder := astral.GenerateIdentity()
	f.auth.permit(&vfsapi.ReadAction{}, holder, f.vfs)

	for name, caller := range map[string]*astral.Identity{"holder": holder, "node": f.mod.identity} {
		res := f.run(t, "statfs", caller, "vfs", f.vfs.String())
		if got := res.info(t).VFS; !got.IsEqual(f.vfs) {
			t.Errorf("%s: statfs answered the VFS %v, want %v", name, got, f.vfs)
		}
	}
}

func TestReadOpsWritePermitDoesNotAllowReading(t *testing.T) {
	f := newReadOpsFixture(t)
	writer := astral.GenerateIdentity()
	f.auth.permit(&vfsapi.WriteAction{}, writer, f.vfs)

	f.run(t, "ls", writer, "vfs", f.vfs.String()).wantError(t, vfsapi.ErrNotPermitted)
}

func TestReadOpsDefaultVFSIsTheCaller(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a", "x")

	// another caller without a VFS of its own is told so
	f.run(t, "ls", f.other).wantError(t, vfsapi.ErrNoSuchVFS)

	if got := f.run(t, "ls", f.vfs).entries(t); len(got) != 1 {
		t.Fatalf("ls answered %d entries, want 1", len(got))
	}
}

// vfs.ls

func TestLsListsInByteOrderOfName(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "z", "1")
	f.putFile(t, f.vol.RootID, "é", "3")
	f.putFile(t, f.vol.RootID, "è", "2")
	addFolder(t, f.mod, f.vol, f.vol.RootID, "dir")

	// the default path is the root
	got := f.run(t, "ls", f.vfs).entries(t)

	var names []string
	for _, e := range got {
		names = append(names, string(e.Name))
	}
	if want := "dir z è é"; strings.Join(names, " ") != want {
		t.Fatalf("names = %q, want %q", strings.Join(names, " "), want)
	}
	if got[1].ID != contentID("1") || got[1].Flags != 0 || time.Time(got[1].ModTime).UnixNano() != testModTime ||
		time.Time(got[1].CreationTime).UnixNano() != testCreationTime {
		t.Errorf("entry z = %+v", got[1])
	}
	if !got[0].IsFolder() {
		t.Errorf("entry dir is not a folder: %+v", got[0])
	}
}

func TestLsOfASubfolder(t *testing.T) {
	f := newReadOpsFixture(t)
	dir := addFolder(t, f.mod, f.vol, f.vol.RootID, "dir")
	f.putFile(t, dir.ID, "a.txt", "hello\n")
	f.putFile(t, f.vol.RootID, "top", "x")

	got := f.run(t, "ls", f.vfs, "path", "/dir").entries(t)
	if len(got) != 1 || got[0].Name != "a.txt" || got[0].ID != contentID("hello\n") {
		t.Fatalf("ls /dir = %+v", got)
	}
}

func TestLsOfAnEmptyFolderIsEos(t *testing.T) {
	f := newReadOpsFixture(t)
	addFolder(t, f.mod, f.vol, f.vol.RootID, "empty")

	if got := f.run(t, "ls", f.vfs, "path", "/empty").entries(t); len(got) != 0 {
		t.Fatalf("ls /empty = %+v, want nothing", got)
	}
}

func TestLsErrors(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "file", "x")

	for path, want := range map[string]error{
		"/missing":    vfsapi.ErrNotFound,
		"/file":       vfsapi.ErrNotAFolder,
		"/file/below": vfsapi.ErrNotAFolder,
		"":            vfsapi.ErrInvalidPath,
		"/dir/":       vfsapi.ErrInvalidPath,
		"//":          vfsapi.ErrInvalidPath,
	} {
		f.run(t, "ls", f.vfs, "path", path).wantError(t, want)
	}
}

// expectedFolderID computes the ObjectID of a directory with the SDK writer, independent of the module.
func expectedFolderID(t *testing.T, entries ...*vfsapi.Entry) astral.ObjectID {
	t.Helper()

	w, err := vfsapi.NewDirectoryWriter(nil, uint32(len(entries)))
	if err != nil {
		t.Fatalf("NewDirectoryWriter: %v", err)
	}
	for _, e := range entries {
		if err = w.Write(e); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	id, err := w.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	return *id
}

func readOpsNestedFixture(t *testing.T) (f *readOpsFixture, deepID, subID, rootID astral.ObjectID) {
	t.Helper()

	f = newReadOpsFixture(t)
	sub := addFolder(t, f.mod, f.vol, f.vol.RootID, "sub")
	deep := addFolder(t, f.mod, f.vol, sub.ID, "deep")
	f.putFile(t, deep.ID, "a.txt", "hello\n")

	modTime := astral.Time(time.Unix(0, testModTime).UTC())
	cTime := astral.Time(time.Unix(0, testCreationTime).UTC())
	deepID = expectedFolderID(t, &vfsapi.Entry{Name: "a.txt", ID: contentID("hello\n"), ModTime: modTime, CreationTime: cTime})
	subID = expectedFolderID(t, &vfsapi.Entry{Name: "deep", ID: deepID, Flags: vfsapi.FlagFolder, ModTime: modTime, CreationTime: cTime})
	rootID = expectedFolderID(t, &vfsapi.Entry{Name: "sub", ID: subID, Flags: vfsapi.FlagFolder, ModTime: modTime, CreationTime: cTime})

	return
}

func TestLsAnswersSubfolderIDsComputedBottomUp(t *testing.T) {
	f, deepID, subID, _ := readOpsNestedFixture(t)

	if readOpsHash(deepID) != v2Hash {
		t.Fatalf("the independent computation gives %s, want vector V2 %s", readOpsHash(deepID), v2Hash)
	}

	if got := f.run(t, "ls", f.vfs).entries(t); len(got) != 1 || got[0].ID != subID {
		t.Fatalf("ls / = %+v, want sub with ID %v", got, subID)
	}
	if got := f.run(t, "ls", f.vfs, "path", "/sub").entries(t); len(got) != 1 || got[0].ID != deepID {
		t.Fatalf("ls /sub = %+v, want deep with ID %v", got, deepID)
	}

	// the computation cached the IDs
	var rows []dbEntry
	if err := f.mod.db.Where("vfs = ? AND flags & 1 = 1 AND object_id IS NULL", f.vfs).Find(&rows).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	// note: only the root row is left; an ls of a folder does not need the folder's own ID
	if len(rows) != 1 || rows[0].ParentID != rows[0].ID {
		t.Errorf("%d folders without a cached ID, want only the root", len(rows))
	}
}

func TestLsOfEmptyFolderEntryIsVectorV1(t *testing.T) {
	f := newReadOpsFixture(t)
	addFolder(t, f.mod, f.vol, f.vol.RootID, "sub")

	got := f.run(t, "ls", f.vfs).entries(t)
	if len(got) != 1 || got[0].ID.Size != v1Size || readOpsHash(got[0].ID) != v1Hash {
		t.Fatalf("ls / = %+v, want sub with the ObjectID of vector V1", got)
	}
}

// vfs.stat

func TestStatAnswersAFileAndAFolder(t *testing.T) {
	f, _, subID, _ := readOpsNestedFixture(t)
	f.putFile(t, f.vol.RootID, "top", "x")

	file := f.run(t, "stat", f.vfs, "path", "/top").entry(t)
	if file.Name != "top" || file.ID != contentID("x") || file.IsFolder() || time.Time(file.ModTime).UnixNano() != testModTime ||
		time.Time(file.CreationTime).UnixNano() != testCreationTime {
		t.Errorf("stat /top = %+v", file)
	}

	folder := f.run(t, "stat", f.vfs, "path", "/sub").entry(t)
	if folder.Name != "sub" || folder.ID != subID || !folder.IsFolder() || time.Time(folder.ModTime).UnixNano() != testModTime ||
		time.Time(folder.CreationTime).UnixNano() != testCreationTime {
		t.Errorf("stat /sub = %+v, want ID %v", folder, subID)
	}
}

func TestStatOfTheRoot(t *testing.T) {
	f := newReadOpsFixture(t)
	addFolder(t, f.mod, f.vol, f.vol.RootID, "sub")

	const rootTime int64 = 1800000000000000000
	if err := f.mod.db.Model(&dbEntry{}).Where("id = ?", f.vol.RootID).Update("mod_time", rootTime).Error; err != nil {
		t.Fatalf("update: %v", err)
	}

	root := f.run(t, "stat", f.vfs, "path", "/").entry(t)

	// the directory is vector V3: one empty sub-folder, the root's own time is not hashed
	if root.ID.Size != v3Size || readOpsHash(root.ID) != v3Hash {
		t.Errorf("root ID = size %d %s, want vector V3", root.ID.Size, readOpsHash(root.ID))
	}
	if root.Name != "" || !root.IsFolder() || time.Time(root.ModTime).UnixNano() != rootTime {
		t.Errorf("stat / = %+v, want empty name, folder flag, time %d", root, rootTime)
	}
}

func TestStatOfAnEmptyVFSRootIsVectorV1(t *testing.T) {
	f := newReadOpsFixture(t)

	root := f.run(t, "stat", f.vfs, "path", "/").entry(t)
	if root.ID.Size != v1Size || readOpsHash(root.ID) != v1Hash {
		t.Errorf("root ID = size %d %s, want vector V1", root.ID.Size, readOpsHash(root.ID))
	}
}

func TestStatErrors(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "file", "x")

	for path, want := range map[string]error{
		"/missing":    vfsapi.ErrNotFound,
		"/file/below": vfsapi.ErrNotAFolder,
		"":            vfsapi.ErrInvalidPath,
		"/file/":      vfsapi.ErrInvalidPath,
		"/a/../b":     vfsapi.ErrInvalidPath,
	} {
		f.run(t, "stat", f.vfs, "path", path).wantError(t, want)
	}
}

func TestStatNeedsAPath(t *testing.T) {
	f := newReadOpsFixture(t)

	// note: the path is required, so the router answers the missing argument before the op runs
	readOpsRoute(t, f.mod.OpStat, f.vfs, "stat").wantError(t, errors.New("argument required: path"))
	// vfs.read has no channel, so the missing argument rejects the query
	readOpsRoute(t, f.mod.OpRead, f.vfs, "read").wantRejected(t)
}

// vfs.statfs

func TestStatfsAnswersTheInfo(t *testing.T) {
	f, _, _, rootID := readOpsNestedFixture(t)
	f.putFile(t, f.vol.RootID, "a", "abc")
	f.putFile(t, f.vol.RootID, "b", "abc") // the same content counts once
	f.putFile(t, f.vol.RootID, "c", "de")

	info := f.run(t, "statfs", f.vfs).info(t)

	if !info.VFS.IsEqual(f.vfs) {
		t.Errorf("VFS = %v, want %v", info.VFS, f.vfs)
	}
	if info.Usage != astral.Uint64(6+3+2) {
		t.Errorf("Usage = %d, want 11", info.Usage)
	}
	// sub, deep, a.txt, a, b, c
	if info.Inodes != 6 {
		t.Errorf("Inodes = %d, want 6", info.Inodes)
	}
	if uint64(info.Quota) != defaultConfig.ByteLimit || uint64(info.InodeLimit) != defaultConfig.InodeLimit {
		t.Errorf("limits = %d, %d, want the defaults %d, %d", info.Quota, info.InodeLimit, defaultConfig.ByteLimit, defaultConfig.InodeLimit)
	}
	// note: the root now also holds a, b and c, so it differs from the nested root
	if info.Root.Size == 0 || info.Root == rootID {
		t.Errorf("Root = %v", info.Root)
	}
}

// TestVFSInfoCachesTheRootItReports checks that the info of statfs and volumes computes a cleared root in its
// transaction and caches the same ObjectID it answers.
func TestVFSInfoCachesTheRootItReports(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a", "abc")
	if err := f.mod.db.clearFolderIDs(f.vol.RootID, 0); err != nil {
		t.Fatalf("clearFolderIDs: %v", err)
	}

	info, err := f.mod.vfsInfo(f.vol)
	if err != nil {
		t.Fatalf("vfsInfo: %v", err)
	}

	root, err := f.mod.db.findEntry(f.vol.RootID)
	if err != nil {
		t.Fatalf("findEntry: %v", err)
	}
	if root.ObjectID == nil || *root.ObjectID != info.Root {
		t.Errorf("cached root = %v, want the reported %v", root.ObjectID, info.Root)
	}
	if info.Usage != 3 || info.Inodes != 1 {
		t.Errorf("Usage, Inodes = %d, %d, want 3, 1", info.Usage, info.Inodes)
	}
}

func TestStatfsRootIsTheRootObjectID(t *testing.T) {
	f, _, _, rootID := readOpsNestedFixture(t)

	if got := f.run(t, "statfs", f.vfs).info(t).Root; got != rootID {
		t.Errorf("Root = %v, want %v", got, rootID)
	}
}

func TestStatfsOfAnEmptyVFS(t *testing.T) {
	f := newReadOpsFixture(t)

	info := f.run(t, "statfs", f.vfs).info(t)
	if info.Usage != 0 || info.Inodes != 0 || readOpsHash(info.Root) != v1Hash {
		t.Errorf("info = %+v, want no usage, no entries, vector V1", info)
	}
}

func TestStatfsZeroLimitsMeanNoLimit(t *testing.T) {
	f := newReadOpsFixture(t)
	if err := f.mod.db.Model(&dbVolume{}).Where("id = ?", f.vol.ID).
		Updates(map[string]any{"byte_limit": limitColumn(0), "inode_limit": limitColumn(0)}).Error; err != nil {
		t.Fatalf("update: %v", err)
	}

	info := f.run(t, "statfs", f.vfs).info(t)
	if info.Quota != 0 || info.InodeLimit != 0 {
		t.Errorf("limits = %d, %d, want 0 (no limit)", info.Quota, info.InodeLimit)
	}
}

func TestStatfsReportsTheSetLimits(t *testing.T) {
	f := newReadOpsFixture(t)
	if err := f.mod.db.Model(&dbVolume{}).Where("id = ?", f.vol.ID).
		Updates(map[string]any{"byte_limit": limitColumn(100), "inode_limit": limitColumn(7)}).Error; err != nil {
		t.Fatalf("update: %v", err)
	}

	info := f.run(t, "statfs", f.vfs).info(t)
	if info.Quota != 100 || info.InodeLimit != 7 {
		t.Errorf("limits = %d, %d, want 100, 7", info.Quota, info.InodeLimit)
	}
}

// vfs.read

func TestReadAnswersTheWholeFile(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "hello world")

	res := f.run(t, "read", f.vfs, "path", "/a.txt")
	if res.Err != nil || string(res.Raw) != "hello world" {
		t.Fatalf("read = %q, %v; want the content", res.Raw, res.Err)
	}
}

func TestReadFromLocalWithTheDeviceZoneOnly(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "hello world")

	f.run(t, "read", f.vfs, "path", "/a.txt")

	if len(f.repo.reads) != 1 {
		t.Fatalf("%d repository reads, want 1", len(f.repo.reads))
	}
	if got := f.repo.reads[0].zone; got != astral.ZoneDevice {
		t.Errorf("zone = %v, want the device zone", got)
	}
}

// TestReadNeverWidensTheZone checks that vfs.read narrows the caller's zone to the device zone: a context without
// the device zone reaches the repository with no zone at all.
func TestReadNeverWidensTheZone(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "hello world")

	ctx := astral.NewContext(nil).WithZone(astral.ZoneVirtual | astral.ZoneNetwork)
	readOpsRouteIn(t, ctx, f.mod.OpRead, f.vfs, readOpsQuery("read", "path", "/a.txt"))

	if len(f.repo.reads) != 1 {
		t.Fatalf("%d repository reads, want 1", len(f.repo.reads))
	}
	if got := f.repo.reads[0].zone; got != 0 {
		t.Errorf("zone = %v, want no zone", got)
	}
}

func TestReadWindows(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "0123456789")

	for _, tc := range []struct {
		offset, limit uint64
		want          string
	}{
		{0, 0, "0123456789"},
		{4, 0, "456789"},
		{0, 3, "012"},
		{4, 3, "456"},
		{8, 100, "89"},
		{10, 0, ""}, // an offset equal to the size answers zero bytes
		{10, 5, ""},
	} {
		res := f.run(t, "read", f.vfs, "path", "/a.txt", "offset", fmt.Sprint(tc.offset), "limit", fmt.Sprint(tc.limit))
		if res.Err != nil || string(res.Raw) != tc.want {
			t.Errorf("offset %d limit %d = %q, %v; want %q", tc.offset, tc.limit, res.Raw, res.Err, tc.want)
		}
	}
}

func TestReadRejectsAnOffsetBeyondTheSize(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "0123456789")

	f.run(t, "read", f.vfs, "path", "/a.txt", "offset", "11").wantRejected(t)
	f.run(t, "read", f.vfs, "path", "/a.txt", "offset", "18446744073709551615").wantRejected(t)
}

func TestReadAHugeLimitReadsToTheEnd(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "0123456789")

	res := f.run(t, "read", f.vfs, "path", "/a.txt", "offset", "2", "limit", "18446744073709551615")
	if res.Err != nil || string(res.Raw) != "23456789" {
		t.Fatalf("read = %q, %v", res.Raw, res.Err)
	}
}

func TestReadRejectsEveryFailure(t *testing.T) {
	f := newReadOpsFixture(t)
	addFolder(t, f.mod, f.vol, f.vol.RootID, "dir")
	f.putFile(t, f.vol.RootID, "file", "x")
	addFile(t, f.mod, f.vol, f.vol.RootID, "lost", "content nobody stored")

	for name, path := range map[string]string{
		"a folder":        "/dir",
		"the root":        "/",
		"not found":       "/missing",
		"not a folder":    "/file/below",
		"invalid path":    "file",
		"missing content": "/lost",
	} {
		t.Run(name, func(t *testing.T) { f.run(t, "read", f.vfs, "path", path).wantRejected(t) })
	}
}

func TestReadRejectsWithoutALocalRepository(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "x")
	f.mod.Objects = &readOpsObjects{dirTestObjects: f.dirObjects, repos: map[string]objectsmod.Repository{}}

	f.run(t, "read", f.vfs, "path", "/a.txt").wantRejected(t)
}

func TestReadAnOtherVFSNeedsAPermit(t *testing.T) {
	f := newReadOpsFixture(t)
	f.putFile(t, f.vol.RootID, "a.txt", "mine")

	f.run(t, "read", f.other, "vfs", f.vfs.String(), "path", "/a.txt").wantRejected(t)

	f.auth.permit(&vfsapi.ReadAction{}, f.other, f.vfs)
	if res := f.run(t, "read", f.other, "vfs", f.vfs.String(), "path", "/a.txt"); res.Err != nil || string(res.Raw) != "mine" {
		t.Fatalf("read = %q, %v", res.Raw, res.Err)
	}
}

// TestStatfsWhileWritingDoesNotDeadlock: on the module with one database connection, concurrent vfs.write and
// vfs.statfs calls on one VFS all complete within a timeout.
func TestStatfsWhileWritingDoesNotDeadlock(t *testing.T) {
	mod, vol, _ := changeTestModule(t)
	const n = 8

	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := range n {
		wg.Add(2)
		go func() {
			defer wg.Done()
			path := fmt.Sprintf("/d/f%02d", i)
			answer, err := changeTestTryWrite(mod, vol, path, path)
			if _, ok := answer.(*vfsapi.Entry); err != nil || !ok {
				errs <- fmt.Errorf("write %s: %#v, %v", path, answer, err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := readOpsTryStatfs(mod, vol); err != nil {
				errs <- err
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(4 * changeTestWait):
		t.Fatal("the calls did not complete")
	}
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// readOpsTryStatfs calls vfs.statfs as the VFS identity and fails unless it answers an info. Any goroutine may
// call it.
func readOpsTryStatfs(mod *Module, vol *dbVolume) error {
	c, err := changeTestOpen(mod.OpStatfs, vol.Identity, map[string]any{})
	if err != nil {
		return err
	}
	defer c.close()

	answer, err := c.next()
	if err != nil {
		return fmt.Errorf("statfs: %w", err)
	}
	if _, ok := answer.(*vfsapi.Info); !ok {
		return fmt.Errorf("statfs answered %#v, want an info", answer)
	}

	return nil
}

// TestStatfsRootMatchesARecomputeAfterEveryChange: after each of mkdir, write, replace, move, touch and remove, the
// Root that vfs.statfs answers equals the root ObjectID recomputed from cleared caches.
func TestStatfsRootMatchesARecomputeAfterEveryChange(t *testing.T) {
	mod, vol, _ := changeTestModule(t)

	steps := map[string]func(){
		"mkdir": func() { changeTestAck(t, changeTestCall(t, mod.OpMkdir, vol, map[string]any{"path": "/a/b"})) },
		"write": func() { changeTestEntry(t, changeTestWrite(t, mod, vol, "/a/b/f", "content")) },
		// note: the statfs of the step before cached the root, so a replace that keeps the cache shows here.
		"replace": func() { changeTestEntry(t, changeTestWrite(t, mod, vol, "/a/b/f", "other content")) },
		"move": func() {
			changeTestEntry(t, changeTestCall(t, mod.OpMove, vol, map[string]any{"from": "/a/b/f", "to": "/c/g"}))
		},
		"touch": func() { changeTestEntry(t, changeTestCall(t, mod.OpTouch, vol, map[string]any{"path": "/a"})) },
		"remove": func() {
			changeTestAck(t, changeTestCall(t, mod.OpRemove, vol, map[string]any{"path": "/a", "recursive": "true"}))
		},
	}
	for _, name := range []string{"mkdir", "write", "replace", "move", "touch", "remove"} {
		steps[name]()

		info := readOpsStatfsInfo(t, mod, vol)
		changeTestCheckRecompute(t, mod, vol)

		fresh, err := mod.folderObjectID(vol.Identity, vol.RootID)
		if err != nil {
			t.Fatal(err)
		}
		if info.Root != fresh {
			t.Errorf("after %s: statfs Root %v, recomputed %v", name, info.Root, fresh)
		}
	}
}

// readOpsStatfsInfo calls vfs.statfs as the VFS identity and returns its info.
func readOpsStatfsInfo(t *testing.T, mod *Module, vol *dbVolume) *vfsapi.Info {
	t.Helper()

	answer := changeTestCall(t, mod.OpStatfs, vol, map[string]any{})
	info, ok := answer.(*vfsapi.Info)
	if !ok {
		t.Fatalf("statfs answered %#v, want an info", answer)
	}

	return info
}
