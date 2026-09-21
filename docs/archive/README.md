# Archive

Executed plans, retired specs, and past measurements. How the system got here,
not what it is now — for that, read [`../design/`](../design/).

**Nothing here is rewritten to stay true.** A record edited to match today is
not a record, and the reasoning behind a retired decision is why nobody rebuilds
it. Read every document as of its date, which is in its filename and its header.

That cuts both ways: a document here may name a command that no longer exists,
a directory that was deleted, or a workflow that was retired. `agents handoff`,
`agents review`, `agents index`, `.agents/memory/` and `.agents/reports/handoff/`
all appear in these pages and none of them exist — they were retired on
2026-08-20 by
[knowledge is documentation](../design/2026-08-19-knowledge-is-documentation.md).
If an archived document tells you to run one, the document predates the change.

| directory | holds |
|---|---|
| `plans/` | implementation plans, all executed |
| `specs/` | specs that were retired without being implemented |
| `design/` | designs whose whole subject was later deleted, so they no longer describe anything in force |
| `analysis/` | measurements and experiments whose results are cited elsewhere |
| `experiment/` | the harness that produced one of those measurements; no longer runnable |

The 2026-09-21 reduction, which deleted the layout manifest and its migration,
the trace record and its machine-local store, `agents drift`, `agents update`
and the fleet registry, moved two documents here from `docs/design/`:
`design/2026-09-18-layout-manifest-and-store-root-design.md` (its subject was
the manifest) and `design/2026-08-12-spec-7-capture-and-review.md` (its subject
was the trace store and the review queue). Each entry's reason is in
[`design/README.md`](design/README.md).
