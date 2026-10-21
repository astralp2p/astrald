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
		err  string
	}{
		{name: "no args", args: nil, want: map[string]string{}},
		{name: "flag pairs", args: []string{"-a", "1", "-b", "2"}, want: map[string]string{"a": "1", "b": "2"}},
		{name: "positional", args: []string{"pos"}, want: map[string]string{query.DefaultArgKey: "pos"}},
		{name: "pair and positional", args: []string{"-a", "1", "pos"}, want: map[string]string{"a": "1", query.DefaultArgKey: "pos"}},
		{name: "repeated flag", args: []string{"-a", "1", "-a", "2"}, want: map[string]string{"a": "2"}},
		{name: "positional value with =", args: []string{"-arg", "a=b"}, want: map[string]string{query.DefaultArgKey: "a=b"}},
		{name: "positional without key", args: []string{"=x"}, want: map[string]string{query.DefaultArgKey: "=x"}},
		{name: "key=value positional", args: []string{"user=X"}, err: "unexpected argument user=X: pass it as -user X"},
		{name: "key=value before a pair", args: []string{"a=1", "-b", "2"}, err: "unexpected argument a=1: pass it as -a 1"},
		{name: "flag without value", args: []string{"-user"}, err: "argument -user has no value"},
		{name: "pair and flag without value", args: []string{"-a", "1", "-user"}, err: "argument -user has no value"},
		{name: "positional before a pair", args: []string{"pos", "-a", "1"}, err: "unexpected argument pos"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseQueryArgs(tt.args)
			if tt.err != "" {
				if err == nil || err.Error() != tt.err {
					t.Fatalf("parseQueryArgs(%q) error = %v, want %q", tt.args, err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseQueryArgs(%q) error = %v, want nil", tt.args, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
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
