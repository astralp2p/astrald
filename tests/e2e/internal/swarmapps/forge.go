package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	cryptoclient "github.com/astralp2p/astral-go/api/crypto/client"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	secp256k1sig "github.com/astralp2p/astrald/mod/secp256k1"
)

// signApproval signs an Approval of the request reqID as issuer, with the
// node key $SWARMAPPS_NODE_KEY holds (the node's data/node_key, hex), and
// offers it with user.swarm_push to the node c reaches and to every node of
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

// push offers obj with user.swarm_push and returns "ack" or the error.
func push(ctx *astral.Context, c *astrald.Client, obj astral.Object) string {
	ch, err := c.QueryChannel(ctx, "user.swarm_push", nil)
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

// offerEntry fetches the entry entryID from the node c reaches
// (user.swarm_fetch) and offers it with user.swarm_push, as that node's
// token, to every node of to. It reports, per node, "ack" or the error the
// push answered.
//
// why: user.adopt and the contract push of objects.push are gone (G7), so a
// node of one swarm meets an entry of another only as an offered entry, the
// way a merge meets it.
func offerEntry(ctx *astral.Context, c *astrald.Client, entryID string, to []string) (map[string]any, error) {
	id, err := astral.ParseID(entryID)
	if err != nil {
		return nil, err
	}
	objs, err := fetchEntries(ctx, c, []*astral.ObjectID{id})
	if err != nil {
		return nil, err
	}
	if len(objs) != 1 {
		return nil, errors.New("the node does not store the entry")
	}
	pushed := map[string]string{}
	for _, target := range to {
		tid, err := astral.ParseIdentity(target)
		if err != nil {
			return nil, err
		}
		pushed[target] = push(ctx, c.WithTarget(tid), objs[0])
	}
	return map[string]any{"pushed": pushed}, nil
}

// signMembership builds a membership of subject at tier from issuer, valid
// for validity, with no Approval link, signs it as issuer through
// crypto.sign_hash on the node c reaches (c's token must be issuer's, the
// User's), and offers it with user.swarm_push to that node and to every node
// of to. It reports the membership's entry ID and, per node, "ack" or the
// error the push answered.
//
// why: user.adopt is gone (G7); the User's own signature on a new membership
// is how the User would bring a node back, so a test of an expel offers one.
func signMembership(ctx *astral.Context, c *astrald.Client, issuer, subject, tier string, validity time.Duration, to []string) (map[string]any, error) {
	issuerID, err := astral.ParseIdentity(issuer)
	if err != nil {
		return nil, err
	}
	subjectID, err := astral.ParseIdentity(subject)
	if err != nil {
		return nil, err
	}
	contract, err := user.NewMembership(issuerID, subjectID, user.Tier(tier), nil, validity)
	if err != nil {
		return nil, err
	}
	sig, err := cryptoclient.New(nil, c).SignHash(ctx, contract.SignableHash(), nil, "")
	if err != nil {
		return nil, err
	}
	signed := &auth.SignedContract{Contract: contract, IssuerSig: sig}
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
	return map[string]any{"membership_id": id.String(), "pushed": pushed}, nil
}
