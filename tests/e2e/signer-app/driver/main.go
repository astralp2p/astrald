// Command driver claims a node with a User key only this process holds, through
// an app holding a crypto.register_signer binding, and probes the binding's rules.
//
//	driver <session.json>   prints facts as JSON
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	authcli "github.com/astralp2p/astral-go/api/auth/client"
	"github.com/astralp2p/astral-go/api/crypto"
	cryptocli "github.com/astralp2p/astral-go/api/crypto/client"
	"github.com/astralp2p/astral-go/api/secp256k1"
	usercli "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
)

const (
	declineText = "decline this"
	forgeText   = "forge this"
	declined    = "declined on the device"
	waitFor     = 15 * time.Second
)

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}

	facts := map[string]bool{}
	user, err := run(os.Args[1], facts)

	out, _ := json.Marshal(map[string]any{"signer_app": facts, "signer_app_user": user})
	fmt.Println(string(out))

	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// recordingSigner signs with a key the node never sees and records every request.
type recordingSigner struct {
	key    *crypto.PrivateKey
	forger *crypto.PrivateKey

	mu    sync.Mutex
	texts []string
}

func (s *recordingSigner) SignText(_ *astral.Context, req *crypto.SignTextRequest) (*crypto.Signature, error) {
	text := string(req.Text)

	s.mu.Lock()
	s.texts = append(s.texts, text)
	s.mu.Unlock()

	switch text {
	case declineText:
		return nil, errors.New(declined)
	case forgeText:
		return signBIP137(s.forger, text), nil
	}
	return signBIP137(s.key, text), nil
}

func (s *recordingSigner) SignHash(*astral.Context, *crypto.SignHashRequest) (*crypto.Signature, error) {
	return nil, errors.New("this signer claims no hash scheme")
}

func (s *recordingSigner) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

// signBIP137 is a software stand-in for what a hardware wallet produces.
func signBIP137(key *crypto.PrivateKey, text string) *crypto.Signature {
	private, _ := btcec.PrivKeyFromBytes(key.Key)

	var buf bytes.Buffer
	for _, s := range []string{"Bitcoin Signed Message:\n", text} {
		buf.WriteByte(byte(len(s)))
		buf.WriteString(s)
	}
	first := sha256.Sum256(buf.Bytes())
	second := sha256.Sum256(first[:])

	return &crypto.Signature{Scheme: crypto.SchemeBIP137, Data: ecdsa.SignCompact(private, second[:], true)}
}

// claim opens a binding by hand and returns what the node answered, because the
// client retries a claimed key for 30 seconds and these probes want the refusal.
func claim(ctx *astral.Context, client *astrald.Client, set *crypto.SignerKeys) string {
	ch, err := client.QueryChannel(ctx, crypto.MethodRegisterSigner, nil)
	if err != nil {
		return err.Error()
	}
	defer ch.Close()

	if err = ch.Send(set); err != nil {
		return err.Error()
	}

	obj, err := ch.Receive()
	switch {
	case err != nil:
		return err.Error()
	case obj.ObjectType() == (astral.Ack{}).ObjectType():
		return "ack"
	}
	if refusal, ok := obj.(error); ok {
		return refusal.Error()
	}
	return obj.ObjectType()
}

type world struct {
	ctx        *astral.Context
	endpoint   string
	apps       *apphostcli.Client
	userKey    *crypto.PublicKey
	nodeKey    *crypto.PublicKey
	signer     *recordingSigner
	signerApp  *astrald.Client
	userCrypto *cryptocli.Client
}

// appClient registers one app holding permits and returns its session and identity.
func (w *world) appClient(permits ...string) (*astrald.Client, *astral.Identity, error) {
	token, err := w.apps.Register(w.ctx, permits...)
	if err != nil {
		return nil, nil, fmt.Errorf("register: %w", err)
	}
	return connect(w.endpoint, string(token.Token)), token.Identity, nil
}

func (w *world) bindUserKey() (*cryptocli.SignerBinding, error) {
	keys := []*crypto.SignerKey{{Key: w.userKey, Scheme: crypto.SchemeBIP137}}
	return cryptocli.New(nil, w.signerApp).RegisterSigner(w.ctx, keys, w.signer)
}

func run(path string, facts map[string]bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	n := s.Nodes["signer1"]
	ctx := astral.NewContext(nil)
	admin := connect(n.Endpoint, n.Token)

	nodeID, err := astral.ParseIdentity(n.Identity)
	if err != nil {
		return "", err
	}

	userPrivate := secp256k1.New()
	w := &world{
		ctx:      ctx,
		endpoint: n.Endpoint,
		apps:     apphostcli.New(nil, admin),
		userKey:  secp256k1.PublicKey(userPrivate),
		nodeKey:  secp256k1.FromIdentity(nodeID),
		signer:   &recordingSigner{key: userPrivate, forger: secp256k1.New()},
	}
	userID := secp256k1.Identity(w.userKey)

	w.signerApp, _, err = w.appClient(auth.RegisterSignerAction{}.ObjectType())
	if err != nil {
		return userID.String(), err
	}

	binding, err := w.bindUserKey()
	if err != nil {
		return userID.String(), fmt.Errorf("bind: %w", err)
	}
	facts["bound"] = true

	if err = claimNode(w, admin, userID, facts); err != nil {
		return userID.String(), err
	}

	token, err := w.apps.CreateToken(ctx, userID, 0)
	if err != nil {
		return userID.String(), fmt.Errorf("user token: %w", err)
	}
	w.userCrypto = cryptocli.New(nil, connect(n.Endpoint, string(token.Token)))

	if err = signAsUser(w, facts); err != nil {
		return userID.String(), err
	}
	if err = probeAdmission(w, facts); err != nil {
		return userID.String(), err
	}
	if err = probeLocalKey(w, facts); err != nil {
		return userID.String(), err
	}
	probeDecline(w, facts)

	if binding, err = probeForgery(w, binding, facts); err != nil {
		return userID.String(), err
	}

	return userID.String(), probeClose(w, binding, userID, connect(n.Endpoint, string(token.Token)), facts)
}

// claimNode claims the node with the User key, which only the app can sign for.
func claimNode(w *world, admin *astrald.Client, userID *astral.Identity, facts map[string]bool) error {
	users := usercli.New(nil, admin)

	contract, err := users.NewContract(w.ctx, userID.String())
	if err != nil {
		return fmt.Errorf("new contract: %w", err)
	}

	signed, err := authcli.New(nil, admin).SignContract(w.ctx, contract)
	if err != nil {
		return fmt.Errorf("sign contract: %w", err)
	}
	facts["claim_signed"] = true
	facts["claim_issuer_is_bip137"] = signed.IssuerSig != nil && signed.IssuerSig.Scheme == crypto.SchemeBIP137
	facts["claim_subject_is_asn1"] = signed.SubjectSig != nil && signed.SubjectSig.Scheme == crypto.SchemeASN1

	if err = users.AcceptContract(w.ctx, signed); err != nil {
		return fmt.Errorf("accept contract: %w", err)
	}
	facts["claim_accepted"] = true

	return nil
}

func signAsUser(w *world, facts map[string]bool) error {
	const text = "hello from the user"

	signature, err := w.userCrypto.SignText(w.ctx, text, nil, "")
	if err != nil {
		return fmt.Errorf("sign text: %w", err)
	}

	err = w.userCrypto.VerifyTextSignature(w.ctx, text, w.userKey, signature)
	facts["text_signed_and_verified"] = err == nil

	seen := w.signer.seen()
	facts["app_saw_the_text"] = len(seen) > 0 && seen[len(seen)-1] == text

	return nil
}

func probeAdmission(w *world, facts map[string]bool) error {
	stranger, _, err := w.appClient()
	if err != nil {
		return err
	}
	strangerCrypto := cryptocli.New(nil, stranger)

	before := len(w.signer.seen())
	_, err = strangerCrypto.SignText(w.ctx, "not mine", w.userKey, "")
	facts["foreign_caller_refused"] = err != nil && strings.Contains(err.Error(), "another identity")
	facts["foreign_caller_never_reached_the_app"] = len(w.signer.seen()) == before

	userPair := &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: w.userKey, Scheme: crypto.SchemeBIP137}}}
	facts["no_permit_refused"] = claim(w.ctx, stranger, userPair) == auth.ErrNotPermitted.Error()

	permitted, _, err := w.appClient(auth.RegisterSignerAction{}.ObjectType())
	if err != nil {
		return err
	}

	nodePair := &crypto.SignerKeys{Keys: []*crypto.SignerKey{{Key: w.nodeKey, Scheme: crypto.SchemeBIP137}}}
	facts["node_key_refused"] = strings.Contains(claim(w.ctx, permitted, nodePair), "node's key")
	facts["claimed_key_refused"] = claim(w.ctx, permitted, userPair) == crypto.ErrKeyClaimed.Error()
	facts["empty_set_refused"] = strings.Contains(claim(w.ctx, permitted, &crypto.SignerKeys{}), "claims no keys")

	return nil
}

// countingSigner counts the requests that reach it, and signs none of them.
type countingSigner struct {
	mu    sync.Mutex
	count int
}

func (s *countingSigner) SignText(*astral.Context, *crypto.SignTextRequest) (*crypto.Signature, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return nil, errors.New("this app signs nothing")
}

func (s *countingSigner) SignHash(*astral.Context, *crypto.SignHashRequest) (*crypto.Signature, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return nil, errors.New("this app signs nothing")
}

// probeLocalKey has an app claim its own key, which the node generated and holds.
func probeLocalKey(w *world, facts map[string]bool) error {
	app, id, err := w.appClient(auth.RegisterSignerAction{}.ObjectType())
	if err != nil {
		return err
	}

	counter := &countingSigner{}
	key := secp256k1.FromIdentity(id)
	binding, err := cryptocli.New(nil, app).RegisterSigner(w.ctx, []*crypto.SignerKey{{Key: key, Scheme: crypto.SchemeBIP137}}, counter)
	if err != nil {
		return fmt.Errorf("claim the app's own key: %w", err)
	}
	defer binding.Close()

	signature, err := cryptocli.New(nil, app).SignText(w.ctx, "my own key", nil, "")
	facts["local_key_signed_locally"] = err == nil && signature != nil

	counter.mu.Lock()
	defer counter.mu.Unlock()
	facts["local_key_never_reached_the_app"] = counter.count == 0

	return nil
}

func probeDecline(w *world, facts map[string]bool) {
	before := len(w.signer.seen())
	_, err := w.userCrypto.SignText(w.ctx, declineText, nil, "")
	facts["decline_reached_the_caller"] = err != nil && strings.Contains(err.Error(), declined)
	facts["decline_asked_once"] = len(w.signer.seen()) == before+1
}

func probeForgery(w *world, binding *cryptocli.SignerBinding, facts map[string]bool) (*cryptocli.SignerBinding, error) {
	_, err := w.userCrypto.SignText(w.ctx, forgeText, nil, "")
	facts["forgery_refused"] = err != nil && strings.Contains(err.Error(), "invalid signature")

	select {
	case <-binding.Done():
		facts["forgery_ended_the_binding"] = true
	case <-time.After(waitFor):
	}

	rebound, err := w.bindUserKey()
	if err != nil {
		return nil, fmt.Errorf("rebind: %w", err)
	}

	_, err = w.userCrypto.SignText(w.ctx, "after the forgery", nil, "")
	facts["rebound_after_forgery"] = err == nil

	return rebound, nil
}

// probeClose closes the binding and asks for a contract signature, which names
// the held-key error in full.
func probeClose(w *world, binding *cryptocli.SignerBinding, userID *astral.Identity, asUser *astrald.Client, facts map[string]bool) error {
	if err := binding.Close(); err != nil {
		return err
	}

	contract := &auth.Contract{
		Issuer:    userID,
		Subject:   userID,
		ExpiresAt: astral.Time(time.Now().Add(time.Hour)),
	}

	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		_, err := authcli.New(nil, asUser).SignContract(w.ctx, contract)
		if err != nil && strings.Contains(err.Error(), "no signing scheme available") {
			facts["closed_binding_holds_nothing"] = true
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}
