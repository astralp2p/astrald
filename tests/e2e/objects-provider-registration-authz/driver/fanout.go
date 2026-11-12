package main

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// probe is the object id and search word of one fan-out: unique to the run,
// so each app's counter for them counts this fan-out alone.
type probe struct {
	id   *astral.ObjectID
	word string
}

// opOf is the op through which node1 dials a registrant of role.
var opOf = map[astral.String8]provider.Op{
	auth.RoleFinder:    provider.Find,
	auth.RoleDescriber: provider.Describe,
	auth.RoleSearcher:  provider.Search,
}

func (p probe) key(role astral.String8) string {
	if role == auth.RoleSearcher {
		return p.word
	}
	return p.id.String()
}

// fanOut runs the User's find, describe and search on node1 at once, each for
// a run-unique id or word, while every accepted lease is still live.
func fanOut(ctx *astral.Context, user *astrald.Client, firstLease time.Time, f *facts) (probe, error) {
	nonce := astral.NewNonce().String()
	id, err := astral.ResolveObjectID(astral.NewString16("objects-provider-registration-authz " + nonce))
	if err != nil {
		return probe{}, err
	}
	current := probe{id: id, word: "objects-provider-registration-authz-" + nonce}

	f.FanoutAfter = time.Since(firstLease).Seconds()
	if err := f.check("fanout_within_lease", time.Since(firstLease) < leaseMargin,
		"the fan-out started %.1f s after the first lease, past %v: the 10 s leases may lapse under it",
		f.FanoutAfter, leaseMargin); err != nil {
		return current, err
	}

	cli := objectscli.New(nil, user)
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	for _, call := range []func() error{
		func() error { _, err := provider.FindAll(ctx, cli, current.id, callTimeout); return err },
		func() error { _, err := provider.DescribeAll(ctx, cli, current.id, callTimeout); return err },
		func() error { _, err := provider.SearchAll(ctx, cli, current.word, callTimeout); return err },
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- call()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return current, fmt.Errorf("the User's fan-out on node1: %w", err)
		}
	}
	return current, nil
}

// checkDials checks that node1 dialed each accepted (app, role) once and no
// other pair at all.
func checkDials(providers map[string]*provider.Provider, current probe, f *facts) error {
	var bad []string
	for _, c := range matrix {
		for _, role := range provider.Roles {
			cell := c.name + "/" + string(role)
			n := providers[c.name].Count(opOf[role], current.key(role))
			f.Dials[cell] = n
			want := 0
			if accepts(c, role) {
				want = 1
			}
			if n != want {
				bad = append(bad, fmt.Sprintf("%s dialed %d times, want %d", cell, n, want))
			}
		}
	}
	sort.Strings(bad)
	return f.check("dials", len(bad) == 0, "node1's fan-out dialed otherwise than the accepted cells once each: %v", bad)
}

// revokeAll revokes ServeObjects from every app the matrix granted it, so no
// app stays a registered provider in the world after the driver.
func revokeAll(ctx *astral.Context, user *astrald.Client, providers map[string]*provider.Provider) {
	for _, c := range matrix {
		p, ok := providers[c.name]
		if !ok || !grantsServeObjects(c) {
			continue
		}
		if err := provider.Revoke(ctx, user, p.App.ID, provider.ServeObjects); err != nil {
			fmt.Fprintf(os.Stderr, "revoke ServeObjects of app %s: %v\n", c.name, err)
		}
	}
}

func grantsServeObjects(c cellApp) bool {
	permits, _ := c.permits()
	for _, p := range permits {
		if string(p.Action) == provider.ServeObjects {
			return true
		}
	}
	return false
}
