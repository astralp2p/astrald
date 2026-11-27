package swarmset

import (
	"crypto/sha256"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// memFiles is an in-memory Files: one map of paths to content per identity.
type memFiles struct {
	mu  sync.Mutex
	vfs map[string]map[string][]byte
}

func newMemFiles() *memFiles { return &memFiles{vfs: map[string]map[string][]byte{}} }

func (f *memFiles) CreateVFS(id *astral.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vfs[id.String()] == nil {
		f.vfs[id.String()] = map[string][]byte{}
	}
	return nil
}

func (f *memFiles) WriteFile(_ *astral.Context, id *astral.Identity, path string, content []byte) (*astral.ObjectID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files, ok := f.vfs[id.String()]
	if !ok {
		return nil, vfsapi.ErrNoSuchVFS
	}
	files[path] = slices.Clone(content)
	return &astral.ObjectID{Size: uint64(len(content)), Hash: sha256.Sum256(content)}, nil
}

func (f *memFiles) ReadFile(_ *astral.Context, id *astral.Identity, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.vfs[id.String()][path]
	if !ok {
		return nil, vfsapi.ErrNotFound
	}
	return slices.Clone(data), nil
}

// ListFolder lists the names one level below path that some file path passes through.
func (f *memFiles) ListFolder(id *astral.Identity, path string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	for p := range f.vfs[id.String()] {
		rest, ok := strings.CutPrefix(p, path+"/")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		seen[name] = true
	}
	if len(seen) == 0 {
		return nil, vfsapi.ErrNotFound
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// put writes content at path as if placed directly in the VFS.
func (f *memFiles) put(t *testing.T, id *astral.Identity, path string, content []byte) {
	t.Helper()
	if _, err := f.WriteFile(nil, id, path, content); err != nil {
		t.Fatal(err)
	}
}

func encodeEntry(t *testing.T, obj astral.Object) []byte {
	t.Helper()
	data, err := astral.EncodeBytes(obj, astral.WithEncoder(astral.CanonicalTypeEncoder))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func entryIDOf(t *testing.T, obj astral.Object) *astral.ObjectID {
	t.Helper()
	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// setFixture is a Set over in-memory Files with the real Rules, a root key
// and a Trusted member the root signed in.
type setFixture struct {
	t       *testing.T
	files   *memFiles
	owner   *astral.Identity
	root    *astral.Identity
	trusted *astral.Identity
	set     *Set
	now     time.Time
	logged  []string
}

func newSetFixture(t *testing.T) *setFixture {
	f := &setFixture{t: t, files: newMemFiles(), owner: astral.GenerateIdentity(),
		root: astral.GenerateIdentity(), trusted: astral.GenerateIdentity(), now: time.Now()}
	f.open()
	f.mustAdd(f.membership(f.root, f.trusted, user.TierTrusted, time.Hour))
	return f
}

// open opens a new Set over the fixture's files, as a restart does.
func (f *setFixture) open() {
	f.t.Helper()
	f.set = New(Config{
		Checker: NewRules(keyVerifier{}, func() *astral.Identity { return f.root }),
		Now:     func() time.Time { return f.now },
		Log:     func(format string, v ...any) { f.logged = append(f.logged, format) },
	})
	if err := f.set.Open(astral.NewContext(nil), f.files, f.owner); err != nil {
		f.t.Fatalf("open: %v", err)
	}
}

func (f *setFixture) membership(issuer, subject *astral.Identity, tier user.Tier, d time.Duration) *auth.SignedContract {
	f.t.Helper()
	c, err := user.NewMembership(issuer, subject, tier, nil, d)
	if err != nil {
		f.t.Fatal(err)
	}
	return &auth.SignedContract{Contract: c, IssuerSig: sigBy(issuer), SubjectSig: sigBy(subject)}
}

func (f *setFixture) mustAdd(obj astral.Object) {
	f.t.Helper()
	added, err := f.set.Add(astral.NewContext(nil), obj)
	if err != nil || added.Verdict != user.EntryAccept {
		f.t.Fatalf("add %v: %v, %v; want accepted", obj.ObjectType(), added.Verdict, err)
	}
}

func (f *setFixture) stored(obj astral.Object) bool {
	return f.set.Has(entryIDOf(f.t, obj))
}

// TestOpenChecksEveryStoredEntry (criterion 4): entries placed directly in
// the VFS enter the set at Open only when they pass the entry check: a bad
// signature, a content ID that is not its file name, an entry in another
// kind's folder and an undecodable file are left out and logged; a good
// entry, and one that waits for a stored entry, are indexed.
func TestOpenChecksEveryStoredEntry(t *testing.T) {
	f := newSetFixture(t)

	good := contractRequest(f.trusted)
	badSig := contractRequest(f.trusted)
	badSig.FiledBySig = sigBy(f.root)
	renamed := contractRequest(f.trusted)
	answer := decline(f.root, entryIDOf(t, good))
	misfiled := contractRequest(f.trusted)

	f.files.put(t, f.owner, EntryPath(user.EntryDeclines, entryIDOf(t, answer)), encodeEntry(t, answer))
	f.files.put(t, f.owner, EntryPath(user.EntryRequests, entryIDOf(t, good)), encodeEntry(t, good))
	f.files.put(t, f.owner, EntryPath(user.EntryRequests, entryIDOf(t, badSig)), encodeEntry(t, badSig))
	f.files.put(t, f.owner, EntryPath(user.EntryRequests, entryIDOf(t, renamed)), encodeEntry(t, good))
	f.files.put(t, f.owner, EntryPath(user.EntryContracts, entryIDOf(t, misfiled)), encodeEntry(t, misfiled))
	garbage := &astral.ObjectID{Size: 3, Hash: sha256.Sum256([]byte("bad"))}
	f.files.put(t, f.owner, EntryPath(user.EntryRequests, garbage), []byte("bad"))

	f.open()

	switch {
	case !f.stored(good) || !f.stored(answer):
		t.Fatal("a valid stored entry is not indexed after Open")
	case f.stored(badSig):
		t.Fatal("an entry with a bad signature is indexed after Open")
	case f.set.Has(entryIDOf(t, renamed)):
		t.Fatal("a file whose name is not its content ID is indexed after Open")
	case f.stored(misfiled):
		t.Fatal("an entry in another kind's folder is indexed after Open")
	case f.set.Has(garbage):
		t.Fatal("an undecodable file is indexed after Open")
	}
	if got := f.set.Head(); got != 3 {
		t.Fatalf("head = %v; want the membership, the request and its decline", got)
	}
	if len(f.logged) != 4 {
		t.Fatalf("logged %v; want the four left out", f.logged)
	}
	if head, _ := f.files.ReadFile(nil, f.owner, HeadPath); string(head) != "3" {
		t.Fatalf("head file %q; want 3", head)
	}
}

// TestOpenKeepsEntriesWhoseGateLapsed: an entry that entered while its filer
// was a member stays stored after Open although the filer's membership has
// since expired and the filer is expelled.
func TestOpenKeepsEntriesWhoseGateLapsed(t *testing.T) {
	f := newSetFixture(t)
	member := astral.GenerateIdentity()
	f.mustAdd(f.membership(f.root, member, user.TierMember, time.Minute))
	req := contractRequest(member)
	f.mustAdd(req)
	f.mustAdd(&user.SignedExpulsion{
		Expulsion: &user.Expulsion{Issuer: f.root, Subject: member, ExpelledAt: astral.Now()},
		IssuerSig: sigBy(f.root),
	})

	f.now = f.now.Add(time.Hour)
	late := contractRequest(member)
	if added, _ := f.set.Add(astral.NewContext(nil), late); added.Verdict != user.EntryWait {
		t.Fatalf("a lapsed member's new request: %v; want wait", added.Verdict)
	}

	f.open()
	if !f.stored(req) || f.set.Head() != 4 {
		t.Fatalf("after Open: request stored %v, head %v; want stored, 4", f.stored(req), f.set.Head())
	}
}

// TestHeldSetIsBounded: past the held limit, the oldest held entry is dropped
// first.
func TestHeldSetIsBounded(t *testing.T) {
	f := newSetFixture(t)
	f.set.cfg.HeldLimit = 3

	var first *astral.ObjectID
	for i := range 4 {
		req := contractRequest(astral.GenerateIdentity())
		if i == 0 {
			first = entryIDOf(t, req)
		}
		if added, err := f.set.Add(astral.NewContext(nil), req); err != nil || added.Verdict != user.EntryWait {
			t.Fatalf("entry %v: %v, %v; want held", i, added.Verdict, err)
		}
	}
	if len(f.set.held) != 3 {
		t.Fatalf("held = %v; want 3", len(f.set.held))
	}
	if f.set.held[0].id == *first {
		t.Fatal("the oldest held entry was kept")
	}
}

// TestAddAdmitsHeldEntries: an entry that waits for its request is stored with
// the request, after it, and a duplicate is a no-op that stores nothing.
func TestAddAdmitsHeldEntries(t *testing.T) {
	f := newSetFixture(t)
	req := contractRequest(f.trusted)
	answer := decline(f.root, entryIDOf(t, req))

	added, err := f.set.Add(astral.NewContext(nil), answer)
	if err != nil || added.Verdict != user.EntryWait || f.stored(answer) {
		t.Fatalf("an answer before its request: %v, %v; want held", added.Verdict, err)
	}
	added, err = f.set.Add(astral.NewContext(nil), req)
	if err != nil || len(added.Stored) != 2 || !f.stored(answer) {
		t.Fatalf("the request stored %v entries, %v; want it and its answer", len(added.Stored), err)
	}
	added, err = f.set.Add(astral.NewContext(nil), req)
	if err != nil || added.Verdict != user.EntryAccept || len(added.Stored) != 0 {
		t.Fatalf("a duplicate: %v, %v stored, %v; want accepted, none stored", added.Verdict, len(added.Stored), err)
	}
}

// TestClosedSetRefuses: before Open the set lists nothing, its head is 0, and
// a read or an add is ErrNotOpen.
func TestClosedSetRefuses(t *testing.T) {
	s := New(Config{Checker: NewRules(keyVerifier{}, func() *astral.Identity { return nil })})
	if ids, err := s.Entries(user.EntryRequests); ids != nil || err != nil || s.Head() != 0 {
		t.Fatalf("closed: entries %v, %v, head %v; want none", ids, err, s.Head())
	}
	if _, err := s.Entry(&astral.ObjectID{}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("closed read: %v; want ErrNotOpen", err)
	}
	if _, err := s.Add(astral.NewContext(nil), contractRequest(astral.GenerateIdentity())); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("closed add: %v; want ErrNotOpen", err)
	}
}
