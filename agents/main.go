// Command agents maintains repo-tracked agent context: the .agents/ directory,
// the harness wiring that feeds it, and the git hooks that guard it.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	// --help and -h ask for help about the command path in front of them, at any
	// depth. Intercepting only args[0] made the flag a top-level idiom: `agents
	// trace --help` answered `unknown subcommand "--help"` and `agents doctor
	// --help` fell through to the flag package's own dump, both exit 3. `agents
	// help` itself needs no interception -- it is a command in the tree.
	if i := helpFlagIndex(args); i >= 0 {
		return runHelp(commandPathPrefix(args[:i]), os.Stdout)
	}
	cmd, rest := root.Find(args)
	if cmd == nil {
		fmt.Fprintf(os.Stderr, "agents: unknown command %q\n", args[0])
		RenderUsage(root, os.Stderr, false)
		return exitcode.Malformed
	}
	if cmd.Run == nil {
		// stderr, like the two clauses above it. All four are the same event --
		// nothing ran, because the invocation named nothing runnable -- and the
		// old code split them across two streams only because the handlers
		// these clauses replaced happened to print to stdout. A caller piping
		// `agents trace` somewhere got the complaint in the pipe.
		if len(rest) > 0 {
			fmt.Fprintf(os.Stderr, "agents %s: unknown subcommand %q\n", cmd.Name, rest[0])
		} else {
			fmt.Fprintf(os.Stderr, "usage: %s\n", cmd.Usage)
		}
		return exitcode.Malformed
	}
	return cmd.Run(rest, IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

func helpFlagIndex(args []string) int {
	for i, a := range args {
		if a == "--help" || a == "-h" {
			return i
		}
	}
	return -1
}

// commandPathPrefix keeps the leading command tokens and drops the first flag
// and everything after it, so `agents trace cache prune --lane x --help` still
// resolves to the leaf rather than to a path with --lane in it.
func commandPathPrefix(args []string) []string {
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			return args[:i]
		}
	}
	return args
}
