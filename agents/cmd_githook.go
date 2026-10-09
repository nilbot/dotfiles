package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/githook"
)

// noCheckout is the value the chain record carries when the machine has no
// checkout to take personal stages from. The installer writes it through to
// the entry verbatim, so this is what arrives as `--checkout -`.
const noCheckout = "-"

// runGithook runs one Git hook on behalf of the chain entry that invoked it.
//
// The invocation is generated, not typed: an entry ends with
//
//	exec "$binary" githook <name> --checkout "$checkout" "$@"
//
// so the shape is fixed -- the hook's own name, then exactly one flag and its
// value, then Git's arguments. Those trailing arguments are forwarded verbatim
// and never interpreted, which is what commit-msg depends on: Git hands it the
// message file's path.
//
// The shape is parsed by hand rather than with a flag.FlagSet, for the same
// reason `help` is: a FlagSet stops at the first non-flag argument, and this
// invocation leads with one. Everything that is not the shape above is
// refused. The alternative -- running a hook against a checkout nobody named
// -- is the silent wrong-directory failure this design exists to remove, and
// it is worse than refusing because it succeeds.
func runGithook(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	name, checkout, hookArgs, ok := parseGithookArgs(args)
	if !ok {
		fmt.Fprintln(stderr, usageFor("githook"))
		return exitcode.Malformed
	}
	extras := personalStagesDir(checkout)
	reportMissingPersonalStages(name, checkout, extras, stderr)
	return runHookChain(name, extras, hookArgs, stdin, stdout, stderr)
}

// reportMissingPersonalStages names the loss when the record pointed at a
// checkout whose personal stages are not there.
//
// Stage 2 is what makes this the case worth a line rather than a broken
// machine: the chain is machine-owned, so deleting or moving the checkout no
// longer takes the commit guard with it. The guard still runs; what is now
// missing is the stages the checkout used to supply, and nothing else would
// say so -- githook.Chain reads an absent directory as "no personal stages" and
// carries on at exit 0.
//
// `-` is the record declaring there are none, and that stays silent. One line
// on stderr, never an exit code: a missing banner must not fail a `git
// checkout` or a `git switch`.
func reportMissingPersonalStages(name, checkout, extras string, stderr io.Writer) {
	if extras == "" {
		return
	}
	info, err := os.Stat(extras)
	if err == nil && info.IsDir() {
		return
	}
	fmt.Fprintf(stderr,
		"agents: %s: the personal stages under %s are missing; the record names %s as the checkout\n",
		name, extras, checkout)
}

// parseGithookArgs reads exactly the invocation the installer generates and
// nothing else.
//
// `--checkout` must be absolute or the single "-". A relative path here would
// be resolved against whichever directory Git happened to run the hook from --
// the process's own working directory, which is the ambient input this design
// removes -- so it is refused rather than joined.
func parseGithookArgs(args []string) (name, checkout string, hookArgs []string, ok bool) {
	if len(args) < 3 || args[1] != "--checkout" {
		return "", "", nil, false
	}
	if !githook.IsHookName(args[0]) {
		return "", "", nil, false
	}
	checkout = args[2]
	if checkout != noCheckout && !filepath.IsAbs(checkout) {
		return "", "", nil, false
	}
	return args[0], checkout, args[3:], true
}

// personalStagesDir turns the record's checkout into the directory the
// personal stages live in, or "" when the record says there is none.
//
// An empty directory is what githook.Chain reads as "no personal stages", so
// both ways of having none -- the record's "-", and a checkout whose git/hooks
// is absent -- end here. That silence is deliberate and it is safe only
// because the entry already stated which checkout it read; the shim, which has
// no such statement, has to say so itself.
func personalStagesDir(checkout string) string {
	if checkout == noCheckout {
		return ""
	}
	return filepath.Join(checkout, "git", "hooks")
}
