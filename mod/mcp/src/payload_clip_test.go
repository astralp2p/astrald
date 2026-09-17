package mcp

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func TestDecodePayload(t *testing.T) {
	for _, c := range []struct {
		name     string
		data     string
		isBase64 bool
		want     []byte
	}{
		{"empty", "", true, nil},
		{"base64", "aGk=", true, []byte("hi")},
		{"text", "hi", false, []byte("hi")},
	} {
		got, err := decodePayload(c.data, c.isBase64)
		if err != nil {
			t.Errorf("%s: decodePayload(%q, %v) error = %v, want nil", c.name, c.data, c.isBase64, err)
			continue
		}
		if !bytes.Equal(got, c.want) || (got == nil) != (c.want == nil) {
			t.Errorf("%s: decodePayload(%q, %v) = %#v, want %#v", c.name, c.data, c.isBase64, got, c.want)
		}
	}

	if got, err := decodePayload("!!", true); err == nil {
		t.Errorf("decodePayload(%q, true) = %v, nil; want a base64 error", "!!", got)
	}
}

func TestEncodePayload(t *testing.T) {
	for _, c := range []struct {
		name         string
		data         []byte
		wantPayload  string
		wantEncoding string
	}{
		{"nil", nil, "", ""},
		{"text", []byte("hi"), "hi", "utf8"},
		{"binary", []byte{0xff, 0xfe}, "//4=", "base64"},
	} {
		payload, encoding := encodePayload(c.data)
		if payload != c.wantPayload || encoding != c.wantEncoding {
			t.Errorf("%s: encodePayload(%#v) = %q, %q; want %q, %q",
				c.name, c.data, payload, encoding, c.wantPayload, c.wantEncoding)
		}
	}
}

func TestABinaryPayloadRoundTrips(t *testing.T) {
	want := []byte{0x00, 0xff, 0x80, 0x41, 0xc3}

	payload, encoding := encodePayload(want)
	got, err := decodePayload(payload, encoding == "base64")
	if err != nil {
		t.Fatalf("decodePayload(%q, %q) error = %v", payload, encoding, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestClipCutsOnARuneBoundary(t *testing.T) {
	for _, c := range []struct {
		text string
		n    int
		want string
	}{
		{"hello", 5, "hello"},
		{"hello", 3, "hel… (cut)"},
		{"aé", 2, "a… (cut)"},
		{"é", 1, "… (cut)"},
	} {
		got := clip(c.text, c.n)
		if got != c.want {
			t.Errorf("clip(%q, %d) = %q, want %q", c.text, c.n, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("clip(%q, %d) = %q, which is not valid UTF-8", c.text, c.n, got)
		}
	}
}
