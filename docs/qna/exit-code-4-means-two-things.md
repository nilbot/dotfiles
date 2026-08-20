# Why do `agents` and `bootstrap` disagree about exit code 4?

## Context

Both binaries print an exit-code table in their usage, and the two tables are
not the same:

| code | `agents` | `bootstrap` |
|---|---|---|
| 0 | ok | ok |
| 1 | advisory | advisory |
| 2 | block | block |
| 3 | malformed | malformed input |
| 4 | **skip** | **not applicable** |
| 5 | could not complete the operation | *absent* |

Code 4 carries two different meanings, and `bootstrap` has no 5 at all.

## Answer

**Reconcile against spec 1, which defines the shared table — not against either
binary.** Both drifted from it independently, so agreeing with one is not
evidence of being right.

The substantive half is code 5. `bootstrap` has two sites that genuinely mean *I
tried and could not*: a profile phase that fails, and a failed migration. Today
both collapse into 2 (block), which tells a caller to stop but not whether
stopping was a refusal or a failure. Adding 5 to `bootstrap` is the likely
answer, but it changes what existing callers see and has not been done.

`agents` "skip" and `bootstrap` "not applicable" appear to be the same idea
under two names. Confirm that before unifying the wording — if they are the same,
the table is one edit; if they are not, one of the two binaries has a code
meaning something spec 1 never defined, which is the more interesting outcome.

**Open.** This is a known seam, recorded so the next person does not rediscover
it by being confused at a call site.
