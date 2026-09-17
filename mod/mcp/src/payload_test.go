package mcp

import "testing"

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
