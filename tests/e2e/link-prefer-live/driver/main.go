// Command driver runs the link-prefer-live story and prints its facts as one
// JSON line.
//
// node2 holds two links to node1, and one of them goes dark: a TCP relay on
// the test host drops every byte while it keeps both connections open. Phase
// A darkens the older link (relay R1) beside a direct link D; phase B darkens
// the newest link (relay R2) beside D. In each phase node1 queries node2's
// user.info every 0.5 s, each query in its own goroutine, while the driver
// polls both nodes' links until the dark link closes.
//
//	driver <session.json>
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	// why: nodes.LinkInfo carries its endpoints as typed objects, and only the
	// SDK's root package registers their blueprints.
	_ "github.com/astralp2p/astral-go"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// converge bounds every wait for a link change.
const converge = 30 * time.Second

// relayQuiet is how long a relay forwards nothing before it black-holes.
// why: the syncs a new link starts are in flight for a moment; a dark link
// must carry no answer that was already on its way.
const relayQuiet = time.Second

type node struct {
	Endpoint    string `json:"endpoint"`
	Token       string `json:"token"`
	Identity    string `json:"identity"`
	HostIP      string `json:"host_ip"`
	LanEndpoint string `json:"lan_endpoint"`
}

type session struct {
	Nodes map[string]node `json:"nodes"`
}

// pair is node1 and node2 with an admin client on each.
type pair struct {
	n1, n2         node
	id2            *astral.Identity
	admin1, admin2 *astrald.Client
}

// facts is the prefer_live fact.
type facts struct {
	DirectLink string `json:"direct_link"`
	PhaseA     *phase `json:"phase_a"`
	PhaseB     *phase `json:"phase_b"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	f := &facts{}
	err := run(os.Args[1], f)
	out, _ := json.Marshal(map[string]any{"prefer_live": f})
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func loadPair(path string) (pair, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pair{}, err
	}
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		return pair{}, err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	id2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return pair{}, err
	}
	return pair{n1: n1, n2: n2, id2: id2,
		admin1: connect(n1.Endpoint, n1.Token), admin2: connect(n2.Endpoint, n2.Token)}, nil
}

func run(path string, f *facts) error {
	p, err := loadPair(path)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)
	target := strings.TrimPrefix(p.n1.LanEndpoint, "tcp:")

	r1, err := startRelay(p.n2.HostIP, target)
	if err != nil {
		return err
	}
	defer r1.Close()
	relayed, err := relayOnly(ctx, p, r1)
	if err != nil {
		return err
	}
	if !r1.waitQuiet(relayQuiet, converge) {
		return errors.New("the relayed link R1 never went quiet")
	}
	direct, err := linkBoth(ctx, p, p.n1.LanEndpoint)
	if err != nil {
		return fmt.Errorf("linking node2 to node1 directly: %w", err)
	}
	f.DirectLink = direct.String()
	if f.PhaseA, err = darken(ctx, p, r1, relayed); err != nil {
		return fmt.Errorf("phase A: %w", err)
	}

	r2, err := startRelay(p.n2.HostIP, target)
	if err != nil {
		return err
	}
	defer r2.Close()
	newest, err := linkBoth(ctx, p, "tcp:"+r2.Addr())
	if err != nil {
		return fmt.Errorf("linking node2 to node1 through R2: %w", err)
	}
	if !r2.waitQuiet(relayQuiet, converge) {
		return errors.New("the relayed link R2 never went quiet")
	}
	if f.PhaseB, err = darken(ctx, p, r2, newest); err != nil {
		return fmt.Errorf("phase B: %w", err)
	}
	return nil
}
