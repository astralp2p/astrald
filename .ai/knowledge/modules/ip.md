# mod/ip

Maintains the node's view of IP-layer reachability: local interface addresses, public IP candidates supplied by other modules, and the OS default gateway. Owns the IP query ops, provider aggregation, and network-address-change emission; the `IP` object type and `EventNetworkAddressChanged` are defined in astral-go `api/ip`.

## Dependencies

| Module | Why |
|---|---|
| `objects` | delivers `EventNetworkAddressChanged` through `Objects.Receive` when the local address set changes |
| `core.Node` | `LoadDependencies` scans loaded modules and auto-registers any `PublicIPCandidateProvider` |
| `core/assets` | loader signature dependency; this module does not read YAML |

## Flows

- Address watch: `Run` starts `watchAddresses` -> take initial non-loopback snapshot -> poll every 3s -> diff by `IP.String()` -> send `EventNetworkAddressChanged` with removed, added, and full address sets through `Objects.Receive` when changed.
- Local address query: `ip.local_addrs` -> `localAddresses(false)` -> `InterfaceAddrs` -> keep `*net.IPNet` values with non-nil IPs -> drop loopback -> stream IPs -> `EOS`.
- Public candidates query: `ip.public_ip_candidates` -> clone provider set -> call each provider's `PublicIPCandidates()` -> dedupe by IP string with first result winning -> stream IPs -> `EOS`.
- Default gateway query: `ip.default_gateway` -> choose parser by `runtime.GOOS` -> run `ip route`, `netstat -nr`, or `netsh` -> parse default route -> send the gateway IP or an `astral.Error`.
- Provider registration: `LoadDependencies` injects `objects`, then scans `core.Node.Modules().Loaded()` for modules implementing `ip.PublicIPCandidateProvider` and adds them to the provider set.
- Programmatic access: `LocalIPs()` returns the same non-loopback snapshot as the local-address query; `PublicIPCandidates()` returns the deduped provider aggregate without streaming.

## Source

- `mod/ip/module.go` - public interface (`Module`, `PublicIPCandidateProvider`, `ErrDefaultGatewayNotFound`); the `IP` object and `EventNetworkAddressChanged` types live in astral-go `api/ip`.
- `mod/ip/src/loader.go`, `mod/ip/src/deps.go` - module construction, op router setup, dependency injection, and provider auto-discovery.
- `mod/ip/src/module.go` - `Run`, `LocalIPs`, local address filtering, polling watcher, event emission, and router access.
- `mod/ip/src/net.go`, `mod/ip/src/public_ip_candidates.go`, `mod/ip/src/default_gateway.go` - interface shim, public candidate aggregation, and OS gateway parsers.
- `mod/ip/src/op_local_addrs.go`, `mod/ip/src/op_public_ip_candidates.go`, `mod/ip/src/op_default_gateway.go` - query operation handlers.
- `mod/ip/src/arp/`, `mod/ip/src/opendns/` - helper packages; current module wiring does not auto-install them as providers.
- `mod/ip/views/ip_view.go` - terminal rendering for IP values.

## Surface

| What | Why it matters |
|---|---|
| `ip.local_addrs` | streams current non-loopback local interface addresses |
| `ip.public_ip_candidates` | streams deduped public addresses from registered providers |
| `ip.default_gateway` | reports the OS default gateway or an error object |
| `ip.PublicIPCandidateProvider` | extension interface for modules that can publish public IP candidates |
| `EventNetworkAddressChanged` | local polling event consumed by modules that need to react to address changes |
| `mod.ip.ip_address` | object type used for IP values in ops, events, and endpoint objects; the Go type lives in astral-go `api/ip` (encoding specified under `.ai/system/protocols/ip/types`) |

## Invariants

- Public IP status requires both `IsGlobalUnicast()` and not `IsPrivate()`.
- Address watching polls every 3s; it does not subscribe to OS network notifications.
- `EventNetworkAddressChanged.All` contains the post-diff snapshot.
- Provider order comes from `sig.Set` clone order and should not be treated as stable.
- `ip.default_gateway` sends an `astral.Error` on failure and does not send `EOS`.
- There is no `ip.yaml`; loader does not call `LoadYAML`.
- `src/arp` and `src/opendns` are helper packages, not active module dependencies in current wiring.
