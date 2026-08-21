# Module Guide Recipe

One file per module at `.ai/knowledge/modules/<name>.md`. Load it when editing
that module's source. Do not load all module guides by default.

A module guide keeps the context an agent needs to change the module *safely*:
its ownership boundary, the cross-module contracts it honors, and the
invariants that are easy to miss from a single function signature. It is not an
API reference, a changelog, a design essay, or a file inventory. Code is the
source of truth; the spec (`.ai/system/`) owns protocol meaning; astral-go owns
wire types, op-name constants, and clients. The guide records only what none of
those self-document.

## Shape

Keep this order and these headings so guides scan predictably. Omit any section
that would be empty.

1. `# mod/<name>`
2. Description — 1-3 sentences.
3. `## Dependencies` — table `Module | Why`.
4. `## Invariants` — the core (below).
5. `## Flows` — optional; ordering and state machines only.

Do not write a `## Source` file inventory (the reader opens `mod/<name>/`) or a
`## Surface` op table (the spec and a live `<target>:.spec` carry the op names).

## Description

Name the module's ownership boundary in one sentence: what state or capability
it owns, and what the rest of the node relies on it to do. Add a second
sentence only if what breaks when it is disabled is non-obvious. No op lists, no
lifecycle boilerplate (`Load`, `LoadDependencies`, `Run`).

## Dependencies

Table `Module | Why`. Each `Why` names the concrete call, interface, data, or
lifecycle hook — `objects | Store signed contracts; ReadDefault backs HTTP
reads`, never "used by module". Mark optional dependencies `(opt)`.

## Invariants

The highest-value section. Each bullet should prevent a bug an edit could
introduce:

* State-machine transitions and the CAS rules that guard them.
* Idempotency and ownership rules; who consumes what, and when.
* Required stream terminators and boundary markers.
* Config values with cross-cutting effects.
* Concurrency assumptions (what serializes, what may race).
* Persistence and conflict behavior.
* Cross-module contracts another module relies on.

Do not restate a global rule from `.ai/rules.md` unless this module gives it a
specific consequence. Trace every bullet to a line that enforces it.

## Flows

Optional, and only where ordering is non-obvious: a state machine, a migration,
a credit or backpressure loop, a retry or cleanup path spread across files. One
bullet per flow — a human label, then the `->` sequence. Skip the pure
call-path narration the code already self-documents.

## Style

Follow `.ai/rules.md` "Documentation Style" (the minimal English of
`.ai/system/`). `*` bullets. Backtick code identifiers. Replace stale text
instead of appending exceptions. Code wins: if the guide disagrees with code,
fix the guide or flag the conflict before relying on it.
