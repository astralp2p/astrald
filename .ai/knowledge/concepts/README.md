# Concept Notes

Concept files are compact cross-module notes. They define shared vocabulary, mental models, invariants, and boundaries that module guides assume.

Use a concept file when the idea spans modules, such as identity, queries, links, wire encoding, objects, zones, or auth. Use a module guide when the note is mostly about one module's source files.

## Style

* Follow `.ai/rules.md` "Documentation Style" (the minimal English of
  `.ai/system/`).
* Start with `# Name`.
* Add a short opening paragraph only when it clarifies the concept.
* Use concrete headings named after the concept's parts, not a fixed template.
* Prefer bullets for facts, invariants, and rules.
* Use small tables only for compact vocabularies or mappings.
* Keep prose sparse and source-grounded.
* Link or name modules only when ownership matters.

## Content

Good concept notes usually answer:

* What is this thing?
* What are its core types, states, or roles?
* What invariants must not be broken?
* How does it cross module boundaries?
* What nearby concept or module should be read next?

Avoid tutorials, long API inventories, file listings, and module-specific implementation detail.
