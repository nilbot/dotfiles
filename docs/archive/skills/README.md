# Archived skills

Skill text kept as evidence after the skill stopped being installed.

A skill lives in `.agents/skills/` while a harness is meant to load it. When one
stops being installed — because the work it described no longer exists, or
because nothing read it — its text moves here rather than being deleted, so the
reasoning it carried stays readable and its removal stays reviewable. Nothing in
this directory is loaded by anything.

- `migrating-fleet-context/` — removed 2026-09-21. It described migrating a
  repository between layout schemas (`agents layout migrate`, `.agents/layout.json`).
  The tool no longer has a layout schema: there is one fixed set of stores, so
  there is nothing left to migrate and no command it names still exists.
