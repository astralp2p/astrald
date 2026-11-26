package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	cryptoclient "github.com/astralp2p/astral-go/api/crypto/client"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	userapi "github.com/astralp2p/astral-go/api/user"
	userclient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/mod/secp256k1"
)

// expiryValidity is the validity of ssm_expiry's membership contract: the
// join reaches the open follow within it.
const expiryValidity = 90 * time.Second

// expiryGrace bounds how long after the contract's expiry the open follow
// still shows the member's offering.
// note: node1 republishes its member set one second after the expiry.
const expiryGrace = 15 * time.Second

// expirySteps admits ssm_expiry to the swarm with a contract that expires in
// expiryValidity, waits until node1's open follow shows its offering, and
// checks that the follow drops the offering at the contract's expiry.
func expirySteps(w *world, s session, watch *servicescli.Watcher, name string, facts map[string]bool) error {
	exp := s.Nodes["ssm_expiry"]
	expAdmin := connect(exp.Endpoint, exp.Token)

	signed, err := shortContract(w, s)
	if err != nil {
		return fmt.Errorf("short contract: %w", err)
	}
	// note: no push to node1 (G7): a pushed membership is refused, and the
	// claim adds it to ssm_expiry's SwarmState, which node1 syncs.
	if err := userclient.New(nil, expAdmin).AcceptContract(w.ctx, signed); err != nil {
		return fmt.Errorf("accept contract on ssm_expiry: %w", err)
	}

	// why after the claim: an app's contract signed while ssm_expiry was
	// unclaimed does not outlive the claim (a Non-goal of G7).
	providerID, cl, err := w.app(expAdmin, exp, serveApps)
	if err != nil {
		return err
	}
	b, err := cl.AdvertiseOpen(w.ctx, []string{name}, true, (&evaluated{}).offer)
	if err != nil {
		return err
	}
	defer b.Close()

	expiresAt := signed.ExpiresAt.Time()
	if err := step(facts, "short_member_reaches_open_follow", holdsWithin(watch, time.Until(expiresAt), providerID.String()+"/"+name)); err != nil {
		return err
	}
	err = holdsWithin(watch, time.Until(expiresAt)+expiryGrace)
	if err == nil && time.Now().Before(expiresAt) {
		err = fmt.Errorf("the follow dropped the member %v before its contract expired", time.Until(expiresAt))
	}
	if err == nil {
		fmt.Fprintf(os.Stderr, "the follow dropped the member %v after its contract expired\n", time.Since(expiresAt))
	}
	return step(facts, "expiry_removes_from_open_follow", err)
}

// shortContract builds ssm_expiry's membership contract, valid for
// expiryValidity, signed by the User on node1 and by ssm_expiry's node key.
func shortContract(w *world, s session) (*auth.SignedContract, error) {
	key, err := expiryNodeKey()
	if err != nil {
		return nil, err
	}
	return signNodeContract(w, s, s.Nodes["ssm_expiry"].Identity, key, expiryValidity)
}

// signNodeContract builds subject's PrivilegedMembership, valid for validity,
// signed by the User on node1 and by key, the subject's node key.
//
// why the node key from the node's files: every op that collects a node's
// subject signature refuses a contract shorter than an hour
// (user.MinimalContractLength), and a node signs nothing else with its key.
func signNodeContract(w *world, s session, subject string, key *crypto.PrivateKey, validity time.Duration) (*auth.SignedContract, error) {
	userID, err := astral.ParseIdentity(s.Facts.UserID)
	if err != nil {
		return nil, err
	}
	subjectID, err := astral.ParseIdentity(subject)
	if err != nil {
		return nil, err
	}
	// why a PrivilegedMembership: since G7 the user key signs no other
	// membership; a Member's is signed by its carrier for an Approval.
	contract, err := userapi.NewMembership(userID, subjectID, userapi.TierPrivilegedMember, nil, validity)
	if err != nil {
		return nil, err
	}
	hash := contract.SignableHash()
	issuerSig, err := cryptoclient.New(w.node1, connect(w.n1.Endpoint, s.Facts.UserToken)).SignHash(w.ctx, hash, nil, "")
	if err != nil {
		return nil, fmt.Errorf("sign as the user: %w", err)
	}
	subjectSig, err := secp256k1.SignASN1(key, hash)
	if err != nil {
		return nil, fmt.Errorf("sign as %v: %w", subject, err)
	}
	return &auth.SignedContract{Contract: contract, IssuerSig: issuerSig, SubjectSig: subjectSig}, nil
}

// admitClaimed makes the node subject, just claimed with a membership the
// User signed, and node1 learn each other's membership: subject's is offered
// to node1, node1's to subject, each with nodes.swarm_push.
//
// why both ways: node1 and subject linked while subject was unclaimed, so
// subject, then its own root, dropped node1's membership on that first link,
// and nodes exchange SwarmState only on a first link (mod/nodes
// onSwarmLink). Without node1's membership subject counts no node1, refuses
// node1's relay contracts and answers no discovery node1 carries for its
// apps.
func admitClaimed(w *world, subjectAdmin *astrald.Client, subject string, signed *auth.SignedContract) error {
	if err := swarmPush(w.ctx, subjectAdmin.WithTarget(w.node1), signed); err != nil {
		return fmt.Errorf("offer the membership to node1: %w", err)
	}
	// why subject asks: user.contract answers a caller linked to the node
	// asked, as a nearby peer reads a node's contract
	own, err := userclient.New(w.node1, subjectAdmin).Contract(w.ctx)
	if err != nil {
		return fmt.Errorf("node1's membership: %w", err)
	}
	if err := swarmPush(w.ctx, subjectAdmin, own); err != nil {
		return fmt.Errorf("offer node1's membership to %v: %w", subject, err)
	}
	return nil
}

// swarmPush offers obj, a SwarmState entry, to the node c targets with
// nodes.swarm_push.
func swarmPush(ctx *astral.Context, c *astrald.Client, obj astral.Object) error {
	ch, err := c.QueryChannel(ctx, "nodes.swarm_push", nil)
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.Send(obj); err != nil {
		return err
	}
	return ch.Switch(channel.ExpectAck, channel.PassErrors, channel.FailOnEOF())
}

// expiryNodeKey decodes ssm_expiry's node_key, which the script passes in
// EXPIRY_NODE_KEY as hex.
func expiryNodeKey() (*crypto.PrivateKey, error) { return nodeKeyFromEnv("EXPIRY_NODE_KEY") }

// nodeKeyFromEnv decodes a node_key the script passes in the variable name
// as hex.
func nodeKeyFromEnv(name string) (*crypto.PrivateKey, error) {
	raw, err := hex.DecodeString(os.Getenv(name))
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("%v: %v", name, err)
	}
	obj, _, err := astral.Decode(bytes.NewReader(raw), astral.Canonical())
	if err != nil {
		return nil, err
	}
	key, ok := obj.(*crypto.PrivateKey)
	if !ok {
		return nil, astral.NewErrUnexpectedObject(obj)
	}
	return key, nil
}
