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
			Usage:    "agents help [<command> [<subcommand>...]] [--all]\nagents help --render=markdown",
			Detail:   "Prints the command listing, or one command's own page at any depth -- `agents help trace cache prune` reaches the leaf. --all adds the commands only git and harnesses invoke, which the listing a person reads leaves out. --help and -h anywhere in an invocation mean the same as `agents help` for the command path in front of them. --render=markdown emits the whole surface as a markdown table for the generated README block, and takes no command path.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runHelp(a, io.Out) },
		},
		{
			Name: "init", Summary: "create .agents/, triggers, wiring, fleet entry",
			Usage:    "agents init [--local] [--template <p>]\n            [--stores <role=path>] [--archive <path>]",
			Detail:   "Scaffolds .agents/, writes harness wiring, and registers this repository in the machine-local fleet. Prints the remaining trust steps and exits 1 (advisory) so the state is visible rather than assumed. --local keeps .agents/ git-ignored. --template, --stores, and --archive create an agents.layout/v2 layout instead of the implicit v1 one: --template code-repo expands to docs/{design,plans,journal,qna}, --template content-vault expands to .context/{design,plans,journal,qna}, and --template custom (or no --template) takes no defaults; --stores role=path overrides one role on top of the defaults, is repeatable, and with no template must name all four roles; --archive names the repository-relative archive to record, and with no --archive the manifest inherits docs/archive when that directory exists. The template name is a creation input and is never written to the manifest. A repository that already has a v1 layout (AGENTS.md or docs/) is refused, because adopting it is `agents layout migrate`'s job, and --local with any layout flag is refused.",
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
			Usage:    "agents doctor\nagents doctor [--lane-window <d>] [--lane-modules <n>] [--lane-days <n>]\n              [--lane-sessions <n>] [--recording-freshness <d>]",
			Detail:   "Reports what is wired, what the harnesses trust, which pointers are reachable, and how healthy each lane is. Observes; never changes state. Exits 1 when any check is advisory.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runDoctor(a, io.Out) },
		},
		{
			Name: "drift", Summary: "inspect context layout and router drift",
			Usage:    "agents drift [--json] [--repo <path>] [--all]",
			Detail:   "Inspects the repository or fleet for context layout drift, canonical router diffs, domain context, skills, and misplaced documentation. Non-mutating.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runDrift(a, io.Out) },
		},
		{
			Name: "layout", Summary: "inspect the resolved layout, or migrate v1 to v2",
			Usage:    "agents layout show|validate|path|migrate",
			Detail:   "Reads .agents/layout.json and reports the layout this repository resolves to. A repository with no manifest resolves to the implicit v1 layout, whose four stores are under docs/. `show`, `validate`, and `path` are read-only: nothing they do creates a store, writes a file, or moves a document. `migrate` is the family's one mutation -- it moves the v1 stores to the v2 paths with `git mv`, writes the manifest, and replaces the router.",
			Audience: []Audience{Human, Agent},
			Sub: []*Command{
				{
					Name: "show", Summary: "print the resolved layout, or the canonical router",
					Usage:    "agents layout show [--json] [--router]",
					Detail:   "Prints the schema, status, min_mut_ver_floor, stores, archive, and one line per role. --json emits the normalized layout object, whose stores are the resolved role-to-path map rather than the manifest's spelling, and carries any problems in the object. --router prints the canonical router for the resolved layout and nothing else, so the migration skill can restore AGENTS.md byte-for-byte; a manifest that exists but did not resolve prints nothing and exits 1, so a caller must capture the bytes before writing them. An invalid manifest exits 1: the human report prints one line per problem before it, and --json stays one parseable object.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runLayoutShow(a, io.Out) },
				},
				{
					Name: "validate", Summary: "check the layout against every validation rule",
					Usage:    "agents layout validate [--json]",
					Detail:   "Runs the layout validation rules and prints one line per problem. Exits 0 for a valid layout this binary may mutate; 1 when the layout is invalid or this binary may not mutate it; 4 outside a repository with .agents/. --json emits one object with manifest_path, problems, supported, reason, schema, and layout_status: supported is false when the layout has problems or the version gate refuses, and reason names which.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runLayoutValidate(a, io.Out) },
				},
				{
					Name: "path", Summary: "print one store path by role",
					Usage:    "agents layout path <role>",
					Detail:   "Prints the repository-relative path the role resolves to and nothing else, so a skill can use it in a command substitution. Roles are design, plans, journal, and qna. A missing operand or an unknown role is malformed (exit 3); an unsupported, invalid, or migrating layout prints nothing and exits 4 or 1.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runLayoutPath(a, io.Out) },
				},
				{
					Name: "migrate", Summary: "plan, apply, resume, or abort a v1 to v2 migration",
					Usage: "agents layout migrate [--template <p>] [--stores <role=path> ...]\n" +
						"                       [--archive <path>]\n" +
						"                       [--dry-run | --apply | --resume --apply | --abort --apply]\n" +
						"                       [--backup-tag <name>] [--json]",
					Detail:   "Plans a v1 to v2 migration and applies, resumes, or aborts it. Dry run by default, and --dry-run is an accepted explicit synonym; --apply performs it and requires --backup-tag <name>, an annotated tag created at HEAD before the first write, so the rollback point exists even without a branch -- and a name that already exists or is not a valid ref is malformed, as is --backup-tag with --resume or --abort, where it cannot take effect. --template supplies the target store map from a template's defaults and --stores <role=path> overrides one role; with no template, --stores must name all four roles. The plan is refused, naming each reason, when the router is diverged or missing, a source store is missing or a symlink, a target path already exists, docs/ holds anything but the four stores and the declared archive, or this binary is below the target's min_mut_ver_floor. Planning also requires a clean working tree, no merge, rebase, cherry-pick, revert, am, or bisect in progress, and a branch that is not master or main. A `migrating` manifest is never re-planned: --resume --apply continues the journal it froze and --abort --apply deletes it while nothing can have moved, and every other invocation refuses, naming the phase and the remedy. --json emits exactly one object on every path: the plan, whose phase is planned for a dry run or a fresh --apply and the journal phase a --resume continued from, or a refusal object with repo, dry_run, phase, and the same error sentence the human surface prints when the refusal precedes a plan. Exits 0 when applied with no link candidates or when --abort completed, 1 for a dry-run plan, blockers, remaining link candidates, or a refused abort, 3 for malformed flags, 4 outside a repository with .agents/, and 5 when apply failed mid-way with the manifest left migrating for the next resume.",
					Audience: []Audience{Human, Agent},
					Run:      func(a []string, io IO) int { return runLayoutMigrate(a, io.Out) },
				},
			},
		},
		{
			Name: "save", Summary: "commit .agents/ paths and nothing else (escape hatch)",
			Usage:    "agents save [-m msg]",
			Detail:   "Commits .agents/ paths and nothing else, so machine wiring never rides along in a code commit. Knowledge is documentation and lives in docs/, committed normally.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runSave(a, io.Out) },
		},
		{
			Name: "trace", Summary: "query records; read one back; copy reachable ones",
			Usage:    "agents trace ls|show|cache|migrate",
			Detail:   "Reads the machine-local trace store: the pointer index, the transcript cache, and the migration from the retired tracked location.",
			Audience: []Audience{Human, Agent},
			Sub: []*Command{
				{
					Name: "ls", Summary: "query records",
					Usage:    "agents trace ls [--lane <n>] [--module <p>] [--machine <m>] [--harness <h>]\n                [--event <e>] [--grep <s>] [--limit <n>] [--since <d>]",
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
							Usage:    "agents trace cache prune --lane <name> [--yes]\nagents trace cache prune --retention [--age <d>] [--size <bytes>] [--yes]",
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
			Name: "version", Summary: "print binary version and build provenance",
			Usage:    "agents version",
			Detail:   "Prints binary version, git commit, and build timestamp.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runVersion(a, io.Out) },
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
			Usage:    "agents hook <event> --harness <name> [--lane <n>]",
			Detail:   "Records one harness lifecycle event. Reads the payload on stdin and writes diagnostics to stderr, because the harness consumes stdout. Exits 0 on every path: a failed record must never disrupt a dispatch.",
			Audience: []Audience{Harness},
			Run:      func(a []string, io IO) int { return runHook(a, io.In, io.Err) },
		},
	}}
}
