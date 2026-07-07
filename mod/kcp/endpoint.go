package kcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
)

var _ exonet.Endpoint = &Endpoint{}
var _ astral.Object = &Endpoint{}

// Endpoint is an astral.Object that holds information about a KCP endpoint (UDP host:port).
// Supports JSON and text.
type Endpoint struct {
	IP   ip.IP
	Port astral.Uint16
}

func (e Endpoint) ObjectType() string {
	return "mod.kcp.endpoint"
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
	return "kcp"
}

// HostString returns the IP address as a string
func (e *Endpoint) HostString() string {
	return e.IP.String()
}

// PortNumber returns the port number as an int
func (e *Endpoint) PortNumber() int {
	return int(e.Port)
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

	return e.UnmarshalText([]byte(s))
}

func (e *Endpoint) String() string {
	return e.Address()
}

// IsZero reports whether the endpoint is uninitialised; a nil receiver is treated as zero.
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

func (e *Endpoint) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   net.ParseIP(e.IP.String()),
		Port: int(e.Port),
	}
}

func init() {
	_ = astral.Add(&Endpoint{})
}
