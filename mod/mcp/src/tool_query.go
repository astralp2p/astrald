package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type queryIn struct {
	Target        string            `json:"target" jsonschema:"target identity or alias"`
	Path          string            `json:"path" jsonschema:"query path, e.g. user.info"`
	Args          map[string]string `json:"args,omitempty" jsonschema:"query arguments"`
	Payload       string            `json:"payload,omitempty" jsonschema:"request payload written after the query is accepted"`
	PayloadBase64 bool              `json:"payload_base64,omitempty" jsonschema:"payload is base64-encoded binary"`
	Format        string            `json:"format,omitempty" jsonschema:"response handling: auto (default) detects framed objects vs plain data, raw forces bytes, objects forces framed decoding"`
	Session       bool              `json:"session,omitempty" jsonschema:"keep the stream open as a dialog session instead of collecting a response"`
	TimeoutMs     int               `json:"timeout_ms,omitempty" jsonschema:"response window in milliseconds"`
}

// note: response objects are []any, not []json.RawMessage — the SDK infers a
// byte-array schema for RawMessage and then rejects the actual output.
type queryOut struct {
	Objects   []any  `json:"objects,omitempty" jsonschema:"framed response objects"`
	Payload   string `json:"payload,omitempty" jsonschema:"raw response payload"`
	Encoding  string `json:"encoding,omitempty" jsonschema:"payload encoding: utf8 or base64"`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"response hit a size cap"`
	SessionID string `json:"session_id,omitempty" jsonschema:"dialog session handle (session mode)"`
}

func (mod *Module) queryTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[queryIn, queryOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in queryIn) (res *mcpsdk.CallToolResult, out queryOut, err error) {
		targetID, err := mod.Dir.ResolveIdentity(in.Target)
		if err != nil {
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

		conn, err := query.RouteInFlight(qctx, mod.node, astral.Launch(q))
		if err != nil {
			return nil, out, fmt.Errorf("query failed: %v", err)
		}

		if len(payload) > 0 {
			if _, err = conn.Write(payload); err != nil {
				conn.Close()
				return nil, out, fmt.Errorf("write payload: %v", err)
			}
		}

		if in.Session {
			// dialog sessions default to raw — agent-to-agent talk is text
			format := sessionFormatRaw
			if in.Format == sessionFormatObjects {
				format = sessionFormatObjects
			}
			s := mod.newSession(sessionInfo{
				agent:  agentID,
				conn:   conn,
				caller: agentID,
				path:   in.Path,
				params: in.Args,
				format: format,
			})
			out.SessionID = s.id
			return nil, out, nil
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

	if len(data) == 0 || format == sessionFormatRaw {
		out.Payload, out.Encoding = encodePayload(data)
		return
	}

	objs, complete := decodeObjects(data, mod.config.MaxResponseObjects)

	// why: ops answer in framed objects while agents answer in plain text,
	// and the caller cannot know which — decide from the bytes themselves.
	switch {
	case format == sessionFormatObjects,
		len(objs) > 0 && complete,
		len(objs) > 0 && !utf8.Valid(data):
		out.Objects = objs
		out.Truncated = out.Truncated || len(objs) >= mod.config.MaxResponseObjects
	default:
		out.Payload, out.Encoding = encodePayload(data)
	}
	return
}

// decodeObjects parses data as a framed object stream. complete reports the
// stream decoded to its end (EOS or full consumption).
func decodeObjects(data []byte, maxObjs int) (objs []any, complete bool) {
	cr := &countingReader{r: bytes.NewReader(data)}
	recv := channel.NewReceiver(cr, channel.AllowUnparsed(true))

	for len(objs) < maxObjs {
		obj, err := recv.Receive()
		if err != nil {
			return objs, cr.n == len(data)
		}
		if _, ok := obj.(*astral.EOS); ok {
			return objs, true
		}
		j, err := objectJSON(obj)
		if err != nil {
			continue // note: an object with no JSON form is dropped
		}
		objs = append(objs, jsonValue(j))
	}
	return objs, true
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
