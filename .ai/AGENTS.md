# astrald

* `astrald` is the reference `Node` daemon of the Astral Network, written in Go.
* `go.mod` sets the minimum Go version, `1.25.0`.
* This file is the entry point for coding agents. The root `AGENTS.md` is a symlink to this file. The root `CLAUDE.md` imports this file and holds nothing else.
* Agent knowledge lives in `.ai/`: this file and `.ai/rules.md`. `.ai/` holds no curated knowledge or pattern notes.

## Sources

* `.ai/system/` is the specification: every wire, protocol, and domain fact. `.ai/system/` is the astral-docs repository, pinned as a git submodule. Its index is `.ai/system/README.md`.
* `git submodule update --init .ai/system` fills `.ai/system/` in a fresh clone or worktree. An empty `.ai/system/` is an uninitialized submodule.
* The source is the implementation truth. A daemon fact lives in the source that implements it. A module's internals live under `mod/<name>/`.
* A non-obvious decision lives in the source as a `// why:` comment. A known gap lives in the source as a `// fixme:` or `// todo:` comment.
* Primitives, wire types (`api/`), and client libraries live in astral-go (`github.com/astralp2p/astral-go`). `.ai/` text cites them by package and never restates them.

## Authority

* When two sources conflict, the higher source wins, and the agent reports the conflict:
  1. The user's instruction.
  2. Code and tests.
  3. The specification in `.ai/system/`.
  4. `.ai/rules.md`.

## Layout

* `brontide/` implements the `Noise_XK_secp256k1_ChaChaPoly_SHA256` handshake.
* `core/` holds the `Node`, the module manager, and the router.
* `mod/` holds the pluggable modules, one directory per module.
* `lib/` holds `aliasgen`, `apphost-js`, `arl`, and `paths`.
* `cmd/` holds the binaries: `astrald` and the `astral-*` tools.
* `mobile/` is the gomobile-bind entry point for Android and iOS hosts.
* `tests/` holds the integration tests. `tests/README.md` describes them.

## Configuration

* The config directory is `astrald/` under Go's `os.UserConfigDir()`: `$XDG_CONFIG_HOME/astrald/` or `$HOME/.config/astrald/` on Linux, and `~/Library/Application Support/astrald/` on macOS.
* `astrald -root <dir>` moves the config to `<dir>/config` and the data to `<dir>/data`.
* The node config file is `node.yaml`. A module reads its config from `<name>.yaml` in the same directory.

## Checks

* `go build ./...` builds every package. `go test ./...` runs the unit tests.
* `./tests/run` runs the default integration suite, `tests/suites/main.suite`, against local daemons.
* `scripts/check-astral-blueprints.sh` checks that every Go type with an `ObjectType()` method is registered through `astral.Add`. The pre-commit hook in `.githooks/` runs it.

## Rules

* `.ai/rules.md` holds the rules for every change: @rules.md
