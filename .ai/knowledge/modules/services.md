# mod/services

Aggregates service advertisements from registered discoverers into a local stream and can mirror another node's service stream into a cached database. Owns discoverer registration, `services.discover` fan-in, `services.sync` remote cache refresh, and the `services__services` table.

## Dependencies

| Module | Why |
|---|---|
| `dir` | `OpSync` resolves the requested provider identity before opening the remote stream |
| `core/assets` | creates the database handle and migrates `services__services` |
| `core.Node` | `LoadDependencies` scans loaded modules and auto-registers `services.Discoverer` implementations |

## Invariants

* `services.discover` streams a snapshot, then a nil separator, then live updates; the nil is a separator, not a service value.
* `syncServices` deletes all cached rows for a provider before applying the remote stream (destructive refresh).
* `OpSync` runs remote discovery under `ZoneNetwork` and cancels when the accepted channel receives any object.
* `services__services` uniqueness is (service name, provider identity).
* A discoverer that errors is logged at verbosity 2 and skipped; one bad source does not fail the aggregate.
* `services.Discoverer` is the extension point for modules that advertise availability (`nat` is a consumer).
* `LoadDependencies` never registers the services module as its own discoverer.
