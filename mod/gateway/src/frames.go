package gateway

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
)

var _ astral.Object = &Ping{}

// Ping is the keepalive message for idle connections; Pong==false is a request,
// Pong==true is the reply. A Ping without a timely response causes the conn to close.
type Ping struct {
	Pong astral.Bool
}

func (Ping) ObjectType() string                     { return "mod.gateway.ping" }
func (p Ping) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&p).WriteTo(w) }
func (p *Ping) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(p).ReadFrom(r) }

var _ astral.Object = &Handoff{}

// Handoff is sent by the gateway to an idle connection to trigger the activation
// handshake; the peer replies with HandoffAck and both sides leave idle mode.
type Handoff struct{}

func (Handoff) ObjectType() string                     { return "mod.gateway.handoff" }
func (s Handoff) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&s).WriteTo(w) }
func (s *Handoff) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(s).ReadFrom(r) }

var _ astral.Object = &HandoffAck{}

// HandoffAck is the peer's confirmation that it received a Handoff; after sending
// this, the idle connection transitions to an active data-carrying state.
type HandoffAck struct{}

func (HandoffAck) ObjectType() string                     { return "mod.gateway.handoff_ack" }
func (s HandoffAck) WriteTo(w io.Writer) (int64, error)   { return astral.Objectify(&s).WriteTo(w) }
func (s *HandoffAck) ReadFrom(r io.Reader) (int64, error) { return astral.Objectify(s).ReadFrom(r) }

func init() { _ = astral.Add(&Handoff{}) }
func init() { _ = astral.Add(&HandoffAck{}) }
func init() { _ = astral.Add(&Ping{}) }
