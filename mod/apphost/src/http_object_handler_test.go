package apphost

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/objects/mem"
)

// gatewayData is the content of the object the gateway serves.
var gatewayData = []byte("the gateway names the object it opened")

// gatewayObjects is an objects module whose default read repository is repo.
type gatewayObjects struct {
	objectsmod.Module
	repo objectsmod.Repository
}

func (o gatewayObjects) ReadDefault() objectsmod.Repository { return o.repo }

// gatewayModule returns a Module whose objects module reads from a memory repository
// holding gatewayData, the full ID of that object, and the module's authority.
func gatewayModule(t *testing.T, verdict bool) (*Module, *astral.ObjectID, *recordingAuth) {
	t.Helper()

	repo := mem.New("gateway", 0)
	w, err := repo.Create(astral.NewContext(nil), nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.Write(gatewayData); err != nil {
		t.Fatalf("write: %v", err)
	}
	full, err := w.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	authority := &recordingAuth{verdict: verdict}
	mod := &Module{
		Deps: Deps{Auth: authority, Objects: gatewayObjects{repo: repo}},
		node: &serveAppsNode{id: astral.GenerateIdentity()},
	}

	return mod, full, authority
}

// TestHTTPObjectHandler_PartialIDNamesTheOpenedObject requests an object by its data0
// ID. Content-Disposition names the full ID, and Content-Length is the full size.
func TestHTTPObjectHandler_PartialIDNamesTheOpenedObject(t *testing.T) {
	mod, full, authority := gatewayModule(t, true)
	partial := full.PartialString()

	rec := httptest.NewRecorder()
	NewHTTPObjectHandler(mod, astral.GenerateIdentity()).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+partial, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Header().Get("Content-Disposition"), "inline; filename="+full.String(); got != want {
		t.Fatalf("Content-Disposition %q; want %q", got, want)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(gatewayData)) {
		t.Fatalf("Content-Length %q; want %d", got, len(gatewayData))
	}
	if !bytes.Equal(rec.Body.Bytes(), gatewayData) {
		t.Fatalf("body %q; want %q", rec.Body.Bytes(), gatewayData)
	}

	// why: authorization keys on the request ID, so the action carries the partial ID.
	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("Authorize called %d times; want 1", len(actions))
	}
	action, ok := actions[0].(*auth.SeeObjectsAction)
	if !ok {
		t.Fatalf("authorized %T; want *auth.SeeObjectsAction", actions[0])
	}
	if !action.ObjectID.IsEqual(&astral.ObjectID{Hash: full.Hash}) {
		t.Fatalf("authorized object %v; want the partial request %v", action.ObjectID, partial)
	}
}

// TestHTTPObjectHandler_MissingObjectIsNotNamed requests an object no repository holds.
// The response names no object.
func TestHTTPObjectHandler_MissingObjectIsNotNamed(t *testing.T) {
	mod, _, _ := gatewayModule(t, true)
	missing := &astral.ObjectID{Hash: [32]byte{0x5a}}

	rec := httptest.NewRecorder()
	NewHTTPObjectHandler(mod, astral.GenerateIdentity()).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+missing.PartialString(), nil))

	if rec.Code == http.StatusOK {
		t.Fatalf("status %d for a missing object", rec.Code)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("Content-Disposition %q for a missing object; want none", got)
	}
}
