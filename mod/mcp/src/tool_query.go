package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Response formats an agent may ask for; the absence of both is auto.
const (
	formatRaw     = "raw"
	formatObjects = "objects"
)

type queryIn struct {
	Target        string            `json:"target" jsonschema:"target identity or alias"`
	Path          string            `json:"path" jsonschema:"query path, e.g. user.info"`
	Args          map[string]string `json:"args,omitempty" jsonschema:"query arguments"`
	Payload       string            `json:"payload,omitempty" jsonschema:"request payload written after the query is accepted"`
	PayloadBase64 bool              `json:"payload_base64,omitempty" jsonschema:"payload is base64-encoded binary"`
	Format        string            `json:"format,omitempty" jsonschema:"response handling: auto (default) detects framed objects vs plain data, raw forces bytes, objects forces framed decoding"`
	TimeoutMs     int               `json:"timeout_ms,omitempty" jsonschema:"response window in milliseconds"`
}

// note: response objects are []any, not []json.RawMessage — the SDK infers a
// byte-array schema for RawMessage and then rejects the actual output.
type queryOut struct {
	// why no omitempty: an empty list is an answer — the op said it was done
	// having sent nothing — and omitempty drops it, leaving the agent an object
	// carrying neither objects nor a payload. Null is the absence of an object
	// answer; [] is an object answer of none.
	Objects   []any  `json:"objects" jsonschema:"framed response objects"`
	Payload   string `json:"payload,omitempty" jsonschema:"raw response payload"`
	Encoding  string `json:"encoding,omitempty" jsonschema:"payload encoding: utf8 or base64"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"response hit a size cap"`
}

func (mod *Module) queryTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[queryIn, queryOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in queryIn) (res *mcpsdk.CallToolResult, out queryOut, err error) {
		targetID, err := mod.Dir.ResolveIdentity(in.Target)
		if err != nil {
			return nil, out, fmt.Errorf("unknown target: %v", in.Target)
		}

		// Which agents this one may call is its own owner's decision, and the
		// node does not hold it. What the target permits is a separate question,
		// asked where the call arrives.
		//
		// why the refusal reads as an unresolvable target: an agent learns that
		// it cannot reach this one, and not whether this one exists — the
		// property the answering side has, applied to the calling side.
		if !mod.Auth.Authorize(mod.ctx, &mcp.CallAgentAction{
			Action: auth.NewAction(agentID),
			ToID:   targetID,
		}) {
			return nil, out, fmt.Errorf("unknown target: %v", in.Target)
		}

		timeout := mod.config.QueryTimeout
		if in.TimeoutMs > 0 {
			timeout = time.Duration(in.TimeoutMs) * time.Millisecond
		}

		payload, err := decodePayload(in.Payload, in.PayloadBase64)
		if err != nil {
			return nil, out, err
		}

		q := query.New(agentID, targetID, in.Path, in.Args)
		qctx, cancel := mod.ctx.WithIdentity(agentID).WithTimeout(timeout)
		defer cancel()

		conn, err := query.RouteInFlight(qctx, mod.node, launch(q))
		if err != nil {
			return nil, out, fmt.Errorf("query failed: %v", err)
		}

		if len(payload) > 0 {
			if _, err = conn.Write(payload); err != nil {
				conn.Close()
				return nil, out, fmt.Errorf("write payload: %v", err)
			}
		}

		out = mod.collectResponse(conn, in.Format, timeout)
		return nil, out, nil
	}
}

// collectResponse reads the single-shot response off conn, bounded by the
// module's caps. Closing the conn on timeout is what unblocks the reads.
func (mod *Module) collectResponse(conn astral.Conn, format string, timeout time.Duration) (out queryOut) {
	timer := time.AfterFunc(timeout, func() { conn.Close() })
	defer timer.Stop()
	defer conn.Close()

	buf := make([]byte, mod.config.MaxResponseBytes)
	var total int
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			break
		}
	}
	out.Truncated = total == len(buf)
	data := buf[:total]

	if len(data) == 0 || format == formatRaw {
		out.Payload, out.Encoding = encodePayload(data)
		return
	}

	stream := decodeObjects(data, mod.config.MaxResponseObjects)

	// why: ops answer in framed objects while agents answer in plain text,
	// and the caller cannot know which — decide from the bytes themselves.
	//
	// why a stream that ended having carried nothing is an answer: an op that
	// says it is done having sent no object has answered "none", and handing
	// the agent the terminator's bytes presents that answer as an opaque blob.
	// eos is what tells it from bytes that merely decoded to nothing — a
	// truncated frame consumes everything and yields no object too, and that is
	// a payload the agent should see rather than an answer it should believe.
	switch {
	case format == formatObjects,
		stream.eos && len(stream.objs) == 0,
		len(stream.objs) > 0 && stream.complete,
		len(stream.objs) > 0 && !utf8.Valid(data):
		out.Objects = stream.objs
		out.Truncated = out.Truncated || len(stream.objs) >= mod.config.MaxResponseObjects
	default:
		out.Payload, out.Encoding = encodePayload(data)
	}
	return
}

// framedStream is what a framed decode yielded, and how the stream ended.
type framedStream struct {
	// objs is never nil, so an answer of no objects is a list rather than an
	// absence — see queryOut.Objects.
	objs []any

	// complete reports the decode reached the stream's end: an EOS frame, the
	// object cap, or every byte consumed.
	complete bool

	// eos reports the stream ended by saying so. It is the only thing that
	// tells a stream carrying nothing from bytes that decoded to nothing, and
	// those are different answers.
	eos bool
}

// decodeObjects parses data as a framed object stream.
func decodeObjects(data []byte, maxObjs int) framedStream {
	cr := &countingReader{r: bytes.NewReader(data)}
	recv := channel.NewReceiver(cr, channel.AllowUnparsed(true))

	stream := framedStream{objs: []any{}}

	for len(stream.objs) < maxObjs {
		obj, err := recv.Receive()
		if err != nil {
			stream.complete = cr.n == len(data)
			return stream
		}
		if _, ok := obj.(*astral.EOS); ok {
			stream.complete, stream.eos = true, true
			return stream
		}
		j, err := objectJSON(obj)
		if err != nil {
			continue // note: an object with no JSON form is dropped
		}
		stream.objs = append(stream.objs, jsonValue(j))
	}

	stream.complete = true
	return stream
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// objectJSON renders an object in the canonical JSON envelope {Type, Object}.
func objectJSON(obj astral.Object) ([]byte, error) {
	if u, ok := obj.(*astral.UnparsedObject); ok {
		return json.Marshal(map[string]any{
			"Type":    u.Type,
			"Payload": u.Payload, // marshals as base64
		})
	}
	payload, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return json.Marshal(&astral.JSONAdapter{Type: obj.ObjectType(), Object: payload})
}
