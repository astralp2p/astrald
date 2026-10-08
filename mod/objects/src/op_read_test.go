package objects

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// zoneRecordingRepo misses every read and keeps the zone each read was asked in.
// note: the embedded nil interface panics on any other method, which asserts that a read calls nothing else.
type zoneRecordingRepo struct {
	objectsmod.Repository

	mu    sync.Mutex
	zones []astral.Zone
}

func (repo *zoneRecordingRepo) Read(ctx *astral.Context, _ *astral.ObjectID, _ int64, _ int64) (objectsmod.Reader, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.zones = append(repo.zones, ctx.Zone())
	return nil, objectsmod.ErrNotFound
}

func (repo *zoneRecordingRepo) recorded() []astral.Zone {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return append([]astral.Zone(nil), repo.zones...)
}

// TestReadFromTheNetworkExcludesTheNetworkZone: a read that arrives from the
// network reaches the repository without the network zone, even when it asks for
// it, so a network repository never forwards it. A local read keeps the zone it
// asks for.
func TestReadFromTheNetworkExcludesTheNetworkZone(t *testing.T) {
	cases := []struct {
		origin      string
		wantNetwork bool
	}{
		{astral.OriginNetwork, false},
		{astral.OriginLocal, true},
	}

	for _, c := range cases {
		t.Run(c.origin, func(t *testing.T) {
			repo := &zoneRecordingRepo{}
			mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}, log: log.New(nil)}
			mod.repos.Set("main", repo)

			op, err := routing.NewOp(mod.OpRead)
			if err != nil {
				t.Fatalf("new op: %v", err)
			}

			caller := astral.GenerateIdentity()
			q := astral.Launch(query.New(caller, caller, "objects.read?zone=dvn&id="+testObjectID().String(), nil))
			q.Extra.Set("origin", c.origin)

			ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
			defer cancel()

			_, err = op.RouteQuery(ctx, q, newRecordingWriter())

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("a read the repository missed answered %v; want a rejection", err)
			}

			zones := repo.recorded()
			if len(zones) != 1 {
				t.Fatalf("the repository was asked %d times; want 1", len(zones))
			}
			if got := zones[0].Is(astral.ZoneNetwork); got != c.wantNetwork {
				t.Fatalf("the repository was asked in zone %v; network included %v, want %v", zones[0], got, c.wantNetwork)
			}
		})
	}
}

// failingRepo answers every read with err.
// note: the embedded nil interface panics on any other method, which asserts that a read calls nothing else.
type failingRepo struct {
	objectsmod.Repository
	err error
}

func (repo *failingRepo) Read(*astral.Context, *astral.ObjectID, int64, int64) (objectsmod.Reader, error) {
	return nil, repo.err
}

// TestReadRejectsWithACodePerFailure: a refused caller is rejected with
// astral.CodeRejected, an object no repository supplies with CodeUnavailable,
// and every other failure keeps astral.CodeRejected.
func TestReadRejectsWithACodePerFailure(t *testing.T) {
	cases := []struct {
		name     string
		allow    bool
		readErr  error
		repoArg  string
		wantCode uint8
	}{
		{"refused", false, objectsmod.ErrNotFound, "", astral.CodeRejected},
		{"not found", true, objectsmod.ErrNotFound, "", objectsmod.CodeUnavailable},
		{"wrapped not found", true, fmt.Errorf("group: %w", objectsmod.ErrNotFound), "", objectsmod.CodeUnavailable},
		{"other read error", true, errors.New("decode failed"), "", astral.CodeRejected},
		{"partial id", true, objectsmod.ErrHashLookupUnsupported, "", astral.CodeRejected},
		{"unknown repository", true, objectsmod.ErrNotFound, "nowhere", astral.CodeRejected},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: c.allow}}, log: log.New(nil)}
			mod.repos.Set("main", &failingRepo{err: c.readErr})

			op, err := routing.NewOp(mod.OpRead)
			if err != nil {
				t.Fatalf("new op: %v", err)
			}

			caller := astral.GenerateIdentity()
			queryString := "objects.read?id=" + testObjectID().String()
			if c.repoArg != "" {
				queryString += "&repo=" + c.repoArg
			}
			q := astral.Launch(query.New(caller, caller, queryString, nil))
			q.Extra.Set("origin", astral.OriginLocal)

			ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
			defer cancel()

			_, err = op.RouteQuery(ctx, q, newRecordingWriter())

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("the read answered %v; want a rejection", err)
			}
			if rejected.Code != c.wantCode {
				t.Fatalf("the read was rejected with code %d; want %d", rejected.Code, c.wantCode)
			}
		})
	}
}
