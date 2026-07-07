package ether

import (
	"bytes"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/streams"
)

var _ astral.Object = &Broadcast{}

type Broadcast struct {
	Timestamp astral.Time
	Source    *astral.Identity
	Object    astral.Object
}

var _ astral.Object = &Broadcast{}

// astral

func (Broadcast) ObjectType() string { return "mod.ether.broadcast" }

func (b Broadcast) WriteTo(w io.Writer) (n int64, err error) {
	var buf = &bytes.Buffer{}
	astral.Encode(buf, b.Object)

	return streams.WriteAllTo(w,
		b.Timestamp,
		b.Source,
		astral.Bytes16(buf.Bytes()),
	)
}

func (b *Broadcast) ReadFrom(r io.Reader) (n int64, err error) {
	var buf astral.Bytes16
	b.Source = &astral.Identity{}

	n, err = streams.ReadAllFrom(r, &b.Timestamp, b.Source, &buf)
	if err != nil {
		return
	}

	b.Object, _, err = astral.Decode(bytes.NewReader(buf))
	if err != nil {
		return
	}

	return
}

func init() {
	_ = astral.Add(&Broadcast{})
}
