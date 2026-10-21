// Command driver checks the delegated app registration policy and the
// authorizer assignments of one node.
//
//	driver drive  <session.json>   the delegate checks, then sets two authorizers; prints facts as JSON
//	driver verify <session.json>   after a node restart: both authorizers listed, then removed one by one
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

const factsKey = "policy_delegate"

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

type session struct {
	Nodes map[string]node            `json:"nodes"`
	Facts map[string]json.RawMessage `json:"facts"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	s, err := readSession(os.Args[2])
	if err != nil {
		fail(err)
	}
	ctx := astral.NewContext(nil)
	switch os.Args[1] {
	case "drive":
		facts := map[string]any{}
		err = drive(ctx, s.Nodes["node1"], facts)
		out, _ := json.Marshal(map[string]any{factsKey: facts})
		fmt.Println(string(out))
	case "verify":
		err = verify(ctx, s)
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "FAIL:", err)
	os.Exit(1)
}

func step(format string, args ...any) { fmt.Fprintf(os.Stderr, "ok: "+format+"\n", args...) }

func readSession(path string) (*session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if _, ok := s.Nodes["node1"]; !ok {
		return nil, errors.New("session.json names no node1")
	}
	return &s, nil
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// drive runs the delegate checks, then leaves two authorizers set for the
// oracle to find after the restart.
func drive(ctx *astral.Context, n node, facts map[string]any) error {
	admin := connect(n.Endpoint, n.Token)
	cp, err := startControlPlane(ctx, admin, n.Endpoint)
	if err != nil {
		return err
	}
	step("control plane %v serves the decision and authorizer ops", cp.id)
	if err := checkDelegate(ctx, admin, cp, facts); err != nil {
		return err
	}
	return setAuthorizers(ctx, admin, cp, facts)
}

// checkDelegate runs the four delegate checks and restores the bundled
// accept-all policy whatever their outcome.
func checkDelegate(ctx *astral.Context, admin *astrald.Client, cp *controlPlane, facts map[string]any) (err error) {
	// why: a policy left delegated to this driver's app, which exits with
	// the driver, refuses every later registration on the node.
	defer func() {
		if rerr := setPolicy(ctx, admin, query.Args{}); rerr != nil && err == nil {
			err = fmt.Errorf("restore accept-all: %w", rerr)
		}
	}()
	for _, check := range []func(*astral.Context, *astrald.Client, *controlPlane, map[string]any) error{
		checkHold, checkDisconnect, checkTimeout, checkDecisionAuthorizer,
	} {
		if err := check(ctx, admin, cp, facts); err != nil {
			return err
		}
	}
	return nil
}

// verify finds the two authorizers the driver set, which the node held across
// the harness's restart, and removes them one by one.
func verify(ctx *astral.Context, s *session) error {
	var st authzState
	if err := json.Unmarshal(s.Facts[factsKey], &st); err != nil {
		return fmt.Errorf("read the driver's facts: %w", err)
	}
	n := s.Nodes["node1"]
	return verifyAuthorizers(ctx, connect(n.Endpoint, n.Token), st)
}

func await[T any](ch <-chan T, d time.Duration, what string) (T, error) {
	var zero T
	select {
	case v := <-ch:
		return v, nil
	case <-time.After(d):
		return zero, fmt.Errorf("timed out after %v waiting for %s", d, what)
	}
}

func ack(ch *channel.Channel, err error) error {
	if err != nil {
		return err
	}
	defer ch.Close()
	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF())
}

// drain reads objects until EOS and returns them; an error object ends it.
func drain(ctx *astral.Context, c *astrald.Client, method string) ([]astral.Object, error) {
	ch, err := c.QueryChannel(ctx, method, nil)
	if err != nil {
		return nil, err
	}
	defer ch.Close()
	var out []astral.Object
	for {
		obj, err := ch.Receive()
		if err != nil {
			return out, err
		}
		switch o := obj.(type) {
		case *astral.EOS, nil:
			return out, nil
		case *astral.ErrorMessage:
			return out, errors.New(o.Error())
		}
		out = append(out, obj)
	}
}

func countTokens(ctx *astral.Context, c *astrald.Client) (int, error) {
	objs, err := drain(ctx, c, "apphost.list_tokens")
	return len(objs), err
}
