// Command agents maintains repo-tracked agent context: the .agents/ directory,
// the harness wiring that feeds it, and the git hooks that guard it.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/githook"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func main() {
	if name := filepath.Base(os.Args[0]); githook.IsHookName(name) {
		os.Exit(runGitHook(name, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(run(os.Args[1:]))
}

func runGitHook(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "agents: git hook could not resolve the current directory")
		return exitcode.Malformed
	}
	repoHooksDir, err := repo.LegacyHooksPath(cwd)
	if err != nil {
		fmt.Fprintln(stderr, "agents: git hook could not resolve the repository hooks directory")
		return exitcode.Malformed
	}
	dispatcherPath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "agents: git hook could not resolve the dispatcher executable")
		return exitcode.Malformed
	}
	// DotfilesRoot(), not $HOME/dotfiles: githook treats a missing extras
	// directory as "no personal hooks" and carries on, so a binary that looked
	// for them under a checkout that is not this one would run none of them and
	// say nothing about it.
	chain := githook.Chain{
		RepoHooksDir:   repoHooksDir,
		ExtrasDir:      filepath.Join(DotfilesRoot(), "git", "hooks"),
		DispatcherPath: dispatcherPath,
	}
	if code := githook.Run(chain, name, args, stdin, stdout, stderr); code != 0 {
		return code
	}
	if name != "pre-commit" {
		return exitcode.OK
	}
	code := runGuard([]string{"--staged"}, stdout)
	if code == exitcode.OK || code == exitcode.Advisory {
		return exitcode.OK
	}
	return code
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return exitcode.Malformed
	}
	switch args[0] {
	case "hook":
		return runHook(args[1:], os.Stdin, os.Stderr)
	case "init":
		return runInit(args[1:], os.Stdout)
	case "wire":
		return runWire(args[1:], os.Stdout)
	case "trace":
		return runTrace(args[1:], os.Stdout)
	case "handoff":
		return runHandoff(args[1:], os.Stdin, os.Stdout)
	case "review":
		return runReview(args[1:], os.Stdout)
	case "index":
		return runIndex(args[1:], os.Stdout)
	case "save":
		return runSave(args[1:], os.Stdout)
	case "guard":
		return runGuard(args[1:], os.Stdout)
	case "ls":
		return runFleetLS(args[1:], os.Stdout)
	case "update":
		return runFleetUpdate(args[1:], os.Stdout)
	case "doctor":
		return runDoctor(args[1:], os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		usage()
		return exitcode.Malformed
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: agents <command> [flags]

  init [--local]              create .agents/, triggers, wiring, fleet entry
  wire                        regenerate harness configs (merges, never overwrites)
  doctor                      report wiring, trust evidence, reachability, and lane health
  index                       regenerate memory and handoff indexes
  save [-m msg]               commit .agents/ paths and nothing else (escape hatch)
  handoff write|draft|prune   write a reviewed note, queue an unreviewed one, prune
  review [--keep|--bin <id>]  read pending drafts; promote one, or bin it
  trace ls|show|cache         query records; read one back; copy reachable ones
  trace cache prune --lane    remove one lane's cached copies (never the records)
  trace cache prune --retention  evict by age and size
  trace migrate [--yes]       move a tracked index into the machine-local store
  ls [--prune]                list the fleet on this machine
  update --all [--apply]      rewire every registered repo (dry run by default)
  guard --staged              pre-commit checks (the only command that blocks)
  hook <event> --harness <n>  harness hook entrypoint

exit codes: 0 ok, 1 advisory, 2 block, 3 malformed, 4 skip,
            5 could not complete the operation
`)
}
