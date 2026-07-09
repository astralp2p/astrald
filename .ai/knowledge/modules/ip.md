# mod/ip

Maintains the node's view of IP-layer reachability: local interface addresses, public IP candidates supplied by other modules, and the OS default gateway. Owns the IP query ops, provider aggregation, and network-address-change emission.

## Dependencies

| Module | Why |
|---|---|
| `objects` | delivers `EventNetworkAddressChanged` through `Objects.Receive` when the local address set changes |
| `core.Node` | `LoadDependencies` scans loaded modules and auto-registers any `PublicIPCandidateProvider` |

## Invariants

* Public IP status requires both `IsGlobalUnicast()` and not `IsPrivate()`.
* The address watch polls every 3s; it does not subscribe to OS network notifications.
* `EventNetworkAddressChanged.All` is the post-diff full snapshot; consumers rely on it.
* Provider order comes from the `sig.Set` clone and is not stable.
* `ip.default_gateway` sends `astral.Error` on failure and does not send `EOS`; other streaming ops end with `EOS`.
* `PublicIPCandidateProvider` registration is automatic: `LoadDependencies` scans loaded modules for the interface.
