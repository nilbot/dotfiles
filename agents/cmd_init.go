package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// runInit scaffolds a repository: the .agents/ tree, the four documentation
// stores, the router pair, the bundled skill, and the harness wiring.
//
// It takes one flag. There is no layout to choose: this tool writes the fixed
// set of stores under docs/ and nothing else, so an invocation cannot describe
// a repository shape that a later command would have to interpret. That is the
// whole of what "one layout" buys -- the manifest, its validation, its version
// gate and its migration are all gone with the choice.
//
// Exit is advisory, not OK. Wiring is written but not yet live -- a hook cannot
// install itself, and a trust prompt no process can answer still stands between
// this run and a wired repository. Reporting success here would be the silent
// failure this tool exists to prevent.
func runInit(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stdout)
	local := fs.Bool("local", false, "keep .agents/ out of the repository")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents init: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents init: not inside a git repository; nothing to do")
		return exitcode.Skip
	}

	if err := scaffold.Create(rc.Root, *local); err != nil {
		fmt.Fprintf(stdout, "agents init: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintf(stdout, "initialized %s\n", repo.AgentsDir(rc.Root))

	if code := wireAll(rc.Root, stdout); code != exitcode.OK {
		return code
	}

	fmt.Fprintln(stdout, "\nRemaining trust steps (a hook cannot install itself):")
	for _, a := range harness.All() {
		for _, step := range a.TrustSteps(rc.Root) {
			fmt.Fprintf(stdout, "  - %s\n", step)
		}
	}
	return exitcode.Advisory
}

// binaryPath is the running executable's own path. The doctor's binary check
// needs it: it proves the `agents` on PATH and the `agents` that is running are
// the same file. Nothing in the wiring path uses it -- `wire` removes entries by
// reading the config's text, not by naming a binary -- so a removal no longer
// fails on a machine where the executable cannot be resolved.
func binaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

// wireAll removes this tool's entries from every harness config and reports
// what each run actually did.
//
// It reports what the run did, never what it was expected to do. The two are no
// longer the same statement: `wire` removes this tool's entries and writes none,
// so on a repository that never carried any -- every freshly initialized one --
// the run touches no config at all.
func wireAll(root string, stdout io.Writer) int {
	for _, a := range harness.All() {
		res, err := a.Wire(root)
		if err != nil {
			fmt.Fprintf(stdout, "agents: wiring %s: %v\n", a.Name(), err)
			return exitcode.NoRecord
		}
		switch {
		case res.Removed == 0:
			fmt.Fprintf(stdout, "%s: no entries of this tool's to remove\n", a.Name())
		case res.ConfigRemoved:
			fmt.Fprintf(stdout, "%s: removed %d entr%s and the config that held them\n",
				a.Name(), res.Removed, plural(res.Removed, "y", "ies"))
		default:
			fmt.Fprintf(stdout, "%s: removed %d entr%s, kept the rest of %s\n",
				a.Name(), res.Removed, plural(res.Removed, "y", "ies"),
				relPath(root, a.WireConfigPath(root)))
		}
	}
	return exitcode.OK
}

// plural picks the suffix for a count, so the line reads as prose at 1 and at n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// relPath shortens a path to the repository it is in, for output a reader
// compares against what they see in their own checkout.
func relPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
