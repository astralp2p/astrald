# Rules

* Every change in `astrald` follows these rules.
* `.ai/AGENTS.md` names where each kind of fact lives. These rules name how work is done.

## Work

* An agent reads the relevant code before it edits.
* A change stays within the scope of its task.
* An agent preserves the user's changes and never reverts unrelated work.
* An agent reports every conflict between code, docs, and `.ai/` text.
* A code change is verified by focused tests or checks.

## Context

* `.ai/AGENTS.md` and this file load into every session. Both stay short.
* A fact lives in exactly one file. A rule is never repeated in a second file.
* Stale `.ai/` text is corrected when it is found. The correction replaces the stale text and adds no exception.

## Design

* A design starts from data, invariants, and state transitions.
* Explicit state is preferred over hidden control flow.
* A change removes special cases instead of layering branches.
* The Go standard library is the first choice.
* An abstraction is added on the third use, and only for the same algorithm or data flow.

## Code shape

* A function has one responsibility, at most 50 lines, and at most 4 parameters.
* A function with 3 or more related return values returns a named struct.
* A package holds one concept. `util`, `common`, and `helpers` are never package names.
* An interface lives at its consumer. An interface has 1 method by preference; an interface with 3 or more methods is suspect.
* A struct is flat. `nil` is the sentinel value.
* 2 options are 2 parameters. An options struct is reserved for the rare case of 5 or more options.

## Code style

* A name uses a precise verb, e.g. `delete`, `find`, `create`.
* A log call formats values with `%v`.
* The log levels are `Log` (level 0), `Logv(1)` (verbose), and `Logv(2)` (debug).

## Code comments

* A comment is written in the same edit as the code it describes.
* A comment carries one of four tags, written in lowercase:
  * `// todo:` — deferred work.
  * `// fixme:` — a shipped gap. The comment names the skipped invariant.
  * `// note:` — a clarification, or a thing to watch for.
  * `// why:` — the reason for a non-obvious decision, not the alternative.
* A comment states intent, not mechanics, with one fact per line.
* A comment never restates the code (`i++ // increment i`). A tag is never empty.
* A non-obvious decision carries a `// why:` tag and is never buried in prose.
* A tag stays in sync with the code. A `todo` or `fixme` is removed when it is resolved.
* A change never comments, reformats, or re-tags code outside its scope.
* A comment links to the spec or the source for context held elsewhere and never copies that context inline.

## Documentation style

* `.ai/` text uses the minimal English of `.ai/system/`:
  * Declarative present tense, with one fact per sentence or bullet.
  * No motivation, hype, hedging, or meta-commentary.
  * The subject is repeated. Pronouns never chain across sentences.
  * Code identifiers and defined terms are in backticks.
  * Defaults, limits, and terminators are stated explicitly.
  * A list uses `*` bullets. A table is reserved for a small, compact vocabulary.

## Project APIs

* A `WriteTo` or `ReadFrom` method delegates to `astral.Objectify`.
* `astral.Objectify` handles sized Go kinds through reflection. `astral.Objectify` rejects only the platform-width `int` and `uint`, so a field uses a sized type such as `int64` or `uint64`.
* `astral.Adapt(v)` wraps a native Go value into an astral `Object`. A hand-rolled type switch never replaces it.
* When the spec dictates a narrower width than `astral.Adapt` picks, the code dispatches on the spec first.
* `astral.Adapt` and its native-type mapping live in the astral-go `astral` package.
* `objects.Load` (`mod/objects`) reads, decodes, and type-asserts an object from a `Repository`.
* A write goes through `Repository.Create` and the returned `objects.Writer`, never through a raw `WriteTo`.
* `core.Inject(node, &mod.Deps)` injects a module's dependencies in `LoadDependencies`.
* `sig.Map`, `sig.Set`, and `sig.Queue` are preferred over a mutex guarding a map or a slice.
* `sig.RecvErr`, `sig.Recv`, and `sig.Send` are the context-aware channel operations.

## Domain invariants

* Every repository writer ends with `Commit()` or `Discard()`.
* A module never accesses another module during `Load`.
* A `Zone` only narrows. A hop never expands it.
* Network work is preceded by a `ctx.Zone().Is(astral.ZoneNetwork)` check.
* The default context is `Device|Virtual`. The original caller adds `Network`.
* `query.Reject()` is terminal.
* `query.RouteNotFound(r, ...)` is not terminal.
* `RouteQuery` never returns `nil, nil`.
* A streaming op ends with `ch.Send(&astral.EOS{})`.
* A stream error is sent with `ch.Send(astral.Err(err))`.

## Concurrency

* A mutex field is named `mu` and is never embedded.
* `defer Unlock()` sits on the same line as `Lock()`.
* `sync.RWMutex` is used when reads dominate.
* An atomic flag is an `atomic.Bool`, an atomic state is an `atomic.Int32`, and an atomic counter is an `atomic.Uint64`.
* An idempotent close uses `CompareAndSwap(false, true)`.
* `sync.Once` is never used. `atomic.Bool.CompareAndSwap` replaces it.
* `sync.Cond` is reserved for computed blocking conditions. `.Wait()` runs inside a `for` loop.
* A simple done or ready signal is a channel.
* `wg.Add(1)` runs before `go`. `defer wg.Done()` is the first statement of the goroutine.
* A `sync.WaitGroup` is a local variable, never a struct field.
* Done is signaled by `close()`, never by a send. The exposed type is `<-chan struct{}`.
* `sig.Sig` is the canonical read-only signal. `sig.New()` returns a signal with a buffer of 1.
* An error channel has a buffer at least as large as its number of senders.
* `<-ctx.Done()` appears only in a `select` that has another case.
