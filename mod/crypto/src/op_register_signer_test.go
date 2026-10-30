package crypto

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authapi "github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	secp256k1api "github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
	bip137sig "github.com/astralp2p/astrald/mod/bip137sig/src"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	secp256k1mod "github.com/astralp2p/astrald/mod/secp256k1"
	secp256k1 "github.com/astralp2p/astrald/mod/secp256k1/src"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	yaml "gopkg.in/yaml.v2"
)

const signerWait = 5 * time.Second

// quietWait is how long a test watches for an object that must not arrive.
const quietWait = 100 * time.Millisecond

// signerAuth answers RegisterSignerAction for its verdict and sudo for nothing,
// counting what it was asked. Every other auth method panics through the embedded
// nil interface.
type signerAuth struct {
	authmod.Module
	verdict atomic.Bool
	asked   atomic.Int32
}

func (a *signerAuth) Authorize(_ *astral.Context, action authapi.ActionObject) bool {
	if _, ok := action.(*authapi.RegisterSignerAction); ok {
		a.asked.Add(1)
		return a.verdict.Load()
	}
	return false
}

// verifyBIP137 and verifyASN1 expose only the verifying half of the real engines,
// because their signing half reaches for a module these tests do not build.
type verifyBIP137 struct{ cryptomod.TextVerifier }
type verifyASN1 struct{ cryptomod.HashVerifier }

// newSignerModule returns a bare Module that verifies with the real engines and
// admits every claim.
func newSignerModule() (*Module, *signerAuth) {
	authority := &signerAuth{}
	authority.verdict.Store(true)
	mod := &Module{
		Deps: Deps{Auth: authority},
		node: &identityNode{id: astral.GenerateIdentity()},
		log:  log.New(nil),
	}
	mod.AddEngine(verifyBIP137{bip137sig.Engine{}})
	mod.AddEngine(verifyASN1{secp256k1.Engine{}})
	return mod, authority
}

type signerConn struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (c signerConn) Close() error {
	for _, closer := range c.closers {
		closer.Close()
	}
	return nil
}

// openBinding routes one crypto.register_signer query from caller and returns the
// app's end of the channel.
func openBinding(t *testing.T, mod *Module, caller *astral.Identity, origin string) *channel.Channel {
	t.Helper()

	op, err := routing.NewOp(mod.OpRegisterSigner)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	t.Cleanup(cancel)

	q := astral.Launch(query.New(caller, caller, "crypto.register_signer", nil))
	if origin != "" {
		q.Extra.Set("origin", origin)
	}

	in, out := io.Pipe()
	conn, err := op.RouteQuery(ctx, q, out)
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	ch := channel.New(signerConn{in, conn, []io.Closer{in, conn}}, channel.WithLockedWrites())
	t.Cleanup(func() { ch.Close() })
	return ch
}

type receivedObject struct {
	obj astral.Object
	err error
}

func receiveFrom(t *testing.T, ch *channel.Channel) receivedObject {
	t.Helper()
	got := make(chan receivedObject, 1)
	go func() {
		obj, err := ch.Receive()
		got <- receivedObject{obj, err}
	}()
	select {
	case r := <-got:
		return r
	case <-time.After(signerWait):
		t.Fatal("nothing received")
		return receivedObject{}
	}
}

func expectObject[T astral.Object](t *testing.T, ch *channel.Channel) T {
	t.Helper()
	r := receiveFrom(t, ch)
	if r.err != nil {
		t.Fatalf("receive: %v", r.err)
	}
	v, ok := r.obj.(T)
	if !ok {
		var zero T
		t.Fatalf("received %s %v; want %T", r.obj.ObjectType(), r.obj, zero)
	}
	return v
}

// expectClosed reads until the channel ends, failing on anything but a refusal.
func expectClosed(t *testing.T, ch *channel.Channel) {
	t.Helper()
	for {
		r := receiveFrom(t, ch)
		if r.err != nil {
			return
		}
		if _, ok := r.obj.(*crypto.SignAnswer); ok {
			continue
		}
	}
}

func awaitDone(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(signerWait)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// signerIdentity is one key held only by a test's app.
type signerIdentity struct {
	private *crypto.PrivateKey
	public  *crypto.PublicKey
}

func newSignerIdentity() signerIdentity {
	private := secp256k1api.New()
	return signerIdentity{private: private, public: secp256k1api.PublicKey(private)}
}

func (id signerIdentity) signText(text string) *crypto.Signature {
	private, _ := btcec.PrivKeyFromBytes(id.private.Key)

	var buf bytes.Buffer
	writeBitcoinString(&buf, "Bitcoin Signed Message:\n")
	writeBitcoinString(&buf, text)
	first := sha256.Sum256(buf.Bytes())
	second := sha256.Sum256(first[:])

	return &crypto.Signature{Scheme: crypto.SchemeBIP137, Data: ecdsa.SignCompact(private, second[:], true)}
}

func writeBitcoinString(buf *bytes.Buffer, s string) {
	switch {
	case len(s) < 0xfd:
		buf.WriteByte(byte(len(s)))
	default:
		buf.WriteByte(0xfd)
		_ = binary.Write(buf, binary.LittleEndian, uint16(len(s)))
	}
	buf.WriteString(s)
}

func (id signerIdentity) signHash(t *testing.T, hash []byte) *crypto.Signature {
	t.Helper()
	signature, err := secp256k1mod.NewHashSignerASN1(id.private).SignHash(astral.NewContext(nil), hash)
	if err != nil {
		t.Fatalf("sign hash: %v", err)
	}
	return signature
}

func claimsOf(id signerIdentity, schemes ...string) *crypto.SignerKeys {
	set := &crypto.SignerKeys{}
	for _, scheme := range schemes {
		set.Keys = append(set.Keys, &crypto.SignerKey{Key: id.public, Scheme: astral.String8(scheme)})
	}
	return set
}

// bind opens a binding that claims set and is acknowledged.
func bind(t *testing.T, mod *Module, set *crypto.SignerKeys) *channel.Channel {
	t.Helper()
	ch := openBinding(t, mod, astral.GenerateIdentity(), "")
	if err := ch.Send(set); err != nil {
		t.Fatalf("send keys: %v", err)
	}
	expectObject[*astral.Ack](t, ch)
	return ch
}

// answerText answers the next request on ch through sign.
func answerText(t *testing.T, ch *channel.Channel, sign func(*crypto.SignTextRequest) *crypto.SignAnswer) *crypto.SignTextRequest {
	t.Helper()
	req := expectObject[*crypto.SignTextRequest](t, ch)
	answer := sign(req)
	answer.RequestID = req.RequestID
	if err := ch.Send(answer); err != nil {
		t.Fatalf("send answer: %v", err)
	}
	return req
}

type signResult struct {
	signature *crypto.Signature
	err       error
}

// signTextAsync signs through the module the way a signing op does.
func signTextAsync(t *testing.T, mod *Module, ctx *astral.Context, key *crypto.PublicKey, text string) <-chan signResult {
	t.Helper()
	out := make(chan signResult, 1)
	go func() {
		signer, err := mod.NewTextSigner(key, crypto.SchemeBIP137)
		if err != nil {
			out <- signResult{nil, err}
			return
		}
		signature, err := signer.SignText(ctx, text)
		out <- signResult{signature, err}
	}()
	return out
}

func awaitSign(t *testing.T, out <-chan signResult) signResult {
	t.Helper()
	select {
	case r := <-out:
		return r
	case <-time.After(signerWait):
		t.Fatal("signing did not return")
		return signResult{}
	}
}

// TestRegisterSignerAdmission: each refusal holds nothing, so a key refused here
// signs through nobody afterwards.
func TestRegisterSignerAdmission(t *testing.T) {
	id := newSignerIdentity()
	uncompressed := &crypto.PublicKey{Type: secp256k1api.KeyType, Key: append([]byte{4}, bytes.Repeat([]byte{1}, 64)...)}
	badPoint := &crypto.PublicKey{Type: secp256k1api.KeyType, Key: append([]byte{2}, bytes.Repeat([]byte{0xff}, 32)...)}

	tooMany := &crypto.SignerKeys{}
	for range crypto.MaxSignerKeys + 1 {
		tooMany.Keys = append(tooMany.Keys, &crypto.SignerKey{Key: newSignerIdentity().public, Scheme: crypto.SchemeBIP137})
	}

	for _, tc := range []struct {
		name string
		set  func(*Module) *crypto.SignerKeys
		want string
	}{
		{"an empty set", func(*Module) *crypto.SignerKeys { return &crypto.SignerKeys{} }, "claims no keys"},
		{"65 pairs", func(*Module) *crypto.SignerKeys { return tooMany }, "at most 64"},
		{"a duplicate pair", func(*Module) *crypto.SignerKeys { return claimsOf(id, crypto.SchemeBIP137, crypto.SchemeBIP137) }, "twice"},
		{"no key", func(*Module) *crypto.SignerKeys {
			return &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Scheme: crypto.SchemeBIP137}}}
		}, "names no key"},
		{"an unparseable key", func(*Module) *crypto.SignerKeys {
			return &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: badPoint, Scheme: crypto.SchemeBIP137}}}
		}, "invalid key"},
		{"an uncompressed key", func(*Module) *crypto.SignerKeys {
			return &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: uncompressed, Scheme: crypto.SchemeBIP137}}}
		}, "compressed"},
		{"a key of another type", func(*Module) *crypto.SignerKeys {
			return &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: &crypto.PublicKey{Type: "ed25519", Key: make([]byte, 33)}, Scheme: crypto.SchemeBIP137}}}
		}, cryptomod.ErrUnsupportedKeyType.Error()},
		{"an unsupported scheme", func(*Module) *crypto.SignerKeys { return claimsOf(id, "any") }, cryptomod.ErrUnsupportedScheme.Error()},
		{"the node's own key", func(mod *Module) *crypto.SignerKeys {
			return &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: secp256k1api.FromIdentity(mod.node.Identity()), Scheme: crypto.SchemeBIP137}}}
		}, cryptomod.ErrNodeKeyNotSignable.Error()},
		{"one good pair among bad ones", func(*Module) *crypto.SignerKeys {
			return &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: id.public, Scheme: crypto.SchemeBIP137}, {Key: uncompressed, Scheme: crypto.SchemeBIP137}}}
		}, "compressed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod, authority := newSignerModule()
			ch := openBinding(t, mod, astral.GenerateIdentity(), "")
			if err := ch.Send(tc.set(mod)); err != nil {
				t.Fatalf("send keys: %v", err)
			}

			refusal := expectObject[*astral.ErrorMessage](t, ch)
			if !strings.Contains(refusal.Error(), tc.want) {
				t.Fatalf("refusal = %q; want it to name %q", refusal, tc.want)
			}
			expectClosed(t, ch)

			if authority.asked.Load() != 0 {
				t.Fatal("a set that fails its check reached authorization")
			}
			if mod.signers.find(id.public, crypto.SchemeBIP137) != nil {
				t.Fatal("a refused set holds a claim")
			}
		})
	}
}

// TestRegisterSignerRefusesBeforeReadingKeys covers the refusals that need no key
// set: they answer without waiting for one.
func TestRegisterSignerRefusesBeforeReadingKeys(t *testing.T) {
	t.Run("a network origin", func(t *testing.T) {
		mod, _ := newSignerModule()
		ch := openBinding(t, mod, astral.GenerateIdentity(), astral.OriginNetwork)
		refusal := expectObject[*astral.ErrorMessage](t, ch)
		if !strings.Contains(refusal.Error(), "origin not allowed") {
			t.Fatalf("refusal = %q", refusal)
		}
	})
	t.Run("an origin not yet judged", func(t *testing.T) {
		mod, _ := newSignerModule()
		ch := openBinding(t, mod, astral.GenerateIdentity(), "mcp")
		refusal := expectObject[*astral.ErrorMessage](t, ch)
		if !strings.Contains(refusal.Error(), "origin not allowed") {
			t.Fatalf("refusal = %q", refusal)
		}
	})
	t.Run("the node's identity", func(t *testing.T) {
		mod, _ := newSignerModule()
		ch := openBinding(t, mod, mod.node.Identity(), "")
		refusal := expectObject[*astral.ErrorMessage](t, ch)
		if refusal.Error() != authapi.ErrNotPermitted.Error() {
			t.Fatalf("refusal = %q", refusal)
		}
	})
}

// TestSilentChannelIsClosed: a channel that names no keys holds nothing open.
func TestSilentChannelIsClosed(t *testing.T) {
	near, far := io.Pipe()
	defer far.Close()
	ch := channel.New(signerConn{near, far, []io.Closer{near, far}})

	if _, err := receiveSignerKeys(ch, 10*time.Millisecond); err == nil {
		t.Fatal("a silent channel answered no error")
	}
	if _, err := far.Write([]byte{0}); err == nil {
		t.Fatal("the channel is still open")
	}
}

// TestKeysInTimeLeaveTheChannelOpen: the op goes on using the channel, however soon
// the first object arrives.
func TestKeysInTimeLeaveTheChannelOpen(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()

	for range 200 {
		ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))
		binding := mod.signers.find(id.public, crypto.SchemeBIP137)
		if binding == nil {
			t.Fatal("the bound key has no binding")
		}
		ch.Close()
		awaitDone(t, "releasing the pair", func() bool {
			return mod.signers.find(id.public, crypto.SchemeBIP137) == nil
		})
	}
}

// TestRegisterSignerNeedsThePermit: the action decides, once per distinct key.
func TestRegisterSignerNeedsThePermit(t *testing.T) {
	mod, authority := newSignerModule()
	authority.verdict.Store(false)
	id := newSignerIdentity()

	ch := openBinding(t, mod, astral.GenerateIdentity(), "")
	if err := ch.Send(claimsOf(id, crypto.SchemeBIP137, crypto.SchemeASN1)); err != nil {
		t.Fatal(err)
	}

	refusal := expectObject[*astral.ErrorMessage](t, ch)
	if refusal.Error() != authapi.ErrNotPermitted.Error() {
		t.Fatalf("refusal = %q", refusal)
	}
	if got := authority.asked.Load(); got != 1 {
		t.Fatalf("authorization ran %d times for one distinct key; want 1", got)
	}
	if mod.signers.find(id.public, crypto.SchemeBIP137) != nil {
		t.Fatal("a refused caller holds a claim")
	}
}

// TestRegisterSignerClaims: a pair has one binding, admission takes all or none,
// and closing a binding frees its pairs.
func TestRegisterSignerClaims(t *testing.T) {
	mod, _ := newSignerModule()
	held, other := newSignerIdentity(), newSignerIdentity()

	first := bind(t, mod, claimsOf(held, crypto.SchemeBIP137))
	holder := mod.signers.find(held.public, crypto.SchemeBIP137)

	second := openBinding(t, mod, astral.GenerateIdentity(), "")
	if err := second.Send(&crypto.SignerKeys{Keys: []*crypto.SignerKey{
		{Key: other.public, Scheme: crypto.SchemeBIP137},
		{Key: held.public, Scheme: crypto.SchemeBIP137},
	}}); err != nil {
		t.Fatal(err)
	}
	refusal := expectObject[*astral.ErrorMessage](t, second)
	if refusal.Error() != crypto.ErrKeyClaimed.Error() {
		t.Fatalf("refusal = %q; want %q", refusal, crypto.ErrKeyClaimed)
	}
	if mod.signers.find(other.public, crypto.SchemeBIP137) != nil {
		t.Fatal("a refused set kept the pair that was free")
	}
	if mod.signers.find(held.public, crypto.SchemeBIP137) != holder {
		t.Fatal("a refused set disturbed the binding that held the pair")
	}

	first.Close()
	awaitDone(t, "releasing the closed binding's pair", func() bool {
		return mod.signers.find(held.public, crypto.SchemeBIP137) == nil
	})

	bind(t, mod, claimsOf(held, crypto.SchemeBIP137))
}

// TestSignRoutesToTheClaim covers the path a hardware signer takes, for both
// request kinds.
func TestSignRoutesToTheClaim(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137, crypto.SchemeASN1))
	ctx := astral.NewContext(nil)

	out := signTextAsync(t, mod, ctx, id.public, "hello")
	req := answerText(t, ch, func(req *crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Signature: id.signText(string(req.Text))}
	})
	if req.Text != "hello" || req.Scheme != crypto.SchemeBIP137 || !bytes.Equal(req.Key.Key, id.public.Key) {
		t.Fatalf("request = %+v", req)
	}
	if req.RequestID == 0 {
		t.Fatal("a request carries a zero RequestID")
	}
	if r := awaitSign(t, out); r.err != nil || r.signature == nil {
		t.Fatalf("sign text = %+v", r)
	}

	hash := bytes.Repeat([]byte{7}, 32)
	hashOut := make(chan signResult, 1)
	go func() {
		signer, err := mod.NewHashSigner(id.public, crypto.SchemeASN1)
		if err != nil {
			hashOut <- signResult{nil, err}
			return
		}
		signature, err := signer.SignHash(ctx, hash)
		hashOut <- signResult{signature, err}
	}()
	hashReq := expectObject[*crypto.SignHashRequest](t, ch)
	if !bytes.Equal(hashReq.Hash, hash) {
		t.Fatalf("hash request = %+v", hashReq)
	}
	if err := ch.Send(&crypto.SignAnswer{RequestID: hashReq.RequestID, Signature: id.signHash(t, hash)}); err != nil {
		t.Fatal(err)
	}
	if r := awaitSign(t, hashOut); r.err != nil || r.signature == nil {
		t.Fatalf("sign hash = %+v", r)
	}
}

// localEngine holds one key and signs for it, standing in for a key the node
// holds.
type localEngine struct {
	key   *crypto.PublicKey
	calls atomic.Int32
}

func (e *localEngine) NewTextSigner(key *crypto.PublicKey, _ string) (cryptomod.TextSigner, error) {
	if !bytes.Equal(key.Key, e.key.Key) {
		return nil, cryptomod.ErrUnsupported
	}
	return e, nil
}

func (e *localEngine) NewHashSigner(key *crypto.PublicKey, _ string) (cryptomod.HashSigner, error) {
	if !bytes.Equal(key.Key, e.key.Key) {
		return nil, cryptomod.ErrUnsupported
	}
	return e, nil
}

func (e *localEngine) SignText(*astral.Context, string) (*crypto.Signature, error) {
	e.calls.Add(1)
	return &crypto.Signature{Scheme: crypto.SchemeBIP137, Data: []byte("local")}, nil
}

func (e *localEngine) SignHash(*astral.Context, []byte) (*crypto.Signature, error) {
	e.calls.Add(1)
	return &crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte("local")}, nil
}

// TestLocalKeySignsLocally: an app claiming a key the node holds sees nothing,
// which is what keeps an app from reading what is signed under the User's key.
func TestLocalKeySignsLocally(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	local := &localEngine{key: id.public}
	mod.AddEngine(local)
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137, crypto.SchemeASN1))
	ctx := astral.NewContext(nil)

	if r := awaitSign(t, signTextAsync(t, mod, ctx, id.public, "secret")); r.err != nil || string(r.signature.Data) != "local" {
		t.Fatalf("sign text = %+v", r)
	}
	signer, err := mod.NewHashSigner(id.public, crypto.SchemeASN1)
	if err != nil {
		t.Fatal(err)
	}
	if signature, err := signer.SignHash(ctx, []byte{1}); err != nil || string(signature.Data) != "local" {
		t.Fatalf("sign hash = %v, %v", signature, err)
	}

	got := make(chan astral.Object, 1)
	go func() {
		if obj, err := ch.Receive(); err == nil {
			got <- obj
		}
	}()
	select {
	case obj := <-got:
		t.Fatalf("the claiming app received %s", obj.ObjectType())
	case <-time.After(quietWait):
	}
}

// TestUnclaimedPairsFindNoSigner: a claim covers its exact key and scheme, and a
// scheme never reaches the other kind of signer.
func TestUnclaimedPairsFindNoSigner(t *testing.T) {
	mod, _ := newSignerModule()
	id, other := newSignerIdentity(), newSignerIdentity()
	bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	if _, err := mod.NewTextSigner(other.public, crypto.SchemeBIP137); !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Fatalf("another key: %v", err)
	}
	if _, err := mod.NewHashSigner(id.public, crypto.SchemeASN1); !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Fatalf("a scheme it did not claim: %v", err)
	}
	if _, err := mod.NewHashSigner(id.public, crypto.SchemeBIP137); !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Fatalf("a text scheme asked of a hash signer: %v", err)
	}

	mod2, _ := newSignerModule()
	bind(t, mod2, claimsOf(id, crypto.SchemeASN1))
	if _, err := mod2.NewTextSigner(id.public, crypto.SchemeASN1); !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Fatalf("a hash scheme asked of a text signer: %v", err)
	}

	if _, err := mod.Sign(astral.NewContext(nil), other.public, &authapi.Contract{}); !errors.Is(err, &cryptomod.ErrKeyNotHeld{}) {
		t.Fatalf("Sign with an unclaimed key: %v", err)
	}
}

// TestSignatureMustVerify: what an app returns reaches no caller until it
// verifies, and an app that fails loses its binding.
func TestSignatureMustVerify(t *testing.T) {
	stranger := newSignerIdentity()

	for _, tc := range []struct {
		name string
		sign func(signerIdentity, string) *crypto.Signature
	}{
		{"another key's signature", func(_ signerIdentity, text string) *crypto.Signature { return stranger.signText(text) }},
		{"a signature over another text", func(id signerIdentity, _ string) *crypto.Signature { return id.signText("other") }},
		{"another scheme", func(id signerIdentity, text string) *crypto.Signature {
			signature := id.signText(text)
			signature.Scheme = crypto.SchemeASN1
			return signature
		}},
		{"garbage", func(signerIdentity, string) *crypto.Signature {
			return &crypto.Signature{Scheme: crypto.SchemeBIP137, Data: []byte{1, 2, 3}}
		}},
		{"an empty signature", func(signerIdentity, string) *crypto.Signature {
			return &crypto.Signature{Scheme: crypto.SchemeBIP137}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod, _ := newSignerModule()
			id := newSignerIdentity()
			ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

			out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
			answerText(t, ch, func(req *crypto.SignTextRequest) *crypto.SignAnswer {
				return &crypto.SignAnswer{Signature: tc.sign(id, string(req.Text))}
			})

			r := awaitSign(t, out)
			if !errors.Is(r.err, cryptomod.ErrInvalidSignature) || r.signature != nil {
				t.Fatalf("sign = %+v; want ErrInvalidSignature and no signature", r)
			}

			expectClosed(t, ch)
			awaitDone(t, "releasing the failed binding's pair", func() bool {
				return mod.signers.find(id.public, crypto.SchemeBIP137) == nil
			})
		})
	}
}

// acceptAll verifies anything. It stands in for a verifier a later engine brings
// that takes a scheme this request was not asked for.
type acceptAll struct{}

func (acceptAll) VerifyTextSignature(*crypto.PublicKey, *crypto.Signature, string) error { return nil }

// TestSignatureMustCarryTheAskedScheme: the caller asked for one scheme, and a
// verifier accepting another does not change what it asked for.
func TestSignatureMustCarryTheAskedScheme(t *testing.T) {
	mod := &Module{Deps: Deps{Auth: &signerAuth{}}, node: &identityNode{id: astral.GenerateIdentity()}, log: log.New(nil)}
	mod.Auth.(*signerAuth).verdict.Store(true)
	mod.AddEngine(acceptAll{})
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
	answerText(t, ch, func(*crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Signature: &crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte{1}}}
	})

	if r := awaitSign(t, out); !errors.Is(r.err, cryptomod.ErrInvalidSignature) {
		t.Fatalf("sign = %+v; want ErrInvalidSignature", r)
	}
}

// TestHashSignatureMustVerify is the same bar for the other request kind.
func TestHashSignatureMustVerify(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeASN1))
	hash := bytes.Repeat([]byte{7}, 32)

	out := make(chan signResult, 1)
	go func() {
		signer, err := mod.NewHashSigner(id.public, crypto.SchemeASN1)
		if err != nil {
			out <- signResult{nil, err}
			return
		}
		signature, err := signer.SignHash(astral.NewContext(nil), hash)
		out <- signResult{signature, err}
	}()

	req := expectObject[*crypto.SignHashRequest](t, ch)
	if err := ch.Send(&crypto.SignAnswer{RequestID: req.RequestID, Signature: id.signHash(t, []byte{1})}); err != nil {
		t.Fatal(err)
	}
	if r := awaitSign(t, out); !errors.Is(r.err, cryptomod.ErrInvalidSignature) {
		t.Fatalf("sign = %+v; want ErrInvalidSignature", r)
	}
	expectClosed(t, ch)
}

// TestRefusalReachesTheCaller: a person declining is an answer, so the binding
// stays and the caller reads why.
func TestRefusalReachesTheCaller(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
	answerText(t, ch, func(*crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Error: "declined on the device"}
	})

	r := awaitSign(t, out)
	if r.err == nil || !strings.Contains(r.err.Error(), "declined on the device") {
		t.Fatalf("sign = %+v; want the refusal", r)
	}
	if mod.signers.find(id.public, crypto.SchemeBIP137) == nil {
		t.Fatal("a refusal ended the binding")
	}
}

// TestBindingCloseFailsItsRequests: the caller learns at once, with no wait for
// the timeout and no retry.
func TestBindingCloseFailsItsRequests(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
	expectObject[*crypto.SignTextRequest](t, ch)
	ch.Close()

	if r := awaitSign(t, out); !errors.Is(r.err, cryptomod.ErrSignerGone) {
		t.Fatalf("sign = %+v; want ErrSignerGone", r)
	}
}

// TestCallerGoneCancelsTheRequest: the app is told, and its late answer changes
// nothing.
func TestCallerGoneCancelsTheRequest(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	out := signTextAsync(t, mod, ctx, id.public, "hello")
	req := expectObject[*crypto.SignTextRequest](t, ch)
	cancel()

	if r := awaitSign(t, out); r.err == nil {
		t.Fatalf("sign = %+v; want an error", r)
	}
	if got := expectObject[*crypto.SignCancel](t, ch); got.RequestID != req.RequestID {
		t.Fatalf("cancel names %v; want %v", got.RequestID, req.RequestID)
	}

	if err := ch.Send(&crypto.SignAnswer{RequestID: req.RequestID, Signature: id.signText("hello")}); err != nil {
		t.Fatal(err)
	}
	second := signTextAsync(t, mod, astral.NewContext(nil), id.public, "again")
	answerText(t, ch, func(req *crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Signature: id.signText(string(req.Text))}
	})
	if r := awaitSign(t, second); r.err != nil {
		t.Fatalf("after a late answer: %+v", r)
	}
}

// TestSignerTimeoutBoundsTheWait: an op's context is detached, so without this a
// signer that never answers holds its caller forever.
func TestSignerTimeoutBoundsTheWait(t *testing.T) {
	mod, _ := newSignerModule()
	mod.config.SignerTimeout = 10 * time.Millisecond
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
	req := expectObject[*crypto.SignTextRequest](t, ch)

	if r := awaitSign(t, out); r.err == nil {
		t.Fatalf("sign = %+v; want a timeout", r)
	}
	if got := expectObject[*crypto.SignCancel](t, ch); got.RequestID != req.RequestID {
		t.Fatalf("cancel names %v; want %v", got.RequestID, req.RequestID)
	}
}

// TestSignerTimeoutSurvivesAnAppThatDoesNotRead: the bound holds even when the
// request itself cannot be written.
func TestSignerTimeoutSurvivesAnAppThatDoesNotRead(t *testing.T) {
	mod, _ := newSignerModule()
	mod.config.SignerTimeout = 10 * time.Millisecond
	id := newSignerIdentity()
	bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	if r := awaitSign(t, signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")); r.err == nil {
		t.Fatalf("sign = %+v; want a timeout", r)
	}
}

// TestSignerTimeoutConfig pins the key crypto.yaml names, under the strict decoder
// the node reads it with.
func TestSignerTimeoutConfig(t *testing.T) {
	cfg := defaultConfig
	if err := yaml.UnmarshalStrict([]byte("signer_timeout: 30s\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SignerTimeout != 30*time.Second {
		t.Fatalf("SignerTimeout = %v; want 30s", cfg.SignerTimeout)
	}
}

func TestSignerTimeoutDefault(t *testing.T) {
	mod := &Module{}
	if got := mod.signerTimeout(); got != defaultConfig.SignerTimeout {
		t.Fatalf("signerTimeout = %v; want the default %v", got, defaultConfig.SignerTimeout)
	}
	mod.config.SignerTimeout = -time.Second
	if got := mod.signerTimeout(); got != defaultConfig.SignerTimeout {
		t.Fatalf("a negative value gave %v; want the default", got)
	}
}

// TestConcurrentRequestsResolve: answers out of order each reach their own caller.
func TestConcurrentRequestsResolve(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))
	ctx := astral.NewContext(nil)

	first := signTextAsync(t, mod, ctx, id.public, "first")
	firstReq := expectObject[*crypto.SignTextRequest](t, ch)
	second := signTextAsync(t, mod, ctx, id.public, "second")
	secondReq := expectObject[*crypto.SignTextRequest](t, ch)

	if firstReq.RequestID == secondReq.RequestID {
		t.Fatal("two requests share an ID")
	}

	if err := ch.Send(&crypto.SignAnswer{RequestID: secondReq.RequestID, Signature: id.signText(string(secondReq.Text))}); err != nil {
		t.Fatal(err)
	}
	if r := awaitSign(t, second); r.err != nil {
		t.Fatalf("second = %+v", r)
	}
	if err := ch.Send(&crypto.SignAnswer{RequestID: firstReq.RequestID, Signature: id.signText(string(firstReq.Text))}); err != nil {
		t.Fatal(err)
	}
	if r := awaitSign(t, first); r.err != nil {
		t.Fatalf("first = %+v", r)
	}
}

// TestMalformedFramesEndTheBinding: a frame the protocol does not name releases
// the claims and fails what was outstanding.
func TestMalformedFramesEndTheBinding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame func(signerIdentity, astral.Nonce) astral.Object
	}{
		{"an answer with neither", func(_ signerIdentity, id astral.Nonce) astral.Object {
			return &crypto.SignAnswer{RequestID: id}
		}},
		{"an answer with both", func(key signerIdentity, id astral.Nonce) astral.Object {
			return &crypto.SignAnswer{RequestID: id, Signature: key.signText("hello"), Error: "no"}
		}},
		{"an unexpected object", func(signerIdentity, astral.Nonce) astral.Object { return &astral.Ack{} }},
		{"a second key set", func(key signerIdentity, _ astral.Nonce) astral.Object { return claimsOf(key, crypto.SchemeASN1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod, _ := newSignerModule()
			id := newSignerIdentity()
			ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

			out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
			req := expectObject[*crypto.SignTextRequest](t, ch)
			if err := ch.Send(tc.frame(id, req.RequestID)); err != nil {
				t.Fatal(err)
			}

			if r := awaitSign(t, out); !errors.Is(r.err, cryptomod.ErrSignerGone) {
				t.Fatalf("sign = %+v; want ErrSignerGone", r)
			}
			awaitDone(t, "releasing the pair", func() bool {
				return mod.signers.find(id.public, crypto.SchemeBIP137) == nil
			})
		})
	}
}

// TestAnswerWithUnknownIDIsIgnored: the binding survives, and so does the request
// the answer did not name.
func TestAnswerWithUnknownIDIsIgnored(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	if err := ch.Send(&crypto.SignAnswer{RequestID: astral.NewNonce(), Error: "for nobody"}); err != nil {
		t.Fatal(err)
	}

	out := signTextAsync(t, mod, astral.NewContext(nil), id.public, "hello")
	answerText(t, ch, func(req *crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Signature: id.signText(string(req.Text))}
	})
	if r := awaitSign(t, out); r.err != nil {
		t.Fatalf("sign = %+v", r)
	}
}

// TestDuplicateAnswerDoesNotStall: the answering loop is the channel's only
// reader, so a second answer to one request must not block it.
func TestDuplicateAnswerDoesNotStall(t *testing.T) {
	b := newSignerBinding(&Module{}, astral.GenerateIdentity(), nil, nil)
	id, reply := b.addPending()

	answer := &crypto.SignAnswer{RequestID: id, Error: "first"}
	done := make(chan error, 1)
	go func() {
		if err := b.receiveAnswer(answer); err != nil {
			done <- err
			return
		}
		done <- b.receiveAnswer(&crypto.SignAnswer{RequestID: id, Error: "second"})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(signerWait):
		t.Fatal("a second answer blocked the reader")
	}
	if got := <-reply; got != answer {
		t.Fatalf("reply = %+v; want the first answer", got)
	}
}

// TestUnsignablePayloadKeepsTheBinding: a caller chooses the payload, and one that
// cannot be sent or verified must not end the binding for every other caller.
func TestUnsignablePayloadKeepsTheBinding(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137, crypto.SchemeASN1))
	ctx := astral.NewContext(nil)

	text, err := mod.NewTextSigner(id.public, crypto.SchemeBIP137)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := text.SignText(ctx, strings.Repeat("x", 70000)); err == nil {
		t.Fatal("a text too long for a frame was accepted")
	}
	if _, err := text.SignText(ctx, ""); err == nil {
		t.Fatal("an empty text was accepted")
	}

	hash, err := mod.NewHashSigner(id.public, crypto.SchemeASN1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hash.SignHash(ctx, make([]byte, 300)); err == nil {
		t.Fatal("a hash too long for a frame was accepted")
	}
	if _, err := hash.SignHash(ctx, nil); err == nil {
		t.Fatal("an empty hash was accepted")
	}

	// why a signature and not a lookup: the claims are released after the binding
	// ends, so a lookup can still find a binding that is gone.
	out := signTextAsync(t, mod, ctx, id.public, "still here")
	answerText(t, ch, func(req *crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Signature: id.signText(string(req.Text))}
	})
	if r := awaitSign(t, out); r.err != nil {
		t.Fatalf("an oversized payload ended the binding: %v", r.err)
	}
}

// failingHashSigner stands in for a signer that fails after being asked, and
// textCounter counts any text attempt that follows.
type failingHashSigner struct{ key *crypto.PublicKey }

func (e *failingHashSigner) NewHashSigner(*crypto.PublicKey, string) (cryptomod.HashSigner, error) {
	return e, nil
}

func (e *failingHashSigner) SignHash(*astral.Context, []byte) (*crypto.Signature, error) {
	return nil, errors.New("declined on the device")
}

type textCounter struct{ calls atomic.Int32 }

func (e *textCounter) NewTextSigner(*crypto.PublicKey, string) (cryptomod.TextSigner, error) {
	e.calls.Add(1)
	return nil, cryptomod.ErrUnsupported
}

// TestSignDoesNotFallBackAfterAFailure: the scheme is chosen by which signer
// exists. A signer that was asked and failed gets no second prompt under another
// scheme.
func TestSignDoesNotFallBackAfterAFailure(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	id := newSignerIdentity()
	counter := &textCounter{}
	mod.AddEngine(&failingHashSigner{key: id.public})
	mod.AddEngine(counter)

	_, err := mod.Sign(astral.NewContext(nil), id.public, &authapi.Contract{Issuer: astral.GenerateIdentity(), Subject: astral.GenerateIdentity()})
	if err == nil || !strings.Contains(err.Error(), "declined on the device") {
		t.Fatalf("Sign = %v; want the hash signer's failure", err)
	}
	if got := counter.calls.Load(); got != 0 {
		t.Fatalf("Sign asked for a text signer %d times after a hash signer failed", got)
	}
}

// TestSignFallsBackToAClaimedTextKey: a key claimed for bip137 alone has no hash
// signer, so Sign takes the text path. This is the hardware signer's path.
func TestSignFallsBackToAClaimedTextKey(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	contract := &authapi.Contract{Issuer: astral.GenerateIdentity(), Subject: astral.GenerateIdentity()}
	out := make(chan signResult, 1)
	go func() {
		signature, err := mod.Sign(astral.NewContext(nil), id.public, contract)
		out <- signResult{signature, err}
	}()

	answerText(t, ch, func(req *crypto.SignTextRequest) *crypto.SignAnswer {
		return &crypto.SignAnswer{Signature: id.signText(string(req.Text))}
	})

	r := awaitSign(t, out)
	if r.err != nil || r.signature == nil || r.signature.Scheme != crypto.SchemeBIP137 {
		t.Fatalf("Sign = %+v; want a bip137 signature", r)
	}
	if err := mod.Verify(id.public, r.signature, contract); err != nil {
		t.Fatalf("the signature does not verify: %v", err)
	}
}

// TestAuthorizeSignerGuardsTheApp: a caller the signer rule refuses causes no
// traffic to the app.
func TestAuthorizeSignerGuardsTheApp(t *testing.T) {
	mod, _ := newSignerModule()
	id := newSignerIdentity()
	ch := bind(t, mod, claimsOf(id, crypto.SchemeBIP137))

	keyText, err := id.public.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	rig := startOp(t, "crypto.sign_text?text=hello&key="+string(keyText), mod.OpSignText)

	refusal, ok := rig.receiveOne(t).(*astral.ErrorMessage)
	if !ok || refusal.Error() != cryptomod.ErrForeignKey.Error() {
		t.Fatalf("answer = %v; want %v", refusal, cryptomod.ErrForeignKey)
	}

	got := make(chan astral.Object, 1)
	go func() {
		if obj, err := ch.Receive(); err == nil {
			got <- obj
		}
	}()
	select {
	case obj := <-got:
		t.Fatalf("the app received %s for a refused caller", obj.ObjectType())
	case <-time.After(quietWait):
	}
}
