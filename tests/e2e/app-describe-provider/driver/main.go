// Command driver runs a labeler app that serves objects.describe for node1,
// and asks node1 to describe a note as the User: the app's label comes back
// with the app as its source. Once the User revokes the app's describer grant,
// the same describe no longer reaches the app.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with a fresh app and note
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
)

const serveObjects = "mod.auth.serve_objects_action"

type node struct {
	Endpoint string `json:"endpoint"`
}

type session struct {
	Nodes map[string]node `json:"nodes"`
	Facts struct {
		UserToken string `json:"user_token"`
	} `json:"facts"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_describe_provider": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]bool, error) {
	facts := map[string]bool{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return facts, err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1 := s.Nodes["node1"]
	if s.Facts.UserToken == "" {
		return facts, fmt.Errorf("the session holds no user_token fact")
	}
	ctx := astral.NewContext(nil)
	user := connect(n1.Endpoint, s.Facts.UserToken)

	// the labeler knows one label, for a test-unique note
	nonce := astral.NewNonce().String()
	note, err := astral.ResolveObjectID(astral.NewString16("app-describe-provider note " + nonce))
	if err != nil {
		return facts, err
	}
	label := "holiday photos " + nonce

	// 1. the User registers the labeler app, asking for the describer role alone
	roles := astral.NewBundle()
	if err := roles.Append(astral.NewString8(string(auth.RoleDescriber))); err != nil {
		return facts, err
	}
	app, err := apphostcli.New(nil, user).Register(ctx, &auth.Permit{Action: serveObjects, Constraints: roles})
	if err != nil {
		return facts, fmt.Errorf("registering the labeler app: %w", err)
	}

	// 2. the app serves objects.describe and registers with node1 as a describer
	lab := &labeler{note: note, label: label}
	stop, err := serveLabeler(ctx, n1.Endpoint, string(app.Token), lab)
	facts["registered_as_describer"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("serving the labeler: %w", err)
	}
	defer stop()

	// 3. the User asks node1 to describe the note
	descriptors, err := describe(ctx, user, note)
	if err != nil {
		return facts, fmt.Errorf("objects.describe as the User: %w", err)
	}
	facts["describe_returned_the_label"] = len(descriptors) == 1 && labelOf(descriptors[0]) == label
	if !facts["describe_returned_the_label"] {
		return facts, fmt.Errorf("objects.describe %v answered %v; want the label %q alone", note, show(descriptors), label)
	}
	facts["source_is_the_app"] = descriptors[0].SourceID.IsEqual(app.Identity)
	if !facts["source_is_the_app"] {
		return facts, fmt.Errorf("the descriptor's source is %v; want the app %v", descriptors[0].SourceID, app.Identity)
	}
	facts["app_was_asked_once"] = lab.asked() == 1
	if !facts["app_was_asked_once"] {
		return facts, fmt.Errorf("the app was asked %d times for %v; want once", lab.asked(), note)
	}

	// 4. without the grant, the node no longer asks the app
	// note: the node checks the grant on every fan-out and drops a describer that
	// lost it, so this also leaves no describer registered for later tests.
	if err := apphostcli.New(nil, user).RevokeAction(ctx, app.Identity, serveObjects); err != nil {
		return facts, fmt.Errorf("revoking the app's grant: %w", err)
	}
	descriptors, err = describe(ctx, user, note)
	if err != nil {
		return facts, fmt.Errorf("objects.describe after the revoke: %w", err)
	}
	facts["revoked_app_is_not_asked"] = len(descriptors) == 0 && lab.asked() == 1
	if !facts["revoked_app_is_not_asked"] {
		return facts, fmt.Errorf("after the revoke, objects.describe answered %v and the app was asked %d times; want nothing and once",
			show(descriptors), lab.asked())
	}

	fmt.Fprintln(os.Stderr, "ok: the User's objects.describe on node1 returned the labeler app's label, sourced to the app")
	return facts, nil
}

// labeler is the app's describer: it labels its one note.
type labeler struct {
	note  *astral.ObjectID
	label string
	count atomic.Uint64
}

func (l *labeler) DescribeObject(ctx *astral.Context, id *astral.ObjectID) (<-chan *objects.Descriptor, error) {
	if id.IsEqual(l.note) {
		l.count.Add(1)
	}

	out := make(chan *objects.Descriptor)
	go func() {
		defer close(out)
		if id.IsEqual(l.note) {
			_ = sig.Send(ctx, out, &objects.Descriptor{ObjectID: id, Data: astral.NewString16(l.label)})
		}
	}()
	return out, nil
}

func (l *labeler) asked() uint64 { return l.count.Load() }

// serveLabeler serves the app on node1 and returns once node1 has registered
// it as a describer.
//
// note: WithObjectDescriber's lease keeper registers through the process-default
// objects client, so the app's client is also the process default here. The
// User's client stays a separate one.
func serveLabeler(ctx *astral.Context, endpoint, token string, lab *labeler) (func(), error) {
	router := apphost.NewRouter(endpoint, token)
	apphost.SetDefaultRouter(router)
	appCli := astrald.New(router)
	astrald.SetDefault(appCli)

	sctx, cancel := ctx.WithCancel()
	registered := make(chan struct{})
	var closed atomic.Bool
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, appCli)))

	served := make(chan error, 1)
	go func() {
		served <- apps.ServeWith(sctx, routing.NewApp(&struct{}{}), reg,
			apps.WithObjectDescriber(lab),
			// why last: hooks run in order, so this one runs once the describer
			// registration before it has succeeded.
			apps.WithRegistrationHook(func(*astral.Context) error {
				if closed.CompareAndSwap(false, true) {
					close(registered)
				}
				return nil
			}),
		)
	}()

	select {
	case <-registered:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(60 * time.Second):
		cancel()
		return nil, fmt.Errorf("the app did not register within 60 s")
	}
}

// describe runs objects.describe for id as the given client and collects every
// descriptor.
func describe(ctx *astral.Context, cli *astrald.Client, id *astral.ObjectID) ([]*objects.Descriptor, error) {
	dctx, cancel := ctx.WithTimeout(30 * time.Second)
	defer cancel()
	stream, errPtr := objectscli.New(nil, cli).Describe(dctx, id)
	var descriptors []*objects.Descriptor
	for d := range stream {
		descriptors = append(descriptors, d)
	}
	if errPtr != nil && *errPtr != nil {
		return descriptors, *errPtr
	}
	return descriptors, nil
}

// labelOf returns the text a descriptor carries, or "" when it carries no text.
func labelOf(d *objects.Descriptor) string {
	if s, ok := d.Data.(*astral.String16); ok {
		return string(*s)
	}
	return ""
}

func show(descriptors []*objects.Descriptor) []string {
	out := []string{}
	for _, d := range descriptors {
		out = append(out, fmt.Sprintf("%T %v from %v", d.Data, d.Data, d.SourceID))
	}
	return out
}
