package objects

import (
	"context"

	"github.com/astralp2p/astral-go/astral"
)

// housekeepingKey is the context key of the housekeeping mark.
type housekeepingKey struct{}

// WithHousekeeping returns a copy of ctx marked as housekeeping. Tracking counts no read made with a marked context.
// note: the mark is a context value of the node process. It is never sent on the wire, so an app or another node cannot set it.
// note: ctx.Detach() and ReadSeeker drop the mark, because both rebuild the context from identity and zone.
// note: the rules are in VFS 6, §11 "objects: last touched": https://wiki.satforge.dev/doc/vfs-6-specification-BxBnrSeTfT
func WithHousekeeping(ctx *astral.Context) *astral.Context {
	marked := ctx.WithFilters(ctx.Filters()...)
	marked.Context = context.WithValue(marked.Context, housekeepingKey{}, true)
	return marked
}

// IsHousekeeping reports whether ctx carries the housekeeping mark.
func IsHousekeeping(ctx *astral.Context) bool {
	marked, _ := ctx.Value(housekeepingKey{}).(bool)
	return marked
}
