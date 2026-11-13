// Command driver runs a provider app on node2 that serves four ops, and a
// caller app on node1 that calls them by the provider's app identity across
// the link, to check that an app's arguments and streams survive the trip:
//
//   - opsurface_echo takes a typed struct of unusual argument values and
//     answers what the app received;
//
//   - opsurface_count streams 50 objects and an EOS to the caller;
//
//   - opsurface_bulk streams 1 MiB of raw bytes to the caller;
//
//   - opsurface_sink takes 1 MiB of objects from the caller and answers their
//     length and digest.
//
// The driver records what each call returned; verify.py judges it.
//
//	driver drive <session.json>   prints facts as JSON
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

const (
	streamItems = 50        // objects in the counted stream
	bulkSize    = 1 << 20   // bytes in the raw stream and in the chunked upload
	chunkSize   = 16 << 10  // bytes in one uploaded chunk
	unusedNote  = "<unset>" // what the echo reports for an argument the caller left out
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "drive" {
		fmt.Fprintln(os.Stderr, "usage: driver drive <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	out, _ := json.Marshal(map[string]any{"app_op_surface_remote": facts})
	fmt.Println(string(out))
	// why: once both apps are up, the oracle judges each call; only a flow
	// that could not start is the driver's failure.
	if err != nil && facts["failures"] == nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "call failed:", err)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]any, error) {
	facts := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return facts, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	ctx := astral.NewContext(nil)
	admin1, admin2 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token)

	// the provider app on node2: its own identity, token and client
	provider, err := apphostcli.New(nil, admin2).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the provider on node2: %w", err)
	}
	facts["provider"] = provider.Identity.String()
	stop, err := serve(ctx, n2, string(provider.Token))
	facts["provider_serves"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("the provider could not serve on node2: %w", err)
	}
	defer stop()

	// note: node1 routes to the provider through its relay contract, which
	// apphost.register pushes to the siblings in the background.
	facts["node1_knows_provider"] = waitHost(ctx, admin1, provider.Identity, n2.Identity)
	if facts["node1_knows_provider"] != true {
		return facts, fmt.Errorf("apphost.hosts on node1 never named node2 for the provider %v", provider.Identity)
	}

	// the caller app on node1: another identity, token and client
	caller, err := apphostcli.New(nil, admin1).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the caller on node1: %w", err)
	}
	facts["caller"] = caller.Identity.String()
	cli := connect(n1.Endpoint, string(caller.Token)).WithTarget(provider.Identity)
	// why: a query for an app on another node needs the network zone.
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	steps := []struct {
		fact string
		run  func() (any, error)
	}{
		{"echo", func() (any, error) { return callEcho(ctx, cli) }},
		{"count", func() (any, error) { return callCount(ctx, cli) }},
		{"bulk", func() (any, error) { return callBulk(ctx, cli) }},
		{"sink", func() (any, error) { return callSink(ctx, cli) }},
	}
	failures := map[string]string{}
	var failed []error
	for _, step := range steps {
		obs, err := step.run()
		facts[step.fact] = obs
		if err != nil {
			failures[step.fact] = err.Error()
			failed = append(failed, fmt.Errorf("%s: %w", step.fact, err))
		}
	}
	if len(failed) > 0 {
		facts["failures"] = failures
		return facts, errors.Join(failed...)
	}

	fmt.Fprintln(os.Stderr, "ok: the caller on node1 sent arguments and streams to the provider on node2 and got them back")
	return facts, nil
}

// Provider is the provider app's API.
type Provider struct{}

// EchoArgs holds argument values that a query string has to carry with care.
type EchoArgs struct {
	Text  string
	Small int64
	Big   uint64
	Ratio float64
	On    bool
	Off   bool
	Raw   []byte
	Who   *astral.Identity
	Note  *string // the caller leaves it out
}

// OpsurfaceEcho answers the arguments as the app received them, then the
// caller and the target the node delivered, then EOS.
func (*Provider) OpsurfaceEcho(_ *astral.Context, q *routing.IncomingQuery, args EchoArgs) error {
	ch := q.Accept()
	defer ch.Close()
	note := unusedNote
	if args.Note != nil {
		note = *args.Note
	}
	for _, obj := range []astral.Object{
		astral.NewString16(args.Text),
		astral.NewInt64(args.Small),
		(*astral.Uint64)(&args.Big),
		astral.NewFloat64(args.Ratio),
		astral.NewBool(args.On),
		astral.NewBool(args.Off),
		(*astral.Bytes16)(&args.Raw),
		args.Who,
		astral.NewString16(note),
		q.Caller(),
		q.Target(),
		&astral.EOS{},
	} {
		if err := ch.Send(obj); err != nil {
			return err
		}
	}
	return nil
}

// CountArgs is the argument of opsurface_count.
type CountArgs struct {
	N uint32 `query:"required"`
}

// OpsurfaceCount streams the integers 0..N-1, then EOS.
func (*Provider) OpsurfaceCount(_ *astral.Context, q *routing.IncomingQuery, args CountArgs) error {
	ch := q.Accept()
	defer ch.Close()
	for i := range args.N {
		if err := ch.Send(astral.NewInt64(int64(i))); err != nil {
			return err
		}
	}
	return ch.Send(&astral.EOS{})
}

// BulkArgs is the argument of opsurface_bulk.
type BulkArgs struct {
	Size uint32 `query:"required"`
}

// OpsurfaceBulk writes Size raw bytes of the test pattern, then closes.
func (*Provider) OpsurfaceBulk(_ *astral.Context, q *routing.IncomingQuery, args BulkArgs) error {
	conn := q.AcceptRaw()
	defer conn.Close()
	_, err := conn.Write(pattern(int(args.Size)))
	return err
}

// OpsurfaceSink reads chunks until EOS, then answers their total length and
// SHA-256.
func (*Provider) OpsurfaceSink(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	h := sha256.New()
	var total uint64
	for {
		obj, err := ch.Receive()
		if err != nil {
			return err
		}
		switch o := obj.(type) {
		case *astral.Bytes32:
			h.Write(*o)
			total += uint64(len(*o))
			continue
		case *astral.EOS:
		default:
			return fmt.Errorf("unexpected %s", obj.ObjectType())
		}
		break
	}
	digest := astral.Bytes8(h.Sum(nil))
	for _, obj := range []astral.Object{(*astral.Uint64)(&total), &digest, &astral.EOS{}} {
		if err := ch.Send(obj); err != nil {
			return err
		}
	}
	return nil
}

// pattern is n bytes that no truncation, shift or repeat of itself can pass for.
// verify.py computes the same bytes.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i>>8 + i>>16)
	}
	return b
}

// serve runs the provider API as the app that token authenticates, through its
// own client and registrar. It returns once the node has attached the app's
// handler, or with the error that stopped the registration.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, n node, token string) (func(), error) {
	cli := apphostcli.New(nil, connect(n.Endpoint, token))
	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(cli))
	attached := make(chan struct{})
	var once atomic.Bool
	onAttach := apps.WithRegistrationHook(func(*astral.Context) error {
		if once.CompareAndSwap(false, true) {
			close(attached)
		}
		return nil
	})
	// why host_ip: the node dials the handler's registered endpoint for every
	// query, and only an address at the node's host_ip reaches the test host
	// from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(n.HostIP, "0"))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Provider{}), reg, at, onAttach) }()

	select {
	case <-attached:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(30 * time.Second):
		cancel()
		return nil, errors.New("the app's handler was not attached within 30s")
	}
}

// waitHost polls apphost.hosts on the admin's node until it names host for
// app, within 30 seconds.
func waitHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
		found := false
		for h := range hosts {
			found = found || h.String() == host
		}
		if *errp == nil && found {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// open queries op with args and returns a channel to the provider.
func open(ctx *astral.Context, cli *astrald.Client, op string, args any) (*channel.Channel, func(), error) {
	cctx, cancel := ctx.WithTimeout(30 * time.Second)
	ch, err := cli.QueryChannel(cctx, op, args)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("querying %s: %w", op, err)
	}
	// why: a call that loses data would otherwise wait for it for ever.
	timer := time.AfterFunc(30*time.Second, func() { ch.Close() })
	return ch, func() { timer.Stop(); ch.Close(); cancel() }, nil
}

// recv receives one object of type T.
func recv[T astral.Object](ch *channel.Channel) (T, error) {
	var zero T
	obj, err := ch.Receive()
	if err != nil {
		return zero, err
	}
	t, ok := obj.(T)
	if !ok {
		return zero, fmt.Errorf("got %s (%v), want %T", obj.ObjectType(), obj, zero)
	}
	return t, nil
}

// echoed is what the echo call returned; verify.py holds the values sent.
type echoed struct {
	Text    string  `json:"text"`
	Small   int64   `json:"small"`
	Big     uint64  `json:"big"`
	Ratio   float64 `json:"ratio"`
	On      bool    `json:"on"`
	Off     bool    `json:"off"`
	Raw     string  `json:"raw"`      // hex
	Who     string  `json:"who"`      // the identity the provider received
	SentWho string  `json:"sent_who"` // the identity the caller sent
	Note    string  `json:"note"`
	Caller  string  `json:"caller"`
	Target  string  `json:"target"`
	EOS     bool    `json:"eos"`
}

// callEcho sends the typed arguments and returns what the provider received.
// It returns the observation it has even when the call stops short.
func callEcho(ctx *astral.Context, cli *astrald.Client) (any, error) {
	who := astral.GenerateIdentity()
	args := EchoArgs{
		Text:  "Héllo Wörld & 日本語 = 100% + more; /slash\\ ?query #hash 'q' \"dq\" <tag> a,b\ttab",
		Small: -9223372036854775808,
		Big:   18446744073709551615,
		Ratio: -0.000123456789,
		On:    true,
		Off:   false,
		Raw:   []byte{0, 1, 2, 0xfb, 0xff, 0xfe, '=', '+', '/', 0, 0x80},
		Who:   who,
	}
	// why: verify.py cannot see who was generated, so the driver records it.
	obs := &echoed{SentWho: who.String()}
	ch, done, err := open(ctx, cli, "opsurface_echo", args)
	if err != nil {
		return obs, err
	}
	defer done()

	text, err := recv[*astral.String16](ch)
	if err != nil {
		return obs, err
	}
	obs.Text = text.String()
	small, err := recv[*astral.Int64](ch)
	if err != nil {
		return obs, err
	}
	obs.Small = int64(*small)
	big, err := recv[*astral.Uint64](ch)
	if err != nil {
		return obs, err
	}
	obs.Big = uint64(*big)
	ratio, err := recv[*astral.Float64](ch)
	if err != nil {
		return obs, err
	}
	obs.Ratio = float64(*ratio)
	on, err := recv[*astral.Bool](ch)
	if err != nil {
		return obs, err
	}
	obs.On = bool(*on)
	off, err := recv[*astral.Bool](ch)
	if err != nil {
		return obs, err
	}
	obs.Off = bool(*off)
	rawb, err := recv[*astral.Bytes16](ch)
	if err != nil {
		return obs, err
	}
	obs.Raw = hex.EncodeToString(*rawb)
	gotWho, err := recv[*astral.Identity](ch)
	if err != nil {
		return obs, err
	}
	obs.Who = gotWho.String()
	note, err := recv[*astral.String16](ch)
	if err != nil {
		return obs, err
	}
	obs.Note = note.String()
	gotCaller, err := recv[*astral.Identity](ch)
	if err != nil {
		return obs, err
	}
	obs.Caller = gotCaller.String()
	gotTarget, err := recv[*astral.Identity](ch)
	if err != nil {
		return obs, err
	}
	obs.Target = gotTarget.String()
	if _, err := recv[*astral.EOS](ch); err != nil {
		return obs, fmt.Errorf("want EOS: %w", err)
	}
	obs.EOS = true
	return obs, nil
}

// counted is what the counted stream returned.
type counted struct {
	Items []int64 `json:"items"`
	EOS   bool    `json:"eos"`
}

func callCount(ctx *astral.Context, cli *astrald.Client) (any, error) {
	obs := &counted{Items: []int64{}}
	ch, done, err := open(ctx, cli, "opsurface_count", map[string]any{"n": streamItems})
	if err != nil {
		return obs, err
	}
	defer done()
	for i := range streamItems {
		v, err := recv[*astral.Int64](ch)
		if err != nil {
			return obs, fmt.Errorf("item %d of %d: %w", i, streamItems, err)
		}
		obs.Items = append(obs.Items, int64(*v))
	}
	if _, err := recv[*astral.EOS](ch); err != nil {
		return obs, fmt.Errorf("after %d items, want EOS: %w", streamItems, err)
	}
	obs.EOS = true
	return obs, nil
}

// digested is a length and a SHA-256 (hex) of bytes that crossed the link.
type digested struct {
	Length uint64 `json:"length"`
	SHA256 string `json:"sha256"`
	EOS    bool   `json:"eos,omitempty"`
}

func callBulk(ctx *astral.Context, cli *astrald.Client) (any, error) {
	obs := &digested{}
	cctx, cancel := ctx.WithTimeout(30 * time.Second)
	defer cancel()
	conn, err := cli.Query(cctx, "opsurface_bulk", map[string]any{"size": bulkSize})
	if err != nil {
		return obs, fmt.Errorf("querying opsurface_bulk: %w", err)
	}
	defer conn.Close()
	defer time.AfterFunc(30*time.Second, func() { conn.Close() }).Stop()
	got, err := io.ReadAll(conn)
	sum := sha256.Sum256(got)
	obs.Length, obs.SHA256 = uint64(len(got)), hex.EncodeToString(sum[:])
	if err != nil {
		return obs, fmt.Errorf("reading after %d bytes: %w", len(got), err)
	}
	return obs, nil
}

// callSink uploads the pattern and returns the length and digest the provider
// answered.
func callSink(ctx *astral.Context, cli *astrald.Client) (any, error) {
	obs := &digested{}
	ch, done, err := open(ctx, cli, "opsurface_sink", nil)
	if err != nil {
		return obs, err
	}
	defer done()
	data := pattern(bulkSize)
	for off := 0; off < len(data); off += chunkSize {
		chunk := astral.Bytes32(data[off : off+chunkSize])
		if err := ch.Send(&chunk); err != nil {
			return obs, fmt.Errorf("sending the chunk at %d: %w", off, err)
		}
	}
	if err := ch.Send(&astral.EOS{}); err != nil {
		return obs, fmt.Errorf("sending EOS: %w", err)
	}
	total, err := recv[*astral.Uint64](ch)
	if err != nil {
		return obs, err
	}
	obs.Length = uint64(*total)
	digest, err := recv[*astral.Bytes8](ch)
	if err != nil {
		return obs, err
	}
	obs.SHA256 = hex.EncodeToString(*digest)
	if _, err := recv[*astral.EOS](ch); err != nil {
		return obs, fmt.Errorf("want EOS: %w", err)
	}
	obs.EOS = true
	return obs, nil
}
