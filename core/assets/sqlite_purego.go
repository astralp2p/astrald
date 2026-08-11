//go:build !sqlite_native

package assets

import (
	"net/url"

	"github.com/glebarez/sqlite"
)

func init() {
	dbOpen = sqlite.Open

	// note: the pure-Go driver is modernc-backed and takes pragmas as
	// repeated _pragma=name(value) parameters. url.URL escapes the path
	// without eating its separators, which PathEscape would.
	dbDSN = func(path string) string {
		u := url.URL{
			Scheme:   "file",
			Path:     path,
			RawQuery: "_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)",
		}
		return u.String()
	}
}
