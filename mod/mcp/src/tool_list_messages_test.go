package mcp

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

func TestAnOutboxEntryNamesTheRecipientAndItsStamps(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	landed := astral.Time(time.Date(2024, 1, 2, 5, 4, 5, 6, time.FixedZone("+02", 2*60*60)))
	refusal := astral.String16("no")

	e := entry(&mcp.StoredMessage{
		ID:        mcp.NewMessageID(),
		Box:       mcp.BoxOutbox,
		Sender:    a,
		Recipient: b,
		CreatedAt: astral.Now(),
		LandedAt:  &landed,
		Err:       &refusal,
	})

	if e.Peer != b.String() {
		t.Errorf("Peer = %q, want the recipient %q", e.Peer, b.String())
	}
	if want := "2024-01-02T03:04:05.000000006Z"; e.LandedAt != want {
		t.Errorf("LandedAt = %q, want %q", e.LandedAt, want)
	}
	if e.FailedAt != "" || e.FetchedAt != "" {
		t.Errorf("FailedAt = %q, FetchedAt = %q; want both empty", e.FailedAt, e.FetchedAt)
	}
	if e.Err != "no" {
		t.Errorf("Err = %q, want %q", e.Err, "no")
	}
	if e.Read {
		t.Error("Read = true on an outbox entry, want false")
	}
}

func TestAnInboxEntryCarriesNoOutboxStamps(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	now := astral.Now()

	e := entry(&mcp.StoredMessage{
		ID:        mcp.NewMessageID(),
		Box:       mcp.BoxInbox,
		Sender:    b,
		Recipient: a,
		CreatedAt: now,
		ReadAt:    &now,
		LandedAt:  &now,
	})

	if e.Peer != b.String() {
		t.Errorf("Peer = %q, want the sender %q", e.Peer, b.String())
	}
	if !e.Read {
		t.Error("Read = false, want true for a row with ReadAt set")
	}
	if e.LandedAt != "" {
		t.Errorf("LandedAt = %q on an inbox entry, want empty", e.LandedAt)
	}
}

func TestAnEntryNamesItsParentOnlyWhenItHasOne(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	parent := mcp.NewMessageID()

	for _, c := range []struct {
		name   string
		parent mcp.MessageID
		want   string
	}{
		{"root", mcp.MessageID{}, ""},
		{"reply", parent, parent.String()},
	} {
		e := entry(&mcp.StoredMessage{
			ID:        mcp.NewMessageID(),
			Box:       mcp.BoxInbox,
			Sender:    b,
			Recipient: a,
			ParentID:  c.parent,
			CreatedAt: astral.Now(),
		})
		if e.ParentID != c.want {
			t.Errorf("%s: ParentID = %q, want %q", c.name, e.ParentID, c.want)
		}
	}
}

func TestAnAbsentInstantRendersEmpty(t *testing.T) {
	if got := stampOptionalTime(nil); got != "" {
		t.Fatalf("stampOptionalTime(nil) = %q, want empty", got)
	}
}
