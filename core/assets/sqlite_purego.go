//go:build !sqlite_native

package assets

import (
	"github.com/glebarez/sqlite"
)

func init() {
	dbOpen = sqlite.Open

	// note: the pure-Go driver is modernc-backed and takes pragmas as
	// repeated _pragma=name(value) parameters.
	dbSettings = "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
}
