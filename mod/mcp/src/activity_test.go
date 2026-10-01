package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// activityNode takes every query routed to it and keeps it with the identity
// its routing context named. When hang is set it holds the query until the
// routing context ends, as a target that never accepts does.
type activityNode struct {
	stubNode
	hang bool

	mu     sync.Mutex
	took   []*astral.InFlightQuery
	tookAs []*astral.Identity
	routed chan struct{}
}

func newActivityNode(hang bool) *activityNode {
	return &activityNode{hang: hang, routed: make(chan struct{}, 64)}
}

func (n *activityNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.mu.Lock()
	n.took = append(n.took, q)
	n.tookAs = append(n.tookAs, ctx.Identity())
	n.mu.Unlock()
	n.routed <- struct{}{}

	if n.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	go w.Close()
	return newRecordingWriter(), nil
}

func (n *activityNode) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.took)
}

// awaitReport waits for the next routed report.
func (n *activityNode) awaitReport(t *testing.T) {
	t.Helper()
	select {
	case <-n.routed:
	case <-time.After(5 * time.Second):
		t.Fatal("no report was routed")
	}
}

// testActivityServer serves MCP with the activity query set, over a node that
// routes reports to node, and the clock the test moves.
func testActivityServer(t *testing.T, node astral.Node, query string, tokens map[string]*astral.Identity) (*httptest.Server, *Module, *testClock) {
	t.Helper()

	nodeID, targetID := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := &Module{
		ctx:    astral.NewContext(nil).WithIdentity(nodeID).IncludeZone(astral.ZoneNetwork),
		node:   node,
		log:    testLogger(),
		config: defaultConfig,
	}
	mod.config.ActivityQuery = query
	mod.Apphost = &stubApphost{tokens: tokens}
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{"telepathy": targetID}}

	if err := mod.readActivity(); err != nil {
		t.Fatal(err)
	}

	// why the reporter is built here: the clock is the reporter's, and the test
	// moves it between requests.
	clock := &testClock{at: time.Now()}
	var reporter *activityReporter
	if query != "" {
		reporter = newActivityReporter(mod)
		reporter.now = clock.now
	}

	ts := httptest.NewServer(NewMCPServer(mod).handlerWith(reporter))
	t.Cleanup(ts.Close)

	return ts, mod, clock
}

type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func get(t *testing.T, url, token string) int {
	t.Helper()

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// One report per interval per identity, however many requests the identity
// sends; another identity reports on its own; the interval passing makes the
// identity due again.
func TestActivityReportsOncePerIntervalPerIdentity(t *testing.T) {
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	node := newActivityNode(false)
	ts, _, clock := testActivityServer(t, node, "astral://telepathy:agents.seen",
		map[string]*astral.Identity{"token-a": a, "token-b": b})

	for range 5 {
		get(t, ts.URL, "token-a")
	}
	node.awaitReport(t)
	time.Sleep(100 * time.Millisecond)
	if got := node.count(); got != 1 {
		t.Fatalf("%v reports for 5 requests of one identity, want 1", got)
	}

	get(t, ts.URL, "token-b")
	node.awaitReport(t)
	if got := node.count(); got != 2 {
		t.Fatalf("%v reports after a second identity, want 2", got)
	}

	clock.advance(defaultConfig.ActivityInterval)
	get(t, ts.URL, "token-a")
	node.awaitReport(t)
	if got := node.count(); got != 3 {
		t.Fatalf("%v reports after the interval, want 3", got)
	}
}

// A request with an invalid token is refused and reports nothing.
func TestActivityIgnoresARefusedRequest(t *testing.T) {
	node := newActivityNode(false)
	ts, _, _ := testActivityServer(t, node, "astral://telepathy:agents.seen",
		map[string]*astral.Identity{})

	if status := get(t, ts.URL, "bad"); status != http.StatusUnauthorized {
		t.Fatalf("status %v, want 401", status)
	}
	time.Sleep(100 * time.Millisecond)
	if got := node.count(); got != 0 {
		t.Fatalf("%v reports for a refused request, want 0", got)
	}
}

// With no activity query the node sends no report.
func TestActivityOffWhenQueryIsEmpty(t *testing.T) {
	a := astral.GenerateIdentity()
	node := newActivityNode(false)
	ts, mod, _ := testActivityServer(t, node, "", map[string]*astral.Identity{"token-a": a})

	if status := get(t, ts.URL, "token-a"); status == http.StatusUnauthorized {
		t.Fatal("an authenticated request was refused")
	}
	time.Sleep(100 * time.Millisecond)
	if got := node.count(); got != 0 {
		t.Fatalf("%v reports with no activity_query, want 0", got)
	}
	if mod.activityPath != "" {
		t.Fatal("the module holds an activity path")
	}
}

// A request is served while the target holds the report, and a second request
// of the identity starts no second report while the first is in flight.
func TestActivityNeverBlocksARequest(t *testing.T) {
	a := astral.GenerateIdentity()
	node := newActivityNode(true)
	ts, _, clock := testActivityServer(t, node, "astral://telepathy:agents.seen",
		map[string]*astral.Identity{"token-a": a})

	done := make(chan int, 2)
	go func() { done <- get(t, ts.URL, "token-a") }()
	node.awaitReport(t)

	// The interval has passed, so only the report in flight holds the second back.
	clock.advance(2 * defaultConfig.ActivityInterval)
	go func() { done <- get(t, ts.URL, "token-a") }()

	for range 2 {
		select {
		case status := <-done:
			if status == http.StatusUnauthorized {
				t.Fatalf("an authenticated request was refused")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a request waited on the held report")
		}
	}
	if got := node.count(); got != 1 {
		t.Fatalf("%v reports while one was in flight, want 1", got)
	}
}

// The report is put as the bearer identity, carries the MCP origin, goes to the
// configured target and path, and is routed on the node's context.
func TestActivityReportIsTheBearersAndRoutedAsTheNode(t *testing.T) {
	a := astral.GenerateIdentity()
	node := newActivityNode(false)
	ts, mod, _ := testActivityServer(t, node, "astral://telepathy:agents.seen",
		map[string]*astral.Identity{"token-a": a})

	get(t, ts.URL, "token-a")
	node.awaitReport(t)

	node.mu.Lock()
	defer node.mu.Unlock()
	q := node.took[0]
	targetID, _ := mod.Dir.ResolveIdentity("telepathy")

	switch {
	case !q.Caller.IsEqual(a):
		t.Fatalf("the report was put as %v, not the bearer", q.Caller)
	case !q.Target.IsEqual(targetID):
		t.Fatalf("the report went to %v, not the configured target", q.Target)
	case string(q.QueryString) != "agents.seen":
		t.Fatalf("the report asked %q", q.QueryString)
	case !q.IsMCP():
		t.Fatal("the report does not carry the MCP origin")
	case !node.tookAs[0].IsEqual(mod.ctx.Identity()):
		t.Fatalf("the report was routed on a context naming %v, not the node", node.tookAs[0])
	}
}

// A value that is not astral://<identity-or-alias>:<query> fails the load.
func TestActivityBadQueryFailsLoad(t *testing.T) {
	for _, value := range []string{"telepathy:agents.seen", "astral://agents.seen", "astral://telepathy:"} {
		mod := &Module{config: defaultConfig}
		mod.config.ActivityQuery = value
		if err := mod.readActivity(); err == nil {
			t.Fatalf("%q was read", value)
		}
	}

	mod := &Module{config: defaultConfig}
	mod.config.ActivityQuery = "astral://telepathy:agents.seen"
	if err := mod.readActivity(); err != nil {
		t.Fatalf("a valid query failed: %v", err)
	}
	if mod.activityTarget != "telepathy" || mod.activityPath != "agents.seen" {
		t.Fatalf("read %q %q", mod.activityTarget, mod.activityPath)
	}
}
