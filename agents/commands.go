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
			Name: "init", Summary: "create .agents/, the doc stores, and the wiring",
			Usage:    "agents init [--local]",
			Detail:   "Creates the .agents/ tree, the four documentation stores under docs/, the router pair, the bundled skill, and the harness wiring. Prints the remaining trust steps and exits 1 (advisory), because wiring is written but not yet live. --local keeps .agents/ out of the repository, and is refused inside a linked worktree where info/exclude is shared with the main checkout.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runInit(a, io.Out) },
		},
		{
			Name: "wire", Summary: "remove this tool's entries from harness configs",
			Usage:    "agents wire",
			Detail:   "Removes from each harness config the hook entries this tool wrote in an earlier version, and installs the skills symlink each harness needs. It adds no hook entries of its own: nothing records harness lifecycle events any more. Every other setting in those files is preserved, and a config left holding nothing is removed.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runWire(a, io.Out) },
		},
		{
			Name: "doctor", Summary: "report wiring, trust, and scaffold state",
			Usage:    "agents doctor",
			Detail:   "Reports whether a harness config still holds an entry this tool wrote and no longer answers, whether the root AGENTS.md and its CLAUDE.md symlink are intact, what the harnesses trust, and the state of the git hook chain. Observes; never changes state. Exits 1 when any check is advisory.",
			Audience: []Audience{Human, Agent},
			Run:      func(a []string, io IO) int { return runDoctor(a, io.Out) },
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
			Detail:   "Scans staged .agents/ content for secrets, and warns on a commit mixing agent context with code. Invoked automatically on every pre-commit; main.go maps its advisory result to success so a warning does not abort the commit.",
			Audience: []Audience{Git, CI},
			Run:      func(a []string, io IO) int { return runGuard(a, io.Out) },
		},
	}}
}
