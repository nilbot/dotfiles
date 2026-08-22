# Do vendors' built-in knowledge subsystems actually get used?

## Context

The [2026-08-19 redesign](../design/2026-08-19-knowledge-is-documentation.md)
retired `.agents/memory/` after it produced two entries in a week while the same
operator's other repository, with no tooling at all, produced nine Q&As, six
session logs and a lessons index. Its finding was that capture was never the
bottleneck — content was, and the mechanism only looked responsible.

That argument had one weakness: `.agents/memory/` was *our* mechanism. Perhaps it
starved because it was badly built.

Antigravity supplies the control. It ships a full Knowledge Items system — a
directory-backed markdown store with frontmatter, a KI Creation Workflow prompt,
a consolidation tool, deletion rules, a filesystem watcher, and automatic
injection into context (*"Here are the N most recently accessed knowledge items
from your knowledge base"*, *"The following knowledge items reference the listed
resources you accessed in the last turn"*). It is built by a vendor with
resources we do not have, and it is on by default.

Measured 2026-08-22, across all three Antigravity installs on this machine:

```
~/.gemini/antigravity-cli/knowledge/   → knowledge.lock, nothing else
~/.gemini/antigravity/knowledge/       → knowledge.lock, nothing else
~/.gemini/antigravity-ide/knowledge/   → knowledge.lock, nothing else
```

`~/.gemini/antigravity/` holds **96 conversations** and 96 brain directories over
roughly nine months. The knowledge store is empty.

## Answer

**No — and this is an independent replication of the redesign's central finding,
with a different vendor's mechanism as the control.**

Over the same period, driven by the same operator, in the repository those
sessions were working on, a ~60-line `.agents/AGENTS.md` produced nine entries in
`autogo-mlx/docs/qna/`. The instruction is unremarkable — it says to write a Q&A
when the human says *"wow good to know, can you save it to QnA?"*, gives a
kebab-case naming rule and a four-line template. That is the entire mechanism,
and it beat a vendor subsystem with automatic retrieval by nine to zero.

`.agents/memory/` did not starve because it was badly built. It starved because
directing model output into a dedicated store is the wrong shape for the problem,
and a better-resourced team building the same shape got the same result.

**What follows.** Two things, one narrow and one not.

Narrowly: when adopting a repository for Antigravity, there is nothing to
migrate *from* and nothing to integrate *with*. The KI store is not a competing
source of truth, because it has never held anything.

Broadly: it is the strongest available evidence for keeping knowledge as ordinary
committed markdown, and against ever rebuilding a private knowledge store —
including one that a future vendor makes tempting by shipping it. The retrieval
axis that works is `grep` over files a human asked for. See also
[what shapes does knowledge actually take](what-shapes-does-knowledge-actually-take.md).

One caution: an empty store is consistent with the operator never having been
shown the feature, and neither install's onboarding state was checked. That would
explain zero KIs without saying anything about the shape. It would not explain
the nine, which came from an instruction the same operator wrote by hand for the
same work.
