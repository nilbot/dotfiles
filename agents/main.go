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
	root := rootCommand()
	if len(args) == 0 {
		// Still exit 3 on stderr: a bare invocation is a usage error, while an
		// explicit `agents help` is not. Same text, different disposition.
		RenderUsage(root, os.Stderr, false)
		return exitcode.Malformed
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return runHelp(args[1:], os.Stdout)
	}
	cmd, rest := root.Find(args)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		RenderUsage(root, os.Stderr, false)
		return exitcode.Malformed
	}
	if cmd.Run == nil {
		if len(rest) > 0 {
			fmt.Fprintf(os.Stdout, "agents %s: unknown subcommand %q\n", cmd.Name, rest[0])
		} else {
			fmt.Fprintf(os.Stdout, "usage: %s\n", cmd.Usage)
		}
		return exitcode.Malformed
	}
	return cmd.Run(rest, IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

// TEMPORARY: replaced in Task 6.
func runHelp(args []string, w io.Writer) int {
	RenderUsage(rootCommand(), w, false)
	return exitcode.OK
}
