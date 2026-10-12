# Implementation Plans

Step-by-step implementation plans for features, refactors, and migrations.

`YYYY-MM-DD-<slug>-plan.md`, flat. The date sorts it; the slug says what it implements.

Plans follow the `writing-plans` workflow, breaking approved specifications from [`../design/`](../design/) into discrete, test-driven, verifiable tasks.

**Every plan opens with a `**Status:**` line.** Without one, a plan whose work
finished years ago and a plan someone is about to execute read the same, and this
is the store a reader comes to for what to do next. Say `active`, or
`executed <date>`, or `superseded <date> by <what>`. A plan whose subject was
deleted says that too, because the reader's next question is whether the thing it
describes exists.

Some of the dated plans here open with "REQUIRED SUB-SKILL: Use
`superpowers:subagent-driven-development`". That is the authoring harness's
procedure, not this repository's: nothing here defines it, and a reader without
that harness should follow the plan's own steps and ignore the line.
