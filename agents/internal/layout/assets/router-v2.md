# Agent context

Durable context for this repo is described by `.agents/layout.json`
(`agents.layout/v2`). Read that manifest before assuming where anything lives;
it names the meta stores and their paths. This file is only the pointer.

- `.agents/layout.json` — status and one path per role
- `agents layout show` — the same layout, resolved and validated (when the CLI is installed)
- `agents layout path <role>` — one store, by role: design, plans, journal, qna

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in `.agents/AGENTS.md`.
- Repo-specific procedures and skills are located in `.agents/skills/`.

## Machine Wiring
`.agents/` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently.
- If the `agents` CLI is installed, run `agents doctor` early and report any warnings before relying on this context.
- If `agents` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above.

Recording is covered by the global instruction and the `recording-what-you-learn`
skill; it is not repo-specific and is not restated here.
