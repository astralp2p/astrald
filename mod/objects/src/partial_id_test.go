package objects

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// partialTimeout bounds every wait in these tests, so a stuck op fails instead of hanging.
const partialTimeout = 10 * time.Second

// storedText is the value of the typed object these tests store.
const storedText = "partial"

// partialTexts are the text forms of the partial ID of full.
var partialTexts = []struct {
	name   string
	render func(full *astral.ObjectID) string
}{
	{"data0", func(full *astral.ObjectID) string { return full.PartialString() }},
	{"short data1", func(full *astral.ObjectID) string { return (&astral.ObjectID{Hash: full.Hash}).String() }},
}

// repoArgs are the repository selections of an op with a read default, and the repository each one reads.
var repoArgs = []struct{ name, arg, repo string }{
	{"default", "", "main"},
	{"named", "repo=named", "named"},
}

// readsSink records every key the reads journal flushes.
type readsSink struct {
	mu   sync.Mutex
	keys []astral.ObjectID
}

func (s *readsSink) record(batch map[astral.ObjectID]astral.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id := range batch {
		s.keys = append(s.keys, id)
	}
	return nil
}

// opFixture is a Module that answers object ops from a memory repository registered as main,
// the read default, and a second one registered as named.
type opFixture struct {
	mod   *Module
	repo  *mem.Repository
	reads *readsSink
}

func newOpFixture(t *testing.T) *opFixture {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	// note: an sqlite :memory: database is per connection; one connection keeps every op on the same database.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	db := &DB{DB: gdb}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	reads := &readsSink{}
	lg := log.New(astral.GenerateIdentity())
	mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}, config: defaultConfig, db: db, log: lg}
	mod.objectsReadsJournal = newObjectsReadsJournal(reads.record, lg)

	repo := mem.New("main", 0)
	addRepo(t, mod, "main", repo)
	addRepo(t, mod, "named", mem.New("named", 0))

	return &opFixture{mod: mod, repo: repo, reads: reads}
}

// store commits data to the main repository and returns its full ID.
func (f *opFixture) store(t *testing.T, data []byte) *astral.ObjectID {
	t.Helper()

	return f.storeIn(t, "main", data)
}

// storeIn commits data to the repository registered as name and returns its full ID.
func (f *opFixture) storeIn(t *testing.T, name string, data []byte) *astral.ObjectID {
	t.Helper()

	w, err := f.mod.GetRepository(name).Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	id, err := w.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

// typedData returns storedText as a canonical String8.
func typedData(t *testing.T) []byte {
	t.Helper()

	data, err := astral.EncodeBytes(astral.NewString8(storedText), astral.Canonical())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

// journalKeys flushes the reads journal and returns every key it recorded.
func (f *opFixture) journalKeys(t *testing.T) []astral.ObjectID {
	t.Helper()

	if err := f.mod.objectsReadsJournal.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	f.reads.mu.Lock()
	defer f.reads.mu.Unlock()
	return append([]astral.ObjectID(nil), f.reads.keys...)
}

// trackedIDs returns the ID of every tracking row.
func (f *opFixture) trackedIDs(t *testing.T) []astral.ObjectID {
	t.Helper()

	var rows []*dbObject
	if err := f.mod.db.Find(&rows).Error; err != nil {
		t.Fatalf("list tracking rows: %v", err)
	}

	var ids []astral.ObjectID
	for _, row := range rows {
		ids = append(ids, *row.ID)
	}
	return ids
}

// expectOnly fails unless keys holds exactly want.
func expectOnly(t *testing.T, what string, keys []astral.ObjectID, want *astral.ObjectID) {
	t.Helper()

	if len(keys) != 1 || !keys[0].IsEqual(want) {
		t.Fatalf("%s keys %v; want only %v", what, keys, want)
	}
}

// queryOf joins op and its non-empty arguments into a query string.
func queryOf(op string, args ...string) string {
	var set []string
	for _, arg := range args {
		if arg != "" {
			set = append(set, arg)
		}
	}
	if len(set) == 0 {
		return op
	}
	return op + "?" + strings.Join(set, "&")
}

// awaitReply waits for the op to close the caller's end and returns the bytes it answered.
func awaitReply(t *testing.T, w *bufferWriter) []byte {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(partialTimeout):
		t.Fatal("the op never closed the caller's connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.buf.Bytes())
}

// decodeReplies decodes every object in a binary reply.
func decodeReplies(t *testing.T, data []byte) []astral.Object {
	t.Helper()

	receiver := channel.NewReceiver(bytes.NewReader(data))
	var replies []astral.Object
	for {
		o, err := receiver.Receive()
		if errors.Is(err, io.EOF) {
			return replies
		}
		if err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		replies = append(replies, o)
	}
}

// callRaw routes one query to fn and returns the raw bytes the op answered.
func callRaw(t *testing.T, fn any, queryString string) []byte {
	t.Helper()

	w := &bufferWriter{closed: make(chan struct{})}
	if err := route(t, fn, astral.GenerateIdentity(), queryString, w); err != nil {
		t.Fatalf("%s: %v", queryString, err)
	}
	return awaitReply(t, w)
}

// call routes one query to fn and returns every object the op answered.
func call(t *testing.T, fn any, queryString string) []astral.Object {
	t.Helper()

	return decodeReplies(t, callRaw(t, fn, queryString))
}

// stream routes a query to fn, sends ids on the op's input, and returns every object the op answered.
// why: route discards the op's input writer, so it cannot drive a streamed ID.
func stream(t *testing.T, fn any, queryString string, ids ...*astral.ObjectID) []astral.Object {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(partialTimeout)
	defer cancel()

	caller := astral.GenerateIdentity()
	w := &bufferWriter{closed: make(chan struct{})}
	in, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	if err != nil {
		t.Fatalf("%s: %v", queryString, err)
	}

	sent := make(chan error, 1)
	go func() { sent <- sendIDs(in, ids) }()

	replies := decodeReplies(t, awaitReply(t, w))

	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("send ids: %v", err)
		}
	case <-time.After(partialTimeout):
		t.Fatal("the op never read its input")
	}

	return replies
}

// sendIDs writes ids and an EOS in the binary format, then closes the op's input.
// note: the binary form of an ID is 40 bytes, Size then Hash.
func sendIDs(in io.WriteCloser, ids []*astral.ObjectID) error {
	defer in.Close()

	sender := channel.NewSender(in)
	for _, id := range ids {
		if err := sender.Send(id); err != nil {
			return err
		}
	}
	return sender.Send(&astral.EOS{})
}

// only fails unless replies hold exactly one object of type T, and returns it.
func only[T astral.Object](t *testing.T, replies []astral.Object) T {
	t.Helper()

	var want T
	if len(replies) != 1 {
		t.Fatalf("answered %d objects %v; want one %T", len(replies), replies, want)
	}
	got, ok := replies[0].(T)
	if !ok {
		t.Fatalf("answered %T %v; want %T", replies[0], replies[0], want)
	}
	return got
}

// streamed fails unless replies hold one object of type T and an EOS, and returns the object.
func streamed[T astral.Object](t *testing.T, replies []astral.Object) T {
	t.Helper()

	if len(replies) != 2 || replies[1].ObjectType() != (&astral.EOS{}).ObjectType() {
		t.Fatalf("answered %v; want one object and an EOS", replies)
	}
	return only[T](t, replies[:1])
}

// TestPartialID_Read reads a stored object by each text form of its partial ID, from
// the default and a named repository. The journal records the full ID.
func TestPartialID_Read(t *testing.T) {
	for _, repo := range repoArgs {
		for _, form := range partialTexts {
			t.Run(repo.name+"/"+form.name, func(t *testing.T) {
				f := newOpFixture(t)
				data := typedData(t)
				full := f.storeIn(t, repo.repo, data)

				got := callRaw(t, f.mod.OpRead, queryOf("objects.read", "id="+form.render(full), repo.arg))
				if !bytes.Equal(got, data) {
					t.Fatalf("read %q; want %q", got, data)
				}

				expectOnly(t, "journal", f.journalKeys(t), full)
			})
		}
	}
}

// TestPartialID_ReadWindow reads a window of a stored object by its partial ID. A limit
// past the end is bounded by the resolved size, and the journal records the full ID.
func TestPartialID_ReadWindow(t *testing.T) {
	f := newOpFixture(t)
	data := typedData(t)
	full := f.store(t, data)
	offset := len(data) - 2

	got := callRaw(t, f.mod.OpRead, fmt.Sprintf("objects.read?offset=%d&limit=10&id=%s", offset, full.PartialString()))
	if !bytes.Equal(got, data[offset:]) {
		t.Fatalf("read %q; want %q", got, data[offset:])
	}

	expectOnly(t, "journal", f.journalKeys(t), full)
}

// TestPartialID_Contains finds a stored object by each text form of its partial ID and
// by its 40-byte binary form on the input stream. Another hash is a miss.
func TestPartialID_Contains(t *testing.T) {
	for _, form := range partialTexts {
		t.Run(form.name, func(t *testing.T) {
			f := newOpFixture(t)
			full := f.store(t, typedData(t))

			has := only[*astral.Bool](t, call(t, f.mod.OpContains, "objects.contains?repo=main&id="+form.render(full)))
			if !*has {
				t.Fatal("contains answered false for a stored object")
			}
		})
	}

	t.Run("binary", func(t *testing.T) {
		f := newOpFixture(t)
		full := f.store(t, typedData(t))

		has := streamed[*astral.Bool](t, stream(t, f.mod.OpContains, "objects.contains?repo=main", &astral.ObjectID{Hash: full.Hash}))
		if !*has {
			t.Fatal("contains answered false for a stored object")
		}
	})

	t.Run("miss", func(t *testing.T) {
		f := newOpFixture(t)
		missing := &astral.ObjectID{Hash: [32]byte{0x5a}}

		has := only[*astral.Bool](t, call(t, f.mod.OpContains, "objects.contains?repo=main&id="+missing.PartialString()))
		if *has {
			t.Fatal("contains answered true for a hash no object has")
		}
	})
}

// TestPartialID_Delete deletes a stored object by each text form of its partial ID and
// by its 40-byte binary form on the input stream.
func TestPartialID_Delete(t *testing.T) {
	for _, form := range partialTexts {
		t.Run(form.name, func(t *testing.T) {
			f := newOpFixture(t)
			full := f.store(t, typedData(t))

			only[*astral.Ack](t, call(t, f.mod.OpDelete, "objects.delete?repo=main&id="+form.render(full)))
			expectDeleted(t, f, full)
		})
	}

	t.Run("binary", func(t *testing.T) {
		f := newOpFixture(t)
		full := f.store(t, typedData(t))

		streamed[*astral.Ack](t, stream(t, f.mod.OpDelete, "objects.delete?repo=main", &astral.ObjectID{Hash: full.Hash}))
		expectDeleted(t, f, full)
	})
}

// expectDeleted fails unless the main repository no longer holds full.
func expectDeleted(t *testing.T, f *opFixture, full *astral.ObjectID) {
	t.Helper()

	has, err := f.repo.Contains(astral.NewContext(nil), full)
	if err != nil || has {
		t.Fatalf("after delete, contains answered %v, %v; want false, nil", has, err)
	}
}

// TestPartialID_Load loads a stored object by each text form of its partial ID and by
// its 40-byte binary form, from the default and a named repository. The journal and
// the tracking table key the full ID.
func TestPartialID_Load(t *testing.T) {
	for _, repo := range repoArgs {
		for _, form := range partialTexts {
			t.Run(repo.name+"/"+form.name, func(t *testing.T) {
				f := newOpFixture(t)
				full := f.storeIn(t, repo.repo, typedData(t))

				o := only[*astral.String8](t, call(t, f.mod.OpLoad, queryOf("objects.load", "id="+form.render(full), repo.arg)))
				expectLoaded(t, f, o, full)
			})
		}

		t.Run(repo.name+"/binary", func(t *testing.T) {
			f := newOpFixture(t)
			full := f.storeIn(t, repo.repo, typedData(t))

			o := streamed[*astral.String8](t, stream(t, f.mod.OpLoad, queryOf("objects.load", repo.arg), &astral.ObjectID{Hash: full.Hash}))
			expectLoaded(t, f, o, full)
		})
	}
}

// expectLoaded fails unless o is the stored String8 and the journal and tracking table hold only full.
func expectLoaded(t *testing.T, f *opFixture, o *astral.String8, full *astral.ObjectID) {
	t.Helper()

	if o.String() != storedText {
		t.Fatalf("loaded %q; want %q", o.String(), storedText)
	}
	expectOnly(t, "journal", f.journalKeys(t), full)
	expectOnly(t, "tracking", f.trackedIDs(t), full)
}

// TestPartialID_Probe probes a stored object by each text form of its partial ID and by
// its 40-byte binary form, from the default and a named repository. The probe reports
// the full ID, and the tracking table keys it.
func TestPartialID_Probe(t *testing.T) {
	for _, repo := range repoArgs {
		for _, form := range partialTexts {
			t.Run(repo.name+"/"+form.name, func(t *testing.T) {
				f := newOpFixture(t)
				full := f.storeIn(t, repo.repo, typedData(t))

				probe := only[*objects.Probe](t, call(t, f.mod.OpProbe, queryOf("objects.probe", "id="+form.render(full), repo.arg)))
				expectProbed(t, f, probe, full, repo.repo)
			})
		}

		t.Run(repo.name+"/binary", func(t *testing.T) {
			f := newOpFixture(t)
			full := f.storeIn(t, repo.repo, typedData(t))

			probe := streamed[*objects.Probe](t, stream(t, f.mod.OpProbe, queryOf("objects.probe", repo.arg), &astral.ObjectID{Hash: full.Hash}))
			expectProbed(t, f, probe, full, repo.repo)
		})
	}
}

// expectProbed fails unless probe reports full, the stored type and the repository named repoName, and tracking holds only full.
func expectProbed(t *testing.T, f *opFixture, probe *objects.Probe, full *astral.ObjectID, repoName string) {
	t.Helper()

	if probe.ObjectID == nil || !probe.ObjectID.IsEqual(full) {
		t.Fatalf("probe reported ID %v; want the full ID %v", probe.ObjectID, full)
	}
	if want := astral.NewString8("").ObjectType(); string(probe.Type) != want {
		t.Fatalf("probe reported type %q; want %q", probe.Type, want)
	}
	if string(probe.Repo) != repoName {
		t.Fatalf("probe reported repo %q; want %q", probe.Repo, repoName)
	}
	expectOnly(t, "tracking", f.trackedIDs(t), full)
}

// TestPartialID_EmptyObject uses the full ID of the empty object, whose Size is 0. Read,
// probe, contains and delete resolve it, and the probe and the journal report it.
func TestPartialID_EmptyObject(t *testing.T) {
	f := newOpFixture(t)
	empty := f.store(t, nil)
	if empty.Size != 0 {
		t.Fatalf("the empty object has size %d", empty.Size)
	}

	if got := callRaw(t, f.mod.OpRead, "objects.read?id="+empty.String()); len(got) != 0 {
		t.Fatalf("read %q; want nothing", got)
	}
	expectOnly(t, "journal", f.journalKeys(t), empty)

	probe := only[*objects.Probe](t, call(t, f.mod.OpProbe, "objects.probe?id="+empty.String()))
	if probe.ObjectID == nil || !probe.ObjectID.IsEqual(empty) {
		t.Fatalf("probe reported ID %v; want %v", probe.ObjectID, empty)
	}

	has := only[*astral.Bool](t, call(t, f.mod.OpContains, "objects.contains?repo=main&id="+empty.String()))
	if !*has {
		t.Fatal("contains answered false for the empty object")
	}

	only[*astral.Ack](t, call(t, f.mod.OpDelete, "objects.delete?repo=main&id="+empty.String()))

	has = only[*astral.Bool](t, call(t, f.mod.OpContains, "objects.contains?repo=main&id="+empty.String()))
	if *has {
		t.Fatal("contains answered true after the empty object was deleted")
	}
}

// TestPartialID_ModuleKeepsTheArgument calls Load and Probe with a partial ID. The
// argument keeps Size 0, and only the full ID becomes a key.
func TestPartialID_ModuleKeepsTheArgument(t *testing.T) {
	f := newOpFixture(t)
	full := f.store(t, typedData(t))
	partial := &astral.ObjectID{Hash: full.Hash}
	ctx := astral.NewContext(nil)

	if _, err := f.mod.Load(ctx, f.repo, partial); err != nil {
		t.Fatalf("load: %v", err)
	}
	probe, err := f.mod.Probe(ctx, f.repo, partial)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}

	if partial.Size != 0 || partial.Hash != full.Hash {
		t.Fatalf("the argument became %v; want the partial ID", partial)
	}
	if probe.ObjectID == nil || !probe.ObjectID.IsEqual(full) {
		t.Fatalf("probe reported ID %v; want the full ID %v", probe.ObjectID, full)
	}
	expectOnly(t, "journal", f.journalKeys(t), full)
	expectOnly(t, "tracking", f.trackedIDs(t), full)
}

// recordingFinder answers FindObject with provider and keeps the IDs it was asked about.
type recordingFinder struct {
	provider *astral.Identity

	mu    sync.Mutex
	asked []astral.ObjectID
}

func (f *recordingFinder) FindObject(_ *astral.Context, objectID *astral.ObjectID) (<-chan *astral.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, *objectID)

	out := make(chan *astral.Identity, 1)
	out <- f.provider
	close(out)
	return out, nil
}

func (f *recordingFinder) recorded() []astral.ObjectID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]astral.ObjectID(nil), f.asked...)
}

// recordingDescriber answers DescribeObject with descriptor and keeps the IDs it was asked about.
type recordingDescriber struct {
	descriptor *objects.Descriptor

	mu    sync.Mutex
	asked []astral.ObjectID
}

func (d *recordingDescriber) DescribeObject(_ *astral.Context, objectID *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, *objectID)

	out := make(chan *objects.Descriptor, 1)
	out <- d.descriptor
	close(out)
	return out, nil
}

func (d *recordingDescriber) recorded() []astral.ObjectID {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]astral.ObjectID(nil), d.asked...)
}

// TestPartialID_FindAndDescribeIgnoreTheRepositories pins the decided limitation:
// find and describe do not resolve a partial ID locally. With no provider, both answer
// only an EOS for an object the main repository holds.
func TestPartialID_FindAndDescribeIgnoreTheRepositories(t *testing.T) {
	f := newOpFixture(t)
	full := f.store(t, typedData(t))
	partial := full.PartialString()

	for _, op := range []struct {
		name string
		fn   any
	}{{"objects.find", f.mod.OpFind}, {"objects.describe", f.mod.OpDescribe}} {
		replies := call(t, op.fn, op.name+"?id="+partial)
		if len(replies) != 1 || replies[0].ObjectType() != (&astral.EOS{}).ObjectType() {
			t.Fatalf("%s answered %v for a stored object; want only an EOS", op.name, replies)
		}
	}
}

// TestPartialID_FindAndDescribePassThePartialToProviders pins the other half of the
// limitation: every provider receives the partial ID unchanged, and its answer is the
// op's answer.
func TestPartialID_FindAndDescribePassThePartialToProviders(t *testing.T) {
	f := newOpFixture(t)
	full := f.store(t, typedData(t))
	partial := &astral.ObjectID{Hash: full.Hash}

	finder, describer := addRecordingProviders(t, f.mod)

	provider := streamed[*astral.Identity](t, call(t, f.mod.OpFind, "objects.find?id="+full.PartialString()))
	if !provider.IsEqual(finder.provider) {
		t.Fatalf("find answered %v; want the finder's provider %v", provider, finder.provider)
	}

	descriptor := streamed[*objects.Descriptor](t, call(t, f.mod.OpDescribe, "objects.describe?id="+full.PartialString()))
	if !descriptor.SourceID.IsEqual(describer.descriptor.SourceID) {
		t.Fatalf("describe answered a descriptor from %v; want the describer's %v", descriptor.SourceID, describer.descriptor.SourceID)
	}

	expectOnly(t, "finder", finder.recorded(), partial)
	expectOnly(t, "describer", describer.recorded(), partial)
}

// capRepo answers Read with its reader.
// note: the embedded nil interface panics on any other method, which asserts that Load calls nothing else.
type capRepo struct {
	objectsmod.Repository
	reader *capReader
}

func (repo *capRepo) Read(*astral.Context, *astral.ObjectID, int64, int64) (objectsmod.Reader, error) {
	return repo.reader, nil
}

// capReader serves data and reports id as the full ID of the object it opened.
type capReader struct {
	id     astral.ObjectID
	data   io.Reader
	served atomic.Uint64
	closed atomic.Bool
}

func (r *capReader) Read(p []byte) (int, error) {
	n, err := r.data.Read(p)
	r.served.Add(uint64(n))
	return n, err
}

func (r *capReader) Close() error {
	r.closed.Store(true)
	return nil
}

func (r *capReader) Repo() objectsmod.Repository { return nil }

func (r *capReader) ID() *astral.ObjectID {
	id := r.id
	return &id
}

// zeroReader serves zero bytes without end.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// overrunReader serves data and then more zero bytes than MaxObjectSize.
// why: the overrun is finite, so a load that lost its cap reads to the end and fails the served count instead of exhausting memory.
func overrunReader(data []byte) io.Reader {
	return io.MultiReader(bytes.NewReader(data), io.LimitReader(zeroReader{}, objectsmod.MaxObjectSize+1))
}

// TestPartialID_LoadCap loads through a stub reader. A resolved size above
// MaxObjectSize is refused before any byte is read, and a reader that serves more than
// MaxObjectSize is refused one byte past the cap. Neither load becomes a key.
func TestPartialID_LoadCap(t *testing.T) {
	data := typedData(t)
	cases := []struct {
		name       string
		reader     *capReader
		wantServed uint64
	}{
		{"resolved size", &capReader{id: astral.ObjectID{Size: uint64(objectsmod.MaxObjectSize) + 1}, data: bytes.NewReader(data)}, 0},
		{"overrun", &capReader{id: astral.ObjectID{Size: uint64(len(data))}, data: overrunReader(data)}, uint64(objectsmod.MaxObjectSize) + 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newOpFixture(t)
			addRepo(t, f.mod, "big", &capRepo{reader: c.reader})
			partial := &astral.ObjectID{Hash: [32]byte{0x5a}}

			reply := only[*astral.ErrorMessage](t, call(t, f.mod.OpLoad, "objects.load?repo=big&id="+partial.PartialString()))
			if reply.Error() != objectsmod.ErrObjectTooLarge.Error() {
				t.Fatalf("load answered %q; want %q", reply.Error(), objectsmod.ErrObjectTooLarge)
			}
			if n := c.reader.served.Load(); n != c.wantServed {
				t.Fatalf("the reader served %d bytes; want %d", n, c.wantServed)
			}
			if !c.reader.closed.Load() {
				t.Fatal("load left the reader open")
			}
			if keys := f.journalKeys(t); len(keys) != 0 {
				t.Fatalf("journal keys %v after a refused load; want none", keys)
			}
			if ids := f.trackedIDs(t); len(ids) != 0 {
				t.Fatalf("tracking keys %v after a refused load; want none", ids)
			}
		})
	}
}

// TestPartialID_LoadAtTheCap loads through a stub reader whose object is exactly
// MaxObjectSize bytes. The load reads the whole object, decodes it, and keys its full ID.
func TestPartialID_LoadAtTheCap(t *testing.T) {
	data := typedData(t)
	full := &astral.ObjectID{Size: uint64(objectsmod.MaxObjectSize), Hash: [32]byte{0x5a}}
	reader := &capReader{
		id:   *full,
		data: io.MultiReader(bytes.NewReader(data), io.LimitReader(zeroReader{}, objectsmod.MaxObjectSize-int64(len(data)))),
	}

	f := newOpFixture(t)
	addRepo(t, f.mod, "big", &capRepo{reader: reader})

	o := only[*astral.String8](t, call(t, f.mod.OpLoad, "objects.load?repo=big&id="+full.PartialString()))
	if n := reader.served.Load(); n != uint64(objectsmod.MaxObjectSize) {
		t.Fatalf("the reader served %d bytes; want %d", n, objectsmod.MaxObjectSize)
	}
	if !reader.closed.Load() {
		t.Fatal("load left the reader open")
	}
	expectLoaded(t, f, o, full)
}

// TestPartialID_ModuleClosesTheReader loads and probes through a stub reader. Both
// close the reader they opened.
func TestPartialID_ModuleClosesTheReader(t *testing.T) {
	data := typedData(t)
	ctx := astral.NewContext(nil)
	partial := &astral.ObjectID{Hash: [32]byte{0x5a}}

	calls := map[string]func(*Module, objectsmod.Repository) error{
		"load": func(mod *Module, repo objectsmod.Repository) error {
			_, err := mod.Load(ctx, repo, partial)
			return err
		},
		"probe": func(mod *Module, repo objectsmod.Repository) error {
			_, err := mod.Probe(ctx, repo, partial)
			return err
		},
	}

	for name, fn := range calls {
		t.Run(name, func(t *testing.T) {
			f := newOpFixture(t)
			reader := &capReader{id: astral.ObjectID{Size: uint64(len(data)), Hash: partial.Hash}, data: bytes.NewReader(data)}

			if err := fn(f.mod, &capRepo{reader: reader}); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !reader.closed.Load() {
				t.Fatalf("%s left the reader open", name)
			}
		})
	}
}

// actionObjectID returns the object an objects action names.
func actionObjectID(action auth.ActionObject) *astral.ObjectID {
	switch a := action.(type) {
	case *auth.SeeObjectsAction:
		return a.ObjectID
	case *auth.AdminObjectsAction:
		return a.ObjectID
	}
	return nil
}

// TestPartialID_RefusedCallerPerformsNoLookup routes a partial ID from a refused caller
// to every op that takes one. The action carries the partial request, and no
// repository, finder or describer is asked.
func TestPartialID_RefusedCallerPerformsNoLookup(t *testing.T) {
	partial := &astral.ObjectID{Hash: [32]byte{0x5a, 0x02}}
	id := "id=" + partial.PartialString()

	ops := []struct {
		name string
		op   func(*Module) any
		args string
	}{
		{"objects.read", func(m *Module) any { return m.OpRead }, queryOf("", id, "repo=main")},
		{"objects.load", func(m *Module) any { return m.OpLoad }, queryOf("", id, "repo=main")},
		{"objects.contains", func(m *Module) any { return m.OpContains }, queryOf("", id, "repo=main")},
		{"objects.probe", func(m *Module) any { return m.OpProbe }, queryOf("", id, "repo=main")},
		{"objects.delete", func(m *Module) any { return m.OpDelete }, queryOf("", id, "repo=main")},
		{"objects.find", func(m *Module) any { return m.OpFind }, queryOf("", id)},
		{"objects.describe", func(m *Module) any { return m.OpDescribe }, queryOf("", id)},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			repo := newFakeRepo(nil)
			addRepo(t, mod, "main", repo)
			finder, describer := addRecordingProviders(t, mod)
			w := newRecordingWriter()

			err := route(t, op.op(mod), astral.GenerateIdentity(), op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a refused caller: got err %v, want a rejection", op.name, err)
			}
			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}
			if n := repo.calls.Load(); n != 0 || len(finder.recorded()) != 0 || len(describer.recorded()) != 0 {
				t.Fatalf("%s looked the object up for a refused caller: %d repository calls, finder %v, describer %v", op.name, n, finder.recorded(), describer.recorded())
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("%s made %d authorization calls; want 1", op.name, len(actions))
			}
			if got := actionObjectID(actions[0]); got == nil || !got.IsEqual(partial) {
				t.Fatalf("%s authorized object %v; want the partial request %v", op.name, got, partial)
			}
		})
	}
}

// addRecordingProviders registers a recording finder and a recording describer in mod.
func addRecordingProviders(t *testing.T, mod *Module) (*recordingFinder, *recordingDescriber) {
	t.Helper()

	finder := &recordingFinder{provider: astral.GenerateIdentity()}
	describer := &recordingDescriber{descriptor: &objects.Descriptor{
		SourceID: astral.GenerateIdentity(),
		ObjectID: &astral.ObjectID{},
		Data:     astral.NewString8("described"),
	}}
	if err := mod.AddFinder(finder); err != nil {
		t.Fatalf("add finder: %v", err)
	}
	if err := mod.AddDescriber(describer); err != nil {
		t.Fatalf("add describer: %v", err)
	}
	return finder, describer
}
