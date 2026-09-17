package fs

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

const colonPath = "/a:b/c.txt"

func requireLocation(t *testing.T, what string, got *FileLocation, want FileLocation) {
	t.Helper()

	if !got.NodeID.IsEqual(want.NodeID) || got.Path != want.Path {
		t.Fatalf("%s: got %v:%q, want %v:%q", what, got.NodeID, got.Path, want.NodeID, want.Path)
	}
}

func TestFileLocation_Text(t *testing.T) {
	loc := FileLocation{NodeID: astral.GenerateIdentity(), Path: colonPath}

	text, err := loc.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if got, want := string(text), loc.NodeID.String()+":"+colonPath; got != want {
		t.Fatalf("MarshalText: got %q, want %q", got, want)
	}

	var parsed FileLocation
	if err := parsed.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", text, err)
	}
	requireLocation(t, "UnmarshalText", &parsed, loc)
}

func TestFileLocation_UnmarshalTextErrors(t *testing.T) {
	var loc FileLocation

	if err := loc.UnmarshalText([]byte("nocolon")); err == nil || err.Error() != "invalid format" {
		t.Errorf("UnmarshalText(\"nocolon\"): got %v, want \"invalid format\"", err)
	}

	_, wantErr := astral.ParseIdentity("zz")
	if wantErr == nil {
		t.Fatal("ParseIdentity(\"zz\"): got nil error")
	}
	if err := loc.UnmarshalText([]byte("zz:/p")); err == nil || err.Error() != wantErr.Error() {
		t.Errorf("UnmarshalText(\"zz:/p\"): got %v, want %v", err, wantErr)
	}
}

func TestFileLocation_JSON(t *testing.T) {
	loc := FileLocation{NodeID: astral.GenerateIdentity(), Path: colonPath}

	data, err := json.Marshal(loc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if got, want := string(data), `{"NodeID":"`+loc.NodeID.String()+`","Path":"`+colonPath+`"}`; got != want {
		t.Fatalf("json.Marshal: got %s, want %s", got, want)
	}

	var parsed FileLocation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", data, err)
	}
	requireLocation(t, "json.Unmarshal", &parsed, loc)
}

func TestFileLocation_AstralCodec(t *testing.T) {
	loc := FileLocation{NodeID: astral.GenerateIdentity(), Path: colonPath}

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, &loc); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, _, err := astral.Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	parsed, ok := decoded.(*FileLocation)
	if !ok {
		t.Fatalf("Decode: got %T, want *FileLocation", decoded)
	}
	requireLocation(t, "Decode", parsed, loc)
}

func TestEventFileChanged_StringWithoutIDs(t *testing.T) {
	event := EventFileChanged{Path: "/x"}

	if got, want := event.String(), "changed /x (<nil> -> <nil>)"; got != want {
		t.Fatalf("String(): got %q, want %q", got, want)
	}
}
