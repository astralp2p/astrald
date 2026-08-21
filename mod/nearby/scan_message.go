package nearby

import (
	"github.com/astralp2p/astral-go/astral"
	"io"
)

// ScanMessage is the discovery probe sent to request a status broadcast from nearby nodes.
type ScanMessage struct{}

// astral

func (ScanMessage) ObjectType() string { return "mod.nearby.scan_message" }

func (s ScanMessage) WriteTo(w io.Writer) (n int64, err error) {
	return 0, nil
}

func (s *ScanMessage) ReadFrom(r io.Reader) (n int64, err error) {
	return 0, nil
}

func init() {
	_ = astral.Add(&ScanMessage{})
}
