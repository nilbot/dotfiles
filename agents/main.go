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

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if name := filepath.Base(os.Args[0]); githook.IsHookName(name) {
		os.Exit(runGitHookShim(name, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(run(os.Args[1:]))
}

// runHookChain runs one hook in the three parts the design names, in order: the
// repository's own hook of that name, then the executable personal stages
// `<anything>.<name>` under extrasDir, then the built-in stage. On pre-commit
// the built-in guard runs last, and its advisory result is mapped to success
// because a warning must not abort a commit.
//
// Both routes into this binary share this one function: the `githook`
// subcommand, which is handed the directory the chain record named, and the
// symlink shim, which has no directory to hand it. Keeping the order and the
// guard mapping in one place is what stops the two from drifting apart while
// both exist.
func runHookChain(name, extrasDir string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
	chain := githook.Chain{
		RepoHooksDir:   repoHooksDir,
		ExtrasDir:      extrasDir,
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

// runGitHookShim is the compatibility route for a machine still wired the old
// way: four symlinks in the hooks directory, each naming this binary, each
// invoking it under a hook's own name. It answers all four names -- a commit
// runs pre-commit AND commit-msg, so a shim that only recognised the bare
// pre-commit form leaves the second one failing every commit -- and it passes
// Git's arguments through untouched.
//
// What it cannot carry is the personal stages, and it has to say so. A symlink
// names a program and nothing else: the checkout used to arrive from the
// link-time stamp or from AGENTS_DOTFILES_ROOT, and both readers are gone.
// githook.Chain reads a missing extras directory as "no personal hooks" and
// returns 0, so a silent shim is a machine whose `<anything>.<hook>` stages
// stopped running with nothing anywhere to say it -- the failure class this
// redesign exists to remove. The notice is one line on stderr and never
// changes an exit code: the guard names still guard, and a stale banner must
// not fail a `git checkout`.
//
// Added in v0.8.0, with the generated entries that replace the symlinks.
// Remove no earlier than v0.9.0 (2027-04-09): an install that has re-run
// git/install-hooks.sh writes four entries, and those never reach this branch.
func runGitHookShim(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fmt.Fprintf(stderr,
		"agents: %s ran through a hook symlink, which names a program and no checkout, "+
			"so the personal stages under <checkout>/git/hooks are not running. "+
			"Re-run the installer to replace the four symlinks with chain entries: "+
			"bash <checkout>/git/install-hooks.sh install --adopt-owned <checkout> \"$HOME\" \"$(command -v agents)\"\n",
		name)
	return runHookChain(name, "", args, stdin, stdout, stderr)
}

func run(args []string) int {
	root := rootCommand()
	if len(args) == 0 {
		// Still exit 3 on stderr: a bare invocation is a usage error, while an
		// explicit `agents help` is not. Same text, different disposition.
		RenderUsage(root, os.Stderr, false)
		return exitcode.Malformed
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		return runVersion(nil, os.Stdout)
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
		// stderr, like the two clauses above it. All three are the same event --
		// nothing ran, because the invocation named nothing runnable -- and the
		// old code split them across two streams only because the handlers
		// these clauses replaced happened to print to stdout. A caller piping
		// `agents trace` somewhere got the complaint in the pipe.
		//
		// This unifies dispatch, not the binary: the handlers still print their
		// own usage errors to stdout, so `agents trace` reports on stderr while
		// `agents trace show` reports on stdout. Settling that means threading a
		// second writer through every handler and is carried as its own task.
		if len(rest) > 0 {
			fmt.Fprintf(os.Stderr, "agents %s: unknown subcommand %q\n", cmd.Name, rest[0])
		} else {
			fmt.Fprintln(os.Stderr, usageBlock(cmd.Usage))
		}
		return exitcode.Malformed
	}
	return cmd.Run(rest, IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

// helpFlagIndex finds --help or -h anywhere in the arguments, including in a
// position where it is really a flag's value: `agents save -m -h` prints help
// rather than committing with the message "-h". Every alternative rule is
// worse. Stopping the scan at the first flag breaks `agents trace cache prune
// --lane x --help`, and knowing that -m takes a value while -h does not means
// teaching dispatch which flags each command declares -- a second copy of the
// thing the tree exists to hold once. `-m -h` is a malformed invocation either
// way, so the cheap rule loses nothing real.
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
