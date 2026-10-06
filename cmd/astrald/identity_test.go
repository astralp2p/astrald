package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/resources"
)

func TestLoadNodeIdentityCorruptKey(t *testing.T) {
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, secp256k1.New(), astral.Canonical()); err != nil {
		t.Fatal(err)
	}

	res := resources.NewMemResources()
	if err := res.Write("node_key", buf.Bytes()[:buf.Len()/2]); err != nil {
		t.Fatal(err)
	}

	id, err := loadNodeIdentity(res)
	if err == nil {
		t.Fatalf("expected error, got identity %v", id)
	}
	var unexpected *astral.ErrUnexpectedObject
	if errors.As(err, &unexpected) {
		t.Fatalf("expected decode error, got %T", err)
	}
	_ = err.Error()
}
