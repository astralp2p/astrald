package mobile

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/astralp2p/astrald/resources"
)

func TestLoadNodeIdentityPersists(t *testing.T) {
	res := resources.NewMemResources()

	first, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("first loadNodeIdentity: %v", err)
	}
	if first.IsZero() {
		t.Fatal("first loadNodeIdentity returned a zero identity")
	}
	if key, err := res.Read("node_key"); err != nil || len(key) == 0 {
		t.Fatalf("node_key after first load = (%d bytes, %v), want non-empty key", len(key), err)
	}

	second, err := loadNodeIdentity(res)
	if err != nil {
		t.Fatalf("second loadNodeIdentity: %v", err)
	}
	if !second.IsEqual(first) {
		t.Fatalf("second loadNodeIdentity = %v, want %v", second, first)
	}
}

func TestParseCIDRList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "mixed entries",
			in:   "192.168.1.10/24\n fe80::1%wlan0/64 \nbad\n10.0.0.1\nfe80::2%eth0\n",
			want: []string{"192.168.1.10/24", "fe80::1/64"},
		},
		{
			name: "empty",
			in:   "",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrs := parseCIDRList(tt.in)
			if addrs == nil {
				t.Fatal("parseCIDRList returned nil, want non-nil slice")
			}
			got := make([]string, 0, len(addrs))
			for _, a := range addrs {
				got = append(got, a.(*net.IPNet).String())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseCIDRList(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSetupResources(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "cfg")
	data := filepath.Join(tmp, "data")

	tests := []struct {
		name     string
		dataDir  string
		wantRoot string
	}{
		{name: "config only", dataDir: "", wantRoot: cfg},
		{name: "separate data dir", dataDir: data, wantRoot: data},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := setupResources(cfg, tt.dataDir)
			if err != nil {
				t.Fatalf("setupResources: %v", err)
			}
			for _, dir := range []string{cfg, tt.wantRoot} {
				if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
					t.Fatalf("directory %q not created: %v", dir, err)
				}
			}
			roots, ok := res.(interface{ DataRoot() string })
			if !ok {
				t.Fatalf("setupResources returned %T, want a type with DataRoot", res)
			}
			if got := roots.DataRoot(); got != tt.wantRoot {
				t.Fatalf("DataRoot() = %q, want %q", got, tt.wantRoot)
			}
		})
	}
}
