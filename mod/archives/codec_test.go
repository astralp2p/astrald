package archives

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

func roundTrip(t *testing.T, obj astral.Object) astral.Object {
	t.Helper()

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, obj); err != nil {
		t.Fatalf("Encode(%s): %v", obj.ObjectType(), err)
	}

	decoded, _, err := astral.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode(%s): %v", obj.ObjectType(), err)
	}
	if buf.Len() != 0 {
		t.Errorf("Decode(%s): %d bytes left unread, want 0", obj.ObjectType(), buf.Len())
	}
	return decoded
}

func TestEventArchiveIndexed_RoundTrip(t *testing.T) {
	objectID, err := astral.Resolve(bytes.NewReader([]byte("archive")))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	modified := astral.Time(time.Unix(1700000000, 123))

	decoded := roundTrip(t, &EventArchiveIndexed{
		ObjectID: objectID,
		Archive: &Archive{
			Format:  "zip",
			Comment: "c",
			Entries: []*Entry{{Path: "dir/e.txt", Comment: "x", Modified: modified}},
		},
	})

	event, ok := decoded.(*EventArchiveIndexed)
	if !ok {
		t.Fatalf("Decode: got %T, want *EventArchiveIndexed", decoded)
	}
	if event.ObjectID == nil || *event.ObjectID != *objectID {
		t.Errorf("ObjectID: got %v, want %v", event.ObjectID, objectID)
	}
	if event.Archive == nil {
		t.Fatal("Archive: got nil")
	}
	if event.Archive.Format != "zip" || event.Archive.Comment != "c" {
		t.Errorf("Archive Format, Comment: got %q, %q, want \"zip\", \"c\"", event.Archive.Format, event.Archive.Comment)
	}
	if len(event.Archive.Entries) != 1 || event.Archive.Entries[0] == nil {
		t.Fatalf("Entries: got %v, want 1 entry", event.Archive.Entries)
	}

	entry := event.Archive.Entries[0]
	if entry.Path != "dir/e.txt" || entry.Comment != "x" {
		t.Errorf("entry Path, Comment: got %q, %q, want \"dir/e.txt\", \"x\"", entry.Path, entry.Comment)
	}
	if got, want := entry.Modified.Time(), modified.Time(); !got.Equal(want) || got.Nanosecond() != 123 {
		t.Errorf("entry Modified: got %v, want %v", got, want)
	}
}

func TestArchiveDescriptor_RoundTrip(t *testing.T) {
	want := ArchiveDescriptor{Format: "zip", Entries: 3, TotalSize: 42}

	decoded := roundTrip(t, &want)

	got, ok := decoded.(*ArchiveDescriptor)
	if !ok {
		t.Fatalf("Decode: got %T, want *ArchiveDescriptor", decoded)
	}
	if *got != want {
		t.Errorf("round trip: got %+v, want %+v", *got, want)
	}
	if got, want := (ArchiveDescriptor{}).ObjectType(), "mod.archives.archive_descriptor"; got != want {
		t.Errorf("ObjectType(): got %q, want %q", got, want)
	}
}

func TestArchiveTypesRegistered(t *testing.T) {
	for _, typeName := range []string{
		"mod.archives.entry",
		"mod.archives.archive",
		"mod.archives.archive_descriptor",
		"astrald.mod.archives.events.archive_indexed",
	} {
		if astral.New(typeName) == nil {
			t.Errorf("astral.New(%q): got nil, want a registered prototype", typeName)
		}
	}
}
