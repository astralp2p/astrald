package nodes

import (
	"bytes"
	"errors"
	"io"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/api/tcp"
	"github.com/cryptopunkscc/astral-go/api/tor"
	"github.com/cryptopunkscc/astral-go/api/utp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/streams"
	"github.com/jxskiss/base62"
)

type NodeInfo struct {
	Alias     astral.String8
	Identity  *astral.Identity
	Endpoints []exonet.Endpoint
}

// astral

func (NodeInfo) ObjectType() string {
	return "mod.nodes.node_info"
}

func (info NodeInfo) WriteTo(w io.Writer) (n int64, err error) {
	n, err = streams.WriteAllTo(w, info.Alias, info.Identity)
	if err != nil {
		return
	}

	var l = len(info.Endpoints)
	if l > 255 {
		return n, errors.New("too many endpoints")
	}

	m, err := astral.Uint8(l).WriteTo(w)
	n += m
	if err != nil {
		return
	}

	for _, e := range info.Endpoints {
		var t astral.Uint8
		switch e.(type) {
		case *tcp.Endpoint:
			t = 0
		case *tor.Endpoint:
			t = 1
		case *gateway.Endpoint:
			t = 2
		case *utp.Endpoint:
			t = 3
		default:
			return n, errors.New("unknown endpoint type")
		}

		m, err = t.WriteTo(w)
		n += m
		if err != nil {
			return
		}

		m, err = e.WriteTo(w)
		n += m
		if err != nil {
			return
		}
	}

	return
}

func (info *NodeInfo) ReadFrom(r io.Reader) (n int64, err error) {
	info.Identity = &astral.Identity{}
	n, err = streams.ReadAllFrom(r, &info.Alias, info.Identity)

	var l astral.Uint8
	m, err := l.ReadFrom(r)
	n += m
	if err != nil {
		return
	}

	for i := 0; i < int(l); i++ {
		// read the type
		var t astral.Uint8
		m, err = t.ReadFrom(r)
		n += m
		if err != nil {
			return
		}

		var e exonet.Endpoint

		switch t {
		case 0:
			e = &tcp.Endpoint{}
		case 1:
			e = &tor.Endpoint{}
		case 2:
			e = &gateway.Endpoint{}
		default:
			return n, errors.New("unknown endpoint type")
		}

		// read the endpoint payload
		m, err = e.ReadFrom(r)
		n += m
		if err != nil {
			return
		}

		info.Endpoints = append(info.Endpoints, e)
	}

	return
}

// text

func (info NodeInfo) MarshalText() (text []byte, err error) {
	var buf = &bytes.Buffer{}

	_, err = info.WriteTo(buf)
	if err != nil {
		return
	}

	return base62.Encode(buf.Bytes()), nil
}

func (info *NodeInfo) UnmarshalText(text []byte) (err error) {
	data, err := base62.Decode(text)
	if err != nil {
		return
	}

	_, err = info.ReadFrom(bytes.NewReader(data))

	return
}

func init() {
	_ = astral.Add(&NodeInfo{})
}
