package nodes

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
)

type SessionInfo struct {
	ID             astral.Nonce
	LinkID         astral.Nonce
	RemoteIdentity *astral.Identity
	Outbound       astral.Bool
	Query          astral.String16
	Bytes          astral.Uint64
	Age            astral.Duration
}

var _ astral.Object = &SessionInfo{}
var _ encoding.TextMarshaler = &SessionInfo{}
var _ json.Marshaler = &SessionInfo{}

func (s SessionInfo) ObjectType() string { return "mod.nodes.session_info" }

func (s SessionInfo) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *SessionInfo) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(s).ReadFrom(r)
}

func (s SessionInfo) MarshalText() ([]byte, error) {
	var b bytes.Buffer
	d := "<"
	if s.Outbound {
		d = ">"
	}
	age := time.Duration(s.Age).Round(time.Second)
	_, err := fmt.Fprintf(&b, "%v link=%v %v %v %v bytes=%v age=%v",
		s.ID, s.LinkID, d, s.RemoteIdentity, s.Query, s.Bytes, age)
	return b.Bytes(), err
}

func (s SessionInfo) MarshalJSON() ([]byte, error) {
	type Alias SessionInfo
	return json.Marshal(Alias(s))
}

func init() {
	astral.Add(&SessionInfo{})
}
