# Zone

Daemon-side zone enforcement. The `Zone` bitmask, its bit values, and defaults
live in astral-go; the group-to-zone mapping lives in `concepts/objects.md`.

* Apphost calls `ExcludeZone(Network)` for unauthenticated guests.
* Unauthenticated means no token or an expired token; mapped to `Anyone`.
