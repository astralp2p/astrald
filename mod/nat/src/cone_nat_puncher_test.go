package nat

import (
	"bytes"
	"context"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
)

func portRange(from, to int) (ports []int) {
	for p := from; p <= to; p++ {
		ports = append(ports, p)
	}
	return
}

func TestCandidatePorts(t *testing.T) {
	tests := []struct {
		name           string
		center, spread int
		want           []int
	}{
		{"window around center", 1000, 10, portRange(990, 1010)},
		{"zero spread", 1000, 0, []int{1000}},
		{"negative spread", 1000, -3, []int{1000}},
		{"clamped at min port", 3, 10, portRange(1, 13)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := candidatePorts(tt.center, tt.spread); !slices.Equal(got, tt.want) {
				t.Errorf("candidatePorts(%d, %d) = %v; want %v", tt.center, tt.spread, got, tt.want)
			}
		})
	}
}

func TestPreparePunchTargets(t *testing.T) {
	peer := ip.IP(net.ParseIP("203.0.113.5"))

	addrs, allowed, err := preparePunchTargets(peer, 1791)
	if err != nil {
		t.Fatalf("preparePunchTargets: %v", err)
	}
	if len(addrs) != 2*portGuessRange+1 {
		t.Errorf("got %d targets; want %d", len(addrs), 2*portGuessRange+1)
	}

	for _, a := range addrs {
		if !a.IP.Equal(net.IP(peer)) {
			t.Errorf("target %v; want host %v", a, peer)
		}
	}

	tests := []struct {
		addr string
		want bool
	}{
		{"203.0.113.5:1781", true},
		{"203.0.113.5:1801", true},
		{"203.0.113.5:1780", false},
	}
	for _, tt := range tests {
		if got := allowed.Contains(tt.addr); got != tt.want {
			t.Errorf("allowed.Contains(%q) = %v; want %v", tt.addr, got, tt.want)
		}
	}
}

func TestJitterBounds(t *testing.T) {
	const base = 25 * time.Millisecond

	if got := jitter(base, 0); got != base {
		t.Errorf("jitter(%v, 0) = %v; want %v", base, got, base)
	}

	for i := 0; i < 1000; i++ {
		got := jitter(base, 3*time.Millisecond)
		if got < 22*time.Millisecond || got > 28*time.Millisecond {
			t.Fatalf("jitter(%v, 3ms) = %v; want within [22ms, 28ms]", base, got)
		}
	}
}

func TestNewConePuncherSession(t *testing.T) {
	p, err := newConePuncher(nil, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher(nil): %v", err)
	}
	if n := len(p.Session()); n != 16 {
		t.Errorf("generated session is %d bytes; want 16", n)
	}

	_, err = newConePuncher(make([]byte, 8), &ConePuncherCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "session must be 16 bytes") {
		t.Errorf("newConePuncher(8 bytes) error = %v; want \"session must be 16 bytes\"", err)
	}
}

func TestConePuncherSessionIsCopied(t *testing.T) {
	session := bytes.Repeat([]byte{0xab}, 16)
	original := bytes.Clone(session)

	p, err := newConePuncher(session, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher: %v", err)
	}

	session[0] = 0
	p.Session()[0] = 0

	if got := p.Session(); !bytes.Equal(got, original) {
		t.Errorf("Session() = % x; want % x", got, original)
	}
}

func TestConePuncherHolePunchValidatesArgs(t *testing.T) {
	p, err := newConePuncher(nil, &ConePuncherCallbacks{})
	if err != nil {
		t.Fatalf("newConePuncher: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	peer := ip.IP(net.ParseIP("203.0.113.5"))

	tests := []struct {
		name    string
		peer    ip.IP
		port    int
		wantErr string
	}{
		{"nil peer", nil, 1791, "empty peer IP"},
		{"port zero", peer, 0, "invalid peer port"},
		{"port above range", peer, 65536, "invalid peer port"},
		{"not opened", peer, 1791, "no UDP connection available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := p.HolePunch(ctx, tt.peer, tt.port)
			if res != nil {
				t.Errorf("HolePunch result = %+v; want nil", res)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("HolePunch(%v, %d) error = %v; want %q", tt.peer, tt.port, err, tt.wantErr)
			}
		})
	}
}
