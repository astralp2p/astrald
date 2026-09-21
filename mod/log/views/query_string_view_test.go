package views

import (
	"testing"
)

// TestQueryStringViewSortsParams: one query string renders identically every
// time, so a log line for a given query can be grepped.
//
// why eight keys and twenty renders: Render re-parses the query on every call,
// so each render draws a fresh map order. Eight entries fill one map group,
// whose iteration starts at a random slot and wraps, so an unsorted render
// matches sorted order with probability at most 1/8 -- twenty renders bound a
// false pass below 1e-18.
func TestQueryStringViewSortsParams(t *testing.T) {
	view := NewQueryStringView("objects.search?h=8&g=7&f=6&e=5&d=4&c=3&b=2&a=1")
	want := "objects.search?a=1&b=2&c=3&d=4&e=5&f=6&g=7&h=8"

	for i := 0; i < 20; i++ {
		if got := ansi.ReplaceAllString(view.Render(), ""); got != want {
			t.Fatalf("render %d = %q; want %q", i, got, want)
		}
	}
}

// TestQueryStringViewNoParams guards the untouched len(params) > 0 branch: a
// query with no parameters renders no "?".
func TestQueryStringViewNoParams(t *testing.T) {
	view := NewQueryStringView("objects.search")

	if got, want := ansi.ReplaceAllString(view.Render(), ""), "objects.search"; got != want {
		t.Errorf("render = %q; want %q", got, want)
	}
}
