package ip

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/cryptopunkscc/astral-go/astral"
)

type IP net.IP

func ParseIP(s string) (IP, error) {
	return IP(net.ParseIP(s)), nil
}

// astral

func (IP) ObjectType() string {
	return "mod.ip.ip_address"
}

func (ip IP) WriteTo(w io.Writer) (n int64, err error) {
	if ip.IsIPv4() {
		return astral.Bytes8(net.IP(ip).To4()).WriteTo(w)
	}

	return astral.Bytes8(ip).WriteTo(w)
}

func (ip *IP) ReadFrom(r io.Reader) (n int64, err error) {
	return (*astral.Bytes8)(ip).ReadFrom(r)
}

// json

func (ip IP) MarshalJSON() ([]byte, error) {
	return json.Marshal(ip.String())
}

func (ip *IP) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}

	parsed := IP(net.ParseIP(str))

	if parsed == nil {
		return fmt.Errorf("invalid IP")
	}

	*ip = parsed
	return nil
}

// text

func (ip IP) MarshalText() (text []byte, err error) {
	return []byte(ip.String()), nil
}

func (ip *IP) UnmarshalText(text []byte) error {
	parsed := IP(net.ParseIP(string(text)))
	if parsed == nil {
		return fmt.Errorf("invalid IP")
	}
	*ip = parsed
	return nil
}

// ...

func (ip IP) IsIPv4() bool {
	return net.IP(ip).To4() != nil
}

func (ip IP) IsIPv6() bool {
	return net.IP(ip).To16() != nil
}

func (ip IP) IsLoopback() bool { return net.IP(ip).IsLoopback() }

// IsGlobalUnicast is an alias for the native net.IP.IsGlobalUnicast.
// note: do not use this call to check if the IP is public
func (ip IP) IsGlobalUnicast() bool { return net.IP(ip).IsGlobalUnicast() }

// IsPrivate is an alias for the native net.IP.IsPrivate.
func (ip IP) IsPrivate() bool { return net.IP(ip).IsPrivate() }

// IsPublic returns true if the IP address is a public unicast address.
func (ip IP) IsPublic() bool { return ip.IsGlobalUnicast() && !ip.IsPrivate() }

// String returns a string representation of the IP address.
func (ip IP) String() string {
	return net.IP(ip).String()
}

func init() {
	_ = astral.Add(&IP{})
}
