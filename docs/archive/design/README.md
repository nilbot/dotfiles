# Archived design

Designs that were written, reviewed and approved while describing a subsystem
this repository has since deleted. They are here rather than in `docs/design/`
because that store is *the design still in force*, and a document under that
heading describing a command that does not exist is a contradiction a reader
cannot resolve.

**Nothing here is rewritten to stay true.** The text, the status headers and the
reasoning are exactly as they were on the date in the filename — including the
present-tense claims, which were true when written. Read them as of that date. A
document here may name a command that no longer exists; that is what the entry
below is for. Only relative links were adjusted, so they still resolve.

- `2026-09-18-layout-manifest-and-store-root-design.md` — moved 2026-09-21. It
  specified `agents.layout/v2`: a tracked `.agents/layout.json`, a role→path
  `stores` map, `agents layout show|path|migrate`, a version floor with a
  deploy-before-flip gate, and a digest-based drift report. The tool no longer
  has a layout schema at all — one fixed set of stores under `docs/`, no
  manifest, no migration — so the design's whole subject was deleted by
  [the reduction](../../plans/2026-09-21-agents-reduction-plan.md). Its reasoning
  about store-root freedom is the record of a decision that was made and then
  reversed; the reversal is why this document is worth keeping. The root router
  text it quoted in §4.4 is still in force and is specified in
  [the two-tier design](../../design/2026-08-29-two-tier-context-and-llm-migration-architecture.md) §2.1.
- `2026-08-12-spec-7-capture-and-review.md` — moved 2026-09-21. It specified
  capture triggers, an untracked draft queue, `agents review`, and one
  machine-local store under the git common directory for the trace index and
  cache. The 2026-08-20 redesign
  ([knowledge is documentation](../../design/2026-08-19-knowledge-is-documentation.md))
  had already retired §3 and §4; the 2026-09-21 reduction deleted the store,
  the index and the cache too, so the two sections the document still claimed
  were in force are gone as well, and nothing in it describes a surviving
  mechanism. §3a's measurement still stands and is the subject of
  [the capture experiment analysis](../analysis/2026-08-12-capture-instruction-experiment.md),
  which is archived next to it.
