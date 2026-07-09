export const meta = {
  name: 'update-protocols',
  description:
    'Drift-gated generation of protocol doc packages (README + ops/ + types/) from mod/ source, staged at .ai/artifacts/protocols/ in the exact structure and writing style of .ai/system/protocols/. No args = every module with ops; { mode: "drift" } = report only; a module name or list scopes the run. Never writes .ai/system/, never executes ops.',
  phases: [
    { title: 'Discover', detail: 'modules with ops vs .ai/system/protocols/ dirs', model: 'haiku' },
    { title: 'Fingerprint', detail: 'hash source+baseline; skip modules unchanged since last clean gate or ship', model: 'haiku' },
    { title: 'Drift', detail: 'read-only gate per module: op set, args, types, README' },
    { title: 'Author', detail: 'stale ops batched per module, then types+README, diff-aware', model: 'sonnet' },
    { title: 'Assemble', detail: 'copy clean baseline docs verbatim; completeness check', model: 'haiku' },
    { title: 'Verify', detail: 'adversarial package check: behavior, form, completeness' },
    { title: 'Merge', detail: 'drop failed packages, provenance note', model: 'haiku' },
  ],
}

// args: undefined -> all modules with ops | "nodes" / ["a","b"] -> scoped
//       { mode: "drift", modules?: [...] } -> report only
// A string that looks like JSON is unwrapped first — some callers deliver
// args JSON-encoded ('["a","b"]' instead of ["a","b"]), which would
// otherwise scope the run to one bogus module named like the whole list.
let input = args
if (typeof input === 'string' && /^[\[{]/.test(input.trim())) {
  try { input = JSON.parse(input) } catch {}
}
const opts = input && typeof input === 'object' && !Array.isArray(input) ? input : {}
const driftOnly = opts.mode === 'drift'
const scoped = Array.isArray(input)
  ? input
  : Array.isArray(opts.modules)
    ? opts.modules
    : typeof input === 'string'
      ? [input]
      : null

const BASE = '.ai/system/protocols'
const STAGE = '.ai/artifacts/protocols'
const PROV = '.ai/artifacts/analysis/protocols-update.md'
const STATE = '.ai/artifacts/protocols-state.json' // module fingerprints; never inside STAGE (packages are rm'd/moved wholesale)
const OPS_PER_AUTHOR = 10 // max ops per author agent; amortizes spec+module.go overhead. Lower if repair rounds tick up.
const BUDGET_FLOOR = 50_000 // with a budget directive, start no new module work below this many remaining tokens

// ---------------------------------------------------------------------------
// Discover — skipped when scoped: the drift gate verifies each module itself.
// ---------------------------------------------------------------------------
let worklist
if (scoped) {
  worklist = scoped.map(name => ({ name, status: 'unknown' }))
} else {
  phase('Discover')
  const DISCOVER_SCHEMA = {
    type: 'object',
    additionalProperties: false,
    required: ['modules'],
    properties: {
      modules: {
        type: 'array',
        items: {
          type: 'object',
          additionalProperties: false,
          required: ['name', 'status'],
          properties: {
            name: { type: 'string', description: 'module name == mod/<name>/ dir name' },
            status: { type: 'string', enum: ['documented', 'undocumented'], description: 'documented = .ai/system/protocols/<name>/ exists' },
          },
        },
      },
    },
  }
  const discovery = await agent(
    `List the modules under \`mod/\` that define ops: immediate subdirectories of \`mod/\` containing \`src/op_*.go\` files (use ls/find only; do not read file contents). For each, check whether \`${BASE}/<name>/\` exists: "documented" if yes, "undocumented" if no. Ignore \`${BASE}\` subdirectories with no matching \`mod/<name>/\` — they are documented upstream from other repos and are out of scope.`,
    { label: 'discover', phase: 'Discover', schema: DISCOVER_SCHEMA, agentType: 'Explore', model: 'haiku' }
  )
  worklist = discovery?.modules ?? []
}

if (!worklist.length) {
  log('No modules to process.')
  return { mode: driftOnly ? 'drift' : 'refresh', stale: [], staged: [], dropped: [], unchanged: [] }
}
log(`${driftOnly ? 'drift' : 'refresh'}: ${worklist.length} module(s)`)

// ---------------------------------------------------------------------------
// Fingerprint — one haiku pass over source AND baseline per module. A module
// whose fingerprint matches its state record skips the strong drift gate:
// 'clean' skips outright, 'staged' skips while its staged package still
// exists (a deleted staging dir means the human rejected it — re-gate).
// A failed fingerprint agent skips nothing; every module gates as before.
// ---------------------------------------------------------------------------
phase('Fingerprint')
const FP_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['modules'],
  properties: {
    modules: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['name', 'hash', 'stagedExists', 'priorHash', 'priorStatus'],
        properties: {
          name: { type: 'string' },
          hash: { type: 'string', description: 'fingerprint from the stated command' },
          stagedExists: { type: 'boolean', description: 'true if the staged package directory exists' },
          priorHash: { type: 'string', description: 'hash recorded in the state file, empty string if none' },
          priorStatus: { type: 'string', enum: ['clean', 'staged', ''], description: 'status recorded in the state file, empty string if none' },
        },
      },
    },
  },
}
const fp = await agent(
  `Read-only fingerprint pass — do NOT write anything.
1. For each module below, compute its fingerprint with exactly this command (substitute the module name):
   \`{ find mod/<name> -name '*.go' -type f 2>/dev/null; find ${BASE}/<name> -type f 2>/dev/null; } | LC_ALL=C sort | xargs shasum -a 256 2>/dev/null | shasum -a 256 | cut -d' ' -f1\`
2. For each module, check whether \`${STAGE}/<name>/\` exists.
3. Then read \`${STATE}\` (JSON: {"fingerprints":{"<module>":{"hash":"...","status":"clean"|"staged"}}}); a missing file means no prior records. Report priorHash/priorStatus verbatim from the file — never copy a computed hash into priorHash.
Modules: ${worklist.map(m => m.name).join(', ')}.`,
  { label: 'fingerprint', phase: 'Fingerprint', schema: FP_SCHEMA, agentType: 'Explore', model: 'haiku' }
)
const fpByName = new Map((fp?.modules ?? []).map(e => [e.name, e]))
const unchangedOf = e => !!e?.hash && e.hash === e.priorHash && (e.priorStatus === 'clean' || (e.priorStatus === 'staged' && e.stagedExists))
const unchanged = worklist.filter(m => unchangedOf(fpByName.get(m.name))).map(m => ({ module: m.name, status: fpByName.get(m.name).priorStatus }))
const gating = worklist.filter(m => !unchangedOf(fpByName.get(m.name)))
log(`fingerprint: ${unchanged.length} unchanged (gate skipped), ${gating.length} to gate`)

// ---------------------------------------------------------------------------
// Schemas
// ---------------------------------------------------------------------------
const ITEM = status => ({
  type: 'array',
  items: {
    type: 'object',
    additionalProperties: false,
    required: ['name', 'status', 'findings'],
    properties: {
      name: { type: 'string', description: status },
      status: { type: 'string', enum: ['missing', 'drifted', 'clean'] },
      findings: { type: 'array', items: { type: 'string', description: 'one concrete drift finding, one declarative sentence' } },
    },
  },
})

const GATE_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['module', 'clean', 'ops', 'types', 'readmeStale', 'orphanedOpDocs', 'findings'],
  properties: {
    module: { type: 'string' },
    clean: { type: 'boolean', description: 'true only if every op and type is clean, the README is current, and there are no orphaned op docs' },
    ops: ITEM('full op name <protocol>.<op> from the Method* constant'),
    types: ITEM('exact ObjectType() string'),
    readmeStale: { type: 'boolean', description: 'true when README.md is missing or no longer matches the op families' },
    orphanedOpDocs: { type: 'array', items: { type: 'string', description: 'baseline ops/*.md file with no matching op in source' } },
    findings: { type: 'array', items: { type: 'string', description: 'package-level finding, one declarative sentence' } },
  },
}

const OPS_AUTHOR_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['ops', 'typesUsed', 'citations'],
  properties: {
    ops: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['op', 'written', 'summary'],
        properties: {
          op: { type: 'string' },
          written: { type: 'boolean', description: 'true if the staged op doc was created or edited' },
          summary: { type: 'string', description: 'one declarative sentence: what was written and why' },
        },
      },
    },
    typesUsed: { type: 'array', items: { type: 'string', description: 'ObjectType() string of a module-defined type any op in the batch accepts or returns' } },
    citations: { type: 'array', items: { type: 'string', description: 'a source path the docs rely on, verified to exist' } },
  },
}

const PKG_AUTHOR_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['written', 'citations', 'summary'],
  properties: {
    written: { type: 'array', items: { type: 'string', description: 'staged file path created or edited' } },
    citations: { type: 'array', items: { type: 'string' } },
    summary: { type: 'string' },
  },
}

const ASSEMBLE_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['copied', 'gaps'],
  properties: {
    copied: { type: 'array', items: { type: 'string', description: 'baseline file copied verbatim into staging' } },
    gaps: { type: 'array', items: { type: 'string', description: 'completeness failure, one declarative sentence naming the missing file' } },
  },
}

const VERIFY_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['module', 'pass', 'issues'],
  properties: {
    module: { type: 'string' },
    pass: { type: 'boolean' },
    issues: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['file', 'claim', 'problem', 'severity'],
        properties: {
          file: { type: 'string', description: 'staged file the issue is in' },
          claim: { type: 'string', description: 'the doc text / structure under question' },
          problem: { type: 'string', description: 'why it fails, traced to source or to the corpus format' },
          severity: { type: 'string', enum: ['block', 'warn'] },
        },
      },
    },
  },
}

const REPAIR_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['fixed', 'summary'],
  properties: {
    fixed: { type: 'array', items: { type: 'string', description: 'staged file edited' } },
    summary: { type: 'string' },
  },
}

const blockers = v => (v?.issues ?? []).filter(i => i.severity === 'block')

// ---------------------------------------------------------------------------
// Prompts. SPEC/TRAVERSAL/CHECKLIST are distilled from SKILL.md ("Format
// spec", "Traversal recipe", "Verification checklist") and .ai/rules.md
// ("Documentation Style") so agents receive the spec instead of re-reading
// those files. An edit to those sections is mirrored here. Each prompt opens
// with the static block — a shared prefix for the prompt cache.
// ---------------------------------------------------------------------------
const SPEC = `## Ground rules

* Code is truth for behavior: every documented argument, stream message, returned object, and field traces to a line in the module source.
* The corpus \`${BASE}/\` is truth for form: a staged doc is indistinguishable in form from a hand-written sibling.
* Write only under \`${STAGE}/\`. Never write under \`${BASE}\` (read-only submodule).
* Never run astrald, astral-query, or any op. Examples are synthesized, never captured: command flags only from documented arguments, output matching the returned type's documented text/JSON form, plausible placeholder values in corpus style.
* Prose is minimal English: declarative present tense, one fact per sentence or bullet, no motivation, hedging, or meta-commentary; repeat the subject, do not chain pronouns; backtick code identifiers; state defaults, limits, and terminators explicitly.
* Primitive types (\`string8\`, \`error_message\`, \`ack\`, \`eos\`, ...) belong to \`.ai/system/primitive-types/\` — link by name, never re-document. Encoding vocabulary: \`.ai/system/topics/{json,text,binary}-encoding.md\`.

## Op doc — \`ops/<protocol>.<op>.md\`

\`\`\`\`
# <protocol>.<op>

<One paragraph, 1–3 sentences: what the op does; notable behavior such as auth gating, key defaults, stream semantics.>

## Arguments

* <name> (<type>[, required]) – <Sentence. Defaults stated explicitly.>
* (stream) – <Typed objects read from the input stream, in order.>

## Returned objects

The operation returns one of:
* An \`error_message\` object if <failure cases>.
* A \`<type>\` object <for what>.
* An \`eos\` object terminating the stream.

## Examples

\`\`\`shellsession
$ astral-query <protocol>.<op> -<arg> <value> -out json
{"Type":"<type>","Object":<...>}
\`\`\`
\`\`\`\`

Op doc rules:

* The argument separator is the en dash \`–\`, never a hyphen.
* Argument names are the lowercase query names of the \`op<Pascal>Args\` fields. \`required\` mirrors the \`query:"required"\` tag.
* Argument types name astral types (\`string8\`, \`uint64\`, \`object_id.sha256\`) when the struct field is one; plain \`string\` for a Go string the corpus documents as such.
* \`in\` and \`out\` are implicit. New docs omit them from Arguments. Baseline docs that list them keep them. Neither direction is drift.
* The \`(stream)\` bullet appears only when the op reads typed objects from the input stream.
* An op whose response is not a typed-object stream (e.g. raw payload) replaces the "returns one of" list with prose.
* Streaming ops document the \`eos\` terminator.
* Stream input examples use the \`echo '<json>' | astral-query <op> -in json -out json\` shape.

## Type doc — \`types/<full.type.name>.md\`

\`\`\`\`
# <full.type.name>

<One sentence: what the type represents.>

## Fields

* <FieldName> (<wiretype>) – <Sentence.>

<Text/JSON form note when the encoding is non-default.>

## Example

\`\`\`json
{
  "Type": "<full.type.name>",
  "Object": <...>
}
\`\`\`
\`\`\`\`

Type doc rules:

* The file name and H1 use the exact \`ObjectType()\` string from source.
* Field names keep their Go casing. Wire types are lowercase astral types (\`nonce64\`, \`identity\`, \`string8\`, \`uint64\`, \`bool\`, \`object\`).

## Protocol README — \`README.md\`

* H1 is the protocol name.
* One to three short paragraphs: what the protocol provides; core concepts with backticked type names; op families with backticked op names.
* No headings beyond the H1. No exhaustive op list. No marketing.

Exemplars under \`${BASE}/\` — consult one only when a template above leaves the shape ambiguous; the corpus wins on any disagreement: ops \`crypto/ops/crypto.sign_text.md\` (stream protocol), \`tree/ops/tree.set.md\` (stream value), \`objects/ops/objects.read.md\` (raw response, auth gating), \`dir/ops/dir.resolve.md\` (simple); types \`crypto/types/mod.crypto.signature.md\` (text form), \`nodes/types/mod.nodes.link_info.md\` (rich struct); READMEs \`objects/README.md\`, \`auth/README.md\`, \`user/README.md\`.`

const TRAVERSAL = `## Traversal recipe — read per op, in order; stop once wire-visible behavior is covered

1. \`mod/<name>/module.go\` — the public \`Module\` interface, and \`ModuleName\` for the modules that still define it. The op-name constants (the \`Method*\` set, and \`ModuleName\` for the modules that moved it) live in astral-go \`api/<protocol>/module.go\`; read them there. The wire name of an op is \`<ModuleName>.<snake_case of the Op* method name without the prefix>\` (astral-go \`lib/routing/op_router.go\` \`AddStructPrefix\`, imported as \`github.com/cryptopunkscc/astral-go/lib/routing\`); a \`Method*\` constant states it explicitly when present.
2. \`src/op_<name>.go\` — the unit of truth for one op: \`op<Pascal>Args\` fields → arguments; \`query:"required"\` → required; \`In\`/\`Out\` fields → implicit encoding params; defaults from code. The body: \`q.Accept\`/\`q.Reject\` → acceptance and rejection conditions; channel reads → the \`(stream)\` bullet; every \`ch.Send(...)\` → a returned object (\`astral.Err\` → \`error_message\`, \`&astral.Ack{}\` → \`ack\`, \`&astral.EOS{}\` → \`eos\` terminator); raw writes → prose.
3. astral-go \`api/<name>/\` (out-of-repo) — the wire-type structs with \`ObjectType()\` the op accepts or returns; their fields and astral field types. A few node-only object types remain in the astrald module root package (\`mod/<name>/*.go\`); read those there.
4. astral-go \`api/<name>/client/\` (out-of-repo) — confirms op names and signatures only; never documented.

An op gated by an auth action (e.g. a \`mod.auth.action\` subtype) names the action type in its intro sentence.`

const CHECKLIST = `## Verification checklist

Form deviations from the templates are severity "block"; prose-level style issues are "warn".

Behavior — against \`mod/<name>/\`:

* Every documented argument exists in the args struct with the documented name, required flag, and default. No struct query field is undocumented (except implicit \`in\`/\`out\`).
* The implicit \`in\`/\`out\` parameters are NEVER an issue at any severity — listed or omitted, in any doc.
* The \`(stream)\` bullet matches the objects the op body actually reads.
* Every returned-objects bullet traces to a send or reject path. Every send path is documented, including the \`eos\` terminator.
* Every type doc field matches the Go struct: name, astral type, order. Text/JSON form claims match the type's encoding.
* Auth gating claims trace to code.

Form — against the corpus:

* File names: \`ops/<protocol>.<op>.md\` matches the op's wire name; \`types/<name>.md\` matches \`ObjectType()\` exactly.
* Heading set and order match the templates. No extra headings, no empty sections, no filler.
* Argument bullets use \`* name (type[, required]) – ...\` with the en dash.
* Typed responses use the "The operation returns one of:" lead-in; raw responses use prose.
* Examples are \`\`\`shellsession blocks; commands use only documented arguments; outputs match documented forms; the JSON envelope is \`{"Type":...,"Object":...}\`.
* No content re-documents \`primitive-types/\` or restates \`topics/\`.
* Prose is declarative, one fact per sentence, no hedging, no meta-commentary.

Completeness — against the package:

* Every source op has exactly one staged op doc; no orphan op docs.
* A baseline doc whose op has no source implementation is an orphan: correct handling is its absence from staging — never staged, and its absence is never a gap or block.
* Every module type referenced by a staged op doc has a staged type doc.
* \`README.md\` exists.
* Verbatim copies are byte-identical to baseline.`

function gatePrompt(m) {
  return `Read-only drift gate for the protocol docs of \`mod/${m.name}/\` — do NOT edit anything. If \`mod/${m.name}/\` does not exist or defines no ops, return clean=true with one finding saying so.
Enumerate the source op inventory from the exported \`Op*\` methods under \`mod/${m.name}/src/\` (one \`src/op_*.go\` per op by convention). The wire name of an op is \`<ModuleName>.<snake_case of the method name without the Op prefix>\` (see astral-go \`lib/routing/op_router.go\` \`AddStructPrefix\`); the \`Method*\` op-name constants in astral-go \`api/${m.name}/module.go\` state it explicitly when present — report a mismatch between a constant and a derived name as a finding. Enumerate module-defined types: the wire-type structs with \`ObjectType()\` that ops accept or return live in astral-go \`api/${m.name}/\`; a few node-only object types remain in the astrald module root package \`mod/${m.name}/*.go\`.
Compare against the baseline package \`${BASE}/${m.name}/\` (if the directory does not exist, every op and type is "missing", readmeStale=true, and clean=false):
- ops: each source op is "missing" (no \`ops/<protocol>.<op>.md\`), "drifted" (doc disagrees with the \`op<Pascal>Args\` struct on argument names/required flags/defaults, or with the body's \`ch.Send\`/reject paths on returned objects or stream protocol), or "clean". The implicit \`in\`/\`out\` parameters are NEVER drift, whether listed or omitted.
- orphanedOpDocs: \`ls ${BASE}/${m.name}/ops/\` and check every file against the source op inventory; each file whose op does not exist in source goes in orphanedOpDocs. Do not skip this sweep — an unreported orphan blocks the package downstream.
- types: each relevant type is "missing" (no \`types/<ObjectType()>.md\`), "drifted" (fields disagree with the Go struct), or "clean". A baseline type doc with NO matching source struct is NOT orphaned — keep status "clean" and report it as a package finding.
- readmeStale: README.md missing, or its description names op families that no longer exist.
clean=true only if all ops and types are clean, readmeStale=false, and orphanedOpDocs is empty. Each finding is one declarative sentence with code identifiers in backticks.`
}

function opsAuthorPrompt(m, ops) {
  return `${SPEC}

${TRAVERSAL}

## Task

Write the protocol op docs for \`mod/${m}/\` under \`${STAGE}/${m}/ops/\` — one doc per op listed below, matching the op-doc template and rules above EXACTLY: heading order, bullet shape with the en dash \`–\`, example format, voice.
Shared source, read once for the whole batch: \`mod/${m}/module.go\` and the module root package \`mod/${m}/*.go\`; reuse for every op.
Ops to document, with the drift gate's findings under each:
${ops.map(o => `- \`${o.name}\` → \`${STAGE}/${m}/ops/${o.name}.md\`${o.findings.length ? '\n' + o.findings.map(f => `  - ${f}`).join('\n') : ''}`).join('\n')}

Then per op: read its \`mod/${m}/src/op_*.go\` source per the Traversal recipe and document only what the code shows.
If the baseline doc \`${BASE}/${m}/ops/<op>.md\` exists, first copy it verbatim to the staged path, then edit diff-aware: replace only stale facts, preserve hand-written prose and any listed \`in\`/\`out\` arguments; do NOT blind-regenerate. If it does not exist, create the staged doc from the template.
Write only the staged op docs listed above (mkdir -p the parent). Return per op: whether its doc was written and a one-sentence summary. Return once for the batch: the module types these ops accept or return (exact ObjectType() strings, module-defined only) and the source citations relied on.`
}

function pkgAuthorPrompt(m, typeNames, readmeStale) {
  return `${SPEC}

## Task

Write the remaining protocol package docs for \`mod/${m}/\` under \`${STAGE}/${m}/\`, matching the templates and rules above EXACTLY.
Tasks:
${typeNames.length ? `- Type docs: ${typeNames.map(t => `\`types/${t}.md\``).join(', ')}. Each documents the Go struct in the module root package whose \`ObjectType()\` returns that name, per the type-doc template.` : ''}
${readmeStale ? `- \`README.md\`: H1 \`# ${m}\`, then 1–3 short paragraphs describing what the protocol provides, per the README rules.` : ''}
If a baseline file exists under \`${BASE}/${m}/\`, copy it verbatim into staging first, then edit diff-aware — replace only stale facts, preserve hand-written prose. Otherwise create the file. If a listed type has no matching struct in source, do not invent one — skip it and say so in the summary.
Write only under \`${STAGE}/${m}/\` (mkdir -p as needed). Return the staged files written, the source citations relied on, and a one-sentence summary.`
}

function assemblePrompt(m, gate) {
  const cleanOps = gate.ops.filter(o => o.status === 'clean').map(o => o.name)
  const cleanTypes = gate.types.filter(t => t.status === 'clean').map(t => t.name)
  return `Assemble the staged protocol package \`${STAGE}/${m}/\` with bash only (mkdir/cp/ls/diff; no file authoring).
1. Copy these baseline docs verbatim from \`${BASE}/${m}/\` into the same relative paths under \`${STAGE}/${m}/\`, skipping any whose staged file already exists:
   - ops: ${cleanOps.length ? cleanOps.map(o => `\`ops/${o}.md\``).join(', ') : '(none)'}
   - types: ${cleanTypes.length ? cleanTypes.map(t => `\`types/${t}.md\``).join(', ') : '(none)'}
   - \`README.md\`${gate.readmeStale ? ' — skip, it was authored' : ''}
2. Do NOT copy these orphaned op docs: ${gate.orphanedOpDocs.length ? gate.orphanedOpDocs.join(', ') : '(none)'}.
3. Completeness check: every op in [${gate.ops.map(o => o.name).join(', ')}] has \`${STAGE}/${m}/ops/<op>.md\`; every type in [${gate.types.map(t => t.name).join(', ')}] has \`${STAGE}/${m}/types/<name>.md\`; \`${STAGE}/${m}/README.md\` exists; every staged \`ops/*.md\` names an op in that same op list — compare against the list, never against the baseline directory; a newly authored doc has no baseline counterpart and that is NOT a gap. Report each failure as one gap sentence naming the missing or extra file.
Never write under \`${BASE}\`. Return the copied files and the gaps.`
}

function verifyPrompt(m, citations, authored, gate) {
  return `${SPEC}

${CHECKLIST}

## Task

Adversarially verify (do NOT edit) the staged protocol package \`${STAGE}/${m}/\` against the source \`mod/${m}/\` and the corpus \`${BASE}/\`. Verification is reading only. Files copied verbatim from the baseline are NOT re-read — they were verified when they entered the corpus. Scope:
1. The Behavior and Form checklist items apply only to the authored files:
${authored.length ? authored.map(f => `   - \`${f}\``).join('\n') : '   - (none authored)'}
2. Copies, one command: \`diff -rq ${BASE}/${m} ${STAGE}/${m}\`. If \`${BASE}/${m}/\` does not exist, instead confirm every staged file is in the authored list. A differing or staging-only file outside the authored list is a "block" issue. A baseline-only op doc: check source first — if its op exists in \`mod/${m}/src/\`, it is a missing copy, severity "block"; if the op does NOT exist in source, the file is an orphaned baseline doc and its absence from staging is CORRECT — report it as severity "warn" with claim "orphaned baseline doc excluded: <file>" so provenance records it. Already-excluded orphans, expected to be baseline-only: ${gate.orphanedOpDocs.length ? gate.orphanedOpDocs.join(', ') : '(none)'}.
3. Completeness, \`ls\` only: every op in [${gate.ops.map(o => o.name).join(', ')}] has \`ops/<op>.md\`; every type in [${gate.types.map(t => t.name).join(', ')}] has \`types/<name>.md\`; \`README.md\` exists.
The authors claim the authored docs rely on these sources — check them directly:
${(citations ?? []).map(c => `- ${c}`).join('\n')}
Default to failing any claim you cannot ground in source. pass=true only if no "block" issues remain.`
}

function repairPrompt(m, blocked) {
  return `${SPEC}

${CHECKLIST}

## Task

Repair the staged protocol package \`${STAGE}/${m}/\`. A verifier found these blocking issues — fix exactly these, nothing else:
${blocked.map(i => `- ${i.file}: ${i.claim} — ${i.problem}`).join('\n')}
Edits are diff-aware and match the templates and rules above exactly. A missing file is created per its template from \`mod/${m}/\` source.
Never copy a baseline doc into staging unless its op exists in \`mod/${m}/src/\`. A baseline doc whose op has no source implementation is an orphan: the fix is its ABSENCE from staging — delete the staged copy if one exists and note the exclusion in the summary.
Return the staged files you edited.`
}

function reverifyPrompt(m, blocked) {
  return `${SPEC}

${CHECKLIST}

## Task

Re-verify (do NOT edit) the repaired staged package \`${STAGE}/${m}/\` against \`mod/${m}/\` and the corpus \`${BASE}/\`. Confirm each previously blocking issue is actually fixed:
${blocked.map(i => `- ${i.file}: ${i.claim} — ${i.problem}`).join('\n')}
Check the files the repair touched for newly introduced issues; do not re-verify the rest of the package. Report anything still broken or newly introduced; pass=true only if no "block" issues remain.`
}

// ---------------------------------------------------------------------------
// Gate + refresh — one independent chain per module, no cross-module barrier.
// A clean module costs one Explore agent; only stale packages pay for authoring.
// ---------------------------------------------------------------------------
const budgetSkipped = []
const lowBudget = () => !!budget.total && budget.remaining() < BUDGET_FLOOR

const processed = (
  await pipeline(
    gating,
    m => {
      if (lowBudget()) { budgetSkipped.push(m.name); return null }
      return agent(gatePrompt(m), { label: `gate:${m.name}`, phase: 'Drift', schema: GATE_SCHEMA, agentType: 'Explore' })
    },
    async (gate, m) => {
      if (!gate) return null
      if (gate.clean) return { module: m.name, gate, clean: true }
      if (driftOnly) return { module: m.name, gate }
      if (lowBudget()) { budgetSkipped.push(m.name); return null }

      const staleOps = gate.ops.filter(o => o.status !== 'clean')
      // balanced chunks under the cap: 25 ops -> 9/8/8, not 10/10/5 or a 1-op tail
      const chunkSize = Math.ceil(staleOps.length / Math.ceil(staleOps.length / OPS_PER_AUTHOR)) || 1
      const opChunks = []
      for (let i = 0; i < staleOps.length; i += chunkSize) opChunks.push(staleOps.slice(i, i + chunkSize))
      const batches = (
        await parallel(opChunks.map((ops, i) => () => agent(opsAuthorPrompt(m.name, ops), {
          label: opChunks.length > 1 ? `ops:${m.name} ${i + 1}/${opChunks.length}` : `ops:${m.name}`,
          phase: 'Author',
          schema: OPS_AUTHOR_SCHEMA,
          model: 'sonnet',
        })))
      ).filter(Boolean)
      const opResults = batches.flatMap(b => b.ops ?? [])

      const gateTypeNames = gate.types.map(t => t.name)
      const staleTypes = gate.types.filter(t => t.status !== 'clean').map(t => t.name)
      const reportedTypes = [...new Set(batches.flatMap(b => b.typesUsed ?? []))]
      const typesTask = [...new Set([...staleTypes, ...reportedTypes.filter(n => !gateTypeNames.includes(n))])]

      let pkg = null
      if (typesTask.length || gate.readmeStale) {
        pkg = await agent(pkgAuthorPrompt(m.name, typesTask, gate.readmeStale), { label: `pkg:${m.name}`, phase: 'Author', schema: PKG_AUTHOR_SCHEMA, model: 'sonnet' })
      }

      const assembled = await agent(assemblePrompt(m.name, gate), { label: `assemble:${m.name}`, phase: 'Assemble', schema: ASSEMBLE_SCHEMA, model: 'haiku' })

      const citations = [...new Set([...batches.flatMap(b => b.citations ?? []), ...(pkg?.citations ?? [])])]
      const authored = [
        ...opResults.filter(r => r.written).map(r => `${STAGE}/${m.name}/ops/${r.op}.md`),
        ...(pkg?.written ?? []),
      ]
      let verdict = await agent(verifyPrompt(m.name, citations, authored, gate), { label: `verify:${m.name}`, phase: 'Verify', schema: VERIFY_SCHEMA, agentType: 'Explore' })
      let blocked = [
        ...blockers(verdict),
        ...(assembled?.gaps ?? []).map(g => ({ file: 'package', claim: g, problem: 'staged package is incomplete', severity: 'block' })),
      ]
      if (blocked.length) {
        await agent(repairPrompt(m.name, blocked), { label: `repair:${m.name}`, phase: 'Verify', schema: REPAIR_SCHEMA, model: 'sonnet' })
        verdict = await agent(reverifyPrompt(m.name, blocked), { label: `reverify:${m.name}`, phase: 'Verify', schema: VERIFY_SCHEMA, agentType: 'Explore' })
        blocked = blockers(verdict)
      }

      return {
        module: m.name,
        gate,
        authoredOps: opResults.filter(r => r.written).map(r => r.op),
        authoredPkg: pkg?.written ?? [],
        copied: assembled?.copied ?? [],
        opSummaries: opResults.map(r => `${r.op}: ${r.summary}`),
        verdict,
        shipped: !!verdict && blocked.length === 0,
      }
    }
  )
).filter(Boolean)

const stale = processed.filter(p => !p.clean)
const clean = processed.filter(p => p.clean).map(p => p.module)
log(`drift gate: ${stale.length} stale, ${clean.length} clean`)
if (budgetSkipped.length) log(`budget floor: ${budgetSkipped.length} module(s) deferred to the next run: ${budgetSkipped.join(', ')}`)

if (driftOnly) {
  return {
    mode: 'drift',
    stale: stale.map(p => ({
      module: p.module,
      ops: p.gate.ops.filter(o => o.status !== 'clean'),
      types: p.gate.types.filter(t => t.status !== 'clean'),
      readmeStale: p.gate.readmeStale,
      orphanedOpDocs: p.gate.orphanedOpDocs,
      findings: p.gate.findings,
    })),
    clean,
    unchanged,
    budgetSkipped,
  }
}

// ---------------------------------------------------------------------------
// Merge — drop failed packages from staging, write provenance.
// ---------------------------------------------------------------------------
const shipped = stale.filter(p => p.shipped)
const dropped = stale.filter(p => !p.shipped).map(p => p.module)

// Record fingerprints (refresh only; drift writes nothing): clean and shipped
// modules skip the gate next run while source+baseline stay unchanged.
// Dropped modules lose their record and re-gate.
const stateSet = {}
const stateDel = []
for (const p of processed) {
  const e = fpByName.get(p.module)
  if (!e?.hash) continue
  if (p.clean) stateSet[p.module] = { hash: e.hash, status: 'clean' }
  else if (p.shipped) stateSet[p.module] = { hash: e.hash, status: 'staged' }
  else stateDel.push(p.module)
}
if (Object.keys(stateSet).length || stateDel.length) {
  await agent(
    `Update the fingerprint state file \`${STATE}\`. Read it first; treat a missing or invalid file as \`{"fingerprints":{}}\`. In the "fingerprints" object: set these keys exactly as given: ${JSON.stringify(stateSet)}; delete these keys: ${stateDel.length ? stateDel.join(', ') : '(none)'}; preserve every other key unchanged. Write the file back as valid JSON. Write nothing else.`,
    { label: 'state', phase: 'Merge', schema: { type: 'object', additionalProperties: false, required: ['written'], properties: { written: { type: 'boolean' } } }, model: 'haiku' }
  )
}

if (!stale.length) {
  log('Everything in sync; nothing staged.')
  return { mode: 'refresh', staged: [], dropped: [], clean, unchanged, budgetSkipped }
}

phase('Merge')
const mergeReport = await agent(
  `Finalize the protocol doc staging run.
1. Remove staging for packages that failed verification: ${dropped.length ? dropped.map(d => `\`rm -rf ${STAGE}/${d}\``).join(', ') : '(none)'}.
2. Append a dated section (use the \`date\` command for today) to \`${PROV}\` (create the file if missing) recording, one fact per bullet, declarative, no commentary:
   - packages staged for review: ${shipped.length ? shipped.map(p => `${p.module} (${p.authoredOps.length} op docs authored, ${p.copied.length} files copied verbatim${p.gate.orphanedOpDocs.length ? `, orphaned docs excluded: ${p.gate.orphanedOpDocs.join(' ')}` : ''})`).join('; ') : 'none'}
   - per-op summaries: ${shipped.flatMap(p => p.opSummaries).join('; ') || 'none'}
   - packages dropped after failed verification: ${dropped.join(', ') || 'none'}
   - modules skipped as unchanged since the last run: ${unchanged.map(u => u.module).join(', ') || 'none'}
   - modules deferred at the token budget floor: ${budgetSkipped.join(', ') || 'none'}
   - package-level findings for a human: ${stale.flatMap(p => p.gate.findings).join('; ') || 'none'}
   - verifier warnings for a human (includes orphaned baseline docs excluded from staging): ${shipped.flatMap(p => (p.verdict?.issues ?? []).filter(i => i.severity === 'warn').map(i => `${p.module} ${i.file}: ${i.problem.slice(0, 200)}`)).join('; ') || 'none'}
   - the move procedure: replace \`${BASE}/<protocol>/\` wholesale with \`${STAGE}/<protocol>/\`, review the submodule diff, commit in astral-docs.
Do NOT git commit. Do NOT write under \`${BASE}\`. Return a short markdown summary of the staged packages.`,
  { label: 'merge', phase: 'Merge', model: 'haiku' }
)

return {
  mode: 'refresh',
  staged: shipped.map(p => p.module),
  dropped,
  clean,
  unchanged,
  budgetSkipped,
  mergeReport,
}
