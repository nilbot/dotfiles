package main

// rootCommand assembles the declarations. This is the one central artifact in
// the design; everything else about a command is declared beside its
// implementation and rendered from here. Nothing writes prose into this file
// beyond the declarations themselves.
func rootCommand() *Command {
	return &Command{Name: "agents", Usage: "agents <command> [flags]", Sub: []*Command{
		{
			// help is a command like any other rather than a special case in
			// dispatch, so its own usage line lives where every other one does.
			// `agents help --all` was advertised by a hardcoded line in the
			// renderer and by nothing else; that is the shape of text living
			// outside the tree that this design is meant to end.
			Name: "help", Summary: "print the listing, or one command's page",
			Usage:    "agents help [<command> [<subcommand>...]] [--all]",
			Detail:   "Prints the command listing, or one command's own page at any depth -- `agents help trace cache prune` reaches the leaf. --all adds the commands only git and harnesses invoke, which the listing a person reads leaves out. --help and -h anywhere in an invocation mean the same as `agents help` for the command path in front of them.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runHelp(a, io.Out) },
		},
		{
			Name: "init", Summary: "create .agents/, triggers, wiring, fleet entry",
			Usage:    "agents init [--local]",
			Detail:   "Scaffolds .agents/, writes harness wiring, and registers this repository in the machine-local fleet. Prints the remaining trust steps and exits 1 (advisory) so the state is visible rather than assumed. --local keeps .agents/ git-ignored.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runInit(a, io.Out) },
		},
		{
			Name: "wire", Summary: "regenerate harness configs (merges, never overwrites)",
			Usage:    "agents wire",
			Detail:   "Regenerates the generated harness configuration for every harness this repository is wired for. Merges into files that hold unrelated settings rather than owning them.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runWire(a, io.Out) },
		},
		{
			Name: "doctor", Summary: "report wiring, trust evidence, reachability, and lane health",
			Usage:    "agents doctor",
			Detail:   "Reports what is wired, what the harnesses trust, which pointers are reachable, and how healthy each lane is. Observes; never changes state. Exits 1 when any check is advisory.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runDoctor(a, io.Out) },
		},
		{
			Name: "index", Summary: "regenerate memory and handoff indexes",
			Usage:    "agents index",
			Detail:   "Regenerates .agents/memory/INDEX.md and .agents/reports/handoff/INDEX.md from the frontmatter of the files they describe. A pure function of tracked content.",
			Audience: []Audience{Human, Agent, CI},
			Run:      func(a []string, io IO) int { return runIndex(a, io.Out) },
		},
		{
			Name: "save", Summary: "commit .agents/ paths and nothing else (escape hatch)",
			Usage:    "agents save [-m msg]",
			Detail:   "Commits .agents/ paths and nothing else. An escape hatch for a hand-edited memory entry or a batch of promotions -- the normal path is `agents review --keep`, which commits as part of promoting.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runSave(a, io.Out) },
		},
		{
			Name: "handoff", Summary: "lane-scoped handoff management",
			Usage:    "agents handoff write|draft|prune",
			Detail:   "Writes, queues and prunes lane-scoped handoff notes. `draft` queues an unreviewed note outside the tracked tree; `write` writes a reviewed one into it.",
			Audience: []Audience{Human, Agent},
			Sub: []*Command{
				{
					Name: "write", Summary: "write a reviewed note into the tracked tree",
					Usage:    "agents handoff write --lane <name> --session <id>",
					Detail:   "Reads the note body on stdin and writes it into .agents/reports/handoff/<lane>/. --session is required: it is what keeps concurrent agents on one branch from clobbering each other.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runHandoffWrite(a, io.In, io.Out) },
				},
				{
					Name: "draft", Summary: "queue an unreviewed note outside the tracked tree",
					Usage:    "agents handoff draft --lane <name> --session <id>",
					Detail:   "Reads the note body on stdin and queues it in the machine-local store. Drafts are untracked until `agents review --keep` promotes one, so drafting costs nothing and commits you to nothing.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runHandoffDraft(a, io.In, io.Out) },
				},
				{
					Name: "prune", Summary: "bound the number of notes per lane",
					Usage:    "agents handoff prune --keep <n>",
					Detail:   "Removes the oldest tracked handoff notes for a lane, keeping the most recent n.",
					Audience: []Audience{Human},
					Run:      func(a []string, io IO) int { return runHandoffPrune(a, io.Out) },
				},
			},
		},
		{
			Name: "review", Summary: "read pending drafts; promote one, or bin it",
			Usage:    "agents review [--show|--keep|--bin|--edit <id>] [--stats]",
			Detail:   "Lists pending drafts, prints one, or promotes it. --keep writes the note into .agents/, regenerates the affected index, and commits, in one act. There is deliberately no --keep --all: promotion is where a human decides.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runReview(a, io.Out) },
		},
		{
			Name: "trace", Summary: "query records; read one back; copy reachable ones",
			Usage:    "agents trace ls|show|cache|migrate",
			Detail:   "Reads the machine-local trace store: the pointer index, the transcript cache, and the migration from the retired tracked location.",
			Audience: []Audience{Human, Agent},
			Sub: []*Command{
				{
					Name: "ls", Summary: "query records",
					Usage:    "agents trace ls [--lane <n>] [--since <d>] [--machine <m>]",
					Detail:   "Filters the trace index mechanically by lane, module, machine, harness and time. Choosing among the survivors is semantic and falls back to matching on description.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runTraceLS(a, io.Out) },
				},
				{
					Name: "show", Summary: "read one transcript back",
					Usage:    "agents trace show <id> [--path]",
					Detail:   "Resolves a record to content: the harness's own copy if it still exists, otherwise ours. Reports on stderr which one answered. Exits 5 when neither holds it, and 3 when the id prefix is ambiguous.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runTraceShow(a, io.Out) },
				},
				{
					Name: "cache", Summary: "copy reachable transcripts into the store",
					Usage:    "agents trace cache [--lane <n>] [--since <d>]",
					Detail:   "Copies transcripts that are still on disk into the machine-local cache. The subagent-stop hook already does this at the earliest moment a finished transcript exists; this is the manual sweep.",
					Audience: []Audience{Human},
					Run:      func(a []string, io IO) int { return runTraceCache(a, io.Out) },
					Sub: []*Command{
						{
							Name: "prune", Summary: "remove cached copies, never the records",
							Usage:    "agents trace cache prune --lane <name> | --retention",
							Detail:   "Removes cached transcript copies. The index is never touched: it is the record that a transcript existed at all. Dry run unless --yes. Prunability is never inferred from git -- a deleted branch is usually a merged one, and a throwaway worktree is often where the interesting work happened.",
							Audience: []Audience{Human},
							Run:      func(a []string, io IO) int { return runTraceCachePrune(a, io.Out) },
						},
					},
				},
				{
					Name: "migrate", Summary: "move a tracked index into the machine-local store",
					Usage:    "agents trace migrate [--yes]",
					Detail:   "Copies a tracked trace index into the machine-local store, unstages it, and drops the merge=union attribute. Dry run unless --yes.",
					Audience: []Audience{Human},
					Run:      func(a []string, io IO) int { return runTraceMigrate(a, io.Out) },
				},
			},
		},
		{
			Name: "ls", Summary: "list the fleet on this machine",
			Usage:    "agents ls [--prune]",
			Detail:   "Lists every repository registered on this machine and reports drift in both directions. Drift is normal news, not an error. --prune forgets only entries confirmed missing.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runFleetLS(a, io.Out) },
		},
		{
			Name: "update", Summary: "rewire every registered repo (dry run by default)",
			Usage:    "agents update --all [--apply]",
			Detail:   "Regenerates harness wiring across the whole fleet. Dry run unless --apply, because this touches many repositories at once.",
			Audience: []Audience{Human},
			Run:      func(a []string, io IO) int { return runFleetUpdate(a, io.Out) },
		},
		{
			Name: "guard", Summary: "pre-commit checks (the only command that blocks)",
			Usage:    "agents guard --staged",
			Detail:   "Scans staged .agents/ content for secrets, regenerates the generated indexes and compares them byte-for-byte, and warns on a commit mixing agent context with code. Invoked automatically on every pre-commit; main.go maps its advisory result to success so a warning does not abort the commit.",
			Audience: []Audience{Git, CI},
			Run:      func(a []string, io IO) int { return runGuard(a, io.Out) },
		},
		{
			Name: "hook", Summary: "harness hook entrypoint",
			Usage:    "agents hook <event> --harness <name>",
			Detail:   "Records one harness lifecycle event. Reads the payload on stdin and writes diagnostics to stderr, because the harness consumes stdout. Exits 0 on every path: a failed record must never disrupt a dispatch.",
			Audience: []Audience{Harness},
			Run:      func(a []string, io IO) int { return runHook(a, io.In, io.Err) },
		},
	}}
}
