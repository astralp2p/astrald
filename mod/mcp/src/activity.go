package mcp

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// activityReporter tells the configured service that an identity uses the
// endpoint. It is app-agnostic: it names a query and reads no answer.
//
// why a report is a middleware and not part of verifyToken: the bearer check
// decides who may enter, and a decision that waits on a service refuses MCP
// whenever that service is down.
type activityReporter struct {
	mod *Module
	now func() time.Time

	mu       sync.Mutex
	last     map[string]time.Time // identity to the start of its last report
	inFlight map[string]bool
}

// readActivity reads the configured activity query.
//
// why a start fails on a query it cannot read: a report that never leaves is a
// deployment that believes it is told of activity.
func (mod *Module) readActivity() (err error) {
	if mod.config.ActivityQuery == "" {
		return nil
	}

	mod.activityTarget, mod.activityPath, err = splitEndpoint(mod.config.ActivityQuery)
	if err != nil {
		return fmt.Errorf("activity_query: %w", err)
	}
	return nil
}

func newActivityReporter(mod *Module) *activityReporter {
	return &activityReporter{
		mod:      mod,
		now:      time.Now,
		last:     map[string]time.Time{},
		inFlight: map[string]bool{},
	}
}

// middleware reports the authenticated identity when a report is due, then
// serves the request. It runs after the bearer check, so the identity is set.
func (r *activityReporter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if info := sdkauth.TokenInfoFromContext(req.Context()); info != nil {
			if id, ok := info.Extra["identity"].(*astral.Identity); ok && r.due(id) {
				// why a goroutine: the query returns once the target accepts, and the
				// request must not wait on a target that is slow or down.
				go r.report(id)
			}
		}

		next.ServeHTTP(w, req)
	})
}

// due claims a report for id and answers whether the caller sends it. A report
// is due when none is in flight and the interval has passed since the last one
// began.
func (r *activityReporter) due(id *astral.Identity) bool {
	key := id.String()
	now := r.now()

	r.mu.Lock()
	defer r.mu.Unlock()

	// why entries are pruned here: the map is keyed by every identity the
	// endpoint has authenticated, and an entry older than the interval carries
	// no information.
	interval := r.mod.config.ActivityInterval
	for k, at := range r.last {
		if now.Sub(at) >= interval && !r.inFlight[k] {
			delete(r.last, k)
		}
	}

	if r.inFlight[key] {
		return false
	}
	if _, found := r.last[key]; found {
		return false
	}

	r.inFlight[key] = true
	r.last[key] = now
	return true
}

// report puts the activity query as id and discards the answer. A failure is
// logged and never retried; the next due request reports again.
func (r *activityReporter) report(id *astral.Identity) {
	defer func() {
		r.mu.Lock()
		delete(r.inFlight, id.String())
		r.mu.Unlock()
	}()

	mod := r.mod

	targetID, err := mod.Dir.ResolveIdentity(mod.activityTarget)
	if err != nil {
		mod.log.Logv(2, "activity report: unknown target %v", mod.activityTarget)
		return
	}

	// why the module context: the request's context ends with the request, and
	// the report outlives it. It routes as the node for the reason a declared
	// tool does — see declaredToolHandler.
	qctx, cancel := mod.ctx.WithTimeout(mod.config.QueryTimeout)
	defer cancel()

	conn, err := query.RouteInFlight(qctx, mod.node, declaredQuery(id, targetID, mod.activityPath))
	if err != nil {
		mod.log.Logv(2, "activity report: %v", err)
		return
	}

	// why the connection is read: the target answers when it has read the
	// query, and closing early reads as a refusal.
	timer := time.AfterFunc(mod.config.QueryTimeout, func() { conn.Close() })
	defer timer.Stop()
	defer conn.Close()

	_, _ = io.Copy(io.Discard, conn)
}
