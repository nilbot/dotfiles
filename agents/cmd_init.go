package main

import (
	"flag"
	"fmt"
	"io"
	"os"

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

// binaryPath is the running executable's own path, which is what the harness
// config must name: a hook command that resolved to anything else would run a
// different binary than the one that wired it.
func binaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe, nil
}

func wireAll(root string, stdout io.Writer) int {
	bin, err := binaryPath()
	if err != nil {
		fmt.Fprintf(stdout, "agents: cannot resolve own path: %v\n", err)
		return exitcode.NoRecord
	}
	for _, a := range harness.All() {
		if err := a.Wire(root, bin); err != nil {
			fmt.Fprintf(stdout, "agents: wiring %s: %v\n", a.Name(), err)
			return exitcode.NoRecord
		}
		fmt.Fprintf(stdout, "wired %s -> %s\n", a.Name(), a.WireConfigPath(root))
	}
	return exitcode.OK
}
