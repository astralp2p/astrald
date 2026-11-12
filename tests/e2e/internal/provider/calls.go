package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
)

// FindAll runs objects.find for id through cli within timeout and returns
// every identity it named, in order.
func FindAll(ctx *astral.Context, cli *objectscli.Client, id *astral.ObjectID, timeout time.Duration) ([]*astral.Identity, error) {
	fctx, cancel := ctx.WithTimeout(timeout)
	defer cancel()
	stream, errPtr := cli.Find(fctx, id)
	var out []*astral.Identity
	for p := range stream {
		out = append(out, p)
	}
	return out, deref(errPtr)
}

// DescribeAll runs objects.describe for id through cli within timeout and
// returns every descriptor.
func DescribeAll(ctx *astral.Context, cli *objectscli.Client, id *astral.ObjectID, timeout time.Duration) ([]*objects.Descriptor, error) {
	dctx, cancel := ctx.WithTimeout(timeout)
	defer cancel()
	stream, errPtr := cli.Describe(dctx, id)
	var out []*objects.Descriptor
	for d := range stream {
		out = append(out, d)
	}
	return out, deref(errPtr)
}

// SearchAll runs objects.search for text through cli within timeout and
// returns every result.
func SearchAll(ctx *astral.Context, cli *objectscli.Client, text string, timeout time.Duration) ([]*objects.SearchResult, error) {
	sctx, cancel := ctx.WithTimeout(timeout)
	defer cancel()
	stream, errPtr := cli.Search(sctx, objects.SearchQuery{Query: astral.String16(text)})
	var out []*objects.SearchResult
	for r := range stream {
		out = append(out, r)
	}
	return out, deref(errPtr)
}

// ReadAll runs objects.read for id through cli, with no repository and the
// network zone included, within timeout, and returns the bytes.
func ReadAll(ctx *astral.Context, cli *objectscli.Client, id *astral.ObjectID, timeout time.Duration) ([]byte, error) {
	rctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(timeout)
	defer cancel()
	r, err := cli.Read(rctx, id, 0, 0)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func deref(errPtr *error) error {
	if errPtr == nil {
		return nil
	}
	return *errPtr
}

// ErrText is err's text, or "" for no error.
func ErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Emit prints facts as the one JSON line a driver's script.py collects, under
// name.
func Emit(name string, facts any) {
	out, err := json.Marshal(map[string]any{name: facts})
	if err != nil {
		fmt.Fprintln(os.Stderr, "facts:", err)
		return
	}
	fmt.Println(string(out))
}

// Checks records which checks of a flow held. In drive mode (Strict false) a
// failed Check is recorded and the flow goes on, so the oracle judges it; in
// verify mode (Strict true) it ends the flow. A failed Require always ends it:
// it is a wiring fault of the driver, not a verdict about astrald.
type Checks struct {
	Strict bool
	Held   map[string]bool
}

// NewChecks returns empty Checks, strict for mode "verify".
func NewChecks(mode string) *Checks {
	return &Checks{Strict: mode == "verify", Held: map[string]bool{}}
}

// Check records a verdict check and returns an error naming it when it failed
// and c is strict.
func (c *Checks) Check(name string, ok bool, format string, args ...any) error {
	c.Held[name] = ok
	if ok {
		fmt.Fprintln(os.Stderr, "ok:", name)
		return nil
	}
	err := fmt.Errorf("%s: "+format, append([]any{name}, args...)...)
	if !c.Strict {
		fmt.Fprintln(os.Stderr, "did not hold:", err)
		return nil
	}
	return err
}

// Require records a wiring check and returns an error naming it when it
// failed.
func (c *Checks) Require(name string, ok bool, format string, args ...any) error {
	c.Held[name] = ok
	if ok {
		fmt.Fprintln(os.Stderr, "ok:", name)
		return nil
	}
	return fmt.Errorf("driver wiring: %s: "+format, append([]any{name}, args...)...)
}
