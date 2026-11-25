package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

// signApproval signs an Approval of the request reqID as issuer, with the
// node key $SWARMAPPS_NODE_KEY holds (the node's data/node_key, hex), and
// offers it with nodes.swarm_push to the node c reaches and to every node of
// to. It reports the Approval's entry ID and, per node, "ack" or the error
// the push answered.
//
// why the node's key file and not the node: crypto.sign_hash refuses the node
// key, and user.answer_request refuses an answer outside the node's tier and
// writes nothing, so a Member's Approval reaches SwarmState only as an
// offered entry, the way a merge meets it.
func signApproval(ctx *astral.Context, c *astrald.Client, issuer, reqID string, to []string) (map[string]any, error) {
	signer, err := astral.ParseIdentity(issuer)
	if err != nil {
		return nil, err
	}
	rid, err := astral.ParseID(reqID)
	if err != nil {
		return nil, err
	}
	key, err := nodeKey()
	if err != nil {
		return nil, err
	}
	body := user.NewApproval(signer, rid)
	sig, err := secp256k1sig.SignASN1(key, body.SignableHash())
	if err != nil {
		return nil, err
	}

	signed := &user.SignedApproval{Approval: body, IssuerSig: sig}
	id, err := astral.ResolveObjectID(signed)
	if err != nil {
		return nil, err
	}

	pushed := map[string]string{"local": push(ctx, c, signed)}
	for _, target := range to {
		tid, err := astral.ParseIdentity(target)
		if err != nil {
			return nil, err
		}
		pushed[target] = push(ctx, c.WithTarget(tid), signed)
	}
	return map[string]any{"approval_id": id.String(), "pushed": pushed}, nil
}

// push offers obj with nodes.swarm_push and returns "ack" or the error.
func push(ctx *astral.Context, c *astrald.Client, obj astral.Object) string {
	ch, err := c.QueryChannel(ctx, "nodes.swarm_push", nil)
	if err != nil {
		return err.Error()
	}
	defer ch.Close()
	if err = ch.Send(obj); err != nil {
		return err.Error()
	}
	if err = ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF()); err != nil {
		return err.Error()
	}
	return "ack"
}

// nodeKey decodes the node key $SWARMAPPS_NODE_KEY holds, as hex of the
// node's data/node_key.
func nodeKey() (*crypto.PrivateKey, error) {
	raw, err := hex.DecodeString(os.Getenv("SWARMAPPS_NODE_KEY"))
	if err != nil {
		return nil, err
	}
	obj, _, err := astral.Decode(bytes.NewReader(raw), astral.Canonical())
	if err != nil {
		return nil, err
	}
	key, ok := obj.(*crypto.PrivateKey)
	if !ok {
		return nil, errors.New("node_key holds no private key")
	}
	return key, nil
}
