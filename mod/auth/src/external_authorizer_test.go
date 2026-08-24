package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// stubAsk answers from a script and records the actors it was asked about,
// which is what proves where in the walk the authority is reached.
type stubAsk struct {
	allow  map[string]bool
	err    error
	asked  []*astral.Identity
	answer bool
}

func (s *stubAsk) String() string { return "stub://authority" }

func (s *stubAsk) Ask(_ *astral.Context, action auth.ActionObject) (bool, error) {
	s.asked = append(s.asked, action.Actor())

	if s.err != nil {
		return false, s.err
	}
	if s.allow != nil {
		return s.allow[action.Actor().String()], nil
	}

	return s.answer, nil
}

func (s *stubAsk) askedAbout(id *astral.Identity) bool {
	for _, a := range s.asked {
		if a.IsEqual(id) {
			return true
		}
	}
	return false
}

// withAuthority wires a stub authority over the module's test.action.
func withAuthority(t *testing.T, mod *Module) *stubAsk {
	t.Helper()

	stub := &stubAsk{}
	if err := mod.AddExternal("test.action", ExternalConfig{}, stub); err != nil {
		t.Fatalf("add external: %v", err)
	}

	return stub
}

// ---------- where the authority sits in the walk ----------

// TestExternalAuthorityDecidesWhenNothingElseDoes is the base case: no handler
// allows the actor and no contract names it, so the authority is the last word.
func TestExternalAuthorityDecidesWhenNothingElseDoes(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	stranger := astral.GenerateIdentity()

	stub := withAuthority(t, mod)
	stub.answer = true

	if !mod.Authorize(ctx, action(stranger)) {
		t.Fatal("the authority allowed and Authorize refused")
	}
	if !stub.askedAbout(stranger) {
		t.Fatal("the authority was not asked about the actor the caller named")
	}
}

// TestExternalAuthorityFailsClosed: a question that could not be put is not an
// answer, and an authority that cannot be reached has permitted nothing.
func TestExternalAuthorityFailsClosed(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)

	stub := withAuthority(t, mod)
	stub.err = errors.New("authority unreachable")

	if mod.Authorize(ctx, action(astral.GenerateIdentity())) {
		t.Fatal("an unanswered question read as permission")
	}
}

// TestExternalAuthorityIsNotAskedWhenAHandlerAllows keeps a question that leaves
// the host off a decision already made in memory.
func TestExternalAuthorityIsNotAskedWhenAHandlerAllows(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	allowRoot(mod, root)

	stub := withAuthority(t, mod)
	stub.answer = true

	if !mod.Authorize(ctx, action(root)) {
		t.Fatal("the handler no longer decides")
	}
	if len(stub.asked) != 0 {
		t.Fatalf("the authority was asked %d times; a handler had already allowed", len(stub.asked))
	}
}

// TestExternalAuthorityIsNotAskedWhenAContractAllows is the same bar for the
// other local answer: a contract resolves without leaving the host.
func TestExternalAuthorityIsNotAskedWhenAContractAllows(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	leaf := astral.GenerateIdentity()
	allowRoot(mod, root)
	seedContract(t, mod, root, leaf, permit(0))

	stub := withAuthority(t, mod)
	stub.answer = true

	if !mod.Authorize(ctx, action(leaf)) {
		t.Fatal("the contract no longer decides")
	}
	if len(stub.asked) != 0 {
		t.Fatalf("the authority was asked %d times; a contract had already allowed", len(stub.asked))
	}
}

// TestAuthorityCarriesAContractsSubject is what the placement inside the walk
// buys: an authority that permits an issuer permits the subject the issuer
// delegated to, exactly as a handler would.
func TestAuthorityCarriesAContractsSubject(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	issuer := astral.GenerateIdentity()
	subject := astral.GenerateIdentity()

	// no handler allows anyone; the only local fact is the contract
	seedContract(t, mod, issuer, subject, permit(0))

	stub := withAuthority(t, mod)
	stub.allow = map[string]bool{issuer.String(): true}

	if !mod.Authorize(ctx, action(subject)) {
		t.Fatal("a subject whose issuer the authority permits was refused")
	}
	if !stub.askedAbout(issuer) {
		t.Fatal("the authority was never asked about the issuer")
	}
}

// TestAuthorityRefusingAnIssuerRefusesTheSubject is the same path denied.
func TestAuthorityRefusingAnIssuerRefusesTheSubject(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	issuer := astral.GenerateIdentity()
	subject := astral.GenerateIdentity()

	seedContract(t, mod, issuer, subject, permit(0))

	stub := withAuthority(t, mod)
	stub.answer = false

	if mod.Authorize(ctx, action(subject)) {
		t.Fatal("an issuer the authority refused still carried its subject")
	}
}

// TestAuthorizeWithoutAnAuthorityIsUnchanged is the no-behaviour-change bar.
func TestAuthorizeWithoutAnAuthorityIsUnchanged(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	root := astral.GenerateIdentity()
	allowRoot(mod, root)

	if !mod.Authorize(ctx, action(root)) {
		t.Fatal("the registered handler no longer decides")
	}
	if mod.Authorize(ctx, action(astral.GenerateIdentity())) {
		t.Fatal("an identity no handler allows was authorized")
	}
}

// ---------- the http transport ----------

// authorityServer serves one scripted answer and records what it was sent.
type authorityServer struct {
	status int
	body   string

	gotAuth string
	gotBody []byte
}

func (a *authorityServer) serve(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.gotAuth = r.Header.Get("Authorization")
		a.gotBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func overHTTP(t *testing.T, url string, config ExternalConfig) *ExternalAuthorizer {
	t.Helper()

	config.Endpoint = url
	config.Actions = []string{"test.action"}

	ask, err := newHTTPAuthorizer(config)
	if err != nil {
		t.Fatalf("build http authorizer: %v", err)
	}

	return NewExternalAuthorizer(log.New(astral.GenerateIdentity()), "test.action", config, ask)
}

// TestHTTPAuthorityAnswers covers the whole contract: 200 with an allow field is
// the only shape that decides, and everything else refuses.
func TestHTTPAuthorityAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   bool
	}{
		"allows":              {http.StatusOK, `{"allow":true}`, true},
		"denies":              {http.StatusOK, `{"allow":false}`, false},
		"omits the field":     {http.StatusOK, `{}`, false},
		"answers a null":      {http.StatusOK, `{"allow":null}`, false},
		"answers not json":    {http.StatusOK, `<html>proxy error</html>`, false},
		"answers 403":         {http.StatusForbidden, `{"allow":false}`, false},
		"answers 500":         {http.StatusInternalServerError, ``, false},
		"answers 200 no body": {http.StatusOK, ``, false},
	} {
		t.Run(name, func(t *testing.T) {
			authority := &authorityServer{status: tc.status, body: tc.body}
			h := overHTTP(t, authority.serve(t).URL, ExternalConfig{})

			got := h.Authorize(astral.NewContext(nil), action(astral.GenerateIdentity()))
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHTTPAuthorityRefusesAProxyPage is the reason the answer is a body and not
// a status: a 200 carrying something else must not read as permission.
func TestHTTPAuthorityRefusesAProxyPage(t *testing.T) {
	authority := &authorityServer{status: http.StatusOK, body: `<html><body>OK</body></html>`}
	h := overHTTP(t, authority.serve(t).URL, ExternalConfig{})

	if h.Authorize(astral.NewContext(nil), action(astral.GenerateIdentity())) {
		t.Fatal("a 200 that carried no answer read as permission")
	}
}

// TestHTTPAuthorityCarriesTheBearerToken: the token is how the node
// authenticates to an http authority.
func TestHTTPAuthorityCarriesTheBearerToken(t *testing.T) {
	authority := &authorityServer{status: http.StatusOK, body: `{"allow":true}`}
	h := overHTTP(t, authority.serve(t).URL, ExternalConfig{Token: "s3cr3t"})

	h.Authorize(astral.NewContext(nil), action(astral.GenerateIdentity()))

	if authority.gotAuth != "Bearer s3cr3t" {
		t.Fatalf("authorization header: got %q, want %q", authority.gotAuth, "Bearer s3cr3t")
	}
}

// TestHTTPAuthoritySendsTheAction: the body is the canonical astral json
// envelope, so an authority decodes it with the same code it would use on a
// channel. The payload's shape is the action type's own business; what this
// pins is that the envelope names the type and the payload decodes back.
func TestHTTPAuthoritySendsTheAction(t *testing.T) {
	authority := &authorityServer{status: http.StatusOK, body: `{"allow":true}`}
	h := overHTTP(t, authority.serve(t).URL, ExternalConfig{})

	actor := astral.GenerateIdentity()
	h.Authorize(astral.NewContext(nil), action(actor))

	var envelope astral.JSONAdapter
	if err := json.Unmarshal(authority.gotBody, &envelope); err != nil {
		t.Fatalf("the body is not an astral json envelope: %v (%s)", err, authority.gotBody)
	}

	if envelope.Type != "test.action" {
		t.Fatalf("type: got %q, want %q", envelope.Type, "test.action")
	}

	var got testAction
	if err := json.Unmarshal(envelope.Object, &got); err != nil {
		t.Fatalf("payload does not decode into the action: %v (%s)", err, envelope.Object)
	}
	if !got.Actor().IsEqual(actor) {
		t.Fatalf("actor: got %v, want %v", got.Actor(), actor)
	}
}

// TestEncodeActionMatchesTheChannel is the seam that makes the two transports
// interchangeable: the bytes posted over http are the bytes a JSONSender writes.
func TestEncodeActionMatchesTheChannel(t *testing.T) {
	a := action(astral.GenerateIdentity())

	posted, err := encodeAction(a)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var framed bytes.Buffer
	if err = channel.NewJSONSender(&framed).Send(a); err != nil {
		t.Fatalf("send: %v", err)
	}

	// the sender writes json lines; compare the object, not the newline
	if got, want := string(posted), strings.TrimSpace(framed.String()); got != want {
		t.Fatalf("http body and channel frame differ:\n http: %s\n chan: %s", got, want)
	}
}

// TestHTTPAskerReadsATokenFile: a token file is read at load, so a missing one
// is a misconfiguration the operator sees at startup.
func TestHTTPAskerReadsATokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  from-a-file\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	asker, err := newHTTPAuthorizer(ExternalConfig{Endpoint: "http://localhost/authorize", TokenFile: path})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if asker.token != "from-a-file" {
		t.Fatalf("token: got %q, want %q", asker.token, "from-a-file")
	}
}

func TestHTTPAskerRefusesAMissingTokenFile(t *testing.T) {
	_, err := newHTTPAuthorizer(ExternalConfig{
		Endpoint:  "http://localhost/authorize",
		TokenFile: filepath.Join(t.TempDir(), "absent"),
	})

	if err == nil {
		t.Fatal("a missing token file was accepted")
	}
}

// ---------- the astral transport ----------

func TestAstralAskerParsesTheEndpoint(t *testing.T) {
	mod := testModule(t)

	ask, err := newAstralAuthorizer(mod, ExternalConfig{Endpoint: "astral://telepathy:auth.authorize"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if ask.name != "telepathy" {
		t.Fatalf("authority: got %q, want %q", ask.name, "telepathy")
	}
	if ask.path != "auth.authorize" {
		t.Fatalf("query: got %q, want %q", ask.path, "auth.authorize")
	}
}

func TestAstralAskerRefusesAnIncompleteEndpoint(t *testing.T) {
	mod := testModule(t)

	for name, endpoint := range map[string]string{
		"no query":     "astral://telepathy",
		"no authority": "astral://:auth.authorize",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newAstralAuthorizer(mod, ExternalConfig{Endpoint: endpoint}); err == nil {
				t.Fatalf("%v was accepted", endpoint)
			}
		})
	}
}

// TestAstralAskerRefusesToAskAboutTheAuthority closes the shortest loop: an
// authority asked whether it may act would have to answer first.
func TestAstralAskerRefusesToAskAboutTheAuthority(t *testing.T) {
	mod := testModule(t)
	authority := astral.GenerateIdentity()

	ask, err := newAstralAuthorizer(mod, ExternalConfig{Endpoint: "astral://telepathy:auth.authorize"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ask.resolve = func(string) (*astral.Identity, error) { return authority, nil }

	h := NewExternalAuthorizer(mod.log, "test.action", ExternalConfig{}, ask)

	if h.Authorize(astral.NewContext(nil), action(authority)) {
		t.Fatal("the authority was asked about itself")
	}
}

// TestAstralAskerResolvesTheAuthorityOnce: resolution reaches the dir module, so
// it happens on first use and is held.
func TestAstralAskerResolvesTheAuthorityOnce(t *testing.T) {
	mod := testModule(t)
	authority := astral.GenerateIdentity()
	var resolved int

	ask, err := newAstralAuthorizer(mod, ExternalConfig{Endpoint: "astral://telepathy:auth.authorize"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ask.resolve = func(string) (*astral.Identity, error) {
		resolved++
		return authority, nil
	}

	for i := range 3 {
		id, err := ask.resolveTarget()
		if err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
		if !id.IsEqual(authority) {
			t.Fatalf("resolve %d: got %v, want %v", i, id, authority)
		}
	}

	if resolved != 1 {
		t.Fatalf("the authority was resolved %d times, want 1", resolved)
	}
}

func TestAstralAskerRefusesAnUnresolvableAuthority(t *testing.T) {
	mod := testModule(t)

	ask, err := newAstralAuthorizer(mod, ExternalConfig{Endpoint: "astral://nobody:auth.authorize"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ask.resolve = func(string) (*astral.Identity, error) { return nil, errors.New("unknown identity") }

	h := NewExternalAuthorizer(mod.log, "test.action", ExternalConfig{}, ask)

	if h.Authorize(astral.NewContext(nil), action(astral.GenerateIdentity())) {
		t.Fatal("an unresolvable authority permitted an action")
	}
}

// ---------- construction ----------

// TestNewAskerSelectsTheTransport: the scheme is what chooses, and an endpoint
// naming none is a refusal rather than a default.
func TestNewAskerSelectsTheTransport(t *testing.T) {
	mod := testModule(t)

	for name, tc := range map[string]struct {
		endpoint string
		want     string
		refuse   bool
	}{
		"http":         {endpoint: "http://127.0.0.1:8081/authorize", want: "*auth.httpAuthorizer"},
		"https":        {endpoint: "https://authz.example/authorize", want: "*auth.httpAuthorizer"},
		"astral":       {endpoint: "astral://telepathy:auth.authorize", want: "*auth.astralAuthorizer"},
		"no scheme":    {endpoint: "telepathy:auth.authorize", refuse: true},
		"unknown":      {endpoint: "grpc://authz.example", refuse: true},
		"bare address": {endpoint: "127.0.0.1:8081", refuse: true},
	} {
		t.Run(name, func(t *testing.T) {
			ask, err := newAuthorizeAsk(mod, ExternalConfig{Endpoint: tc.endpoint})

			if tc.refuse {
				if err == nil {
					t.Fatalf("%v was accepted", tc.endpoint)
				}
				return
			}

			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if got := fmt.Sprintf("%T", ask); got != tc.want {
				t.Fatalf("transport: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAddExternalAuthorizersRefusesTwoAuthoritiesForOneAction(t *testing.T) {
	mod := testModule(t)

	err := mod.addExternalAuthorizers([]ExternalConfig{
		{Endpoint: "http://first/authorize", Actions: []string{"test.action"}},
		{Endpoint: "http://second/authorize", Actions: []string{"test.action"}},
	})

	if err == nil {
		t.Fatal("two authorities were accepted for one action type")
	}
}

func TestAddExternalAuthorizersRefusesAnIncompleteEntry(t *testing.T) {
	for name, config := range map[string]ExternalConfig{
		"no endpoint": {Actions: []string{"test.action"}},
		"no actions":  {Endpoint: "http://authz/authorize"},
		"bad scheme":  {Endpoint: "ftp://authz/authorize", Actions: []string{"test.action"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := testModule(t).addExternalAuthorizers([]ExternalConfig{config}); err == nil {
				t.Fatal("an incomplete entry was accepted")
			}
		})
	}
}

// TestAddExternalAuthorizersAppliesDefaults: a timeout of zero would refuse
// every question the moment it was asked.
func TestAddExternalAuthorizersAppliesDefaults(t *testing.T) {
	mod := testModule(t)

	err := mod.addExternalAuthorizers([]ExternalConfig{
		{Endpoint: "astral://telepathy:auth.authorize", Actions: []string{"test.action"}},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	h, ok := mod.external.Get("test.action")
	if !ok {
		t.Fatal("no authorizer registered for the configured action")
	}
	if h.config.Timeout != defaultExternal.Timeout {
		t.Fatalf("timeout: got %v, want %v", h.config.Timeout, defaultExternal.Timeout)
	}
}

// TestOneAuthorityServesEveryActionItNames: an entry naming two actions
// registers two authorizers over one ask, so one endpoint is reached one way.
func TestOneAuthorityServesEveryActionItNames(t *testing.T) {
	mod := testModule(t)

	err := mod.addExternalAuthorizers([]ExternalConfig{{
		Endpoint: "http://authz/authorize",
		Actions:  []string{"test.action", "test.other_action"},
	}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	first, ok := mod.external.Get("test.action")
	if !ok {
		t.Fatal("no authorizer for test.action")
	}
	second, ok := mod.external.Get("test.other_action")
	if !ok {
		t.Fatal("no authorizer for test.other_action")
	}

	if first.ask != second.ask {
		t.Fatal("two actions of one entry do not share the ask")
	}
}

// TestAddExternalTakesAnyAsk is the openness the interface buys: an authority
// reached by means this module knows nothing about registers like a configured
// one, which is what an op would do.
func TestAddExternalTakesAnyAsk(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)

	stub := &stubAsk{answer: true}
	if err := mod.AddExternal("test.action", ExternalConfig{}, stub); err != nil {
		t.Fatalf("add: %v", err)
	}

	if !mod.Authorize(ctx, action(astral.GenerateIdentity())) {
		t.Fatal("an ask registered without configuration did not decide")
	}
}

func TestExternalAuthorizerIsATypedHandler(t *testing.T) {
	var h any = NewExternalAuthorizer(log.New(astral.GenerateIdentity()), "test.action", ExternalConfig{}, &stubAsk{})

	if _, ok := h.(authmod.TypedHandler); !ok {
		t.Fatal("the authorizer is not a TypedHandler")
	}
}
