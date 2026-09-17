package apphost

import (
	"net/http"
	"testing"
)

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		remoteAddr string
		want       bool
	}{
		{"127.0.0.1:1", true},
		{"[::1]:80", true},
		{"127.0.0.1", true},
		{"10.0.0.1:80", false},
		{"localhost:80", false},
		{"", false},
	}

	for _, c := range cases {
		if got := isLoopback(&http.Request{RemoteAddr: c.remoteAddr}); got != c.want {
			t.Errorf("isLoopback(%q) = %v, want %v", c.remoteAddr, got, c.want)
		}
	}
}
