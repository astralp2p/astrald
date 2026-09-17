package main

import (
	"reflect"
	"slices"
	"testing"
)

func TestStripEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		key  string
		want []string
	}{
		{
			name: "removes every entry for the key",
			env:  []string{"A=1", "TOKEN=x", "TOKEN_2=y", "TOKEN=z"},
			key:  "TOKEN",
			want: []string{"A=1", "TOKEN_2=y"},
		},
		{
			name: "nil env",
			env:  nil,
			key:  "K",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// why: stripEnv filters in place over the input backing array.
			env := append([]string(nil), tt.env...)

			got := stripEnv(env, tt.key)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("stripEnv(%q, %q) = %q, want %q", tt.env, tt.key, got, tt.want)
			}
		})
	}
}

func TestEnvList(t *testing.T) {
	var l envList

	if err := l.Set("K=V"); err != nil {
		t.Fatalf("Set(K=V) = %v, want nil", err)
	}
	if want := (envList{"K=V"}); !reflect.DeepEqual(l, want) {
		t.Fatalf("list after Set(K=V) = %q, want %q", l, want)
	}

	err := l.Set("KV")
	if want := "invalid env format: KV"; err == nil || err.Error() != want {
		t.Fatalf("Set(KV) error = %v, want %q", err, want)
	}

	if err := l.Set("A=B"); err != nil {
		t.Fatalf("Set(A=B) = %v, want nil", err)
	}
	if got, want := l.String(), "K=V A=B"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
