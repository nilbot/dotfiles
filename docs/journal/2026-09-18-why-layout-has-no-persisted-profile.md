# 2026-09-18 — Why the layout manifest has no persisted profile

Q3 asked how profiles should evolve: add, remove, or fork; open versus closed
enum; where the registry lives; and how reserved names are enforced. The review
first leaned toward an open, protobuf-style profile identity: `profile` in
`.agents/layout.json`, a versioned registry in the binary, writer-closed and
reader-open, reserved names forever. It ended by deleting the field.

## What forced the decision

The design could not name a concrete machine consumer for the persisted
profile:

- the bundled skills resolve paths through `agents layout path` and the
  manifest; neither mentions `profile`, `content-vault`, or `code-repo`;
- `CanonicalRouterDigestFor` returns one `V2AgentsMD` for every v2 layout;
- `doctor` has no profile-specific check;
- the only real consumer was creation-time defaults for `init` and `migrate`.

A persisted non-physical label would have required a versioned registry,
per-schema reserved sets, a warning channel, raw-JSON preservation when the
writer rewrites a manifest, and a profile migration path. That is real
machinery for a label that only supplied defaults.

## The decision

`profile` is removed from the manifest. `--template code-repo|content-vault`
is a CLI-only creation input; it expands to a default store map and is never
written to `.agents/layout.json`. No template (or `--template custom`) means
no defaults; `--stores role=path` must supply all four roles. The semantic
profile — content vault versus code repository, per-role collaboration policy,
vocabulary — lives in `.agents/AGENTS.md`.

## What the protobuf review contributed

The protobuf comparison remains useful if a persisted semantic label is needed
later. The accurate rules are:

- removing an enum entry is allowed; its number and name must be reserved;
- adding an enum value is safe; proto3 enums are open and preserve unknown
  values, while proto2 enums are closed and treat them as unknown fields;
- JSON serialization is affected by names. ProtoJSON does not generally
  propagate unknown fields, so a reader that preserves an unknown profile
  string is making a stronger guarantee than protobuf JSON. `layout.json` can
  make that guarantee because it is preserved text, not a generated codec;
- "never reuse" is a convention enforced by tests and migration, not a runtime
  invariant: an older binary can still write a retired name.

If a concrete machine-readable semantic label is required later, it should be
added with a new schema version and a migration, using the open-label model:
per-schema registry, writer-closed and reader-open, reserved names, warnings,
and raw-field preservation.

## Independent review note

Two compatibility reviewers were spawned for this decision but received only
environment context, not the task payload; their reports are absent. The
product/implementation reviewer reconstructed the objective and delivered a
report; its findings — no profile consumer, missing `profileDefaults`, missing
warnings channel, and an undefined plan task — are reflected in the design's
Q3 resolution and in the implementation plan. The review did not modify the
repository tree.
