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

	// Exit advisory, not OK. Wiring is written but not yet live, and reporting
	// success for a setup that is not recording anything would be the exact
	// silent failure this design exists to prevent.
	fmt.Fprintln(stdout, "\nRemaining trust steps (a hook cannot install itself):")
	for _, a := range harness.All() {
		for _, s := range a.TrustSteps(rc.Root) {
			fmt.Fprintf(stdout, "  - %s\n", s)
		}
	}
	fmt.Fprintln(stdout, "\nTo confirm the setup is recording, check Codex `/hooks` for Active hooks, or look for traces in `.agents/reports/traces/`.")
	return exitcode.Advisory
}

// binaryPath is the absolute path to write into generated configs. A harness
// runs hooks with an environment that is not the user's shell, so a bare
// "agents" is not reliably resolvable.
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
