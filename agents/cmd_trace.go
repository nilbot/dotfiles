package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

func runTrace(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: agents trace ls|cache [flags]")
		return exitcode.Malformed
	}
	switch args[0] {
	case "ls":
		return runTraceLS(args[1:], stdout)
	case "cache":
		return runTraceCache(args[1:], stdout)
	default:
		fmt.Fprintf(stdout, "agents trace: unknown subcommand %q\n", args[0])
		return exitcode.Malformed
	}
}

// agentsDirHere is the whole answer for a command that only needs the directory.
func agentsDirHere(stdout io.Writer) (string, int) {
	_, dir, code := repoHere(stdout)
	return dir, code
}

// repoHere is the same single rule as agentsDirHere, handing back the repo
// context alongside the directory for the callers that also need the worktree
// root -- `agents save` runs git in it. Deriving one from the other at the call
// site would mean a second answer to "where am I?", which is what this function
// exists to prevent.
func repoHere(stdout io.Writer) (*repo.Context, string, int) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents: %v\n", err)
		return nil, "", exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents: not inside a git repository")
		return nil, "", exitcode.Skip
	}
	dir := repo.AgentsDir(rc.Root)
	// Being in a git repo is not the same as being in a repo that opted into
	// this tool, and every caller of this function goes on to read or write
	// inside .agents/. Left unchecked, `agents index` MkdirAll'd the tree and
	// `agents trace cache` wrote a .gitignore into a repo where init had never
	// run -- at exit 0, so nothing said a word about it. exitcode.Skip already
	// documents "no .agents/" as one of the things it means; the check belongs
	// here rather than in each command, because the next command to need a
	// .agents/ path would otherwise have to remember to repeat it.
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		fmt.Fprintf(stdout, "agents: no .agents/ directory in %s; run `agents init` there first\n", rc.Root)
		return nil, "", exitcode.Skip
	}
	return rc, dir, exitcode.OK
}

func runTraceLS(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace ls", flag.ContinueOnError)
	fs.SetOutput(stdout)
	var f trace.Filter
	fs.StringVar(&f.Lane, "lane", "", "exact lane")
	fs.StringVar(&f.Module, "module", "", "repo-relative path prefix")
	fs.StringVar(&f.Machine, "machine", "", "exact machine id")
	fs.StringVar(&f.Harness, "harness", "", "exact harness name")
	fs.StringVar(&f.Event, "event", "", "exact event name")
	fs.StringVar(&f.Grep, "grep", "", "substring of description or agent type")
	fs.IntVar(&f.Limit, "limit", 50, "maximum records (0 for all)")
	since := fs.String("since", "", "time window, e.g. 3d, 12h, 2w")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	d, err := trace.ParseSince(*since)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace ls: --since %q: %v\n", *since, err)
		return exitcode.Malformed
	}
	f.Since = d

	// Tab-completing a directory hands this flag "payments/", while the cwd it
	// is matched against is repo-relative and never carries a trailing
	// separator. Left as typed it matches nothing and prints a bare header at
	// exit 0, which reads exactly like "no agent has touched this module" --
	// a wrong answer rather than an error. Normalising is the command's job:
	// the matcher compares paths, it does not guess at spellings.
	f.Module = strings.TrimRight(f.Module, "/")

	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}

	res, err := trace.Query(dir, f, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents trace ls: %v\n", err)
		return exitcode.NoRecord
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tHARNESS\tEVENT\tLANE\tCWD\tAGENT\tOK\tDESCRIPTION")
	for _, r := range res.Records {
		ok := "?"
		if r.PointerVerified {
			ok = "y"
		}
		agent := r.AgentType
		if agent == "" {
			agent = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.When.Format("2006-01-02 15:04"), tableCell(r.Harness), tableCell(r.Event),
			tableCell(r.Lane), tableCell(r.Cwd), tableCell(agent), ok, tableCell(r.Description))
	}
	tw.Flush()

	if res.Skipped > 0 {
		// Advisory rather than silent: unreadable lines mean the history is
		// smaller than it looks.
		fmt.Fprintf(stdout, "\n%d unreadable line(s) skipped — check for merge conflict markers in .agents/reports/traces/\n", res.Skipped)
		return exitcode.Advisory
	}
	return exitcode.OK
}

// tableCell flattens the control characters that would let text this tool did
// not author rewrite the terminal table around it. The rule lives in
// internal/safetext alongside the markdown escapers the two generated indexes
// use, so the three cannot drift apart.
func tableCell(s string) string { return safetext.Flatten(s) }

// runTraceCache materialises the transcripts this machine can still reach.
//
// The ways a transcript fails to arrive are reported apart, because they ask for
// different things: unreachable means it was here and is gone or cannot be read,
// another machine's name is the only route left to what it holds, and an
// unverified pointer means the file may well be right here under a path no
// record can tie to a session. Each of them raises the exit code, since the
// whole point of running this is to find out what is not here -- a class that is
// counted but exits 0 is one a script will never look at.
func runTraceCache(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace cache", flag.ContinueOnError)
	fs.SetOutput(stdout)
	lane := fs.String("lane", "", "only this lane")
	since := fs.String("since", "30d", "time window, e.g. 3d, 12h, 2w")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	d, err := trace.ParseSince(*since)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: --since %q: %v\n", *since, err)
		return exitcode.Malformed
	}

	rc, dir, code := repoHere(stdout)
	if code != exitcode.OK {
		return code
	}
	mid, err := machine.ID()
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}
	// The index is read from .agents/, which is per-worktree and tracked; the
	// copies go to the common directory, which every worktree shares. Two
	// different questions, so two different roots.
	cacheRoot, err := repo.TraceCacheDir(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}

	res, err := trace.Query(dir, trace.Filter{Lane: *lane, Since: d}, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}
	rep, err := trace.Cache(cacheRoot, mid, res.Records)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache: %v\n", err)
		return exitcode.NoRecord
	}

	fmt.Fprintf(stdout, "copied %d, already cached %d, unreachable here %d, on another machine %d, unverified pointer %d\n",
		rep.Copied, rep.Skipped, rep.Unreachable, rep.Elsewhere, rep.Unverified)
	for _, d := range rep.Details {
		fmt.Fprintln(stdout, "  "+tableCell(d))
	}
	if rep.Unreachable > 0 || rep.Elsewhere > 0 || rep.Unverified > 0 {
		return exitcode.Advisory
	}
	return exitcode.OK
}
