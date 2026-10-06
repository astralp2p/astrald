package tor

import (
	"encoding/base32"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/tor"
)

// testServiceID is a syntactically valid v3 onion service ID (all-zero digest).
var testServiceID = strings.ToLower(base32.StdEncoding.EncodeToString(make([]byte, tor.DigestSize)))

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		port    string
		want    uint16
		wantErr bool
	}{
		{port: "0", want: 0},
		{port: "1791", want: 1791},
		{port: "65535", want: 65535},
		{port: "65536", wantErr: true},
		{port: "70000", wantErr: true},
		{port: "-1", wantErr: true},
	}

	for _, tt := range tests {
		addr := testServiceID + ".onion:" + tt.port
		e, err := Parse(addr)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) = port %d, want error", addr, e.Port)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) error: %v", addr, err)
			continue
		}
		if uint16(e.Port) != tt.want {
			t.Errorf("Parse(%q) port = %d, want %d", addr, e.Port, tt.want)
		}
	}
}
