package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"io"

	"github.com/astralp2p/astral-go/api/crypto"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// keyRepo is the repository storeKey writes the key to, the one an app's key
// is stored in at registration (Objects.WriteDefault).
const keyRepo = "local"

// storeKey reads candidate objects from in, one hex-encoded canonical object
// per line (the files of a node's object store), finds the private key of
// identity among them and stores it on the node dst reaches, whose crypto
// module indexes it. This is an app's move to dst: its key, as a backup
// restored there.
//
// why the source node's files and not its ops: an expelled node runs without
// a user, and its gate refuses the object ops.
func storeKey(ctx *astral.Context, dst *astrald.Client, identity string, in io.Reader) (map[string]any, error) {
	id, err := astral.ParseIdentity(identity)
	if err != nil {
		return nil, err
	}
	key, err := findKey(in, id)
	if err != nil {
		return nil, err
	}
	stored, err := objectscli.New(nil, dst).Store(ctx, keyRepo, key)
	if err != nil {
		return nil, err
	}
	return map[string]any{"key_object": stored.String()}, nil
}

// findKey returns the private key of id among the hex-encoded objects in.
func findKey(in io.Reader, id *astral.Identity) (*crypto.PrivateKey, error) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		raw, err := hex.DecodeString(sc.Text())
		if err != nil {
			continue
		}
		obj, _, err := astral.Decode(bytes.NewReader(raw), astral.Canonical())
		if err != nil {
			continue
		}
		key, ok := obj.(*crypto.PrivateKey)
		if !ok || key.Type != secp256k1.KeyType {
			continue
		}
		if secp256k1.Identity(secp256k1.PublicKey(key)).IsEqual(id) {
			return key, nil
		}
	}
	return nil, errors.New("no private key of the identity among the objects")
}
