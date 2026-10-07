package auth

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
)

// rule builds a test.action rule; a nil evaluator removes.
func rule(actor, evaluator *astral.Identity, query string) *authmod.EvaluatorRule {
	return &authmod.EvaluatorRule{
		Actor:     actor,
		Action:    "test.action",
		Evaluator: evaluator,
		Query:     astral.String8(query),
	}
}

// withEvaluators routes every rule's question to the stub for its evaluator.
func withEvaluators(mod *Module, stubs map[string]*stubAsk) {
	mod.evaluatorAsk = func(r *authmod.EvaluatorRule) auth.AuthorizeAsk {
		return stubs[r.Evaluator.String()]
	}
}

// ---------- where the rule sits in the walk ----------

// TestEvaluatorRuleDecidesForItsActor is the base case: no handler or contract
// allows the actor, and the rule names the evaluator that decides.
func TestEvaluatorRuleDecidesForItsActor(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	stub := &stubAsk{answer: true}
	withEvaluators(mod, map[string]*stubAsk{media.String(): stub})

	if err := mod.SetEvaluatorRule(ctx, rule(player, media, "")); err != nil {
		t.Fatalf("set rule: %v", err)
	}

	if !mod.Authorize(ctx, action(player)) {
		t.Fatal("the evaluator allowed and Authorize refused")
	}
	if !stub.askedAbout(player) {
		t.Fatal("the evaluator was not asked about the actor the rule names")
	}

	stub.answer = false
	if mod.Authorize(ctx, action(player)) {
		t.Fatal("the evaluator refused and Authorize allowed")
	}
}

// TestEvaluatorRuleDoesNotSpeakForOtherActors: a rule for one actor leaves
// every other actor to the config-file authority.
func TestEvaluatorRuleDoesNotSpeakForOtherActors(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, media, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	evaluator := &stubAsk{answer: true}
	withEvaluators(mod, map[string]*stubAsk{media.String(): evaluator})
	configured := withAuthority(t, mod)

	if err := mod.SetEvaluatorRule(ctx, rule(player, media, "")); err != nil {
		t.Fatalf("set rule: %v", err)
	}

	if mod.Authorize(ctx, action(other)) {
		t.Fatal("an actor with no rule was allowed; the config authority refuses")
	}
	if evaluator.askedAbout(other) {
		t.Fatal("the evaluator was asked about an actor its rule does not name")
	}
	if !configured.askedAbout(other) {
		t.Fatal("the config authority was not asked about an actor with no rule")
	}
}

// TestEvaluatorRuleTakesPrecedenceOverConfig: for the actor it names, the rule
// is the narrower statement and the config-file authority is not asked.
func TestEvaluatorRuleTakesPrecedenceOverConfig(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	evaluator := &stubAsk{answer: true}
	withEvaluators(mod, map[string]*stubAsk{media.String(): evaluator})
	configured := withAuthority(t, mod)

	if err := mod.SetEvaluatorRule(ctx, rule(player, media, "")); err != nil {
		t.Fatalf("set rule: %v", err)
	}

	if !mod.Authorize(ctx, action(player)) {
		t.Fatal("the rule's evaluator allowed and Authorize refused")
	}
	if configured.askedAbout(player) {
		t.Fatal("the config authority was asked about an actor a rule names")
	}
}

// TestEvaluatorRuleIsNotAskedWhenAHandlerAllows: an existing allowance still
// precedes the evaluator, as it precedes a config-file authority.
func TestEvaluatorRuleIsNotAskedWhenAHandlerAllows(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()
	allowRoot(mod, player)

	stub := &stubAsk{answer: false}
	withEvaluators(mod, map[string]*stubAsk{media.String(): stub})

	if err := mod.SetEvaluatorRule(ctx, rule(player, media, "")); err != nil {
		t.Fatalf("set rule: %v", err)
	}

	if !mod.Authorize(ctx, action(player)) {
		t.Fatal("the handler no longer decides")
	}
	if len(stub.asked) != 0 {
		t.Fatalf("the evaluator was asked %d times; a handler had already allowed", len(stub.asked))
	}
}

// TestEvaluatorRuleFailsClosed: an evaluator that cannot be reached has
// permitted nothing.
func TestEvaluatorRuleFailsClosed(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	stub := &stubAsk{err: errors.New("evaluator unreachable")}
	withEvaluators(mod, map[string]*stubAsk{media.String(): stub})

	if err := mod.SetEvaluatorRule(ctx, rule(player, media, "")); err != nil {
		t.Fatalf("set rule: %v", err)
	}

	if mod.Authorize(ctx, action(player)) {
		t.Fatal("an unanswered question read as permission")
	}
}

// ---------- the rule set ----------

// TestSetEvaluatorRuleReplacesAndRemoves: one rule per (actor, action); a nil
// evaluator removes it and leaves the other rules.
func TestSetEvaluatorRuleReplacesAndRemoves(t *testing.T) {
	mod := testModule(t)
	ctx := astral.NewContext(nil)
	player, web := astral.GenerateIdentity(), astral.GenerateIdentity()
	media, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("set rule: %v", err)
		}
	}

	must(mod.SetEvaluatorRule(ctx, rule(player, media, "")))
	must(mod.SetEvaluatorRule(ctx, rule(web, media, "")))
	must(mod.SetEvaluatorRule(ctx, rule(player, other, "custom.op")))

	if n := len(mod.rules()); n != 2 {
		t.Fatalf("got %d rules; want 2 after replacing one", n)
	}

	r := mod.evaluatorRule(player, "test.action")
	if r == nil || !r.Evaluator.IsEqual(other) || r.Query != "custom.op" {
		t.Fatalf("the replacing rule did not take: %+v", r)
	}

	must(mod.SetEvaluatorRule(ctx, rule(player, nil, "")))

	if mod.evaluatorRule(player, "test.action") != nil {
		t.Fatal("the removed rule is still found")
	}
	if mod.evaluatorRule(web, "test.action") == nil {
		t.Fatal("removing one rule removed another")
	}
}

// TestAstralEvaluatorAskDefaultsTheQuery: a rule naming no op asks auth.evaluate.
func TestAstralEvaluatorAskDefaultsTheQuery(t *testing.T) {
	mod := testModule(t)
	media := astral.GenerateIdentity()

	ask := mod.astralEvaluatorAsk(&authmod.EvaluatorRule{Evaluator: media}).(*astralAuthorizer)
	if ask.path != authmod.OpEvaluate {
		t.Fatalf("query %q; want %q", ask.path, authmod.OpEvaluate)
	}
	if !ask.target.IsEqual(media) {
		t.Fatal("the ask does not target the rule's evaluator")
	}
}

// ---------- the ops ----------

// hexDir resolves hex identities only, which is all the op tests name.
type hexDir struct{ dir.Module }

func (hexDir) ResolveIdentity(s string) (*astral.Identity, error) { return astral.ParseIdentity(s) }

// routeOp dispatches one query with the given origin and returns the router's
// verdict; w receives whatever the op writes back.
func routeOp(t *testing.T, fn any, caller *astral.Identity, origin, queryString string, w io.WriteCloser) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	q := astral.Launch(query.New(caller, caller, queryString, nil))
	q.Extra.Set("origin", origin)

	_, err = op.RouteQuery(ctx, q, w)
	return err
}

type discard struct{ io.Writer }

func (discard) Close() error { return nil }

func setEvaluatorQuery(actor, evaluator *astral.Identity) string {
	return "auth.set_evaluator?actor=" + actor.String() + "&action=test.action&evaluator=" + evaluator.String()
}

// TestSetEvaluatorRefusesAQueryOffALink: the rule set is this node's setting,
// so a link's query is refused before the authority is consulted.
func TestSetEvaluatorRefusesAQueryOffALink(t *testing.T) {
	mod := testModule(t)
	mod.Dir = hexDir{}
	admin := astral.GenerateIdentity()
	mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(*astral.Context, *auth.AdminManageAppsAction) bool { return true }))
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	err := routeOp(t, mod.OpSetEvaluator, admin, astral.OriginNetwork, setEvaluatorQuery(player, media), discard{io.Discard})

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("auth.set_evaluator answered a query off a link: got %v", err)
	}
	if len(mod.rules()) != 0 {
		t.Fatal("a query off a link wrote a rule")
	}
}

// TestSetEvaluatorRefusesACallerWithoutAdmin: a local caller without
// AdminManageApps is refused.
func TestSetEvaluatorRefusesACallerWithoutAdmin(t *testing.T) {
	mod := testModule(t)
	mod.Dir = hexDir{}
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	err := routeOp(t, mod.OpSetEvaluator, astral.GenerateIdentity(), astral.OriginLocal, setEvaluatorQuery(player, media), discard{io.Discard})

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("auth.set_evaluator answered a caller without AdminManageApps: got %v", err)
	}
	if len(mod.rules()) != 0 {
		t.Fatal("an unprivileged caller wrote a rule")
	}
}

// TestSetEvaluatorWritesTheRule: a local admin's query sets the rule, and an
// empty evaluator removes it.
func TestSetEvaluatorWritesTheRule(t *testing.T) {
	mod := testModule(t)
	mod.Dir = hexDir{}
	admin := astral.GenerateIdentity()
	mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(_ *astral.Context, a *auth.AdminManageAppsAction) bool {
		return a.Actor().IsEqual(admin)
	}))
	player, media := astral.GenerateIdentity(), astral.GenerateIdentity()

	if err := routeOp(t, mod.OpSetEvaluator, admin, astral.OriginLocal, setEvaluatorQuery(player, media), discard{io.Discard}); err != nil {
		t.Fatalf("auth.set_evaluator: %v", err)
	}
	waitFor(t, func() bool { return mod.evaluatorRule(player, "test.action") != nil })

	clear := "auth.set_evaluator?actor=" + player.String() + "&action=test.action"
	if err := routeOp(t, mod.OpSetEvaluator, admin, astral.OriginLocal, clear, discard{io.Discard}); err != nil {
		t.Fatalf("auth.set_evaluator clear: %v", err)
	}
	waitFor(t, func() bool { return mod.evaluatorRule(player, "test.action") == nil })
}

// TestSetEvaluatorRefusesTheActorAsItsOwnEvaluator: such a rule could never
// allow, so it is refused when set.
func TestSetEvaluatorRefusesTheActorAsItsOwnEvaluator(t *testing.T) {
	mod := testModule(t)
	mod.Dir = hexDir{}
	admin := astral.GenerateIdentity()
	mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(*astral.Context, *auth.AdminManageAppsAction) bool { return true }))
	player := astral.GenerateIdentity()

	if err := routeOp(t, mod.OpSetEvaluator, admin, astral.OriginLocal, setEvaluatorQuery(player, player), discard{io.Discard}); err != nil {
		t.Fatalf("auth.set_evaluator: %v", err)
	}

	// note: the op answers with an error object and writes nothing, so there is
	// no state change to wait for.
	time.Sleep(100 * time.Millisecond)
	if len(mod.rules()) != 0 {
		t.Fatal("a rule naming the actor as its own evaluator was written")
	}
}

// TestListEvaluatorsRefusesAQueryOffALink: the rule set is not enumerable over
// a link.
func TestListEvaluatorsRefusesAQueryOffALink(t *testing.T) {
	mod := testModule(t)
	mod.Add(authmod.Func[*auth.AdminManageAppsAction](func(*astral.Context, *auth.AdminManageAppsAction) bool { return true }))

	err := routeOp(t, mod.OpListEvaluators, astral.GenerateIdentity(), astral.OriginNetwork, "auth.list_evaluators", discard{io.Discard})

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("auth.list_evaluators answered a query off a link: got %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
