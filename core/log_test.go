package core

import (
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// captureSink records the text of every entry the root logger emits.
//
// why no synchronization: LogEntry runs synchronously under the root mutex.
type captureSink struct{ lines []string }

func (s *captureSink) LogEntry(e *log.Entry) {
	s.lines = append(s.lines, astral.Stringify(e.Objects[len(e.Objects)-1]))
}

// TestAdapterWriteLogsEveryLineAndCarriesPartials: a Write carrying several
// lines logs all of them, and a Write ending mid-line is completed by the next.
// The value receiver discarded a.b on return, so only the first line of a
// multi-line write survived and a partial was lost entirely.
func TestAdapterWriteLogsEveryLineAndCarriesPartials(t *testing.T) {
	logger := log.New(nil)
	// why: the filter gates the console writer alone; subscribers still receive
	// every entry, so this keeps the pump from printing during the test.
	logger.SetFilter(func(*log.Entry) bool { return false })
	sink := &captureSink{}
	logger.AddLogger(sink)

	a := &adapter{Logger: logger}

	n, err := a.Write([]byte("line1\nline2\n"))
	if n != 12 || err != nil {
		t.Fatalf("Write(two lines) = (%d, %v), want (12, nil)", n, err)
	}
	if want := []string{"line1", "line2"}; !slices.Equal(sink.lines, want) {
		t.Fatalf("after two lines, entries = %q, want %q", sink.lines, want)
	}

	if _, err := a.Write([]byte("par")); err != nil {
		t.Fatalf("Write(partial) = %v, want nil", err)
	}
	if len(sink.lines) != 2 {
		t.Fatalf("after the partial write, entries = %q, want the first two only", sink.lines)
	}

	if _, err := a.Write([]byte("tial\n")); err != nil {
		t.Fatalf("Write(rest) = %v, want nil", err)
	}
	if want := []string{"line1", "line2", "partial"}; !slices.Equal(sink.lines, want) {
		t.Fatalf("after completing the partial, entries = %q, want %q", sink.lines, want)
	}
}
