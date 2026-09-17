package mcp

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

func TestAnUnnamedListIsTheInbox(t *testing.T) {
	q := messageQuery{}

	if err := q.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
	if q.List != listInbox {
		t.Fatalf("List = %q after validate, want %q", q.List, listInbox)
	}
	if got := q.order(); got != "seq" {
		t.Fatalf("order() = %q, want %q", got, "seq")
	}
}

func TestEachListAcceptsItsOwnNarrowing(t *testing.T) {
	id := astral.GenerateIdentity()

	for _, c := range []struct {
		name  string
		q     messageQuery
		order string
	}{
		{"inbox by sender, unread, since", messageQuery{List: listInbox, From: id, UnreadOnly: true, Since: 5}, "seq"},
		{"outbox by recipient, awaiting pickup", messageQuery{List: listOutbox, To: id, AwaitingPickup: true}, "seq desc"},
		{"archive", messageQuery{List: listArchive}, "created_at desc"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.q.validate(); err != nil {
				t.Fatalf("validate() = %v, want nil", err)
			}
			if got := c.q.order(); got != c.order {
				t.Fatalf("order() = %q, want %q", got, c.order)
			}
		})
	}
}

func TestTheArchiveRefusesADirectionalNarrowing(t *testing.T) {
	id := astral.GenerateIdentity()

	for _, c := range []struct {
		name string
		q    messageQuery
	}{
		{"from", messageQuery{List: listArchive, From: id}},
		{"to", messageQuery{List: listArchive, To: id}},
		{"awaiting_pickup", messageQuery{List: listArchive, AwaitingPickup: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.q.validate(); !errors.Is(err, errBadNarrowing) {
				t.Fatalf("validate() = %v, want an error wrapping %v", err, errBadNarrowing)
			}
		})
	}
}

func TestAnUnknownListIsNotANarrowingError(t *testing.T) {
	q := messageQuery{List: "elsewhere"}

	err := q.validate()
	if err == nil {
		t.Fatal("validate() = nil, want a refusal")
	}
	if want := "no such list: elsewhere"; err.Error() != want {
		t.Fatalf("validate() = %q, want %q", err, want)
	}
	if errors.Is(err, errBadNarrowing) {
		t.Fatalf("validate() = %v wraps %v, want an unknown-list error", err, errBadNarrowing)
	}
}

func TestParseSince(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"42", 42},
	} {
		got, err := parseSince(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseSince(%q) = %v, %v; want %v, nil", c.in, got, err, c.want)
		}
	}

	for _, in := range []string{"-1", "abc", "", "9223372036854775808"} {
		if got, err := parseSince(in); err == nil {
			t.Errorf("parseSince(%q) = %v, nil; want an error", in, got)
		}
	}
}

func TestParseRef(t *testing.T) {
	id := mcp.NewMessageID()

	ref, err := parseRef(mcp.BoxInbox, id.String())
	if err != nil {
		t.Fatalf("parseRef(inbox, id) error = %v, want nil", err)
	}
	if want := (messageRef{Box: mcp.BoxInbox, ID: id}); ref != want {
		t.Fatalf("parseRef(inbox, id) = %+v, want %+v", ref, want)
	}

	_, err = parseRef(listArchive, id.String())
	if want := "box is inbox or outbox, not archive"; err == nil || err.Error() != want {
		t.Fatalf("parseRef(archive, id) error = %v, want %q", err, want)
	}

	_, err = parseRef(mcp.BoxOutbox, "xyz")
	if want := "invalid message id"; err == nil || err.Error() != want {
		t.Fatalf("parseRef(outbox, xyz) error = %v, want %q", err, want)
	}
}

func TestNextSinceIsTheFurthestCursor(t *testing.T) {
	rows := func(cursors ...astral.Uint64) []*mcp.StoredMessage {
		var list []*mcp.StoredMessage
		for _, c := range cursors {
			list = append(list, &mcp.StoredMessage{Cursor: c})
		}
		return list
	}

	for _, c := range []struct {
		name string
		list []*mcp.StoredMessage
		want string
	}{
		{"no rows", nil, ""},
		{"unordered cursors", rows(3, 7, 5), "7"},
		{"a zero cursor", rows(0), ""},
	} {
		if got := nextSince(c.list); got != c.want {
			t.Errorf("%s: nextSince() = %q, want %q", c.name, got, c.want)
		}
	}
}
