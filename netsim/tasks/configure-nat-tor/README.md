# configure-nat-tor

Relocates a NAT'd node's **Tor into the same network namespace as its astrald** — the
piece `enter-nat` can't do, needed before the pair can re-link (and punch) over Tor.

`enter-nat` moves astrald into netns `priv`, so its `127.0.0.1` becomes the *netns*
loopback. astrald's `tor` module needs Tor at `127.0.0.1:9050`/`:9051`, and — the part a
config knob can't fix — its onion service's **local listener is hardcoded `127.0.0.1:0`**
(`mod/tor/src/server.go`), which Tor dials on **inbound** onion connections. So a root-ns
Tor can neither be reached nor deliver inbound onion to a netns'd astrald; **Tor must live
in the netns too.**

On each `--vm` (default `node1 node2`), run **after** `enable-tor` and `enter-nat`:

1. **WAN masquerade** for `192.168.99.0/24` out the default-route (slirp WAN) NIC, so
   Tor-in-netns can reach the real Tor network. Routing splits by destination:
   `198.51.100.0/24` (peers) → `lan0` via `enter-nat`'s SNAT; internet (Tor) → WAN.
2. **Move `tor@default.service` into netns `priv`** via a `NetworkNamespacePath` systemd
   drop-in (same idiom `enter-nat` uses for astrald; net ns only, so `torrc` is untouched).
3. Restart **Tor first** (binds netns `127.0.0.1:9050/9051`), then **astrald** (its tor
   module connects to the control port once at start, no retry).

Self-validating: waits for the control port inside the netns, then confirms astrald
**re-publishes its onion** — the end-to-end proof that bootstrap (via the WAN masquerade),
control, and the netns-local onion listener all work. **No astrald source/config change.**
Host-driven. Used by the NAT-punch story after `enable-tor` + `enter-nat`.
