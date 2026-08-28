package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestSendEndOfInputEncodesEOS proves the end-of-input marker is written in the requested
// input format — a json op reads `{"Type":"eos",...}` off its input channel and returns,
// rather than blocking for input that never comes.
func TestSendEndOfInputEncodesEOS(t *testing.T) {
	var buf bytes.Buffer

	if err := sendEndOfInput(&buf, "json"); err != nil {
		t.Fatalf("send end-of-input: %v", err)
	}

	if got := buf.String(); !strings.Contains(got, `"eos"`) {
		t.Fatalf("end-of-input marker %q does not carry an eos object", got)
	}
}
