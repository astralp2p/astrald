package user

import (
	"iter"
	"sync"

	"github.com/astralp2p/astral-go/api/auth"
	userClient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/core"
)

// askRelayFunc asks one sibling for the relay contracts issuer issued.
type askRelayFunc func(ctx *astral.Context, sibling, issuer *astral.Identity) ([]*auth.SignedContract, error)

// relayAnswer is one contract a sibling answered.
type relayAnswer struct {
	sender   *astral.Identity
	contract *auth.SignedContract
}

// FetchRelayContracts asks every linked sibling at once for the relay contracts
// issuer issued (user.relay_contracts), and yields each answered contract that a
// node admits from that sibling, with the sibling. It ends when every sibling
// has answered, ctx ends, or the consumer stops. Network zone only.
// Spec: .ai/system/topics/app-routing.md, Resolving the host.
func (mod *Module) FetchRelayContracts(ctx *astral.Context, issuer *astral.Identity) iter.Seq2[*astral.Identity, *auth.SignedContract] {
	return mod.fetchRelayContracts(ctx, mod.getSiblings(), issuer, mod.askRelayContracts)
}

// fetchRelayContracts is FetchRelayContracts over siblings, asking each with ask.
func (mod *Module) fetchRelayContracts(ctx *astral.Context, siblings []*astral.Identity, issuer *astral.Identity, ask askRelayFunc) iter.Seq2[*astral.Identity, *auth.SignedContract] {
	return func(yield func(*astral.Identity, *auth.SignedContract) bool) {
		if !ctx.Zone().Is(astral.ZoneNetwork) || len(siblings) == 0 {
			return
		}

		// why a cancel: a consumer that stops early ends the asks still running.
		fctx, cancel := ctx.WithCancel()
		defer cancel()

		answers := make(chan relayAnswer)
		var wg sync.WaitGroup
		for _, sibling := range siblings {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mod.sendRelayAnswers(fctx, sibling, issuer, ask, answers)
			}()
		}
		go func() {
			wg.Wait()
			close(answers)
		}()

		for {
			select {
			case a, ok := <-answers:
				if !ok || !yield(a.sender, a.contract) {
					return
				}
			case <-fctx.Done():
				return
			}
		}
	}
}

// sendRelayAnswers asks sibling and sends each answered contract the node
// admits from it to answers, until ctx ends.
// why the issuer check: the sibling is trusted to deliver, not to filter; a
// contract of another issuer would route the wrong target.
// why every permit relay-for: between nodes only such a contract travels
// (astral-docs topics/app-routing.md, Reach), and AdmitRelayContract passes
// a contract with no relay-for permit.
func (mod *Module) sendRelayAnswers(ctx *astral.Context, sibling, issuer *astral.Identity, ask askRelayFunc, answers chan<- relayAnswer) {
	contracts, err := ask(ctx, sibling, issuer)
	if err != nil {
		mod.log.Logv(1, "asking %v for relay contracts of %v: %v", sibling, issuer, err)
	}

	for _, sc := range contracts {
		if sc.IsNil() || !sc.Issuer.IsEqual(issuer) || !isRelayForContract(sc.Contract) {
			continue
		}
		if err := mod.AdmitRelayContract(sibling, sc); err != nil {
			mod.log.Logv(1, "refusing relay contract of %v from %v: %v", issuer, sibling, err)
			continue
		}
		if sig.Send(ctx, answers, relayAnswer{sender: sibling, contract: sc}) != nil {
			return
		}
	}
}

// askRelayContracts asks sibling with user.relay_contracts as this node.
// note: the context identity must be the node too, or the link relays for the caller (core/router_adapter.go).
func (mod *Module) askRelayContracts(ctx *astral.Context, sibling, issuer *astral.Identity) ([]*auth.SignedContract, error) {
	client := userClient.New(sibling, core.NewClientAs(mod.node, mod.node.Identity()))
	return client.RelayContracts(ctx.WithIdentity(mod.node.Identity()), issuer)
}
