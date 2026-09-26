package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

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

// collectResponse reads the single-shot response off conn, bounded by the
// module's caps. Closing the conn on timeout is what unblocks the reads.
func (mod *Module) collectResponse(conn astral.Conn, timeout time.Duration) (out queryOut) {
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

	if len(data) == 0 {
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
	case stream.eos && len(stream.objs) == 0,
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
