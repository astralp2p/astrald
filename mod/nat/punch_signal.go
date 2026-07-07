package nat

import (
	"encoding/json"
	"io"

	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/astral"
)

// PunchSignalTypeOffer and friends define the five-step punch handshake order:
// initiator sends offer → passive responds with answer → passive signals ready
// → initiator signals go (both sides punch simultaneously) → initiator sends result.
const (
	PunchSignalTypeOffer  = "offer"
	PunchSignalTypeAnswer = "answer"
	PunchSignalTypeReady  = "ready"
	PunchSignalTypeGo     = "go"
	PunchSignalTypeResult = "result"
)

// PunchSignal represents control messages exchanged over the signalling channel.
type PunchSignal struct {
	Signal    astral.String8 `json:"signal"`
	Session   astral.Bytes8  `json:"session"`
	IP        ip.IP          `json:"ip"`
	Port      astral.Uint16  `json:"port"`
	PairNonce astral.Nonce   `json:"pair_nonce"`
}

func (n PunchSignal) ObjectType() string {
	return "nat.punch_signal"
}

func (e PunchSignal) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&e).WriteTo(w)
}

func (e *PunchSignal) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(e).ReadFrom(r)
}

func (f PunchSignal) MarshalJSON() ([]byte, error) {
	type alias PunchSignal
	return json.Marshal(alias(f))
}

func (f *PunchSignal) UnmarshalJSON(bytes []byte) error {
	type alias PunchSignal
	var a alias

	err := json.Unmarshal(bytes, &a)
	if err != nil {
		return err
	}

	*f = PunchSignal(a)
	return nil
}

func init() {
	_ = astral.Add(&PunchSignal{})
}
