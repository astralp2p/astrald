package main

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
)

// Greeting is an app-defined astral Object used to verify blueprint sync:
// after astral-hello starts, the type "astral_hello.greeting" must appear
// in objects.blueprints on the node.
type Greeting struct {
	Recipient astral.String8
	Message   astral.String16
}

func (*Greeting) ObjectType() string {
	return "astral_hello.greeting"
}

func (g Greeting) WriteTo(w io.Writer) (int64, error) {
	return astral.Objectify(&g).WriteTo(w)
}

func (g *Greeting) ReadFrom(r io.Reader) (int64, error) {
	return astral.Objectify(g).ReadFrom(r)
}

func init() {
	_ = astral.Add(&Greeting{})
}
