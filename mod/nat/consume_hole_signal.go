package nat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
)

// ConsumeHoleSignalTypeLock and friends are the four signal types for the
// two-phase consume-hole handshake: initiator sends lock, holder replies
// locked; initiator sends take, holder replies taken.
const (
	ConsumeHoleSignalTypeLock   = "lock"
	ConsumeHoleSignalTypeLocked = "locked"
	ConsumeHoleSignalTypeTake   = "take"
	ConsumeHoleSignalTypeTaken  = "taken"
)

type ConsumeHoleSignal struct {
	Signal astral.String8
	Pair   astral.Nonce
	Ok     bool
	Error  astral.String8
}

func (s ConsumeHoleSignal) ObjectType() string { return "nat.consume_hole_signal" }

func (s ConsumeHoleSignal) Err() error {
	if s.Error == "" {
		return errors.New("unknown error")
	}
	return errors.New(string(s.Error))
}

func (s ConsumeHoleSignal) WriteTo(w io.Writer) (n int64, err error) {
	return astral.Objectify(&s).WriteTo(w)
}

func (s *ConsumeHoleSignal) ReadFrom(r io.Reader) (n int64, err error) {
	return astral.Objectify(s).ReadFrom(r)
}

func (s ConsumeHoleSignal) MarshalJSON() ([]byte, error) {
	type alias ConsumeHoleSignal
	return json.Marshal(alias(s))
}

func (s *ConsumeHoleSignal) UnmarshalJSON(bytes []byte) error {
	type alias ConsumeHoleSignal
	var a alias
	if err := json.Unmarshal(bytes, &a); err != nil {
		return err
	}
	*s = ConsumeHoleSignal(a)
	return nil
}

func init() {
	_ = astral.Add(&ConsumeHoleSignal{})
}

// ExpectConsumeHoleSignal returns a handler that validates pair and signal type.
func ExpectConsumeHoleSignal(pair astral.Nonce, signalType astral.String8, on func(*ConsumeHoleSignal) error) func(*ConsumeHoleSignal) error {
	return func(sig *ConsumeHoleSignal) error {
		if sig.Pair != pair {
			return fmt.Errorf("mismatched pair id %v (expected %v)", sig.Pair, pair)
		}
		if sig.Signal != signalType {
			return fmt.Errorf("expected %s, got %s", signalType, sig.Signal)
		}
		if on != nil {
			if err := on(sig); err != nil {
				return err
			}
		}
		return channel.ErrBreak
	}
}

// HandleFailedConsumeHoleSignal returns the signal's error when Ok is false, nil otherwise.
func HandleFailedConsumeHoleSignal(sig *ConsumeHoleSignal) error {
	if !sig.Ok {
		return sig.Err()
	}
	return nil
}
