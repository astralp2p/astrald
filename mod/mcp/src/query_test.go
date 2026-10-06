package mcp

import (
	"testing"

	"github.com/astralp2p/astral-go/api/messaging"
)

func TestParseSince(t *testing.T) {
	for _, c := range []struct {
		in   string
		want uint64
	}{
		{"", 0},
		{"0", 0},
		{"42", 42},
	} {
		got, err := parseSince(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseSince(%q) = %v, %v; want %v, nil", c.in, got, err, c.want)
		}
	}

	for _, in := range []string{"-1", "abc", "9223372036854775808"} {
		if got, err := parseSince(in); err == nil {
			t.Errorf("parseSince(%q) = %v, nil; want an error", in, got)
		}
	}
}

func TestParseRef(t *testing.T) {
	id := messaging.NewMessageID()

	ref, err := parseRef(messaging.BoxInbox, id.String())
	if err != nil {
		t.Fatalf("parseRef(inbox, id) error = %v, want nil", err)
	}
	if want := (messaging.MessageRef{Box: messaging.BoxInbox, ID: id}); ref != want {
		t.Fatalf("parseRef(inbox, id) = %+v, want %+v", ref, want)
	}

	_, err = parseRef(messaging.ListArchive, id.String())
	if want := "box is inbox or outbox, not archive"; err == nil || err.Error() != want {
		t.Fatalf("parseRef(archive, id) error = %v, want %q", err, want)
	}

	_, err = parseRef(messaging.BoxOutbox, "xyz")
	if want := "invalid message id"; err == nil || err.Error() != want {
		t.Fatalf("parseRef(outbox, xyz) error = %v, want %q", err, want)
	}
}
