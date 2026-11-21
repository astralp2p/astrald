package nodes

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

// expiryOf returns the expiry of id's row, or the zero time when it holds none unexpired.
func expiryOf(t *testing.T, db *DB, id *astral.Identity, network, address string) time.Time {
	t.Helper()
	rows, err := db.FindEndpoints(id)
	if err != nil {
		t.Fatalf("FindEndpoints: %v", err)
	}
	for _, row := range rows {
		if row.Network == network && row.Address == address && row.ExpiresAt != nil {
			return *row.ExpiresAt
		}
	}
	return time.Time{}
}

// seedShortenRows stores tcp X and Y for 2 h and onion O for 90 d for b and returns their expiries.
func seedShortenRows(t *testing.T, db *DB, b *astral.Identity) (x, y, o time.Time) {
	t.Helper()
	x = time.Now().Add(2 * time.Hour).UTC()
	o = time.Now().Add(90 * 24 * time.Hour).UTC()
	for _, r := range []struct {
		network, address string
		at               time.Time
	}{
		{"tcp", "192.168.1.20:1791", x},
		{"tcp", "192.168.1.21:1791", x},
		{"tor", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaad.onion:1791", o},
	} {
		if err := db.AddEndpoint(b, r.network, r.address, &r.at); err != nil {
			t.Fatalf("seed %v: %v", r.address, err)
		}
	}
	return x, x, o
}

// TestFirstHandPullShortensMissingRows (criterion 3): a first-hand reply of
// only tcp Y leaves tcp X expiring at or before now+1h, renews Y and leaves the
// onion row O unchanged.
func TestFirstHandPullShortensMissingRows(t *testing.T) {
	f := newExchangeFixture(t)
	b := astral.GenerateIdentity()
	x, _, o := seedShortenRows(t, f.mod.db, b)
	f.answer = []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(tcpEndpoint("192.168.1.21"), ipmod.LocalEndpointTTL)}

	if _, err := f.mod.UpdateNodeEndpoints(f.mod.ctx, b, b); err != nil {
		t.Fatalf("UpdateNodeEndpoints: %v", err)
	}

	if got := expiryOf(t, f.mod.db, b, "tcp", "192.168.1.20:1791"); got.IsZero() || got.After(time.Now().Add(missingEndpointTTL)) || !got.Before(x) {
		t.Fatalf("unlisted row X expires at %v; want at or before now+%v", got, missingEndpointTTL)
	}
	if got := expiryOf(t, f.mod.db, b, "tcp", "192.168.1.21:1791"); time.Until(got) < ipmod.LocalEndpointTTL-time.Minute {
		t.Fatalf("listed row Y expires in %v; want about %v", time.Until(got), ipmod.LocalEndpointTTL)
	}
	if got := expiryOf(t, f.mod.db, b, "tor", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaad.onion:1791"); !got.Equal(o) {
		t.Fatalf("onion row O expires at %v; want %v unchanged", got, o)
	}
}

// TestSecondHandOrEmptyPullShortensNothing (criterion 3): the same reply from
// resolver C, and an empty first-hand reply, leave every expiry unchanged.
func TestSecondHandOrEmptyPullShortensNothing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		firstHand bool
		answer    []*nodes.EndpointWithTTL
	}{
		{"second-hand", false, []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(tcpEndpoint("192.168.1.21"), ipmod.LocalEndpointTTL)}},
		{"empty first-hand", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newExchangeFixture(t)
			b := astral.GenerateIdentity()
			x, _, o := seedShortenRows(t, f.mod.db, b)
			f.answer = tc.answer

			resolver := astral.GenerateIdentity()
			if tc.firstHand {
				resolver = b
			}
			if _, err := f.mod.UpdateNodeEndpoints(f.mod.ctx, resolver, b); err != nil {
				t.Fatalf("UpdateNodeEndpoints: %v", err)
			}

			if got := expiryOf(t, f.mod.db, b, "tcp", "192.168.1.20:1791"); !got.Equal(x) {
				t.Fatalf("row X expires at %v; want %v unchanged", got, x)
			}
			if got := expiryOf(t, f.mod.db, b, "tor", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaad.onion:1791"); !got.Equal(o) {
				t.Fatalf("onion row O expires at %v; want %v unchanged", got, o)
			}
		})
	}
}

// TestDBResolverServesRemainingTTL (open question 1): a stored row is served
// with its remaining time, not a fresh TTL, so a row pulled from B ages at C.
func TestDBResolverServesRemainingTTL(t *testing.T) {
	f := newExchangeFixture(t)
	f.mod.Exonet = &parsingExonet{}
	a := astral.GenerateIdentity()
	at := time.Now().Add(20 * time.Minute).UTC()
	if err := f.mod.db.AddEndpoint(a, "tcp", "192.168.1.20:1791", &at); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	ch, err := (&DBEndpointResolver{mod: f.mod}).ResolveEndpoints(f.mod.ctx, a)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}
	var got []*nodes.EndpointWithTTL
	for e := range ch {
		got = append(got, e)
	}
	if len(got) != 1 || got[0].TTL == nil {
		t.Fatalf("served %v; want one row with a TTL", got)
	}
	if ttl := time.Duration(*got[0].TTL) * time.Second; ttl > 20*time.Minute || ttl < 19*time.Minute {
		t.Fatalf("served TTL %v; want the remaining 20 min", ttl)
	}
}

// TestPullNeverShortensManualRow: a row an operator added with
// nodes.add_endpoint keeps its expiry when a first-hand reply omits it or
// lists it with a shorter TTL.
func TestPullNeverShortensManualRow(t *testing.T) {
	const mapped = "203.0.113.9:1791"
	for _, tc := range []struct {
		name   string
		answer []*nodes.EndpointWithTTL
	}{
		{"omitted", []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(tcpEndpoint("172.17.0.2"), ipmod.LocalEndpointTTL)}},
		{"listed shorter", []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(tcpEndpoint("203.0.113.9"), ipmod.LocalEndpointTTL)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newExchangeFixture(t)
			m := astral.GenerateIdentity()
			manual := time.Now().Add(90 * 24 * time.Hour).UTC()
			if err := f.mod.db.AddManualEndpoint(m, "tcp", mapped, &manual); err != nil {
				t.Fatalf("seed manual row: %v", err)
			}
			f.answer = tc.answer

			if _, err := f.mod.UpdateNodeEndpoints(f.mod.ctx, m, m); err != nil {
				t.Fatalf("UpdateNodeEndpoints: %v", err)
			}

			if got := expiryOf(t, f.mod.db, m, "tcp", mapped); !got.Equal(manual) {
				t.Fatalf("manual row expires at %v; want %v unchanged", got, manual)
			}
		})
	}
}

// TestSecondHandPullNeverShortensListedRow: a second-hand reply that lists a
// row with less time left keeps the row's expiry; the same reply first-hand
// sets it.
func TestSecondHandPullNeverShortensListedRow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		firstHand bool
	}{
		{"second-hand", false},
		{"first-hand", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newExchangeFixture(t)
			b := astral.GenerateIdentity()
			x, _, _ := seedShortenRows(t, f.mod.db, b)
			f.answer = []*nodes.EndpointWithTTL{nodes.NewEndpointWithTTL(tcpEndpoint("192.168.1.20"), 10*time.Minute)}

			resolver := astral.GenerateIdentity()
			if tc.firstHand {
				resolver = b
			}
			if _, err := f.mod.UpdateNodeEndpoints(f.mod.ctx, resolver, b); err != nil {
				t.Fatalf("UpdateNodeEndpoints: %v", err)
			}

			got := expiryOf(t, f.mod.db, b, "tcp", "192.168.1.20:1791")
			if !tc.firstHand && !got.Equal(x) {
				t.Fatalf("second-hand reply moved X's expiry to %v; want %v unchanged", got, x)
			}
			if tc.firstHand && (got.After(time.Now().Add(10*time.Minute)) || time.Until(got) < 9*time.Minute) {
				t.Fatalf("first-hand reply set X's expiry to %v; want about now+10m", got)
			}
		})
	}
}
