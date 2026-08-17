package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// exitCodeMeanings is the single description of the shared vocabulary. Help
// renders from it rather than restating it, which is what stops the four
// descriptions of this one table from drifting -- spec 1 §6, these comments,
// and each binary's help text disagreed on code 4 before this existed.
var exitCodeMeanings = []struct {
	Code int
	Text string
}{
	{exitcode.OK, "ok"},
	{exitcode.Advisory, "advisory -- finished, but read the output"},
	{exitcode.Block, "block -- the only code that stops work"},
	{exitcode.Malformed, "malformed input"},
	{exitcode.Skip, "skip -- not applicable here"},
	{exitcode.NoRecord, "could not complete the operation"},
}

func RenderExitCodes(w io.Writer) {
	fmt.Fprint(w, "exit codes:\n")
	for _, m := range exitCodeMeanings {
		fmt.Fprintf(w, "  %d  %s\n", m.Code, m.Text)
	}
}

// usagePrefix is what a usage line is introduced by, and how far a second form
// has to be indented to line up under the first.
const usagePrefix = "usage: "

// usageBlock renders a command's declared usage, which may hold more than one
// form. A command whose flags do not all combine -- `trace cache prune --lane`
// and `--retention` are separate operations, and half of `review`'s flags only
// mean anything with --stats -- documents each form on its own line, and the
// continuation lines align under the first.
func usageBlock(u string) string {
	lines := strings.Split(u, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = strings.Repeat(" ", len(usagePrefix)) + lines[i]
	}
	return usagePrefix + strings.Join(lines, "\n")
}

// usageSynopsis is the first form only. The top-level listing gives each
// command one row, so it shows the common invocation and `agents help <cmd>`
// gives the rest; without this a command with three forms would either lose
// two of them or stretch the column past a terminal's width.
func usageSynopsis(u string) string {
	first, _, _ := strings.Cut(u, "\n")
	return first
}

// usageFor renders the usage line a handler prints when it was called wrongly,
// out of the same declaration `agents help` reads.
//
// Handlers used to carry their own copy of that line, and by the time the tree
// existed four of the six had already drifted from it: `agents review` accepted
// --lane, --stats and --since while printing none of them, and `trace cache
// prune` advertised [--yes] where the tree said | --retention. The handler's
// copy is the one that actually prints, so the divergence was invisible from
// the help surface -- exactly the failure the registry was built to make
// impossible. TestNoSourceFileRestatesAUsageLine keeps the copies from coming
// back, and TestEveryUsageForCallSiteNamesARealCommand keeps a call site from
// naming a path the tree does not have.
func usageFor(path ...string) string {
	cmd, rest := rootCommand().Find(path)
	if cmd == nil || len(rest) > 0 {
		// Deliberately not a plausible line assembled from the arguments: a
		// path the tree does not have is a bug in the caller, and inventing
		// usage text for it would be the divergence this function exists to
		// end, printed by the function that ended it.
		return fmt.Sprintf("usage: (bug: %q names no command)", strings.Join(path, " "))
	}
	return usageBlock(cmd.Usage)
}

// runHelp answers `agents help`, `agents help <path>`, and `--help` at any
// depth. It exits 0 -- an explicit request for help is not a usage error, which
// is the one way it differs from a bare `agents`.
//
// Everything it prints goes to the single writer it was handed, including the
// complaint about an unknown path; dispatch hands it stdout. That matches every
// other handler in this binary -- `agents guard`, `agents review foo`, `agents
// trace show` all print their usage errors to stdout too. It does NOT match
// dispatch's own usage errors in main.go, which go to stderr. That split is
// real, it is wider than this function, and settling it means threading a
// second writer through every handler; it is carried as its own task rather
// than half-fixed here.
func runHelp(args []string, w io.Writer) int {
	root := rootCommand()

	all, markdown := false, false
	var path []string
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		case "--render=markdown":
			markdown = true
		default:
			path = append(path, a)
		}
	}

	if markdown {
		// The table is the whole surface by definition, so a command path or
		// --all alongside it asks for something it cannot answer. Refusing
		// beats ignoring: `agents help trace --render=markdown` used to render
		// the entire tree and say nothing about the path it had dropped.
		if len(path) > 0 || all {
			fmt.Fprintln(w, "agents help: --render=markdown renders the whole command surface; it takes no command path and no --all")
			return exitcode.Malformed
		}
		RenderMarkdown(root, w)
		return exitcode.OK
	}

	if len(path) == 0 {
		RenderUsage(root, w, all)
		return exitcode.OK
	}

	cmd, rest := root.Find(path)
	if cmd == nil || len(rest) > 0 {
		fmt.Fprintf(w, "agents help: no such command %q\n", strings.Join(path, " "))
		return exitcode.Malformed
	}
	RenderHelp(cmd, path, w, all)
	return exitcode.OK
}

// RenderMarkdown emits the whole command surface for the generated README
// block. It writes to stdout rather than to the file: `agents save` commits
// .agents/ paths and nothing else, and the mixed-commit guard exists to keep
// repository content and agent context in separate commits, so teaching this
// binary to write a tracked file outside .agents/ would put those two
// mechanisms in tension.
func RenderMarkdown(root *Command, w io.Writer) {
	fmt.Fprint(w, "| Command | What |\n|---|---|\n")
	root.Walk(func(path []string, c *Command) {
		fmt.Fprintf(w, "| `agents %s` | %s |\n", strings.Join(path, " "), c.Summary)
	})
}
