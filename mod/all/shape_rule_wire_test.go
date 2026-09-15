package all_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	_ "github.com/astralp2p/astrald/mod/all"
	_ "github.com/astralp2p/astrald/mod/all/pub"
	"github.com/astralp2p/astrald/mod/archives"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/nearby"
	"github.com/astralp2p/astrald/mod/nodes/frames"
	"github.com/astralp2p/astrald/mod/shell"
)

// astral-go's reflection codec refuses a struct field no Blueprint describes, so every astrald
// type that held one moved to astral field types. The vectors are what each type wrote at
// astrald 0bad3128, binary and JSON, with two changes made on purpose:
//
//   - mod.nearby.public_profile: NodeAlias is string8, as protocols/nearby/types/
//     mod.nearby.public_profile.md states. The code wrote string32.
//   - astrald.mod.archives.events.archive_indexed: an entry's Modified is astral.Time. The
//     time.Time field wrote no bytes, so the vector gains the 8-byte time 17979cfe362a0000.
//   - mod.nearby.stealth_hint JSON: Commitment and MaskedID are number arrays, the JSON its
//     Blueprint produces. The []byte fields wrote base64.

type wireVector struct {
	name string
	hex  string
	json string
}

var wireVectors = []wireVector{
	{"nodes.frames.ping", "010203040506070801", "{\"Nonce\":\"102030405060708\",\"Pong\":true}"},
	{"nodes.frames.query", "010203040506070800003039000568656c6c6f", "{\"Nonce\":\"102030405060708\",\"Buffer\":12345,\"Query\":\"hello\"}"},
	{"nodes.frames.read", "010203040506070800001000", "{\"Nonce\":\"102030405060708\",\"Len\":4096}"},
	{"nodes.frames.response", "0102030405060708010000000a", "{\"Nonce\":\"102030405060708\",\"ErrCode\":1,\"Buffer\":10}"},
	{"nodes.frames.data", "010203040506070800077061796c6f6164", "{\"Nonce\":\"102030405060708\",\"Payload\":\"cGF5bG9hZA==\"}"},
	{"nodes.frames.relay_query", "0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c010203040506070800003039000568656c6c6f", "{\"CallerID\":\"0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c\",\"TargetID\":\"0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c\",\"Query\":{\"Nonce\":\"102030405060708\",\"Buffer\":12345,\"Query\":\"hello\"}}"},
	{"mod.nearby.public_profile", "010282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c056e6f646531", "{\"NodeAlias\":\"node1\",\"NodeID\":\"0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c\"}"},
	{"mod.nearby.stealth_hint", "0000000301aa01bb01cc00000002010101020102030405060708", "{\"Commitment\":[170,187,204],\"MaskedID\":[1,2],\"Nonce\":\"102030405060708\"}"},
	{"astrald.mod.archives.events.archive_indexed", "0100318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c63180100000001010100318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c6318c631800000005612e747874000000016317979cfe362a00000000000b7a697020636f6d6d656e74000000037a6970", "{\"ObjectID\":\"data1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"Archive\":{\"Entries\":[{\"ObjectID\":\"data1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"Path\":\"a.txt\",\"Comment\":\"c\",\"Modified\":\"2023-11-14T22:13:20Z\"}],\"Comment\":\"zip comment\",\"Format\":\"zip\"}}"},
	{"mod.auth.sudo_action", "0102030405060708010282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c010282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c", "{\"Nonce\":\"102030405060708\",\"ActorID\":\"0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c\",\"AsID\":\"0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c\"}"},
	{"mod.shell.shell_action", "0102030405060708010282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c", "{\"Nonce\":\"102030405060708\",\"ActorID\":\"0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c\"}"},
	{"nodes.frames.ping/zero", "000000000000000000", "{\"Nonce\":\"0\",\"Pong\":false}"},
	{"nodes.frames.query/zero", "0000000000000000000000000000", "{\"Nonce\":\"0\",\"Buffer\":0,\"Query\":\"\"}"},
	{"nodes.frames.read/zero", "000000000000000000000000", "{\"Nonce\":\"0\",\"Len\":0}"},
	{"nodes.frames.response/zero", "00000000000000000000000000", "{\"Nonce\":\"0\",\"ErrCode\":0,\"Buffer\":0}"},
	{"nodes.frames.data/zero", "00000000000000000000", "{\"Nonce\":\"0\",\"Payload\":null}"},
	{"mod.nearby.public_profile/zero", "0000", "{\"NodeAlias\":\"\",\"NodeID\":null}"},
	{"mod.nearby.stealth_hint/zero", "00000000000000000000000000000000", "{\"Commitment\":null,\"MaskedID\":null,\"Nonce\":\"0\"}"},
	{"astrald.mod.archives.events.archive_indexed/zero", "0000", "{\"ObjectID\":null,\"Archive\":null}"},
	{"mod.auth.sudo_action/zero", "00000000000000000000", "{\"Nonce\":\"0\",\"ActorID\":null,\"AsID\":null}"},
	{"mod.shell.shell_action/zero", "000000000000000000", "{\"Nonce\":\"0\",\"ActorID\":null}"},
}

func wireSamples(t *testing.T) map[string]astral.Object {
	id, err := astral.ParseIdentity("0282fee8775757cdd8fda8b220195f5b8611312cd145c5a1a3aa55df210e779b2c")
	if err != nil {
		t.Fatal(err)
	}
	oid, err := astral.ParseID("data1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	nonce := astral.Nonce(0x0102030405060708)
	action := auth.Action{Nonce: nonce, ActorID: id}

	samples := map[string]astral.Object{
		"nodes.frames.ping":         &frames.Ping{Nonce: nonce, Pong: true},
		"nodes.frames.query":        &frames.Query{Nonce: nonce, Buffer: 12345, Query: "hello"},
		"nodes.frames.read":         &frames.Read{Nonce: nonce, Len: 4096},
		"nodes.frames.response":     &frames.Response{Nonce: nonce, ErrCode: 1, Buffer: 10},
		"nodes.frames.data":         &frames.Data{Nonce: nonce, Payload: []byte("payload")},
		"nodes.frames.relay_query":  &frames.RelayQuery{CallerID: id, TargetID: id, Query: frames.Query{Nonce: nonce, Buffer: 12345, Query: "hello"}},
		"mod.nearby.public_profile": &nearby.PublicProfile{NodeID: id, NodeAlias: "node1"},
		"mod.nearby.stealth_hint":   &nearby.StealthHint{Commitment: []astral.Uint8{0xaa, 0xbb, 0xcc}, MaskedID: []astral.Uint8{0x01, 0x02}, Nonce: nonce},
		"astrald.mod.archives.events.archive_indexed": &archives.EventArchiveIndexed{ObjectID: oid, Archive: &archives.Archive{
			Entries: []*archives.Entry{{ObjectID: oid, Path: "a.txt", Comment: "c", Modified: astral.Time(time.Unix(1_700_000_000, 0).UTC())}},
			Comment: "zip comment", Format: "zip"}},
		"mod.auth.sudo_action":   &authmod.SudoAction{Action: action, AsID: id},
		"mod.shell.shell_action": &shell.ShellAction{Action: action},
	}
	for _, v := range wireVectors {
		if name, ok := strings.CutSuffix(v.name, "/zero"); ok {
			samples[v.name] = astral.New(name)
		}
	}
	return samples
}

func TestShapeRule_TypesWriteTheirVectors(t *testing.T) {
	samples := wireSamples(t)

	for _, v := range wireVectors {
		t.Run(v.name, func(t *testing.T) {
			obj := samples[v.name]
			if obj == nil {
				t.Fatalf("no sample named %s", v.name)
			}

			var buf bytes.Buffer
			if _, err := obj.WriteTo(&buf); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got := hex.EncodeToString(buf.Bytes()); got != v.hex {
				t.Errorf("binary:\n want %s\n  got %s", v.hex, got)
			}

			j, err := json.Marshal(obj)
			if err != nil {
				t.Fatalf("json: %v", err)
			}
			if string(j) != v.json {
				t.Errorf("JSON:\n want %s\n  got %s", v.json, j)
			}
		})
	}
}

// The vectors decode and re-encode unchanged, through the Go type and through the Blueprint
// alone, as a peer with no Go type decodes them.
func TestShapeRule_VectorsDecode(t *testing.T) {
	samples := wireSamples(t)
	synced := syncedRegistry(t)

	for _, v := range wireVectors {
		t.Run(v.name, func(t *testing.T) {
			typed := reflect.New(reflect.TypeOf(samples[v.name]).Elem()).Interface().(astral.Object)
			assertRoundTrip(t, "Go type", typed, v.hex)

			name, _ := strings.CutSuffix(v.name, "/zero")
			if name == "nodes.frames.relay_query" {
				// note: RelayQuery's hand-written codec writes CallerID and TargetID with no
				// presence byte, and its Blueprint describes one. It predates this change.
				return
			}
			runtime := synced.New(name)
			if _, ok := runtime.(*astral.RuntimeObject); !ok {
				t.Fatalf("want a RuntimeObject from the synced registry, got %T", runtime)
			}
			assertRoundTrip(t, "Blueprint", runtime, v.hex)
		})
	}
}

func assertRoundTrip(t *testing.T, via string, obj astral.Object, want string) {
	t.Helper()

	raw, _ := hex.DecodeString(want)
	if _, err := obj.ReadFrom(bytes.NewReader(raw)); err != nil {
		t.Fatalf("%s: read: %v", via, err)
	}

	var buf bytes.Buffer
	if _, err := obj.WriteTo(&buf); err != nil {
		t.Fatalf("%s: write: %v", via, err)
	}
	if got := hex.EncodeToString(buf.Bytes()); got != want {
		t.Errorf("%s: re-encoding:\n want %s\n  got %s", via, want, got)
	}
}

// syncedRegistry holds the wire primitives, which every peer compiles in, and replays every
// derivable Blueprint on top.
func syncedRegistry(t *testing.T) *astral.Blueprints {
	t.Helper()

	synced := astral.NewBlueprints(nil)
	for _, name := range astral.DefaultBlueprints().OrderedBlueprints() {
		if astral.IsPrimitiveType(name) {
			if err := synced.Add(astral.New(name)); err != nil {
				t.Fatalf("primitive %s: %v", name, err)
			}
		}
	}

	all, _ := astral.DefaultBlueprints().AllBlueprints()
	for _, bp := range all {
		_, _ = synced.RegisterBlueprint(bp)
	}
	return synced
}

// On a node loading every module, no struct type fails Blueprint derivation on a field shape,
// except routing.op_spec, whose parameter entry has no registered type. The remaining failures
// are non-struct types with no PrimitiveAlias, a separate cause.
func TestShapeRule_NoStructFieldFailsDerivation(t *testing.T) {
	_, err := astral.DefaultBlueprints().AllBlueprints()
	if err == nil {
		return
	}

	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.Contains(line, "want struct or *struct"):
		case strings.HasPrefix(line, "blueprint routing.op_spec:"):
		default:
			t.Errorf("struct field fails derivation: %s", line)
		}
	}
}
