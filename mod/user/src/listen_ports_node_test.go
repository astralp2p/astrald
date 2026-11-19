package user_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	apphostclient "github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astrald/core"
	_ "github.com/astralp2p/astrald/mod/all"
	usersrc "github.com/astralp2p/astrald/mod/user/src"
	"github.com/astralp2p/astrald/resources"
)

// note: these tests run one node in the process, wired by the module manager as astrald wires it, so the
// user module finds the tcp and kcp modules by their registered names.

// listenPortsModules is the smallest module set that loads the user module, without the transports.
const listenPortsModules = "apphost, auth, crypto, dir, ether, events, exonet, fs, ip, nearby, nodes, objects, " +
	"scheduler, secp256k1, shell, sys, tree, user, vfs"

// listenPortsWait bounds the wait for the node to run.
const listenPortsWait = 10 * time.Second

// listenPortsListeners numbers the in-memory apphost listeners, which share one namespace in a process.
var listenPortsListeners atomic.Uint64

// statusRecorder records the attachments of a composed status.
type statusRecorder struct{ objects []astral.Object }

func (r *statusRecorder) Receiver() *astral.Identity { return nil }

func (r *statusRecorder) Attach(o astral.Object) error {
	r.objects = append(r.objects, o)
	return nil
}

func TestNodeStatusCarriesTheTransportListenPorts(t *testing.T) {
	cases := []struct {
		name    string
		modules string
		configs map[string]string
		want    *nearby.ListenPorts // nil: the status holds no ListenPorts
	}{
		{
			name:    "custom ports",
			modules: listenPortsModules + ", tcp, kcp",
			configs: map[string]string{
				"tcp.yaml": "listen: false\nlisten_port: 18001\n",
				"kcp.yaml": "listen: false\nlisten_port: 18002\n",
			},
			want: &nearby.ListenPorts{TCP: 18001, KCP: 18002},
		},
		{
			name:    "default ports",
			modules: listenPortsModules + ", tcp, kcp",
			configs: map[string]string{"tcp.yaml": "listen: false\n", "kcp.yaml": "listen: false\n"},
		},
		{
			name:    "no transports",
			modules: listenPortsModules,
			want:    &nearby.ListenPorts{TCP: 0, KCP: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := runListenPortsNode(t, tc.modules, tc.configs)

			r := &statusRecorder{}
			mod.ComposeStatus(r)

			var got []*nearby.ListenPorts
			for _, o := range r.objects {
				if p, ok := o.(*nearby.ListenPorts); ok {
					got = append(got, p)
				}
			}

			switch {
			case tc.want == nil && len(got) != 0:
				t.Fatalf("ListenPorts = %+v; want none", *got[0])
			case tc.want != nil && len(got) != 1:
				t.Fatalf("got %d ListenPorts; want 1 %+v", len(got), *tc.want)
			case tc.want != nil && *got[0] != *tc.want:
				t.Fatalf("ListenPorts = %+v; want %+v", *got[0], *tc.want)
			}
		})
	}
}

// runListenPortsNode runs an unclaimed node with the modules and config files and returns its user module once
// apphost accepts a session, when every module has loaded its dependencies. The node stops when the test ends.
func runListenPortsNode(t *testing.T, modules string, configs map[string]string) *usersrc.Module {
	t.Helper()

	root := t.TempDir()
	res := listenPortsResources(t, root)

	key := secp256k1.New()
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, key, astral.Canonical()); err != nil {
		t.Fatalf("encode node key: %v", err)
	}
	if err := res.WriteData("node_key", buf.Bytes()); err != nil {
		t.Fatalf("write node key: %v", err)
	}

	endpoint := fmt.Sprintf("memu:user-listen-ports-%d", listenPortsListeners.Add(1))
	files := map[string]string{
		"node.yaml":    "modules: [" + modules + "]\n",
		"apphost.yaml": fmt.Sprintf("listen: [%q]\nbind_http: \"\"\n", endpoint),
		"ether.yaml":   "listen: false\n",
	}
	for name, body := range configs {
		files[name] = body
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatalf("write %v: %v", name, err)
		}
	}

	node, err := core.NewNode(secp256k1.Identity(secp256k1.PublicKey(key)), res)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := make(chan error, 1)
	go func() { done <- node.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitListenPortsNode(t, ctx, endpoint, done)

	mod, ok := node.Modules().Find("user").(*usersrc.Module)
	if !ok {
		t.Fatal("the node has no user module")
	}

	return mod
}

// waitListenPortsNode waits until apphost accepts a session.
// why apphost: modules run only after every module loaded its dependencies, so the transports are set.
func waitListenPortsNode(t *testing.T, ctx *astral.Context, endpoint string, done <-chan error) {
	t.Helper()

	deadline := time.Now().Add(listenPortsWait)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("the node ended: %v", err)
		default:
		}

		host, err := apphostclient.Connect(ctx, endpoint)
		if err == nil {
			host.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("the node did not run")
}

// listenPortsResources returns the resources of a node: the config in root, the data in root/data.
func listenPortsResources(t *testing.T, root string) *resources.FileResources {
	t.Helper()

	res, err := resources.NewFileResources(root, false)
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	dataRoot := filepath.Join(root, "data")
	if err := os.MkdirAll(dataRoot, 0700); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := res.SetDataRoot(dataRoot); err != nil {
		t.Fatalf("data root: %v", err)
	}

	return res
}
