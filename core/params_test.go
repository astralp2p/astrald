package core

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
)

func TestParamsGetInt(t *testing.T) {
	params := Params{"i": "-5"}

	i, err := params.GetInt("i")
	if i != -5 || err != nil {
		t.Fatalf("GetInt(i) = (%d, %v), want (-5, nil)", i, err)
	}

	if _, err := params.GetUint64("i"); !errors.Is(err, strconv.ErrSyntax) {
		t.Fatalf("GetUint64(i) error = %v, want wrapping %v", err, strconv.ErrSyntax)
	}

	if _, err := params.GetInt("missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("GetInt(missing) error = %v, want %v", err, ErrKeyNotFound)
	}
}

func TestParamsNonce(t *testing.T) {
	params := Params{}
	params.SetNonce("n", 0xab)

	if got, want := params["n"], "00000000000000ab"; got != want {
		t.Fatalf("SetNonce stored %q, want %q", got, want)
	}
	if got, want := params["n"], astral.Nonce(0xab).String(); got != want {
		t.Fatalf("SetNonce stored %q, want astral.Nonce.String() %q", got, want)
	}

	n, err := params.GetNonce("n")
	if n != 0xab || err != nil {
		t.Fatalf("GetNonce(n) = (%x, %v), want (ab, nil)", uint64(n), err)
	}
}

func TestParamsGetNonceInvalid(t *testing.T) {
	params := Params{"short": "ab", "hex": "zzzzzzzzzzzzzzzz"}

	_, err := params.GetNonce("short")
	if err == nil || err.Error() != "invalid nonce length" {
		t.Fatalf("GetNonce(short) error = %v, want %q", err, "invalid nonce length")
	}

	if _, err := params.GetNonce("hex"); !errors.Is(err, strconv.ErrSyntax) {
		t.Fatalf("GetNonce(hex) error = %v, want wrapping %v", err, strconv.ErrSyntax)
	}
}

func TestParamsUnixNano(t *testing.T) {
	params := Params{}
	params.SetUnixNano("t", time.Unix(0, 123))

	got, err := params.GetUnixNano("t")
	if err != nil || got.UnixNano() != 123 {
		t.Fatalf("GetUnixNano(t) = (%d, %v), want (123, nil)", got.UnixNano(), err)
	}
}

func TestParamsIdentity(t *testing.T) {
	id := secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
	params := Params{"bad": "bad"}
	params.SetIdentity("id", id)

	got, err := params.GetIdentity("id")
	if err != nil || !got.IsEqual(id) {
		t.Fatalf("GetIdentity(id) = (%v, %v), want (%v, nil)", got, err, id)
	}

	if _, err := params.GetIdentity("bad"); !errors.Is(err, astral.ErrInvalidKeyLength) {
		t.Fatalf("GetIdentity(bad) error = %v, want %v", err, astral.ErrInvalidKeyLength)
	}
}

func TestParamsGetObjectID(t *testing.T) {
	id, err := astral.Resolve(strings.NewReader("object"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	params := Params{"id": id.String(), "bad": "not-an-id"}

	got, err := params.GetObjectID("id")
	if err != nil || !got.IsEqual(id) {
		t.Fatalf("GetObjectID(id) = (%v, %v), want (%v, nil)", got, err, id)
	}

	if got, err := params.GetObjectID("bad"); err == nil {
		t.Fatalf("GetObjectID(bad) = (%v, nil), want non-nil error", got)
	}
}

func TestSplitQueryParams(t *testing.T) {
	tests := []struct {
		query      string
		wantPath   string
		wantParams string
	}{
		{query: "op?a=1", wantPath: "op", wantParams: "a=1"},
		{query: "op", wantPath: "op", wantParams: ""},
	}

	for _, tt := range tests {
		path, params := SplitQueryParams(tt.query)
		if path != tt.wantPath || params != tt.wantParams {
			t.Errorf("SplitQueryParams(%q) = (%q, %q), want (%q, %q)", tt.query, path, params, tt.wantPath, tt.wantParams)
		}
	}
}

func TestParseQuery(t *testing.T) {
	path, params := ParseQuery("op?a=1&c=x=y")

	if path != "op" {
		t.Errorf("ParseQuery path = %q, want %q", path, "op")
	}
	want := Params{"a": "1", "c": "x=y"}
	if !reflect.DeepEqual(params, want) {
		t.Errorf("ParseQuery params = %v, want %v", params, want)
	}
}

func TestQuery(t *testing.T) {
	tests := []struct {
		path   string
		params Params
		want   string
	}{
		{path: "op", params: Params{"a": "1"}, want: "op?a=1"},
		{path: "op", params: Params{}, want: "op"},
	}

	for _, tt := range tests {
		if got := Query(tt.path, tt.params); got != tt.want {
			t.Errorf("Query(%q, %v) = %q, want %q", tt.path, tt.params, got, tt.want)
		}
	}
}
