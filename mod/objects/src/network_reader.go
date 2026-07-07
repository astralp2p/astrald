package objects

import (
	"io"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/lib/query"
)

// NetworkReader streams an object from a remote provider. Seeking reopens the
// underlying connection at a new offset rather than seeking in place.
type NetworkReader struct {
	mod      *Module
	objectID *astral.ObjectID
	consumer *astral.Identity
	provider *astral.Identity

	pos int64
	io.ReadCloser
}

func (r *NetworkReader) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	r.pos += int64(n)
	return n, err
}

// Seek closes the current network read and reopens it at the resolved offset by
// issuing a fresh read query to the provider. Returns 0, not the new position.
func (r *NetworkReader) Seek(offset int64, whence int) (int64, error) {
	r.ReadCloser.Close()

	params := core.Params{
		"id": r.objectID.String(),
	}

	var o int64

	switch whence {
	case io.SeekStart:
		o = offset
	case io.SeekCurrent:
		o = r.pos + offset
	case io.SeekEnd:
		o = int64(r.objectID.Size) + offset
	}
	params.SetInt("offset", int(o))

	var q = astral.NewQuery(
		r.consumer,
		r.provider,
		core.Query(methodRead, params),
	)

	ctx, cancel := astral.
		NewContext(nil).
		WithIdentity(r.mod.node.Identity()).
		IncludeZone(astral.ZoneNetwork).
		WithTimeout(15 * time.Second)
	defer cancel()

	conn, err := query.RouteInFlight(ctx, r.mod.node, astral.Launch(q))
	if err != nil {
		return 0, err
	}

	r.ReadCloser = conn
	r.pos = o

	return 0, nil
}
