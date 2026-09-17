package main

import (
	"reflect"
	"testing"

	"github.com/astralp2p/astral-go/lib/query"
)

func TestParseQueryArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{name: "no args", args: nil, want: map[string]string{}},
		{name: "flag pairs", args: []string{"-a", "1", "-b", "2"}, want: map[string]string{"a": "1", "b": "2"}},
		{name: "positional", args: []string{"pos"}, want: map[string]string{query.DefaultArgKey: "pos"}},
		{name: "pair and positional", args: []string{"-a", "1", "pos"}, want: map[string]string{"a": "1", query.DefaultArgKey: "pos"}},
		{name: "repeated flag", args: []string{"-a", "1", "-a", "2"}, want: map[string]string{"a": "2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseQueryArgs(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseQueryArgs(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestFilterList(t *testing.T) {
	var f filterList

	for _, s := range []string{"x", "y"} {
		if err := f.Set(s); err != nil {
			t.Fatalf("Set(%q) = %v, want nil", s, err)
		}
	}

	if got, want := f.String(), "x,y"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
