---
name: update-knowledge
description: >-
  Analyze module/concept source and bring .ai/knowledge notes plus the knowledge
  index back into agreement with the code, using a drift gate, parallel
  author/verifier chains, and adversarial verification. Use when a module's
  source changed, a module was added or removed, or to sweep the whole repo for
  documentation drift.
---

# update-knowledge

* `update-knowledge` keeps `.ai/knowledge/` in agreement with the code.
* `update-knowledge` reads module source, rewrites drifted notes, curates the
  index at `.ai/knowledge/README.md`, and verifies every claim against source.
* `update-knowledge` leaves all changes in the working tree for human review.

## Prime directives

* Code is truth. A note that disagrees with code is wrong. Fix the note or
  flag the conflict; never write around it.
* `.ai/system/` is read-only reference truth in a separate, human-gated repo.
  Link to it; never write to it; never restate its protocol, wire, or domain
  facts inside a knowledge note.
* A note is an orientation page. A note is not an API reference, a changelog,
  or an essay.
* The workflow never commits to the default branch. Default mode leaves all
  changes in the working tree. PR mode commits to a dedicated docs branch and
  opens a pull request. The human gate is review — of the tree or of the PR.

## Style

* All outputs — notes, index rows, drift findings, provenance — follow
  `.ai/rules.md` "Documentation Style" (the minimal English of `.ai/system/`).
* Every sentence is checkable against source.

## When to run

* A module's source changed and its note may be stale.
* A module was added (no note yet) or removed (orphaned note).
* Before relying on a note for a risky edit — run drift mode first.
* On a schedule. The no-args refresh is self-gating and idempotent.

## Scope

| Action | Targets |
|---|---|
| Writes | `.ai/knowledge/modules/<name>.md`, `.ai/knowledge/concepts/<name>.md`, `.ai/knowledge/README.md` (the index) |
| Reads as truth | `mod/<name>/` source, `.ai/system/`, `.ai/rules.md` |
| Reads to calibrate | `.ai/knowledge/modules/README.md`, `.ai/knowledge/concepts/README.md`, `.ai/patterns/` |
| Never touches | code, `.ai/system/`, `.ai/rules.md`, the two recipe READMEs, git history |

## The knowledge directory

* `modules/<name>.md` — one file per `mod/<name>/`. Format is governed by the
  Module Guide Recipe at `.ai/knowledge/modules/README.md`.
* `concepts/<name>.md` — one file per cross-module idea. Style is governed by
  `.ai/knowledge/concepts/README.md`.
* `README.md` — the index. Three tables: `## Concepts`, `## Rules and
  Patterns`, `## Modules`. Each row is keyword search-bait pointing at one note.
* `modules/README.md` and `concepts/README.md` are recipes, not indexes. New
  rows go in `README.md` only.

Placement rules:

* A fact about one module's source files belongs in that module's note.
* A fact that spans modules and defines shared vocabulary belongs in a concept
  note.
* A Go-implementation binding belongs in knowledge. A protocol, wire, or
  domain truth belongs in `system/`; the note links to it.
* Primitives, wire types, op-name constants, and protocol clients live in the
  astral-go SDK, not in astrald. SDK-implementation facts belong in
  astral-go's own `.ai/knowledge`; an astrald note cross-references them by
  package name (e.g. "op-name constants live in astral-go `api/dir`") and
  never documents them. A cross-repo reference is plain text, never a `../`
  markdown link.
* A reusable "how to write a new one" recipe belongs in `patterns/`.
* A note whose content is jointly covered by `system/` and a sibling module
  note is deleted, with its links fixed. Span-trimmed husks are not left
  behind.

## Execution architecture

The unit of work is one module:

```
discover → drift gate → read source → diff → author → verify → index row
```

Modules run concurrently, one independent chain per module, no cross-module
barrier.

**Phase 0 — Discover.** One cheap pass: list `mod/*` and
`.ai/knowledge/modules/*.md`. Classify each module `new` (source, no note),
`existing` (both), or `orphaned` (note, no source). Scoped runs skip this
phase; the drift gate detects a missing note itself.

**Phase 1 — Per-module chain.**

1. **Drift gate.** A read-only agent checks the note against source: dead
   citations, op-set mismatch, missing index row. A clean module stops here.
   A `new` module is stale by definition and needs no gate agent.
2. **Author.** Fed the drift findings. Traverses the module per the recipe
   below, then edits the note diff-aware: replaces stale text in place,
   preserves hand-curated condensed-domain lines and `system/` links, never
   blind-regenerates. Returns the proposed index row and the source citations
   it relied on. `changed=false` ends the chain; unwritten text needs no
   verification.
3. **Verifier.** Independent and adversarial. Tries to refute the draft
   against source, starting from the author's citations, applying the
   checklist below. Blocking issues trigger one repair round and a re-verify
   scoped to those issues. A note ships only if it survives.

**Phase 2 — Merge.** One writer for the shared index: upsert each shipped
row, drop rows for orphaned notes, delete the orphaned files, write the
provenance note and the concept-gap report.

The author and the verifier are separate agents. A self-graded note is the
main way a bad citation survives.

### Traversal recipe

Read in this order. Stop following a call path once it leaves the module,
mutates durable state, starts a goroutine, touches a stream, or reaches the
result — then write one flow bullet. Module shapes vary: read each file only
if it exists; skip steps that do not apply. A non-standard module (e.g. the
aggregator `mod/all/` with no `module.go` or `src/`) gets a short bundle note,
not a forced template, and is reported as non-standard.

1. `mod/<name>/module.go` — `DBPrefix` (persisting modules only) and the
   public `Module` interface; `ModuleName` for the modules that still define
   it. Op-name constants (the `Method*` set, and `ModuleName` for the modules
   that moved it) live in astral-go `api/<name>/module.go`; read them there for
   the op inventory. The astrald `module.go` imports that package.
2. `src/loader.go` — `Load`, `assets.LoadYAML`, `assets.Database()`
   migrations, `router.AddStructPrefix`, `init()` registration.
3. `src/module.go` — the struct (embeds `routing.OpRouter`), `Run(ctx)`,
   background loops.
4. `src/deps.go` — `Deps` + `LoadDependencies` (`core.Inject`, `mod:alias`
   tags). Each `Why` in `## Dependencies` names a concrete call into that dep.
5. `src/config.go` — `Config` fields. Feeds cross-cutting `## Invariants`.
6. `src/op_*.go` — one per operation (`OpPascalCase` → `snake_case`). Flow
   entry points and `## Surface` op rows; collapsed to a glob in `## Source`.
7. `src/db.go`, `src/db_*.go` (persisting modules only) — one per
   `<prefix>__<table>`. Persistence flows and Surface store rows.
8. Listeners, `*_handler.go`, holders, preprocessors — each registered
   extension point is a flow and possibly a Surface or Invariant row.
9. astral-go `api/<name>/client/` (out-of-repo) — confirms op signatures
   only. Protocol clients live in the SDK, not astrald; client code is never
   mirrored into a note.

Cross into a dependency's note and into `.ai/system/` for protocol truth as
needed. Both are read-only.

## Drift mode

* Drift mode runs Phase 0 and the drift gate only, then reports
  `module → findings`.
* Drift mode writes nothing under `knowledge/`.
* Refresh mode is the same gate plus authoring. A refresh where nothing
  drifted costs only the gate.

## PR mode

* PR mode is refresh mode for autonomous environments.
* Prepare: a dirty working tree aborts the run; nothing is stashed or
  discarded. The agent checks out the default branch and pulls
  fast-forward only.
* The refresh pipeline runs unchanged.
* Publish: the agent reverts unverified note edits, creates the branch
  `<git user, lowercased>/docs/update-knowledge-<date>`, commits
  `.ai/knowledge/` and the provenance note, pushes, and opens a pull request
  against the default branch.
* The PR carries only verified content. Excluded modules are listed in the
  PR body and retried on the next run.
* A run with no drift opens no PR.
* PR mode requires push rights and an authenticated `gh` CLI.

## Verification checklist

A note ships only if every check holds against current source:

* Every path and glob in `## Source` and every file named in `## Flows`
  exists.
* The set of `op_*.go` files matches the ops named in `## Surface` and
  `## Flows`.
* Every `## Dependencies` `Why` names a concrete call, interface, data, or
  lifecycle hook — never "used by X".
* Every `## Invariants` bullet traces to a line that enforces it.
* No content duplicates `.ai/system/` or restates `.ai/rules.md`.
* Section order matches the recipe. No empty section. No filler.
* No brace shorthand (`objects.{read,store}`, `src/{loader,module}.go`).
* Length within budget (~150 lines; over budget → split to a concept page or
  `system/` and link).
* The note has exactly one index row in `.ai/knowledge/README.md`, and the
  file the row points at exists.
* The prose follows the Style rules above.

## Index curation

* A row is grep-bait for an agent, not a sentence.
* A module row leads with the backticked module path, then 6–10 exact
  identifiers from the note's description and flows:
  `` | `mod/objects/`, Load[T], Save, Commit, Discard, Blueprint, repo group, Push, object store, purge, Holder | `modules/objects.md` | ``
* A concept row uses identifiers from the concept's core types and headings.
* The row changes in the same change as the note. A note without a row is
  unreachable. A row pointing at a missing file is broken.

## Concept notes

* The workflow never auto-authors concept notes.
* A module note referencing a concept with no note is reported as a concept
  gap for a human.
* A concept note is authored only on explicit request, following
  `.ai/knowledge/concepts/README.md`.

## Provenance and review

* All edits stay in the working tree. No commits.
* Each run appends a dated section to `.ai/artifacts/analysis/knowledge-update.md`
  recording: modules changed with one-line summaries, citations relied on,
  modules that failed verification, and concept gaps.
* A human reviews against code and `system/`, then commits.

## Drift traps

* Stale citations — a note citing a deleted file is a staleness flag, not a
  typo. Surface it.
* Duplicating `system/` — keep the Go binding, link the rest.
* Blind regeneration — clobbering hand-trimmed domain lines and `system/`
  links. Updates are diff-aware.
* Wrong index file — rows belong in `README.md`, never in `modules/README.md`.
* Hallucinated deps or invariants — a `Why` or invariant with no backing line.
* Concept/module bleed — cross-module ideas in a module note, or
  module-specific file lists in a concept note.
* Self-graded notes — the verifier is a different agent than the author.

## Files in this skill

* `SKILL.md` — this playbook.
* `workflow.js` — the Workflow engine for the drift-gated pipeline. `args`:
  a module name or list scopes the run; `{ mode: "drift" }` reports only;
  `{ mode: "pr" }` publishes a pull request; no args runs the whole repo.
  The no-args form is safe to schedule (e.g. `/loop 144h /update-knowledge`).
* `command.md` — the `/update-knowledge` slash command, symlinked from
  `.claude/commands/`.
