export const meta = {
  name: 'update-knowledge',
  description:
    'Drift-gated sync of .ai/knowledge module notes + index with mod/ source. A cheap read-only drift check runs per module first; only stale or new modules get the author -> verify -> repair chain. No args = whole repo (safe to run on a loop); { mode: "drift" } = report only; { mode: "pr" } = autonomous: pull default branch, refresh, push a docs branch, open a PR; a module name or list scopes the run.',
  phases: [
    { title: 'Prepare', detail: 'pr mode only: clean tree -> default branch -> pull' },
    { title: 'Discover', detail: 'ls mod/* vs notes; classify new/existing/orphaned' },
    { title: 'Drift', detail: 'one cheap read-only staleness check per module (the gate)' },
    { title: 'Refresh', detail: 'stale/new modules only: author -> verify -> one repair round' },
    { title: 'Merge', detail: 'index upsert, orphan cleanup, provenance note' },
    { title: 'Publish', detail: 'pr mode only: branch, commit, push, gh pr create' },
  ],
}

// args: undefined -> whole repo | "nodes" / ["a","b"] -> scoped
//       { mode: "drift", modules?: [...] } -> report only
//       { mode: "pr", modules?: [...] }    -> refresh, then push a branch and open a PR
const opts = args && typeof args === 'object' && !Array.isArray(args) ? args : {}
const driftOnly = opts.mode === 'drift'
const prMode = opts.mode === 'pr'
const scoped = Array.isArray(args)
  ? args
  : Array.isArray(opts.modules)
    ? opts.modules
    : typeof args === 'string'
      ? [args]
      : null

const SKILL = '.ai/skills/update-knowledge/SKILL.md'
const RECIPE = '.ai/knowledge/modules/README.md'
const INDEX = '.ai/knowledge/README.md'

// ---------------------------------------------------------------------------
// Prepare (pr mode only) — clean tree, default branch, pull. Abort on any failure;
// never stash or discard user work.
// ---------------------------------------------------------------------------
let prep = null
if (prMode) {
  phase('Prepare')
  const PREPARE_SCHEMA = {
    type: 'object',
    additionalProperties: false,
    required: ['ok', 'defaultBranch', 'reason'],
    properties: {
      ok: { type: 'boolean' },
      defaultBranch: { type: 'string', description: 'the repository default branch, e.g. master' },
      reason: { type: 'string', description: 'why preparation failed; empty when ok' },
    },
  }
  prep = await agent(
    `Prepare the repository for an autonomous documentation run. Steps, in order:
1. \`git status --porcelain\` — any output means the working tree is dirty. Return ok=false with the reason. Do NOT stash, discard, or commit anything.
2. Detect the default branch: \`git symbolic-ref --short refs/remotes/origin/HEAD\` (strip the \`origin/\` prefix); fall back to \`master\`.
3. \`git checkout <default>\` then \`git pull --ff-only\`.
A failure at any step returns ok=false with the reason. On success return ok=true and the default branch name.`,
    { label: 'prepare', phase: 'Prepare', schema: PREPARE_SCHEMA }
  )
  if (!prep?.ok) {
    log(`pr mode aborted: ${prep?.reason ?? 'prepare agent failed'}`)
    return { mode: 'pr', aborted: prep?.reason ?? 'prepare agent failed' }
  }
}

// ---------------------------------------------------------------------------
// Discover — skipped when scoped: the drift check detects a missing note itself.
// ---------------------------------------------------------------------------
let worklist
let orphans
if (scoped) {
  worklist = scoped.map(name => ({ name, status: 'unknown' }))
  orphans = []
} else {
  phase('Discover')
  const DISCOVER_SCHEMA = {
    type: 'object',
    additionalProperties: false,
    required: ['modules', 'orphanedNotes'],
    properties: {
      modules: {
        type: 'array',
        items: {
          type: 'object',
          additionalProperties: false,
          required: ['name', 'status'],
          properties: {
            name: { type: 'string', description: 'module name == mod/<name>/ dir name' },
            status: { type: 'string', enum: ['new', 'existing'], description: 'new = source but no note; existing = both' },
          },
        },
      },
      orphanedNotes: {
        type: 'array',
        items: { type: 'string' },
        description: '.ai/knowledge/modules/<name>.md files whose mod/<name>/ source no longer exists',
      },
    },
  }
  const discovery = await agent(
    `List the immediate subdirectories of \`mod/\` (each is a module) and the note files under \`.ai/knowledge/modules/\` (ignore README.md on both sides). Classify each module: "new" = source but no \`.ai/knowledge/modules/<name>.md\`; "existing" = both present. Also list orphaned notes: note files whose \`mod/<name>/\` directory no longer exists. Use ls/find only; do not read file contents.`,
    { label: 'discover', phase: 'Discover', schema: DISCOVER_SCHEMA, agentType: 'Explore', model: 'haiku' }
  )
  worklist = discovery?.modules ?? []
  orphans = discovery?.orphanedNotes ?? []
}

if (!worklist.length) {
  log('No modules to process.')
  return { mode: driftOnly ? 'drift' : 'refresh', stale: [], shipped: [], orphans }
}
log(`${driftOnly ? 'drift' : 'refresh'}: ${worklist.length} module(s)${orphans.length ? `, ${orphans.length} orphan note(s)` : ''}`)

// ---------------------------------------------------------------------------
// Schemas + prompts
// ---------------------------------------------------------------------------
const DRIFT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['module', 'stale', 'findings'],
  properties: {
    module: { type: 'string' },
    stale: { type: 'boolean' },
    findings: { type: 'array', items: { type: 'string', description: 'one concrete drift finding' } },
  },
}

const AUTHOR_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['module', 'changed', 'indexRow', 'citations', 'conceptGaps', 'summary'],
  properties: {
    module: { type: 'string' },
    changed: { type: 'boolean', description: 'true if the note file was created or edited' },
    indexRow: {
      type: 'string',
      description: 'full markdown index row for the ## Modules table: module path first, then 6-10 grep-bait identifiers, then the note path',
    },
    citations: { type: 'array', items: { type: 'string', description: 'a source path/glob the note relies on, verified to exist' } },
    conceptGaps: { type: 'array', items: { type: 'string', description: 'a referenced concept with no .ai/knowledge/concepts/<name>.md' } },
    summary: { type: 'string', description: 'one declarative sentence in minimal English: what changed and why' },
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
        required: ['claim', 'problem', 'severity'],
        properties: {
          claim: { type: 'string', description: 'the note text / citation under question' },
          problem: { type: 'string', description: 'why it fails, traced to source' },
          severity: { type: 'string', enum: ['block', 'warn'] },
        },
      },
    },
  },
}

const blockers = v => (v?.issues ?? []).filter(i => i.severity === 'block')

function driftPrompt(m) {
  return `Read-only drift check for the knowledge note of \`mod/${m.name}/\` — do NOT edit anything. Read \`.ai/knowledge/modules/${m.name}.md\` (if the file does not exist, return stale=true with the single finding "note missing"), its row in \`${INDEX}\`, and the source under \`mod/${m.name}/\`. Return stale=true with one finding per problem if any of these fail: a file, method, or path cited in the note (in Flows, Invariants, or Dependencies) no longer exists; a stated invariant no longer matches the code that enforced it; the note has no row in the index, or its row points at a missing file. Do NOT flag a note for omitting ops, files, or flows — the guide deliberately covers only change-critical invariants and contracts, not the full op or file surface. If everything checks out, return stale=false with an empty findings list. Each finding is one declarative sentence with code identifiers in backticks, e.g. "\`src/op_push.go\` is cited in Flows but does not exist."`
}

function authorPrompt(m, findings, repairIssues) {
  let p = `Refresh the knowledge note for \`mod/${m.name}/\`. First read these sections of \`${SKILL}\`: "Prime directives", "Style", "Scope", "Traversal recipe", "Verification checklist" — the Module Guide Recipe at \`${RECIPE}\` — and the "Documentation Style" section of \`.ai/rules.md\`; follow the recipe's section order and rules exactly, and write the note in that Documentation Style (the minimal English of \`.ai/system/\`). A drift check found these problems to fix:\n${findings.map(f => `- ${f}`).join('\n')}\n\nTraverse \`mod/${m.name}/\` per the recipe. If \`.ai/knowledge/modules/${m.name}.md\` exists, edit it diff-aware: replace only stale text, preserve hand-curated condensed-domain lines and \`.ai/system/\` links, do NOT blind-regenerate. If it does not exist, create it. Adapt to the module's actual shape — not every module has dependencies, persistence, ops, or a client; a non-standard module (e.g. an aggregator with no module.go/src/) gets a short bundle note, not a forced template. If \`mod/${m.name}/\` itself does not exist, write nothing and return changed=false saying so. Cross into \`.ai/system/\` for protocol truth (read-only; link, never copy). Verify every path/glob exists before you write it. Write the note with Write/Edit. Do NOT edit \`${INDEX}\` — return the proposed index row instead. Return changed=false only if the drift findings turn out to be false alarms and the note is already correct.`
  if (repairIssues?.length) {
    p += `\n\nA verifier rejected the previous draft. Fix exactly these blocking issues and re-write the note:\n${repairIssues.map(i => `- ${i.claim} — ${i.problem}`).join('\n')}`
  }
  return p
}

function verifyPrompt(m, citations) {
  return `Adversarially verify (do NOT edit) the note \`.ai/knowledge/modules/${m.name}.md\` against current source in \`mod/${m.name}/\` and \`.ai/system/\`. Apply every item of the "Verification checklist" section in \`${SKILL}\`, including the "Documentation Style" rules from \`.ai/rules.md\` that it references (a style violation is severity "warn" unless it obscures a fact). The author claims the note relies on these sources — check them directly:\n${(citations ?? []).map(c => `- ${c}`).join('\n')}\nDefault to failing any claim you cannot ground in source. pass=true only if no "block" issues remain.`
}

function reverifyPrompt(m, fixed) {
  return `Re-verify (do NOT edit) the repaired note \`.ai/knowledge/modules/${m.name}.md\` against \`mod/${m.name}/\`. Confirm each previously blocking issue is actually fixed:\n${fixed.map(i => `- ${i.claim} — ${i.problem}`).join('\n')}\nThen briefly spot-check the rest of the note. Report anything still broken or newly introduced; pass=true only if no "block" issues remain.`
}

// ---------------------------------------------------------------------------
// Drift gate + refresh — one independent chain per module, no cross-module barrier.
// A clean module costs one Explore agent; only stale/new modules pay for authoring.
// ---------------------------------------------------------------------------
const processed = (
  await pipeline(
    worklist,
    m =>
      m.status === 'new'
        ? { module: m.name, stale: true, findings: [`mod/${m.name}/ has source but no note yet`] }
        : agent(driftPrompt(m), { label: `drift:${m.name}`, phase: 'Drift', schema: DRIFT_SCHEMA, agentType: 'Explore' }),
    async (drift, m) => {
      if (!drift) return null
      if (!drift.stale) return { module: m.name, drift, clean: true }
      if (driftOnly) return { module: m.name, drift }
      const author = await agent(authorPrompt(m, drift.findings), { label: `author:${m.name}`, phase: 'Refresh', schema: AUTHOR_SCHEMA })
      if (!author) return { module: m.name, drift }
      if (!author.changed) return { module: m.name, drift, author, shipped: true } // nothing written -> nothing to verify
      let verdict = await agent(verifyPrompt(m, author.citations), { label: `verify:${m.name}`, phase: 'Refresh', schema: VERIFY_SCHEMA, agentType: 'Explore' })
      const blocked = blockers(verdict)
      if (verdict && blocked.length) {
        await agent(authorPrompt(m, drift.findings, blocked), { label: `repair:${m.name}`, phase: 'Refresh', schema: AUTHOR_SCHEMA })
        verdict = await agent(reverifyPrompt(m, blocked), { label: `reverify:${m.name}`, phase: 'Refresh', schema: VERIFY_SCHEMA, agentType: 'Explore' })
      }
      return { module: m.name, drift, author, verdict, shipped: !!verdict && blockers(verdict).length === 0 }
    }
  )
).filter(Boolean)

const stale = processed.filter(p => p.drift.stale)
const clean = processed.filter(p => p.clean).map(p => p.module)
log(`drift gate: ${stale.length} stale, ${clean.length} clean`)

if (driftOnly) {
  return {
    mode: 'drift',
    stale: stale.map(p => ({ module: p.module, findings: p.drift.findings })),
    clean,
    orphans,
  }
}

// ---------------------------------------------------------------------------
// Merge — single writer for the shared index; skipped entirely when nothing changed.
// ---------------------------------------------------------------------------
const shippedChanges = processed.filter(p => p.shipped && p.author?.changed)
const unresolved = stale.filter(p => !p.shipped).map(p => p.module)
const conceptGaps = [...new Set(processed.flatMap(p => p.author?.conceptGaps ?? []))]
const rows = shippedChanges.map(p => p.author.indexRow)

if (!rows.length && !orphans.length) {
  log('Everything in sync; nothing to merge.')
  return { mode: prMode ? 'pr' : 'refresh', shipped: [], unresolved, conceptGaps, orphans, pr: null }
}

phase('Merge')
const mergeReport = await agent(
  `Update the knowledge index transactionally. Read \`${INDEX}\`.
1. Upsert these rows under the \`## Modules\` table (replace the existing row for the same \`modules/<name>.md\`, else insert; keep the table's existing ordering convention):
${rows.length ? rows.map(r => `   ${r}`).join('\n') : '   (none)'}
2. Remove rows under \`## Modules\` that point at these orphaned notes, and delete those orphaned note files: ${orphans.length ? orphans.join(', ') : '(none)'}.
3. Leave the \`## Concepts\` and \`## Rules and Patterns\` tables untouched.
Then append a dated section (use the \`date\` command for today) to \`.ai/artifacts/analysis/knowledge-update.md\` (create the file if missing) recording: modules changed with their one-line summaries (${shippedChanges.map(p => `${p.module}: ${p.author.summary}`).join('; ') || 'none'}), the citations relied on, modules that failed verification and need human eyes (${unresolved.join(', ') || 'none'}), and concept gaps to triage (${conceptGaps.join(', ') || 'none'}). Write the provenance section in the "Documentation Style" of \`.ai/rules.md\`: one fact per bullet, no commentary. Do NOT git commit. Return a short markdown summary of what you changed.`,
  { label: 'merge-index', phase: 'Merge' }
)

// ---------------------------------------------------------------------------
// Publish (pr mode only) — branch, commit verified changes, push, open a PR.
// Unverified note edits are reverted first; the PR carries only verified content.
// ---------------------------------------------------------------------------
let pr = null
if (prMode) {
  phase('Publish')
  const PUBLISH_SCHEMA = {
    type: 'object',
    additionalProperties: false,
    required: ['branch', 'prUrl', 'summary'],
    properties: {
      branch: { type: 'string' },
      prUrl: { type: 'string', description: 'the created pull request URL; empty if creation failed, with the reason in summary' },
      summary: { type: 'string', description: 'one declarative sentence per action taken' },
    },
  }
  const unresolvedFiles = unresolved.map(name => `.ai/knowledge/modules/${name}.md`)
  pr = await agent(
    `Publish the knowledge changes as a pull request. Steps, in order:
1. Revert unverified note edits so the commit carries only verified content${unresolvedFiles.length ? `: for each of ${unresolvedFiles.join(', ')}, run \`git checkout -- <path>\`, or delete the file if untracked` : ': (none)'}.
2. Compute the branch name: \`git config user.name\`, lowercased, spaces replaced by hyphens, then \`/docs/update-knowledge-\` plus \`date +%F\`. If that branch already exists, append \`-\` plus \`date +%H%M\`.
3. \`git checkout -b <branch>\`.
4. \`git add .ai/knowledge .ai/artifacts/analysis/knowledge-update.md\`, then commit with the message \`docs: update knowledge notes (${shippedChanges.map(p => p.module).join(', ') || 'orphan cleanup'})\` ending with the trailer \`Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>\`.
5. \`git push -u origin <branch>\`.
6. \`gh pr create\` targeting \`${prep.defaultBranch}\` with title \`docs: update knowledge notes\` and a body in the "Documentation Style" of \`.ai/rules.md\` listing: modules updated with their one-line summaries (${shippedChanges.map(p => `${p.module}: ${p.author.summary}`).join('; ') || 'none'}), orphaned notes removed (${orphans.join(', ') || 'none'}), modules that failed verification and were excluded (${unresolved.join(', ') || 'none'}), and concept gaps (${conceptGaps.join(', ') || 'none'}). End the body with \`🤖 Generated with [Claude Code](https://claude.com/claude-code)\`.
If a step fails, stop there and report what succeeded. Return the branch name, the PR URL (empty on failure), and a one-sentence-per-action summary.`,
    { label: 'publish', phase: 'Publish', schema: PUBLISH_SCHEMA }
  )
  log(pr?.prUrl ? `PR: ${pr.prUrl}` : 'publish did not produce a PR')
}

return {
  mode: prMode ? 'pr' : 'refresh',
  shipped: shippedChanges.map(p => p.module),
  unresolved,
  conceptGaps,
  orphans,
  mergeReport,
  pr,
}
