# Zone

`Zone` is a bitmask over Device, Virtual, and Network scopes. The type, its
bit values, the defaults, and the context helpers are provided by astral-go —
see astral-go .ai/knowledge/concepts/zone.md and the spec
[core-definitions/zone](../../system/core-definitions/zone.md).

## Resources

| Zone    | Scope            | Resources                     |
|---------|------------------|-------------------------------|
| Device  | local hardware   | memory repos, disk repos      |
| Virtual | computed/derived | archives, encryption wrappers |
| Network | remote peers     | nodes routing, gateway        |

## Enforcement

* Apphost calls `ExcludeZone(Network)` for unauthenticated guests.
* Unauthenticated means no token or an expired token; mapped to `Anyone`.
