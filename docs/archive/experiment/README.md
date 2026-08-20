# Capture experiment harness — retired

The two-arm scenario harness behind
[the capture instruction experiment](../analysis/2026-08-12-capture-instruction-experiment.md),
which measured spec 7 §3a: treatment drafted 5 of 7 sessions and all 5 were
promoted; control drafted 0 of 3.

**These scripts no longer run.** They invoke `agents handoff draft` and
`agents review`, removed on 2026-08-20 with the apparatus they measured. They
are kept, unmodified, because the result is load-bearing for
[knowledge is documentation](../../design/2026-08-19-knowledge-is-documentation.md)
— it is the evidence that an instruction alone causes drafting — and a result
whose method cannot be inspected is worth less.

Rewriting them to use the current commands would produce a harness that never
ran the experiment it claims to document.
