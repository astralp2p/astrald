package nat

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Endpoint represents a single NAT UDP endpoint.
type Endpoint struct {
	IP   ip.IP
	Port astral.Uint16
}

func (e Endpoint) ObjectType() string {
	return "nat.endpoint"
}

func (e Endpoint) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *Endpoint) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (e *Endpoint) Address() string {
	return net.JoinHostPort(e.IP.String(), strconv.Itoa(int(e.Port)))
}

func (e *Endpoint) Network() string {
	return "utp"
}

func (e *Endpoint) HostString() string {
	return e.IP.String()
}

func (e *Endpoint) PortNumber() int {
	return int(e.Port)
}

// Pack serializes the endpoint to its wire representation; returns nil on encoding error.
func (e *Endpoint) Pack() []byte {
	var b = &bytes.Buffer{}
	if _, err := e.WriteTo(b); err != nil {
		return nil
	}
	return b.Bytes()
}

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

	if (port >> 16) > 0 {
		return fmt.Errorf("port out of range")
	}

	e.IP = ip
	e.Port = astral.Uint16(port)

	return nil
}

func (e *Endpoint) String() string {
	return e.Address()
}

// IsZero reports whether the endpoint is unset; safe to call on a nil receiver.
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

	if (port >> 16) > 0 {
		return nil, fmt.Errorf("port out of range")
	}

	return &Endpoint{
		IP:   ip,
		Port: astral.Uint16(port),
	}, nil
}

func (e *Endpoint) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   net.ParseIP(e.IP.String()),
		Port: int(e.Port),
	}
}

func init() {
	_ = astral.Add(&Endpoint{})
}
