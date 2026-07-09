# mod/log

Centralizes node logging by filtering astral-go `astral/log` output, writing log entries to a per-run file, exposing live log streaming, and registering terminal renderers for common astral types. Owns log-level config, the file sink, the `log.listen` query op, and view setup for identities, queries, and entries.

## Dependencies

| Module | Why |
|---|---|
| astral-go `astral/log.Logger` | installs `LogEntryFilter`, adds the file sink, and adds or removes per-listen forwarders |
| `dir` | supplies the identity display-name resolver used by log views |
| `tree` | binds `Config` at `/mod/log/config` so log level can be read dynamically |
| `astral.Node` | supplies the local identity for highlighted identity views and `views.HideOrigin` |

## Flows

- Load setup: `Load` installs op handlers -> sets identity view factory with local identity highlighting -> `log.SetFilter(mod.LogEntryFilter)` -> creates a log file sink when possible -> enables query and entry views -> hides the local origin in entry rendering.
- Config binding: `LoadDependencies` injects `dir` and `tree`, binds `Config` to `/mod/log/config`, and stores the directory resolver in `views.IdentityResolver`.
- Level gate: logger calls `LogEntryFilter` -> read `Config.Level` -> default to `DefaultLogLevel=2` when unset -> allow entries with `entry.Level <= level`.
- File sink: `CreateLogFile` creates `~/.config/astrald/logs/astrald.log.<timestamp>` when the home directory is available -> `LogFile.LogEntry` serializes writes through a mutex and sends entries through a channel wrapper.
- Live stream: `log.listen` accepts the raw query stream -> creates a channel with optional input and output formats -> adds `logForwarder` -> waits until the client closes by reading from the channel -> deferred removal stops forwarding.
- Rendering: package init and loader-installed view functions render primitives, identities, queries, and entries with astral-go `astral/log/theme` colors; identity rendering falls back to fingerprint when no resolver is available.
- External-type rendering: blueprint-backed `astral.RuntimeObject` values have no compile-time view; `RuntimeObjectView` registers via `fmt.SetFallbackView` and renders `Type{Field: value, ...}` (struct) or `Type(value)` (alias), delegating each field to `fmt.ViewFor`. Runtime container carriers (`RuntimeSlice`/`RuntimeArray`/`RuntimeMap`) register per-type under their constant `ObjectType` (`slice`/`array`/`map`). Replaces the prior raw `%v` Go dump. See issue #337.

## Source

- `mod/log/module.go` - public module name and package-level color setup.
- `mod/log/src/loader.go` - op registration, logger filter installation, file sink creation, and view activation.
- `mod/log/src/module.go`, `mod/log/src/config.go`, `mod/log/src/deps.go` - module state, dynamic log-level filter, config binding, and dependency wiring.
- `mod/log/src/log_file.go` - per-run log file creation and serialized entry writes.
- `mod/log/src/op_listen.go` - `log.listen` handler and live forwarding logger.
- `mod/log/views/` - terminal renderers and opt-in view registration functions; `runtime_object_view.go` (external-type fallback) and `runtime_{slice,array,map}_view.go` (container carriers).
- Style and theme helpers now live in astral-go `astral/log/{styles,theme}` (reusable color, gradient, and style helpers), imported by `mod/log/views`.

## Surface

| What | Why it matters |
|---|---|
| `log.listen` | streams live `log.Entry` objects until the caller disconnects |
| `LogEntryFilter` | gates normal logger output by dynamic config level |
| `LogFile` | writes every entry it receives to the per-run file sink |
| `/mod/log/config` | tree-bound config path for `Config.Level` |
| `views.IdentityResolver`, `views.HideOrigin` | controls how identities and local-origin log entries render |

## Invariants

- `Run` only waits for context cancellation; long-lived behavior is installed during load and dependency wiring.
- File sink creation failure is logged but does not fail module load.
- `LogFile` has no rotation and no close path in current source.
- Logger forwarders added by `log.listen` are removed when the caller disconnects.
- `IdentityView` falls back to `Identity.Fingerprint()` when no directory resolver is available.
- `views.HideOrigin` is set to the node identity during load, so matching entries render without an origin prefix.
