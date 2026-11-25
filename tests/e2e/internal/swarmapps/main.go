// Command swarmapps is the app side of swarm-state.suite: it registers apps,
// serves the wallet stand-in, discovers it and calls it. The Python drivers
// answer the requests these apps file (tests/lib/swarmstate.py) and judge
// nothing here: every subcommand but wallet prints one JSON line and exits 0,
// an error included in the line.
//
//	swarmapps register <endpoint> <token> [discovery]   {"identity", "token"} or {"error"}
//	swarmapps wallet   <endpoint> - <host_ip>           serves until killed, as $SWARMAPPS_TOKEN; state file {"serving"}
//	swarmapps discover <endpoint> <token> <reach>       {"providers": [...]} or {"error"}
//	swarmapps call     <endpoint> <token> <app> <amount> {"answer"} or {"error"}
//	swarmapps request  <endpoint> <token> <seconds> [discovery] {"request_id", "contract"} or {"error"}
//	swarmapps token    <endpoint> <token> <identity>    {"token"} or {"error"}
//	swarmapps entries  <endpoint> <token>               {"entries": [{"id", "entry": {Type, Object}}, ...]} or {"error"}
//	swarmapps store-key <endpoint> <token> <identity> < hex objects, one per line
//	                   {"key_object"} or {"error"}: identity's key among them, stored there
//	swarmapps approve  <endpoint> <node_token> <node_id> <request_id> [push_to...]
//	                   signs with $SWARMAPPS_NODE_KEY (hex of data/node_key);
//	                   {"approval_id", "pushed": {"local" or node: "ack" or error}} or {"error"}
//
// `discovery` asks a ServiceDiscovery permit for the service `wallet`.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func emit(v map[string]any, err error) {
	if err != nil {
		v = map[string]any{"error": err.Error()}
	}
	out, _ := json.Marshal(v)
	fmt.Println(string(out))
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: swarmapps <cmd> <endpoint> <token> ...")
		os.Exit(2)
	}
	cmd, ep, tok, rest := os.Args[1], os.Args[2], os.Args[3], os.Args[4:]
	if tok == "-" {
		// why: a long-lived app's argv is recorded (lib/apps.py), its secrets are not.
		tok = os.Getenv("SWARMAPPS_TOKEN")
	}
	ctx := astral.NewContext(nil).IncludeZone(astral.ZoneNetwork)
	c := connect(ep, tok)

	switch cmd {
	case "register":
		emit(register(ctx, c, len(rest) > 0 && rest[0] == "discovery"))
	case "wallet":
		if err := serveWallet(ctx, ep, tok, rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "wallet:", err)
			os.Exit(1)
		}
	case "discover":
		emit(discover(ctx, c, rest[0]))
	case "call":
		amount, _ := strconv.ParseInt(rest[1], 10, 64)
		emit(call(ctx, c, rest[0], amount))
	case "request":
		secs, _ := strconv.Atoi(rest[0])
		emit(request(ctx, c, time.Duration(secs)*time.Second, len(rest) > 1 && rest[1] == "discovery"))
	case "token":
		emit(createToken(ctx, c, rest[0]))
	case "entries":
		emit(listEntries(ctx, c))
	case "store-key":
		emit(storeKey(ctx, c, rest[0], os.Stdin))
	case "approve":
		emit(signApproval(ctx, c, rest[0], rest[1], rest[2:]))
	default:
		fmt.Fprintln(os.Stderr, "unknown command", cmd)
		os.Exit(2)
	}
}
