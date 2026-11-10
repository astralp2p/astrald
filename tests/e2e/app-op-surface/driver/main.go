// Command driver runs one app on node1 that serves five ops, and a second app
// on node1 that calls them, to check the argument and stream round trip an app
// relies on:
//
//   - opsurface_echo takes a typed struct of unusual argument values and
//     answers what the app received;
//
//   - opsurface_count streams 50 objects and an EOS to the caller;
//
//   - opsurface_pipe answers each object the caller sends, in lockstep, then
//     the caller's EOS with its own;
//
//   - opsurface_bulk streams 1 MiB of raw bytes to the caller;
//
//   - opsurface_sink takes 1 MiB of objects from the caller and answers their
//     length and digest.
//
//     driver drive  <session.json>   prints facts as JSON
//     driver verify <session.json>   the same story with fresh identities
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	streamItems = 50        // objects in each stream leg
	bulkSize    = 1 << 20   // bytes in the raw stream and in the chunked upload
	chunkSize   = 16 << 10  // bytes in one uploaded chunk
	unusedNote  = "<unset>" // what the echo reports for an argument the caller left out
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_op_surface": facts})
		fmt.Println(string(out))
		// why: once both apps are up, the oracle judges each step; only a
		// flow that could not start is the driver's failure.
		if err != nil && facts["failures"] == nil {
			fmt.Fprintln(os.Stderr, "FAIL:", err)
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "step failed:", err)
		}
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
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
	n1 := s.Nodes["node1"]
	ctx := astral.NewContext(nil)
	admin := connect(n1.Endpoint, n1.Token)

	// the provider app: its own identity, token and client, serving the ops
	provider, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the provider: %w", err)
	}
	stop, err := serve(ctx, n1.Endpoint, string(provider.Token))
	facts["provider_serves"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("the provider could not serve: %w", err)
	}
	defer stop()

	// the caller app: another identity, token and client on the same node
	visitor, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the caller: %w", err)
	}
	cli := connect(n1.Endpoint, string(visitor.Token)).WithTarget(provider.Identity)

	steps := []struct {
		fact string
		run  func() error
	}{
		{"args_echoed", func() error { return checkEcho(ctx, cli, visitor.Identity, provider.Identity) }},
		{"stream_to_caller", func() error { return checkCount(ctx, cli) }},
		{"stream_both_ways", func() error { return checkPipe(ctx, cli) }},
		{"bulk_to_caller", func() error { return checkBulk(ctx, cli) }},
		{"bulk_from_caller", func() error { return checkSink(ctx, cli) }},
	}
	failures := map[string]string{}
	facts["failures"] = failures
	var failed []error
	for _, step := range steps {
		err := step.run()
		facts[step.fact] = err == nil
		if err != nil {
			failures[step.fact] = err.Error()
			failed = append(failed, fmt.Errorf("%s: %w", step.fact, err))
		}
	}
	if len(failed) > 0 {
		return facts, errors.Join(failed...)
	}

	fmt.Fprintln(os.Stderr, "ok: the caller app's arguments and streams reached the provider app on node1 intact")
	delete(facts, "failures")
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

// OpsurfacePipe answers each integer x the caller sends with 2x+1, and the
// caller's EOS with its own.
func (*Provider) OpsurfacePipe(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	for {
		obj, err := ch.Receive()
		if err != nil {
			return err
		}
		switch o := obj.(type) {
		case *astral.Int64:
			if err := ch.Send(astral.NewInt64(int64(*o)*2 + 1)); err != nil {
				return err
			}
		case *astral.EOS:
			return ch.Send(&astral.EOS{})
		default:
			return fmt.Errorf("unexpected %s", obj.ObjectType())
		}
	}
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
func serve(ctx *astral.Context, endpoint, token string) (func(), error) {
	cli := apphostcli.New(nil, connect(endpoint, token))
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
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Provider{}), reg, onAttach) }()

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

// open queries op with args and returns a channel to the provider.
func open(ctx *astral.Context, cli *astrald.Client, op string, args any) (*channel.Channel, func(), error) {
	cctx, cancel := ctx.WithTimeout(30 * time.Second)
	ch, err := cli.QueryChannel(cctx, op, args)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("querying %s: %w", op, err)
	}
	// why: a step that loses data would otherwise wait for it for ever.
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

// wantEOS checks that the next thing on the channel is EOS.
func wantEOS(ch *channel.Channel) error {
	if _, err := recv[*astral.EOS](ch); err != nil {
		return fmt.Errorf("want EOS: %w", err)
	}
	return nil
}

func checkEcho(ctx *astral.Context, cli *astrald.Client, caller, target *astral.Identity) error {
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
	ch, done, err := open(ctx, cli, "opsurface_echo", args)
	if err != nil {
		return err
	}
	defer done()

	text, err := recv[*astral.String16](ch)
	if err != nil {
		return err
	}
	small, err := recv[*astral.Int64](ch)
	if err != nil {
		return err
	}
	big, err := recv[*astral.Uint64](ch)
	if err != nil {
		return err
	}
	ratio, err := recv[*astral.Float64](ch)
	if err != nil {
		return err
	}
	on, err := recv[*astral.Bool](ch)
	if err != nil {
		return err
	}
	off, err := recv[*astral.Bool](ch)
	if err != nil {
		return err
	}
	rawb, err := recv[*astral.Bytes16](ch)
	if err != nil {
		return err
	}
	gotWho, err := recv[*astral.Identity](ch)
	if err != nil {
		return err
	}
	note, err := recv[*astral.String16](ch)
	if err != nil {
		return err
	}
	gotCaller, err := recv[*astral.Identity](ch)
	if err != nil {
		return err
	}
	gotTarget, err := recv[*astral.Identity](ch)
	if err != nil {
		return err
	}
	if err := wantEOS(ch); err != nil {
		return err
	}

	var bad []string
	if text.String() != args.Text {
		bad = append(bad, fmt.Sprintf("text %q, want %q", text.String(), args.Text))
	}
	if int64(*small) != args.Small {
		bad = append(bad, fmt.Sprintf("small %d, want %d", *small, args.Small))
	}
	if uint64(*big) != args.Big {
		bad = append(bad, fmt.Sprintf("big %d, want %d", *big, args.Big))
	}
	if float64(*ratio) != args.Ratio {
		bad = append(bad, fmt.Sprintf("ratio %v, want %v", float64(*ratio), args.Ratio))
	}
	if !bool(*on) || bool(*off) {
		bad = append(bad, fmt.Sprintf("flags on=%v off=%v, want true false", bool(*on), bool(*off)))
	}
	if !bytes.Equal(*rawb, args.Raw) {
		bad = append(bad, fmt.Sprintf("raw %x, want %x", []byte(*rawb), args.Raw))
	}
	if !gotWho.IsEqual(who) {
		bad = append(bad, fmt.Sprintf("who %v, want %v", gotWho, who))
	}
	if note.String() != unusedNote {
		bad = append(bad, fmt.Sprintf("the unset argument arrived as %q", note.String()))
	}
	if !gotCaller.IsEqual(caller) {
		bad = append(bad, fmt.Sprintf("caller %v, want %v", gotCaller, caller))
	}
	if !gotTarget.IsEqual(target) {
		bad = append(bad, fmt.Sprintf("target %v, want %v", gotTarget, target))
	}
	if len(bad) > 0 {
		return fmt.Errorf("the arguments arrived altered: %v", bad)
	}
	return nil
}

func checkCount(ctx *astral.Context, cli *astrald.Client) error {
	ch, done, err := open(ctx, cli, "opsurface_count", map[string]any{"n": streamItems})
	if err != nil {
		return err
	}
	defer done()
	for i := range streamItems {
		v, err := recv[*astral.Int64](ch)
		if err != nil {
			return fmt.Errorf("item %d of %d: %w", i, streamItems, err)
		}
		if int64(*v) != int64(i) {
			return fmt.Errorf("item %d arrived as %d", i, int64(*v))
		}
	}
	if err := wantEOS(ch); err != nil {
		return fmt.Errorf("after %d items: %w", streamItems, err)
	}
	return nil
}

func checkPipe(ctx *astral.Context, cli *astrald.Client) error {
	ch, done, err := open(ctx, cli, "opsurface_pipe", nil)
	if err != nil {
		return err
	}
	defer done()
	for i := range streamItems {
		if err := ch.Send(astral.NewInt64(int64(i))); err != nil {
			return fmt.Errorf("sending item %d: %w", i, err)
		}
		v, err := recv[*astral.Int64](ch)
		if err != nil {
			return fmt.Errorf("answer %d of %d: %w", i, streamItems, err)
		}
		if int64(*v) != int64(i)*2+1 {
			return fmt.Errorf("item %d was answered %d, want %d", i, int64(*v), i*2+1)
		}
	}
	if err := ch.Send(&astral.EOS{}); err != nil {
		return fmt.Errorf("sending EOS: %w", err)
	}
	if err := wantEOS(ch); err != nil {
		return fmt.Errorf("the caller's EOS was not answered: %w", err)
	}
	return nil
}

func checkBulk(ctx *astral.Context, cli *astrald.Client) error {
	cctx, cancel := ctx.WithTimeout(30 * time.Second)
	defer cancel()
	conn, err := cli.Query(cctx, "opsurface_bulk", map[string]any{"size": bulkSize})
	if err != nil {
		return fmt.Errorf("querying opsurface_bulk: %w", err)
	}
	defer conn.Close()
	defer time.AfterFunc(30*time.Second, func() { conn.Close() }).Stop()
	got, err := io.ReadAll(conn)
	if err != nil {
		return fmt.Errorf("reading after %d bytes: %w", len(got), err)
	}
	if len(got) != bulkSize {
		return fmt.Errorf("%d bytes arrived, want %d", len(got), bulkSize)
	}
	if sha256.Sum256(got) != sha256.Sum256(pattern(bulkSize)) {
		return errors.New("the bytes arrived altered")
	}
	return nil
}

func checkSink(ctx *astral.Context, cli *astrald.Client) error {
	ch, done, err := open(ctx, cli, "opsurface_sink", nil)
	if err != nil {
		return err
	}
	defer done()
	data := pattern(bulkSize)
	for off := 0; off < len(data); off += chunkSize {
		chunk := astral.Bytes32(data[off : off+chunkSize])
		if err := ch.Send(&chunk); err != nil {
			return fmt.Errorf("sending the chunk at %d: %w", off, err)
		}
	}
	if err := ch.Send(&astral.EOS{}); err != nil {
		return fmt.Errorf("sending EOS: %w", err)
	}
	total, err := recv[*astral.Uint64](ch)
	if err != nil {
		return err
	}
	digest, err := recv[*astral.Bytes8](ch)
	if err != nil {
		return err
	}
	if err := wantEOS(ch); err != nil {
		return err
	}
	want := sha256.Sum256(data)
	if uint64(*total) != uint64(bulkSize) || !bytes.Equal(*digest, want[:]) {
		return fmt.Errorf("the app received %d bytes with digest %x, want %d with %x", uint64(*total), []byte(*digest), bulkSize, want)
	}
	return nil
}
