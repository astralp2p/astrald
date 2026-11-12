package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opReadArgs struct {
	VFS    *astral.Identity
	Path   string `query:"required"`
	Offset uint64
	Limit  uint64
}

// todo: implement vfs.read; the stub answers an error until a later part of the VFS ticket fills this file.
// why: the stub reads and changes nothing, so it asks for no named action; the part that fills it authorizes first.
func (mod *Module) OpRead(ctx *astral.Context, q *routing.IncomingQuery, args opReadArgs) error {
	// note: vfs.read answers raw bytes, so every failure rejects the query.
	return q.Reject()
}
