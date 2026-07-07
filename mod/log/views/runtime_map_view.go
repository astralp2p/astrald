package views

import (
	"sort"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	"github.com/cryptopunkscc/astral-go/astral/log/styles"
)

// RuntimeMapView renders a blueprint-backed map as map[key: value, key: value], delegating both
// keys and values to fmt.ViewFor. The carrier's ObjectType is the constant "map", so a per-type
// builder matches it directly. See issue #337.
type RuntimeMapView struct {
	*astral.RuntimeMap
}

type mapEntry struct {
	key   any
	value astral.Object
}

func (v RuntimeMapView) Render() (out string) {
	// why: RuntimeMap.Each iterates in unspecified order; collect and sort so a given map always
	// logs identically. Keys are string or uint64 (homogeneous per map); less() orders both.
	var entries []mapEntry
	_ = v.Each(func(key any, value astral.Object) error {
		entries = append(entries, mapEntry{key: key, value: value})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool {
		return lessMapKey(entries[i].key, entries[j].key)
	})

	out += styles.Highlight.Render("map[")
	for i, e := range entries {
		if i > 0 {
			out += ", "
		}
		out += fmt.ViewFor(e.key).Render() +
			styles.Highlight.Render(": ") +
			fmt.ViewFor(e.value).Render()
	}
	out += styles.Highlight.Render("]")

	return
}

// lessMapKey orders two RuntimeMap keys. uint64 keys (uintN-keyed maps) compare numerically;
// anything else compares by its astral string form.
func lessMapKey(a, b any) bool {
	if ua, ok := a.(uint64); ok {
		if ub, ok := b.(uint64); ok {
			return ua < ub
		}
	}
	return astral.Stringify(a) < astral.Stringify(b)
}

func init() {
	fmt.SetView(func(o *astral.RuntimeMap) fmt.View {
		return RuntimeMapView{RuntimeMap: o}
	})
}
