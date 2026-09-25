package mcp

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

// mcp.create_agent mints through messaging: the alias and duration it was
// asked for reach CreateIdentity, it answers messaging's credential as the
// agent, and the agent row keeps the token for list_agents.
func TestCreateAgentMintsThroughMessaging(t *testing.T) {
	mod, msg := testAgentModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	msg.cred = testCredential()

	agent := onlyAgentAnswer[*mcp.Agent](t, callAgentOp(t, mod.OpCreateAgent, "mcp.create_agent?alias=scout&duration=1h"))

	if !agent.Identity.IsEqual(msg.cred.Identity) || agent.Token != msg.cred.Token || agent.Alias != msg.cred.Alias {
		t.Fatalf("answered %+v, want messaging's credential %+v", agent, msg.cred)
	}
	if msg.createAlias != "scout" || msg.createDuration != astral.Duration(time.Hour) {
		t.Fatalf("messaging was asked for %q for %v, want scout for an hour", msg.createAlias, msg.createDuration)
	}
	if row, err := mod.db.FindAgent(agent.Identity); err != nil || row.Token != string(msg.cred.Token) {
		t.Fatalf("the agent row: %+v, err %v; want the credential's token", row, err)
	}
	if len(msg.deleted) != 0 {
		t.Fatal("a created agent's participant was deleted")
	}
}

// A row that cannot be written undoes the create: the op answers the error
// and no agent, and the participant messaging minted is deleted with it.
func TestCreateAgentUndoesTheParticipantWhenItsRowFails(t *testing.T) {
	mod, msg := testAgentModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	msg.cred = testCredential()

	// note: a row holding the same token makes the new row break the token's unique index.
	if err := mod.db.CreateAgent(&dbAgent{Identity: astral.GenerateIdentity(), Token: string(msg.cred.Token)}); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	objs := callAgentOp(t, mod.OpCreateAgent, "mcp.create_agent")
	if len(objs) != 1 {
		t.Fatalf("answered %v objects, want one error", len(objs))
	}
	if _, ok := objs[0].(astral.Error); !ok {
		t.Fatalf("answered %T, want the row's error", objs[0])
	}
	if len(msg.deleted) != 1 || !msg.deleted[0].IsEqual(msg.cred.Identity) {
		t.Fatalf("deleted participants %v, want the one minted", msg.deleted)
	}
	if _, err := mod.db.FindAgent(msg.cred.Identity); err == nil {
		t.Fatal("an agent row names the undone participant")
	}
}

// mcp.delete_agent deletes through messaging: the identity the name resolves
// to reaches DeleteIdentity, and the agent row goes after it.
func TestDeleteAgentDeletesThroughMessaging(t *testing.T) {
	mod, msg := testAgentModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	agent := testAgent()
	if err := mod.storeAgent(mod.ctx, agent); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	mod.Dir.(*stubDir).aliases["scout"] = agent.Identity

	onlyAgentAnswer[*astral.Ack](t, callAgentOp(t, mod.OpDeleteAgent, "mcp.delete_agent?identity=scout"))

	if len(msg.deleted) != 1 || !msg.deleted[0].IsEqual(agent.Identity) {
		t.Fatalf("deleted participants %v, want the agent's", msg.deleted)
	}
	if _, err := mod.db.FindAgent(agent.Identity); err == nil {
		t.Fatal("the agent row outlived the delete")
	}
}

// testCredential is a credential as messaging mints one.
func testCredential() *messaging.IdentityCredential {
	return &messaging.IdentityCredential{
		Identity:  astral.GenerateIdentity(),
		Alias:     "scout",
		Token:     astral.String8(astral.GenerateIdentity().String()),
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}
}

// callAgentOp routes one query from a local caller to an agent op, accepted,
// and answers everything the op wrote.
func callAgentOp(t *testing.T, fn any, queryString string) []astral.Object {
	t.Helper()

	w := &answerWriter{closed: make(chan struct{})}
	if err := route(t, fn, astral.GenerateIdentity(), queryString, w); err != nil {
		t.Fatalf("%v refused: %v", queryString, err)
	}
	return w.objects(t)
}

// onlyAgentAnswer asserts the op answered exactly one object, of type T.
func onlyAgentAnswer[T astral.Object](t *testing.T, objs []astral.Object) T {
	t.Helper()

	if len(objs) != 1 {
		t.Fatalf("the op answered %v objects, want one", len(objs))
	}
	answer, ok := objs[0].(T)
	if !ok {
		t.Fatalf("the op answered %T: %v", objs[0], objs[0])
	}
	return answer
}

// answerWriter is the caller's end of the connection, kept whole so the answer
// can be decoded once the op closes it.
type answerWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	once   sync.Once
	closed chan struct{}
}

func (w *answerWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *answerWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

// objects decodes every object the op wrote, once it has closed the conn.
func (w *answerWriter) objects(t *testing.T) []astral.Object {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var objs []astral.Object
	recv := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	for {
		obj, err := recv.Receive()
		if errors.Is(err, io.EOF) {
			return objs
		}
		if err != nil {
			t.Fatalf("decode answer: %v", err)
		}
		objs = append(objs, obj)
	}
}
