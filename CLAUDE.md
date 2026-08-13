# Agent context

Durable context for this repo lives in `.agents/`. Read it before assuming;
it is the record, and this file is only the pointer to it.

- `.agents/memory/INDEX.md` — curated knowledge about this codebase (generated)
- `.agents/reports/handoff/INDEX.md` — work in flight, by lane (generated)
- `.agents/reports/` — specs, plans, analysis, and trace pointers
- `.agents/skills/` — procedures specific to this repo

A hook cannot install itself and a missing hook fails silently: an empty or
stale `.agents/` means the setup is broken, not that there is nothing to
say -- report it rather than working around it.

Run `agents doctor` early and report any warnings before relying on this context.

When a stretch of work concludes — a bug understood, a decision made, an approach abandoned — record it before moving on: at most three bullets, covering what a future agent could not get from the code or the git log. Write it with `agents handoff draft --lane <lane> --session <id>`. Drafts are untracked until you review them, so drafting costs nothing and commits you to nothing.

Review what has been drafted with `agents review`; promoting one writes it
into `.agents/` and commits it in the same act.
