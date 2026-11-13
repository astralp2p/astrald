package vfs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

const changeTestWait = 5 * time.Second

// changeTestRepo is a repository in memory that records what its writers did.
type changeTestRepo struct {
	objectsmod.Repository
	mu        sync.Mutex
	createErr error
	created   int
	committed map[astral.ObjectID][]byte
	discarded int
}

func newChangeTestRepo() *changeTestRepo {
	return &changeTestRepo{committed: map[astral.ObjectID][]byte{}}
}

func (r *changeTestRepo) Label() string { return "test" }

func (r *changeTestRepo) Create(*astral.Context, *objectsmod.CreateOpts) (objectsapi.Writer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.createErr != nil {
		return nil, r.createErr
	}
	r.created++

	return &changeTestWriter{repo: r}, nil
}

func (r *changeTestRepo) counts() (created, committed, discarded int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.created, len(r.committed), r.discarded
}

type changeTestWriter struct {
	repo *changeTestRepo
	buf  bytes.Buffer
	done bool
}

func (w *changeTestWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *changeTestWriter) Commit() (*astral.ObjectID, error) {
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()

	w.done = true
	id := astral.ObjectID{Size: uint64(w.buf.Len()), Hash: sha256.Sum256(w.buf.Bytes())}
	w.repo.committed[id] = w.buf.Bytes()

	return &id, nil
}

func (w *changeTestWriter) Discard() error {
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()

	if !w.done {
		w.done = true
		w.repo.discarded++
	}

	return nil
}

// changeTestObjects answers the default write repository.
type changeTestObjects struct {
	objectsmod.Module
	repo objectsmod.Repository
}

func (o changeTestObjects) WriteDefault() objectsmod.Repository { return o.repo }

// changeTestModule returns a test module with a VFS and a repository in memory.
func changeTestModule(t *testing.T) (*Module, *dbVolume, *changeTestRepo) {
	t.Helper()

	mod := testModule(t)
	repo := newChangeTestRepo()
	mod.Objects = changeTestObjects{repo: repo}

	return mod, createVFS(t, mod, astral.GenerateIdentity()), repo
}

// changeTestClient is the caller's end of one routed query.
//
// note: the methods that return an error never call t.Fatal, so a goroutine other than the test's may use them.
type changeTestClient struct {
	t    *testing.T
	in   io.WriteCloser
	out  io.ReadCloser
	recv channel.Receiver
}

// changeTestOpen routes one query to an op and returns the caller's end. The caller closes it.
func changeTestOpen(fn any, caller *astral.Identity, args map[string]any) (*changeTestClient, error) {
	op, err := routing.NewOp(fn)
	if err != nil {
		return nil, fmt.Errorf("new op: %w", err)
	}

	out, outWriter := io.Pipe()
	in, err := op.RouteQuery(testContext(), astral.Launch(query.New(caller, caller, "op", args)), outWriter)
	if err != nil {
		return nil, fmt.Errorf("route: %w", err)
	}

	return &changeTestClient{in: in, out: out, recv: channel.NewReceiver(out)}, nil
}

// changeTestRoute routes one query to an op and returns the caller's end, closed when the test ends.
func changeTestRoute(t *testing.T, fn any, caller *astral.Identity, args map[string]any) *changeTestClient {
	t.Helper()

	c, err := changeTestOpen(fn, caller, args)
	if err != nil {
		t.Fatal(err)
	}
	c.t = t
	t.Cleanup(c.close)

	return c
}

func (c *changeTestClient) close() {
	c.in.Close()
	c.out.Close()
}

// send sends objects in the background; the op may stop reading at any time.
func (c *changeTestClient) send(objects ...astral.Object) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		sender := channel.NewSender(c.in)
		for _, obj := range objects {
			if sender.Send(obj) != nil {
				return
			}
		}
	}()

	return done
}

// next returns the next answer, or nil when the op closed the stream. It fails when no answer comes in time.
func (c *changeTestClient) next() (astral.Object, error) {
	type answer struct {
		obj astral.Object
		err error
	}
	got := make(chan answer, 1)
	go func() {
		obj, err := c.recv.Receive()
		got <- answer{obj, err}
	}()

	select {
	case a := <-got:
		if a.err != nil {
			return nil, nil
		}
		return a.obj, nil
	case <-time.After(changeTestWait):
		return nil, errors.New("no answer")
	}
}

// nextClosed waits until the op closes the stream and fails on any further answer.
func (c *changeTestClient) nextClosed() error {
	answer, err := c.next()
	switch {
	case err != nil:
		return err
	case answer != nil:
		return fmt.Errorf("answer %#v after the last one", answer)
	}

	return nil
}

// receive returns the next answer, or nil when the op closed the stream.
func (c *changeTestClient) receive() astral.Object {
	c.t.Helper()

	answer, err := c.next()
	if err != nil {
		c.t.Fatal(err)
	}

	return answer
}

// waitClosed waits until the op closes the stream and fails on any further answer.
func (c *changeTestClient) waitClosed() {
	c.t.Helper()

	if err := c.nextClosed(); err != nil {
		c.t.Fatal(err)
	}
}

// changeTestCall routes one query from the VFS identity and returns its first answer.
func changeTestCall(t *testing.T, fn any, vol *dbVolume, args map[string]any) astral.Object {
	t.Helper()

	return changeTestRoute(t, fn, vol.Identity, args).receive()
}

// changeTestWrite writes content as the VFS identity, in chunks of at most 4 bytes, and returns the answer after
// the ack. It returns once the op has closed the stream, so the op's writer has ended.
func changeTestWrite(t *testing.T, mod *Module, vol *dbVolume, path, content string) astral.Object {
	t.Helper()

	answer, err := changeTestTryWrite(mod, vol, path, content)
	if err != nil {
		t.Fatal(err)
	}

	return answer
}

// changeTestTryWrite is changeTestWrite for any goroutine: it returns an error instead of failing the test.
func changeTestTryWrite(mod *Module, vol *dbVolume, path, content string) (astral.Object, error) {
	c, err := changeTestOpen(mod.OpWrite, vol.Identity, map[string]any{"path": path})
	if err != nil {
		return nil, err
	}
	defer c.close()

	first, err := c.next()
	if err != nil {
		return nil, err
	}
	if _, ok := first.(*astral.Ack); !ok {
		return first, c.nextClosed()
	}

	var objects []astral.Object
	for b := []byte(content); len(b) > 0; {
		n := min(4, len(b))
		blob := astral.Blob(b[:n])
		objects = append(objects, &blob)
		b = b[n:]
	}
	c.send(append(objects, &objectsapi.CommitMsg{})...)

	answer, err := c.next()
	if err != nil {
		return nil, err
	}

	return answer, c.nextClosed()
}

// changeTestEntry asserts that an answer is an entry and returns it.
func changeTestEntry(t *testing.T, answer astral.Object) *vfsapi.Entry {
	t.Helper()

	entry, ok := answer.(*vfsapi.Entry)
	if !ok {
		t.Fatalf("answer = %#v, want an entry", answer)
	}

	return entry
}

// changeTestErr asserts that an answer is the error want.
func changeTestErr(t *testing.T, answer astral.Object, want error) {
	t.Helper()

	got, ok := answer.(astral.Error)
	if !ok || got.Error() != want.Error() {
		t.Fatalf("answer = %#v, want error %q", answer, want)
	}
}

// changeTestAck asserts that an answer is an ack.
func changeTestAck(t *testing.T, answer astral.Object) {
	t.Helper()

	if _, ok := answer.(*astral.Ack); !ok {
		t.Fatalf("answer = %#v, want ack", answer)
	}
}

// changeTestFind returns the row at a path, or nil when no entry is there.
func changeTestFind(t *testing.T, mod *Module, vol *dbVolume, path string) *dbEntry {
	t.Helper()

	names, err := parsePath(path)
	if err != nil {
		t.Fatalf("parse %q: %v", path, err)
	}

	row, err := mod.db.resolveEntry(vol, names)
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		return nil
	case err != nil:
		t.Fatalf("resolve %q: %v", path, err)
	}

	return row
}

// changeTestMustFind returns the row at a path and fails when none is there.
func changeTestMustFind(t *testing.T, mod *Module, vol *dbVolume, path string) *dbEntry {
	t.Helper()

	row := changeTestFind(t, mod, vol, path)
	if row == nil {
		t.Fatalf("no entry at %q", path)
	}

	return row
}

// changeTestSetLimits sets the limits of a VFS and returns it as stored.
func changeTestSetLimits(t *testing.T, mod *Module, vol *dbVolume, bytes, entries uint64) *dbVolume {
	t.Helper()

	err := mod.db.Model(&dbVolume{}).Where("id = ?", vol.ID).
		Updates(map[string]any{"byte_limit": limitColumn(bytes), "entry_limit": limitColumn(entries)}).Error
	if err != nil {
		t.Fatalf("set limits: %v", err)
	}

	vol, err = mod.db.findVolume(vol.Identity)
	if err != nil {
		t.Fatalf("find volume: %v", err)
	}

	return vol
}

// changeTestCount returns the number of entries of a VFS.
func changeTestCount(t *testing.T, mod *Module, vol *dbVolume) uint64 {
	t.Helper()

	n, err := mod.db.entryCount(vol.Identity)
	if err != nil {
		t.Fatalf("entry count: %v", err)
	}

	return n
}
