//go:build sqlite_native

package assets

import (
	"net/url"

	"gorm.io/driver/sqlite"
)

func init() {
	dbOpen = sqlite.Open

	// note: the cgo driver is mattn-backed and takes pragmas as individual
	// _name=value parameters, not the modernc _pragma=name(value) form.
	// url.URL escapes the path without eating its separators.
	dbDSN = func(path string) string {
		u := url.URL{
			Scheme:   "file",
			Path:     path,
			RawQuery: "_busy_timeout=10000&_journal_mode=WAL",
		}
		return u.String()
	}
}
