package nearby

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// The periodic status interval, drawn uniformly from [minBroadcastInterval, maxBroadcastInterval].
//
// why: well inside statusTTL, so one lost datagram does not drop a peer from the
// caches, and jittered, so nodes started together do not send together.
const (
	minBroadcastInterval = 85 * time.Second
	maxBroadcastInterval = 95 * time.Second
)

// Broadcast sends the node's status on every local subnet. It sends nothing in
// ModeSilent, or when the composers attach nothing.
func (mod *Module) Broadcast() error {
	s, err := mod.status()
	if err != nil || s == nil {
		return err
	}
	return mod.Ether.Push(s)
}

// Scan asks the nodes on every local subnet for their status; it sends nothing in ModeSilent.
func (mod *Module) Scan() error {
	if mod.Mode() == nearby.ModeSilent {
		return nil
	}
	return mod.Ether.Push(&nearby.ScanMessage{})
}

// status composes the node's status and appends its StatusProof. It returns nil when
// nothing is to be sent: in ModeSilent, and when the composers attach nothing (Stealth
// without an active contract).
func (mod *Module) status() (*nearby.StatusMessage, error) {
	mode := mod.Mode()
	if mode == nearby.ModeSilent {
		return nil, nil
	}

	s := &nearby.StatusMessage{Attachments: astral.NewBundle()}
	for _, c := range mod.composers.Clone() {
		c.ComposeStatus(&Composition{receiver: astral.Anyone, s: s})
	}

	others := s.Attachments.Objects()
	if len(others) == 0 {
		return nil, nil
	}

	proof, err := mod.signStatus(others, mode == nearby.ModeStealth)
	if err != nil {
		return nil, err
	}
	if err = s.Attachments.Append(proof); err != nil {
		return nil, err
	}
	return s, nil
}

// signStatus returns the StatusProof of a status made of others, its signature wrapped
// when stealth.
func (mod *Module) signStatus(others []astral.Object, stealth bool) (*nearby.StatusProof, error) {
	proof := &nearby.StatusProof{Nonce: astral.NewNonce(), Timestamp: astral.Time(mod.now())}

	hash, err := nearby.ProofHash(others, proof.Nonce, proof.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("status proof hash: %w", err)
	}

	sig, err := mod.Crypto.NodeSigner().SignHash(astral.NewContext(nil), hash)
	if err != nil {
		return nil, fmt.Errorf("sign status: %w", err)
	}

	proof.Sig = astral.Bytes8(sig.Data)
	if stealth {
		proof.Sig = nearby.WrapProofSig(mod.node.Identity(), proof.Nonce, sig.Data)
	}
	return proof, nil
}

// runPeriodic sends the status at once, after every periodic interval, and on every
// mode change, until ctx ends.
func (mod *Module) runPeriodic(ctx *astral.Context) {
	modeUpdates := mod.mode.Follow(ctx)
	<-modeUpdates // note: the current mode; the first status goes out below

	// why: 255 is no defined mode, so the first pass logs on every node, a silent one included.
	last := nearby.Mode(255)
	for {
		if m := mod.Mode(); m != last {
			mod.log.Log("mode %v", m)
			last = m
		}

		if err := mod.Broadcast(); err != nil {
			mod.log.Error("broadcast status: %v", err)
		}

		select {
		case <-mod.after(broadcastInterval()):
		case _, ok := <-modeUpdates:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// broadcastInterval draws the wait before the next periodic status.
func broadcastInterval() time.Duration {
	return minBroadcastInterval + rand.N(maxBroadcastInterval-minBroadcastInterval+1)
}
