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

Write handoffs with `agents handoff write`, not by hand. Commit `.agents/`
changes with `agents save` so they do not ride along with code changes.
