package vfs_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// The acceptance tests of the ticket "VFS: store folder listings as objects" on a node in the process.

// v1Bytes is the directory of the empty folder, vector V1 of VFS 6, section 5, under mod.vfs.directory.
const v1Bytes = "41444330116d6f642e7666732e6469726563746f727900000000"

// expectDirectory checks that a client probes an ObjectID as a directory and reads bytes that hash to it, and
// returns the bytes.
func expectDirectory(t *testing.T, a *acceptanceNode, c *astrald.Client, id astral.ObjectID) []byte {
	t.Helper()

	answer, err := a.call(c, "objects.probe", query.Args{"id": &id})
	probe, ok := answer.(*objectsapi.Probe)
	if err != nil || !ok || string(probe.Type) != vfsapi.DirectoryType {
		t.Fatalf("objects.probe %v answered %#v, %v; want type %v", id, answer, err, vfsapi.DirectoryType)
	}

	content, err := a.readRaw(c, "objects.read", query.Args{"id": &id})
	if err != nil {
		t.Fatalf("objects.read %v: %v", id, err)
	}
	if sha256.Sum256(content) != id.Hash || uint64(len(content)) != id.Size {
		t.Fatalf("objects.read %v answered %d bytes of another object", id, len(content))
	}

	return content
}

// statfs answers vfs.statfs of the caller's own VFS.
func statfs(t *testing.T, a *acceptanceNode, c *astrald.Client) *vfsapi.Info {
	t.Helper()

	answer, err := a.call(c, "vfs.statfs", nil)
	info, ok := answer.(*vfsapi.Info)
	if err != nil || !ok {
		t.Fatalf("vfs.statfs answered %#v, %v", answer, err)
	}

	return info
}

// stat answers vfs.stat of a path of the caller's own VFS.
func stat(t *testing.T, a *acceptanceNode, c *astrald.Client, path string) *vfsapi.Entry {
	t.Helper()

	answer, err := a.call(c, "vfs.stat", query.Args{"path": path})
	return expectEntry(t, "vfs.stat "+path, answer, err)
}

// TestAcceptanceDirectoryProbeAndRead (acceptance criteria 1 and 2, and Done): after vfs.stat answers a folder's
// ObjectID, an app with no object permit probes it as a directory and reads its bytes; the root of a new VFS is
// the empty folder of vector V1.
func TestAcceptanceDirectoryProbeAndRead(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app, fresh := a.registerApp(), a.registerApp()

	a.mustWrite(app.client, "/docs/a.txt", []byte("hello"))
	docs := stat(t, a, app.client, "/docs")
	expectDirectory(t, a, app.client, docs.ID)

	info := statfs(t, a, fresh.client)
	if got := hex.EncodeToString(expectDirectory(t, a, fresh.client, info.Root)); got != v1Bytes {
		t.Fatalf("the root of a new VFS reads %v, want V1", got)
	}
}

// TestAcceptanceDirectoryOfAnotherVFSRefused (acceptance criterion 13): an app cannot read the directory of a
// folder of another VFS by ObjectID.
func TestAcceptanceDirectoryOfAnotherVFSRefused(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	owner, other := a.registerApp(), a.registerApp()

	// note: not empty, because the empty folder is in every VFS
	a.mustWrite(owner.client, "/docs/a.txt", []byte("private"))
	docs := stat(t, a, owner.client, "/docs")

	if content, err := a.readRaw(other.client, "objects.read", query.Args{"id": &docs.ID}); err == nil {
		t.Fatalf("objects.read of another VFS's directory answered %d bytes; want a rejection", len(content))
	}
	if answer, err := a.call(other.client, "objects.probe", query.Args{"id": &docs.ID}); err == nil {
		if _, ok := answer.(*objectsapi.Probe); ok {
			t.Fatal("objects.probe of another VFS's directory answered a probe")
		}
	}
}

// rootID returns the cached root ObjectID of a VFS as the database holds it, or nil.
func (a *acceptanceNode) rootID(vfsID *astral.Identity) *astral.ObjectID {
	a.t.Helper()

	var ids []sql.NullString
	err := a.db.Table("vfs__entries").Select("vfs__entries.object_id").
		Joins("JOIN vfs__volumes v ON v.root_id = vfs__entries.id").
		Where("v.identity = ?", vfsID).Scan(&ids).Error
	if err != nil {
		a.t.Fatalf("root of %v: %v", vfsID, err)
	}
	if len(ids) != 1 || !ids[0].Valid {
		return nil
	}

	id, err := astral.ParseID(ids[0].String)
	if err != nil {
		a.t.Fatalf("root of %v: %v", vfsID, err)
	}

	return id
}

// TestAcceptanceDirectoriesStoredInBackground (acceptance criterion 4): after the quiet time the directories are
// stored without a request.
func TestAcceptanceDirectoriesStoredInBackground(t *testing.T) {
	a := startAcceptanceNodeWith(t, "directory_quiet_time: 200ms\ndirectory_longest_wait: 2s\n")
	a.claim()
	app := a.registerApp()

	a.mustWrite(app.client, "/docs/a.txt", []byte("hello"))

	a.eventually("the root is stored in the background", func() bool {
		id := a.rootID(app.id)
		return id != nil && a.contains(objectsapi.RepoLocal, id)
	})
}

// TestAcceptanceRestartComputesPending (acceptance criterion 10): a VFS with cleared ObjectIDs is computed after a
// restart without any request.
func TestAcceptanceRestartComputesPending(t *testing.T) {
	a := startAcceptanceNodeWith(t, "directory_quiet_time: 1h\ndirectory_longest_wait: 1h\n")
	a.claim()
	app := a.registerApp()
	a.mustWrite(app.client, "/docs/a.txt", []byte("hello"))
	if a.rootID(app.id) != nil {
		t.Fatal("the root was computed before the restart")
	}

	a.vfsYAML = "directory_quiet_time: 200ms\n"
	a.restart()

	a.eventually("the root is stored after the restart", func() bool {
		id := a.rootID(app.id)
		return id != nil && a.contains(objectsapi.RepoLocal, id)
	})
}

// TestAcceptanceCurrentListingSurvivesPurge (acceptance criterion 12): a purge of local keeps the current
// directory of a folder and deletes a replaced one that nothing holds.
func TestAcceptanceCurrentListingSurvivesPurge(t *testing.T) {
	a := startAcceptanceNode(t)
	a.claim()
	app := a.registerApp()

	a.mustWrite(app.client, "/docs/a.txt", []byte("a"))
	replaced := stat(t, a, app.client, "/docs").ID
	a.mustWrite(app.client, "/docs/b.txt", []byte("b"))
	current := stat(t, a, app.client, "/docs").ID

	a.age(3 * time.Hour)
	a.purge(objectsapi.RepoLocal, time.Hour, false)

	if !a.contains(objectsapi.RepoLocal, &current) {
		t.Fatal("the current directory was purged")
	}
	if a.contains(objectsapi.RepoLocal, &replaced) {
		t.Fatal("the replaced directory that nothing holds was kept")
	}
}
