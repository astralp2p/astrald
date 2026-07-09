# AI Workspace

Vendor-neutral AI context for Astrald.

Use `overview.md` only for the project overview and repository map.

## Load Order

1. `.ai/README.md`
2. `.ai/rules.md`
3. `.ai/comments.md`
4. `.ai/methodology.md`

Then use indexes. Load scoped files only when relevant:

- `.ai/knowledge/README.md` - repo implementation
- `.ai/patterns/README.md` - code recipes
- `.ai/system/README.md` - domain/protocol truth
- `.ai/artifacts/` - ignored unless explicitly referenced

## Authority

1. User instruction
2. Code/tests
3. `.ai/system/`
4. `.ai/rules.md`, `.ai/comments.md`
5. `.ai/knowledge/`
6. `.ai/patterns/`
7. Referenced `.ai/artifacts/`

Call out conflicts.

## Roles

- `overview.md` - project overview and repository map
- `rules.md` - compact always-on rules
- `comments.md` - always-on commenting directive
- `patterns/` - source-grounded recipes
- `knowledge/` - repo implementation notes
- `system/` - domain/system knowledge
- `skills/` - project tool notes
- `artifacts/` - working notes, ignored by default
