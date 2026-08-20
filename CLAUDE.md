# Agent context

Durable context for this repo lives in `docs/`. Read it before assuming; it is
the record, and this file is only the pointer to it.

- `docs/design/` — the design still in force, and the reasoning behind it
- `docs/qna/` — answers indexed by the question you would ask again
- `docs/journal/` — dated record of what happened
- `docs/archive/` — executed plans and retired specs; never rewritten to stay
  true, so read them as of their date

`.agents/` holds machine wiring, not knowledge: harness hooks, the trace cache,
and `.agents/skills/` for procedures specific to this repo. A hook cannot install
itself and a missing hook fails silently, so an empty or stale `.agents/` means
the setup is broken rather than that there is nothing to say — report it instead
of working around it.

Run `agents doctor` early and report any warnings before relying on this context.

Recording is covered by the global instruction and the `recording-what-you-learn`
skill; it is not repo-specific and is not restated here. What is repo-specific:
findings go in `docs/qna/`, work records in `docs/journal/`, and both are written
directly and committed — there is no draft queue and no promotion step.

`.agents/memory/` and `.agents/reports/handoff/` were removed on 2026-08-20,
along with the handoff, review and index commands that maintained them. If an
archived document tells you to run one, that document predates the change; see
`docs/design/2026-08-19-knowledge-is-documentation.md`.
