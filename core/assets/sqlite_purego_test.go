//go:build !sqlite_native

package assets

import "testing"

func TestDBDSNPureGo(t *testing.T) {
	const want = "file:///tmp/a%20b/x.db?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

	if got := dbDSN("/tmp/a b/x.db"); got != want {
		t.Fatalf("dbDSN = %q, want %q", got, want)
	}
}
