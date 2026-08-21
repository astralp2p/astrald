---
name: update-protocols
description: >-
  Generate protocol documentation packages (README, ops, types) from module
  source, staged at .ai/artifacts/protocols/ in the exact structure and
  writing style of .ai/system/protocols/, using a drift gate, batched op
  authoring, and adversarial verification. Use when a module's ops changed,
  a module has ops but no protocol docs, or to sweep the repo for drift
  against astral-docs.
---

# update-protocols

* `update-protocols` produces protocol doc packages from `mod/` source.
* `update-protocols` stages complete, drop-in protocol directories at
  `.ai/artifacts/protocols/<protocol>/` for human review and a manual move
  into astral-docs (the `.ai/system` submodule).
* `update-protocols` matches the structure and writing style of the existing
  `.ai/system/protocols/` corpus exactly. The corpus is the format spec.

## Prime directives

* Code is truth for behavior. Every documented argument, stream message,
  returned object, and field traces to a line in `mod/<name>/`.
* The corpus is truth for form. Heading order, bullet shape, example format,
  file naming, and voice copy `.ai/system/protocols/`. A generated doc is
  indistinguishable in form from a hand-written sibling.
* `.ai/system/` is read-only reference truth in a separate, human-gated repo.
  The workflow writes only under `.ai/artifacts/`.
* The workflow executes nothing: no astrald, no astral-query, no op
  invocations. All content, including examples, is synthesized from reading
  source and `.ai/system/` specs.
* A protocol package ships whole or not at all. A package that fails
  verification is removed from staging and reported.

## Style

* Prose follows `.ai/rules.md` "Documentation Style" — the minimal English
  of `.ai/system/`.
* Voice matches the corpus: declarative present tense, one fact per
  sentence, defaults and terminators stated explicitly.
* Every behavioral sentence is checkable against source.

## When to run

* A module's ops changed (`src/op_*.go` added, removed, or edited).
* A module has ops but no directory under `.ai/system/protocols/`.
* Before publishing docs to astral-docs — run drift mode first.
* On a schedule. The no-args refresh is self-gating and idempotent.

## Scope

| Action | Targets |
|---|---|
| Writes | `.ai/artifacts/protocols/<protocol>/`, `.ai/artifacts/analysis/protocols-update.md`, `.ai/artifacts/protocols-state.json` |
| Reads as truth | `mod/<name>/` source, `.ai/system/protocols/` (baseline and format spec), `.ai/system/primitive-types/`, `.ai/system/topics/*-encoding.md`, `.ai/rules.md` |
| Never touches | code, `.ai/system/`, `.ai/knowledge/`, git history |

* The worklist is source-driven: modules under `mod/` that define ops.
* A protocol documented upstream with no source in this repo (e.g. `lna`)
  is out of scope. The workflow never stages, flags, or deletes it.

## The protocol package

A staged package mirrors the astral-docs layout exactly:

```
.ai/artifacts/protocols/<protocol>/
  README.md                   # protocol overview
  ops/<protocol>.<op>.md      # one per op
  types/<full.type.name>.md   # one per module-defined type
```

* A package is complete. Docs untouched by drift are copied verbatim from
  `.ai/system/protocols/<protocol>/`; missing and drifted docs are authored.
* The human move replaces the upstream directory wholesale: delete
  `.ai/system/protocols/<protocol>/`, copy the staged directory in, review
  the submodule diff, commit in astral-docs.
* An op doc whose op no longer exists in source is absent from the package;
  the move deletes it upstream. Provenance records the removal.
* A baseline type doc with no matching source struct is kept verbatim and
  reported. Type docs also describe wire messages outside the op surface.
* Primitive types (`string8`, `error_message`, `ack`, ...) are linked by
  name, never re-documented. Only types defined by the module get a `types/`
  page.

## Format spec

The corpus defines three document shapes. The exemplars win on any
disagreement with the templates below. `workflow.js` embeds this section,
the Traversal recipe, and the Verification checklist as prompt constants
(`SPEC`, `TRAVERSAL`, `CHECKLIST`); an edit here is mirrored there.

### Op doc — `ops/<protocol>.<op>.md`

Exemplars: `crypto/ops/crypto.sign_text.md` (stream protocol),
`tree/ops/tree.set.md` (stream value), `objects/ops/objects.read.md`
(raw response, auth gating), `dir/ops/dir.resolve.md` (simple).

````
# <protocol>.<op>

<One paragraph, 1–3 sentences: what the op does; notable behavior such as
auth gating, key defaults, stream semantics.>

## Arguments

* <name> (<type>[, required]) – <Sentence. Defaults stated explicitly.>
* (stream) – <Typed objects read from the input stream, in order.>

## Returned objects

The operation returns one of:
* An `error_message` object if <failure cases>.
* A `<type>` object <for what>.
* An `eos` object terminating the stream.

## Examples

```shellsession
$ astral-query <protocol>.<op> -<arg> <value> -out json
{"Type":"<type>","Object":<...>}
```
````

Rules:

* The argument separator is the en dash `–`, never a hyphen.
* Argument names are the lowercase query names of the `op<Pascal>Args`
  fields. `required` mirrors the `query:"required"` tag.
* Argument types name astral types (`string8`, `uint64`,
  `object_id.sha256`) when the struct field is one; plain `string` for a Go
  string the corpus documents as such.
* `in` and `out` are implicit (`protocols/README.md`). New docs omit them
  from `## Arguments`. Baseline docs that list them keep them. Neither
  direction is drift.
* The `(stream)` bullet appears only when the op reads typed objects from
  the input stream.
* An op whose response is not a typed-object stream (e.g. raw payload)
  replaces the "returns one of" list with prose, as `objects.read.md` does.
* Streaming ops document the `eos` terminator.
* Examples are synthesized, never captured. The command line uses only
  documented arguments. Output lines match the returned type's documented
  text or JSON form. Stream input uses the
  `echo '<json>' | astral-query <op> -in json -out json` shape. Values are
  plausible placeholders in corpus style.

### Type doc — `types/<full.type.name>.md`

Exemplars: `crypto/types/mod.crypto.signature.md` (text form),
`nodes/types/mod.nodes.link_info.md` (rich struct).

````
# <full.type.name>

<One sentence: what the type represents.>

## Fields

* <FieldName> (<wiretype>) – <Sentence.>

<Text/JSON form note when the encoding is non-default.>

## Example

```json
{
  "Type": "<full.type.name>",
  "Object": <...>
}
```
````

Rules:

* The file name and H1 use the exact `ObjectType()` string from source.
* Field names keep their Go casing. Wire types are lowercase astral types
  (`nonce64`, `identity`, `string8`, `uint64`, `bool`, `object`).

### Protocol README — `README.md`

Exemplars: `objects/README.md` (one-liner), `auth/README.md`,
`user/README.md` (op-family walkthrough).

* H1 is the protocol name.
* One to three short paragraphs: what the protocol provides; core concepts
  with backticked type names; op families with backticked op names.
* No headings beyond the H1. No exhaustive op list. No marketing.

## Execution architecture

The unit of work is one protocol package:

```
discover → fingerprint → drift gate → author op batches →
author types+README → assemble → verify → repair → ship or drop
```

Modules run concurrently, one independent chain per module, no cross-module
barrier.

Each step runs on the cheapest model it tolerates. Discovery, assembly, and
merge run on haiku. Authoring and repair run on sonnet. The drift gate and
the verifier inherit the session model; they are the correctness backstop,
and the strong re-verify guards every sonnet repair.

A token budget directive is a hard ceiling. Below a 50k-token remaining
floor the run starts no new module gate or authoring chain; deferred
modules are logged, reported in the run result and provenance, and picked
up by the next run.

**Phase 0 — Discover.** One cheap pass: modules under `mod/` with
`src/op_*.go`, against directories under `.ai/system/protocols/`. Classify
`documented` or `undocumented`. Scoped runs skip this phase.

**Phase 0.5 — Fingerprint.** One cheap pass hashes each module's source
(`mod/<name>/**/*.go`) together with its baseline
(`.ai/system/protocols/<name>/`) and compares against
`.ai/artifacts/protocols-state.json`. A module whose fingerprint matches
its record skips the drift gate: status `clean` skips outright; status
`staged` skips while the staged package still exists — a deleted staging
directory re-gates. A refresh records fingerprints for modules that gate
clean or ship; a dropped package loses its record. Drift mode reads the
state file and writes nothing.

**Phase 1 — Per-module chain.**

1. **Drift gate.** A read-only agent compares the baseline package against
   source. Op inventory comes from the exported `Op*` methods (wire name
   per the Traversal recipe), not file names alone; the `Method*` constants
   in astral-go `api/<protocol>/module.go` confirm it when present. Each op
   and each type is classified `missing`,
   `drifted`, or `clean`; the gate also reports orphaned op docs and README
   staleness. The orphan sweep is explicit: the gate lists every baseline
   `ops/*.md` and reports each file whose op does not exist in source — an
   unreported orphan blocks the package downstream. A fully clean package
   stops here and stages nothing. An undocumented module is stale by
   definition; its gate returns the full op and type inventory.
2. **Op author batches.** One author per batch of up to 10 missing or
   drifted ops, balanced across batches, batches in parallel. The author
   prompt carries the distilled format spec; the author reads only source —
   the shared module files once per batch, then each op's file. Exemplars are consulted only
   when a template leaves the shape ambiguous. A drifted doc is edited from
   a verbatim baseline copy — stale facts replaced in place, hand-written
   prose preserved — never regenerated. Authors return citations and the
   module types the batch touches.
3. **Types+README author.** One agent per package for missing or drifted
   type docs and the README, fed the union of gate findings and op-author
   type reports. Same diff-aware rule.
4. **Assemble.** Clean baseline docs are copied verbatim into staging,
   orphaned op docs excluded. A mechanical completeness check follows:
   every op has its doc, every referenced module type has its doc, the
   README exists.
5. **Verify.** Independent and adversarial. Behavior and form checks read
   only the authored files; copied baseline docs are never re-read. Copies
   are checked with one `diff -rq` against the baseline — a differing or
   extra file outside the authored list blocks. A baseline-only op doc is
   checked against source: an op present in source means a missing copy
   (block); an op absent from source means an orphaned baseline doc — its
   absence from staging is correct and is reported as a warning for
   provenance, not a block. Completeness is an `ls`-level check against
   the gate inventory. Blocking issues trigger one repair round and a
   re-verify scoped to those issues. The repair never copies a baseline
   doc whose op lacks a source implementation into staging; orphans are
   excluded, not staged.

**Phase 2 — Merge.** Remove staging for packages that failed verification.
Append the provenance section. Report.

The author and the verifier are separate agents. A self-graded package is
the main way a fabricated argument survives.

## Traversal recipe

Read in this order per op. Stop once the behavior visible on the wire is
covered — internals stay out of protocol docs.

1. `mod/<name>/module.go` — the public `Module` interface, and `ModuleName`
   for the modules that still define it. The op-name constants (the `Method*`
   set, and `ModuleName` for the modules that moved it) live in astral-go
   `api/<protocol>/module.go`; read them there. The wire name of an op is
   `<ModuleName>.<snake_case of the Op* method name without the prefix>`
   (astral-go `lib/routing/op_router.go` `AddStructPrefix`, imported as
   `github.com/astralp2p/astral-go/lib/routing`). A `Method*` constant
   states the wire name explicitly when present; some modules define none.
2. `src/op_<name>.go` — the unit of truth for one op:
   * `op<Pascal>Args`: fields → arguments; `query:"required"` → required;
     `In`/`Out` fields → implicit encoding params; defaults from code.
   * the body: `q.Accept`/`q.Reject` → acceptance and rejection conditions;
     channel reads → the `(stream)` bullet; every `ch.Send(...)` → a
     returned object (`astral.Err` → `error_message`, `&astral.Ack{}` →
     `ack`, `&astral.EOS{}` → `eos` terminator); raw writes → prose.
3. astral-go `api/<name>/` (out-of-repo) — the wire-type structs with
   `ObjectType()` that the op accepts or returns; their fields and astral
   field types. A few node-only object types remain in the astrald module
   root package (`mod/<name>/*.go`); read those there.
4. astral-go `api/<name>/client/` (out-of-repo) — confirms op names and
   signatures only; never documented.
5. `.ai/system/primitive-types/` and `topics/{json,text,binary}-encoding.md` —
   the vocabulary for argument types, field types, and example forms.

An op gated by an auth action (e.g. a `mod.auth.action` subtype) names the
action type in its intro sentence, as `objects.read.md` does.

## Drift mode

* Drift mode runs Phase 0, the fingerprint pass, and the drift gate only,
  then reports `module → op/type findings`.
* Drift mode writes nothing, including the state file.
* Refresh mode is the same gate plus authoring. A refresh where nothing
  drifted costs only the gate; a refresh where nothing changed since the
  last run costs only the discovery and fingerprint passes.

## Verification checklist

A package ships only if every check holds against current source and the
corpus. Form deviations from the format spec are blocking; prose-level style
issues are warnings.

Behavior — against `mod/<name>/`:

* Every documented argument exists in the args struct with the documented
  name, required flag, and default. No struct query field is undocumented
  (except implicit `in`/`out`).
* The implicit `in`/`out` parameters are never an issue at any severity —
  listed or omitted, in any doc.
* The `(stream)` bullet matches the objects the op body actually reads.
* Every returned-objects bullet traces to a send or reject path. Every send
  path is documented, including the `eos` terminator.
* Every type doc field matches the Go struct: name, astral type, order.
  Text/JSON form claims match the type's encoding.
* Auth gating claims trace to code.

Form — against the corpus:

* File names: `ops/<protocol>.<op>.md` matches the op's wire name;
  `types/<name>.md` matches `ObjectType()` exactly.
* Heading set and order match the format spec. No extra headings, no empty
  sections, no filler.
* Argument bullets use `* name (type[, required]) – ...` with the en dash.
* Typed responses use the "The operation returns one of:" lead-in; raw
  responses use prose.
* Examples are ```shellsession blocks; commands use only documented
  arguments; outputs match documented forms; the JSON envelope is
  `{"Type":...,"Object":...}`.
* No content re-documents `primitive-types/` or restates `topics/`.
* Prose follows Documentation Style: declarative, one fact per sentence,
  no hedging, no meta-commentary.

Completeness — against the package:

* Every source op has exactly one staged op doc; no orphan op docs.
* A baseline doc whose op has no source implementation is an orphan:
  correct handling is its absence from staging — never staged, and its
  absence is never a gap or block.
* Every module type referenced by a staged op doc has a staged type doc.
* `README.md` exists.
* Verbatim copies are byte-identical to baseline.

## Provenance and review

* All output stays in the working tree under `.ai/artifacts/`. No commits.
* Each run appends a dated section to
  `.ai/artifacts/analysis/protocols-update.md` recording: packages staged
  with authored/copied/removed counts, citations relied on, packages
  dropped after failed verification, and findings left for a human.
* A human reviews the staged packages against source, replaces the upstream
  directories in the `.ai/system` submodule, and commits in astral-docs.

## Drift traps

* Fabricated arguments — an argument with no struct field. The verifier
  fails any claim it cannot ground in source.
* Example/doc skew — an example flag missing from `## Arguments`, or output
  not matching the returned type's documented form.
* Captured-looking examples — examples are synthesized; an example never
  claims node-specific state that source cannot support.
* Re-documenting primitive types — `string8` belongs to `primitive-types/`;
  link by name, never restate.
* Blind regeneration — clobbering hand-written baseline prose on a drifted
  doc. Updates are diff-aware.
* Hyphen for en dash — argument and field bullets use `–`.
* `in`/`out` noise — implicit parameters; never added to new docs, never
  flagged in baseline docs.
* Doc-name skew — op doc names come from the routing wire name
  (`<ModuleName>.<snake_case Op* method>`) and type doc names from
  `ObjectType()`, never guessed from file names.
* Auto-orphaned type docs — a baseline type doc with no matching struct is
  kept and reported, never silently dropped.
* Orphan re-staging — "repairing" a baseline-only orphaned op doc by
  copying it into staging. An orphan's absence from staging is the correct
  state; the repair deletes a staged orphan, never creates one.
* Upstream-only protocols — no `mod/<name>/` source (e.g. `lna`) means the
  protocol is never staged, flagged, or deleted.
* Self-graded packages — the verifier is a different agent than the author.

## Files in this skill

* `SKILL.md` — this playbook.
* `workflow.js` — the Workflow engine for the drift-gated pipeline. `args`:
  a module name or list scopes the run; `{ mode: "drift" }` reports only;
  no args sweeps every module with ops. The no-args form is safe to
  schedule. Prompt constants `SPEC`, `TRAVERSAL`, and `CHECKLIST` mirror
  the Format spec, Traversal recipe, Verification checklist, and the
  Documentation Style of `.ai/rules.md`; an edit to those sections is
  mirrored there.
* `command.md` — the `/update-protocols` slash command, symlinked from
  `.claude/commands/`.
