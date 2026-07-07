package tor

import (
	"encoding/base32"
	"encoding/json"
	"errors"
	"github.com/cryptopunkscc/astral-go/astral"
	"io"
	"strings"
)

const DigestSize = 35

// Digest is an astral.Object that holds a Tor digest. Supports JSON and text.
type Digest []byte

var _ astral.Object = (*Digest)(nil)

// DigestFromString parses a base32-encoded Tor digest, with or without the ".onion" suffix.
func DigestFromString(s string) (Digest, error) {
	var d = Digest{}
	err := d.UnmarshalText([]byte(s))
	return d, err
}

// astral.Object

func (d Digest) ObjectType() string { return "mod.tor.digest" }

func (d Digest) WriteTo(w io.Writer) (n int64, err error) {
	n2, err := w.Write(d)
	return int64(n2), err
}

func (d *Digest) ReadFrom(r io.Reader) (n int64, err error) {
	var v = make([]byte, DigestSize)
	n2, err := io.ReadFull(r, v)
	if err == nil {
		*d = v
	}
	return int64(n2), err
}

// text support

func (d Digest) MarshalText() (text []byte, err error) {
	txt := strings.ToLower(base32.StdEncoding.EncodeToString(d)) + ".onion"
	return []byte(txt), nil
}

func (d *Digest) UnmarshalText(text []byte) error {
	var s = strings.ToUpper(string(text))
	s, _ = strings.CutSuffix(s, ".ONION")
	b, err := base32.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	if len(b) != DigestSize {
		return errors.New("invalid length")
	}
	*d = b
	return nil
}

// json support

func (d Digest) MarshalJSON() ([]byte, error) {
	txt, err := d.MarshalText()
	if err != nil {
		return nil, err
	}

	return json.Marshal(string(txt))
}

func (d *Digest) UnmarshalJSON(bytes []byte) (err error) {
	var s string
	err = json.Unmarshal(bytes, &s)
	if err != nil {
		return
	}

	return d.UnmarshalText([]byte(s))
}

// other

func (d Digest) String() string {
	return strings.ToLower(base32.StdEncoding.EncodeToString(d)) + ".onion"
}

func init() {
	_ = astral.Add(&Digest{})
}
