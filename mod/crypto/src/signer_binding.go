package crypto

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/sig"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// errMalformedAnswer ends a binding whose answer does not carry exactly one of a
// signature and an error.
var errMalformedAnswer = errors.New("a sign answer carries exactly one of a signature and an error")

// signerBinding is one open crypto.register_signer channel: the app behind it
// and its fixed pairs, with the requests it has not answered.
type signerBinding struct {
	mod      *Module
	provider *astral.Identity
	ch       *channel.Channel
	pairs    []signerClaim
	pending  sig.Map[astral.Nonce, chan *crypto.SignAnswer]

	// why: a claim can draw a request before the ack is sent; ready holds it so
	// the ack is always the first object the app reads.
	ready chan struct{}

	done   chan struct{}
	closed atomic.Bool
}

func newSignerBinding(mod *Module, provider *astral.Identity, ch *channel.Channel, pairs []signerClaim) *signerBinding {
	return &signerBinding{
		mod:      mod,
		provider: provider,
		ch:       ch,
		pairs:    pairs,
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// close ends the binding. Every outstanding request fails with ErrSignerGone,
// and the op holding the channel returns and releases the claims.
func (b *signerBinding) close() {
	if b.closed.CompareAndSwap(false, true) {
		close(b.done)
		_ = b.ch.Close()
	}
}

// send writes one object to the app, ending the binding when that fails.
func (b *signerBinding) send(obj astral.Object) {
	select {
	case <-b.ready:
	case <-b.done:
		return
	}

	if err := b.ch.Send(obj); err != nil {
		b.close()
	}
}

// receiveAnswer hands an answer to the request waiting for it. An answer no
// request waits for, including a late one, is ignored.
func (b *signerBinding) receiveAnswer(answer *crypto.SignAnswer) error {
	if (answer.Signature == nil) == (answer.Error == "") {
		return errMalformedAnswer
	}

	reply, found := b.pending.Get(answer.RequestID)
	if !found {
		return nil
	}

	// why non-blocking: a second answer to one request finds the buffer full, and
	// this is the only reader of the channel.
	select {
	case reply <- answer:
	default:
	}

	return nil
}

// addPending records a request and returns its nonzero, unused ID.
func (b *signerBinding) addPending() (astral.Nonce, chan *crypto.SignAnswer) {
	reply := make(chan *crypto.SignAnswer, 1)

	for {
		id := astral.NewNonce()
		if id == 0 {
			continue
		}
		if _, added := b.pending.Set(id, reply); added {
			return id, reply
		}
	}
}

// ask sends one request and waits for its answer.
//
// note: nothing here retries. A retry is a second prompt on a device.
func (b *signerBinding) ask(ctx *astral.Context, request func(astral.Nonce) astral.Object) (*crypto.SignAnswer, error) {
	// why: a person may take minutes, and routing.Op detaches an op's context, so
	// nothing else limits this wait.
	ctx, cancel := ctx.WithTimeout(b.mod.signerTimeout())
	defer cancel()

	id, reply := b.addPending()
	defer b.pending.Delete(id)

	// why a goroutine: Send blocks while the app is not reading, and an app must
	// not be able to hold a signing caller past the timeout.
	go b.send(request(id))

	select {
	case answer := <-reply:
		return answer, nil
	case <-b.done:
		return nil, cryptomod.ErrSignerGone
	case <-ctx.Done():
		go b.send(&crypto.SignCancel{RequestID: id})
		return nil, fmt.Errorf("signer %v did not answer: %w", b.provider, ctx.Err())
	}
}

// signed reads an answer into the signature a caller may receive. An answer
// without a signature is the app's refusal.
//
// why a signature that fails ends the binding: a claim proves nothing about
// possession of the key, so this check is what stops an app that does not hold
// it, and such an app has no further use.
func (b *signerBinding) signed(answer *crypto.SignAnswer, scheme string, verify func(*crypto.Signature) error) (*crypto.Signature, error) {
	if answer.Signature == nil {
		return nil, fmt.Errorf("signer %v: %v", b.provider, answer.Error)
	}

	err := verify(answer.Signature)
	if err == nil && string(answer.Signature.Scheme) != scheme {
		err = cryptomod.ErrInvalidSignature
	}
	if err != nil {
		b.mod.log.Log("signer %v answered a signature that does not verify, ending its binding: %v", b.provider, err)
		b.close()
		return nil, fmt.Errorf("signer %v: %w", b.provider, cryptomod.ErrInvalidSignature)
	}

	return answer.Signature, nil
}

// bindingTextSigner signs a text through a binding.
type bindingTextSigner struct {
	binding *signerBinding
	key     *crypto.PublicKey
	scheme  string
}

var _ cryptomod.TextSigner = &bindingTextSigner{}

func (s *bindingTextSigner) SignText(ctx *astral.Context, text string) (*crypto.Signature, error) {
	// why: a caller chooses the text. A frame that cannot be encoded, or a text the
	// verifier refuses for being empty, would end the binding for every other caller.
	if len(text) == 0 || len(text) > math.MaxUint16 {
		return nil, fmt.Errorf("a text signed through a signer is 1-%v bytes", math.MaxUint16)
	}

	answer, err := s.binding.ask(ctx, func(id astral.Nonce) astral.Object {
		return &crypto.SignTextRequest{
			RequestID: id,
			Key:       s.key,
			Scheme:    astral.String8(s.scheme),
			Text:      astral.String16(text),
		}
	})
	if err != nil {
		return nil, err
	}

	return s.binding.signed(answer, s.scheme, func(signature *crypto.Signature) error {
		return s.binding.mod.VerifyTextSignature(s.key, signature, text)
	})
}

// bindingHashSigner signs a hash through a binding.
type bindingHashSigner struct {
	binding *signerBinding
	key     *crypto.PublicKey
	scheme  string
}

var _ cryptomod.HashSigner = &bindingHashSigner{}

func (s *bindingHashSigner) SignHash(ctx *astral.Context, hash []byte) (*crypto.Signature, error) {
	// why: a caller chooses the hash. A frame that cannot be encoded, or a hash the
	// verifier refuses for being empty, would end the binding for every other caller.
	if len(hash) == 0 || len(hash) > math.MaxUint8 {
		return nil, fmt.Errorf("a hash signed through a signer is 1-%v bytes", math.MaxUint8)
	}

	answer, err := s.binding.ask(ctx, func(id astral.Nonce) astral.Object {
		return &crypto.SignHashRequest{
			RequestID: id,
			Key:       s.key,
			Scheme:    astral.String8(s.scheme),
			Hash:      hash,
		}
	})
	if err != nil {
		return nil, err
	}

	return s.binding.signed(answer, s.scheme, func(signature *crypto.Signature) error {
		return s.binding.mod.VerifyHashSignature(s.key, signature, hash)
	})
}

// signerTimeout returns the configured wait, or the default when the config sets
// none.
func (mod *Module) signerTimeout() time.Duration {
	if mod.config.SignerTimeout <= 0 {
		return defaultConfig.SignerTimeout
	}

	return mod.config.SignerTimeout
}
