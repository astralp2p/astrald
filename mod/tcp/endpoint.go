package tcp

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
	"io"
	"net"
	"strconv"
)

var _ exonet.Endpoint = &Endpoint{}
var _ astral.Object = &Endpoint{}
var _ json.Marshaler = &Endpoint{}
var _ json.Unmarshaler = &Endpoint{}

// Endpoint is an astral.Object that holds information about a TCP endpoint, i.e. an IP address and a port.
// Supports JSON and text.
type Endpoint struct {
	IP   ip.IP
	Port astral.Uint16
}

func (e Endpoint) ObjectType() string {
	return "mod.tcp.endpoint"
}

func (e Endpoint) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *Endpoint) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

// exonet.Endpoint

func (e *Endpoint) Address() string {
	return net.JoinHostPort(e.IP.String(), strconv.Itoa(int(e.Port)))
}

func (e *Endpoint) Network() string {
	return "tcp"
}

func (e *Endpoint) Pack() []byte {
	var b = &bytes.Buffer{}
	if _, err := e.WriteTo(b); err != nil {
		return nil
	}
	return b.Bytes()
}

// Text marshaling

func (e Endpoint) MarshalText() (text []byte, err error) {
	return []byte(e.Address()), nil
}

func (e *Endpoint) UnmarshalText(text []byte) error {
	h, p, err := net.SplitHostPort(string(text))
	if err != nil {
		return err
	}

	ip, err := ip.ParseIP(h)
	if err != nil {
		return err
	}

	port, err := strconv.Atoi(p)
	if err != nil {
		return err
	}

	// check if port fits in 16 bits
	if (port >> 16) > 0 {
		return errors.New("port out of range")
	}

	e.IP = ip
	e.Port = astral.Uint16(port)

	return nil
}

func (e *Endpoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Address())
}

func (e *Endpoint) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	ep, err := ParseEndpoint(s)
	if err != nil {
		return err
	}
	*e = *ep
	return nil
}

// ...

func (e *Endpoint) String() string {
	return e.Address()
}

// IsZero reports whether the endpoint is unset.
// note: safe to call on a nil pointer; nil IP also counts as zero.
func (e *Endpoint) IsZero() bool {
	return e == nil || e.IP == nil
}

func ParseEndpoint(s string) (*Endpoint, error) {
	hostStr, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return nil, err
	}

	ip, err := ip.ParseIP(hostStr)
	if err != nil {
		return nil, err
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	// check if port fits in 16 bits
	if (port >> 16) > 0 {
		return nil, errors.New("port out of range")
	}

	return &Endpoint{
		IP:   ip,
		Port: astral.Uint16(port),
	}, nil
}

func init() {
	_ = astral.Add(&Endpoint{})
}
