//go:build sqlite_native

package assets

import "testing"

func TestDBDSNCgo(t *testing.T) {
	const want = "/tmp/a b/x.db?_busy_timeout=10000&_journal_mode=WAL"

	if got, err := dbDSN("/tmp/a b/x.db"); err != nil || got != want {
		t.Fatalf("dbDSN = (%q, %v), want (%q, nil)", got, err, want)
	}
}
