package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

// answerJSON renders what the agent is handed. The decision this test pins is
// about the answer's shape, and the shape is the marshalled form rather than
// the struct — an empty list and an absent field are the same Go value under
// omitempty and are different answers to a model.
func answerJSON(t *testing.T, out queryOut) string {
	t.Helper()

	blob, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(blob)
}

// An op that says it is done having sent nothing has answered "no objects".
// Handing the agent the terminator's bytes presents that answer as a blob.
func TestAnEmptyFramedStreamIsAnEmptyObjectList(t *testing.T) {
	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go func() {
		channel.NewSender(peer).Send(&astral.EOS{})
		peer.Close()
	}()

	out := mod.collectResponse(conn, "", time.Second)

	if out.Objects == nil {
		t.Fatal("an empty stream answered no object list at all")
	}
	if len(out.Objects) != 0 {
		t.Fatalf("%v objects, want 0", len(out.Objects))
	}
	if out.Payload != "" {
		t.Fatalf("an empty stream was answered as payload %q", out.Payload)
	}

	if answer := answerJSON(t, out); !strings.Contains(answer, `"objects":[]`) {
		t.Fatalf("the agent is handed %v, which names no empty list", answer)
	}
}

// Plain text decodes to no object and is not a framed stream. It stays a
// payload: the naive fix — dropping the len(objs) > 0 guard — answers it as an
// empty object list and loses what the op actually said.
func TestPlainTextIsStillAPayload(t *testing.T) {
	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go func() {
		peer.Write([]byte("plain text answer"))
		peer.Close()
	}()

	out := mod.collectResponse(conn, "", time.Second)

	if out.Payload != "plain text answer" {
		t.Fatalf("payload %q", out.Payload)
	}
	if out.Objects != nil {
		t.Fatalf("text was answered as %v objects", len(out.Objects))
	}

	if answer := answerJSON(t, out); !strings.Contains(answer, `"objects":null`) {
		t.Fatalf("the agent is handed %v, which does not mark the absence", answer)
	}
}

// A frame cut short consumes every byte and yields no object, exactly as an
// empty stream does. It is not an empty stream: nothing said the answer was
// done, so the bytes go to the agent rather than a claim that the op answered
// none. This is what the eos flag is for.
func TestATruncatedFrameIsAPayloadAndNotAnEmptyList(t *testing.T) {
	var whole bytes.Buffer
	if err := channel.NewSender(&whole).Send(&astral.Ack{}); err != nil {
		t.Fatalf("send: %v", err)
	}
	cut := whole.Bytes()[:whole.Len()-1]

	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go func() {
		peer.Write(cut)
		peer.Close()
	}()

	out := mod.collectResponse(conn, "", time.Second)

	if out.Objects != nil {
		t.Fatalf("a truncated frame was answered as %v objects", len(out.Objects))
	}
	if out.Payload == "" {
		t.Fatal("a truncated frame answered neither objects nor a payload")
	}
}

// An answer of no bytes at all is not a framed stream and is not changed by
// this: nothing was sent, so nothing said the answer was done.
func TestNoBytesAtAllIsUnchanged(t *testing.T) {
	mod := testRouterModule(t)
	conn, peer := collectConn(t)

	go peer.Close()

	out := mod.collectResponse(conn, "", time.Second)

	if out.Objects != nil {
		t.Fatalf("an empty answer became %v objects", len(out.Objects))
	}
}
