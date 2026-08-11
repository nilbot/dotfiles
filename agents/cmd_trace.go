package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

func runTrace(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: agents trace ls|show|cache [flags]")
		return exitcode.Malformed
	}
	switch args[0] {
	case "ls":
		return runTraceLS(args[1:], stdout)
	case "show":
		return runTraceShow(args[1:], stdout)
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

// runTraceShow reads one transcript back, from the harness's copy or from ours.
//
// This is what stops the cache being write-only. `trace cache` preserves bytes;
// without a way to read them, a cached transcript is a file no part of this tool
// can reach, and the agent_id a memory entry cites in its sources: stops
// resolving the moment the harness cleans up -- leaving the entry's derivation
// uncheckable, which is the one thing sources: exists to prevent.
//
// The transcript goes to stdout unflattened and unannotated, because it is data
// rather than a table: whoever asked for it is piping it somewhere. Everything
// about the retrieval goes to stderr, which is also why the origin is reported
// there -- a reader must be able to tell live data from a copy, without that
// note ending up in the file they redirected.
func runTraceShow(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace show", flag.ContinueOnError)
	fs.SetOutput(stdout)
	pathOnly := fs.Bool("path", false, "print the resolved path instead of the content")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdout, "usage: agents trace show [--path] <agent-id or session-id prefix>")
		return exitcode.Malformed
	}
	want := fs.Arg(0)

	rc, dir, code := repoHere(stdout)
	if code != exitcode.OK {
		return code
	}
	cacheRoot, err := repo.TraceCacheDir(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace show: %v\n", err)
		return exitcode.NoRecord
	}
	// No window: a transcript worth reading back is usually an old one, and a
	// default --since would silently answer "not found" for it.
	res, err := trace.Query(dir, trace.Filter{}, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents trace show: %v\n", err)
		return exitcode.NoRecord
	}

	// One entry per transcript, keyed on the identifier that matched: a session
	// writes many records naming one file, and reporting that as ambiguous
	// would make the common case an error.
	matches := map[string]record.Record{}
	for _, r := range res.Records {
		for _, id := range []string{r.AgentID, r.SessionID} {
			if id != "" && strings.HasPrefix(id, want) {
				if prev, seen := matches[id]; !seen || (!prev.PointerVerified && r.PointerVerified) {
					matches[id] = r
				}
			}
		}
	}
	switch len(matches) {
	case 0:
		fmt.Fprintf(stdout, "agents trace show: no record with an agent or session id starting %q\n", want)
		return exitcode.NoRecord
	case 1:
	default:
		ids := make([]string, 0, len(matches))
		for id := range matches {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		// Listed rather than guessed: handing back one transcript while the
		// reader believes they asked for another is worse than refusing.
		fmt.Fprintf(stdout, "agents trace show: %q matches %d records:\n", want, len(ids))
		for _, id := range ids {
			fmt.Fprintf(stdout, "  %s\t%s\n", id, tableCell(matches[id].Transcript))
		}
		return exitcode.Malformed
	}

	var rec record.Record
	for _, r := range matches {
		rec = r
	}
	path, origin, err := trace.Resolve(cacheRoot, rec)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace show: %v\n", err)
		return exitcode.NoRecord
	}
	if *pathOnly {
		fmt.Fprintln(stdout, path)
		return exitcode.OK
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace show: %v\n", err)
		return exitcode.NoRecord
	}
	defer f.Close()
	fmt.Fprintf(os.Stderr, "reading from the %s: %s\n", origin, path)
	if _, err := io.Copy(stdout, f); err != nil {
		fmt.Fprintf(stdout, "agents trace show: %v\n", err)
		return exitcode.NoRecord
	}
	return exitcode.OK
}

// runTraceCache materialises the transcripts this machine can still reach.
//
// The ways a transcript fails to arrive are reported apart, because they ask for
// different things: unreachable means it was here and is gone or cannot be read,
// another machine's name is the only route left to what it holds, and an
// unverified pointer means the file may well be right here under a path no
// record can tie to a session. Each of them raises the exit code, since the
// whole point of running this is to find out what is not here -- a class that is
// counted but exits 0 is one a script will never look at.
// runTraceCachePrune removes cached copies for one named lane.
//
// Spelled `trace cache prune` rather than `trace prune`, because the noun is
// what makes it safe. Two things here could be pruned and only one ever may:
// the cache holds copies, while the index is the tracked, merged record that a
// transcript existed at all -- lose that and you lose the knowledge that there
// was anything to look for. A bare `trace prune` does not say which it touches.
//
// The lane is named by a human and never inferred from git. "The branch is
// gone" does not mean "the content is irrelevant": a deleted branch is usually
// a merged one, and a throwaway worktree is often exactly where the interesting
// investigation happened.
func runTraceCachePrune(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("trace cache prune", flag.ContinueOnError)
	fs.SetOutput(stdout)
	lane := fs.String("lane", "", "the lane whose cached copies to remove (required)")
	apply := fs.Bool("yes", false, "actually remove them; without this it only reports")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if *lane == "" {
		fmt.Fprintln(stdout, "usage: agents trace cache prune --lane <name> [--yes]")
		return exitcode.Malformed
	}

	rc, dir, code := repoHere(stdout)
	if code != exitcode.OK {
		return code
	}
	cacheRoot, err := repo.TraceCacheDir(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache prune: %v\n", err)
		return exitcode.NoRecord
	}
	// Every record, not a window: a lane worth pruning is usually an old one.
	res, err := trace.Query(dir, trace.Filter{}, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache prune: %v\n", err)
		return exitcode.NoRecord
	}

	rep, err := trace.PruneLane(cacheRoot, res.Records, *lane, *apply)
	if err != nil {
		fmt.Fprintf(stdout, "agents trace cache prune: %v\n", err)
		return exitcode.Malformed
	}
	if rep.Removed == 0 {
		fmt.Fprintf(stdout, "no cached transcripts for lane %q\n", *lane)
		return exitcode.OK
	}
	verb := "would remove"
	if *apply {
		verb = "removed"
	}
	fmt.Fprintf(stdout, "%s %d cached transcript(s), %.1f MB, for lane %q\n",
		verb, rep.Removed, float64(rep.Bytes)/(1024*1024), *lane)
	for _, d := range rep.Details {
		fmt.Fprintln(stdout, "  "+tableCell(d))
	}
	if !*apply {
		// Advisory, not OK: there is something here to look at and decide about,
		// and a script must be able to tell "nothing to do" from "did nothing".
		fmt.Fprintln(stdout, "nothing was removed; re-run with --yes to remove them")
		return exitcode.Advisory
	}
	fmt.Fprintln(stdout, "the trace index is unchanged; `agents trace ls` still lists these records")
	return exitcode.OK
}

func runTraceCache(args []string, stdout io.Writer) int {
	if len(args) > 0 && args[0] == "prune" {
		return runTraceCachePrune(args[1:], stdout)
	}
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

	// Before anything else, and reported rather than silent: the old
	// per-worktree cache holds transcripts that are often the only surviving
	// copy, and after the move to the common directory nothing else would ever
	// look there again.
	if mrep, err := trace.MigrateLegacyCache(filepath.Join(dir, ".trace-cache"), cacheRoot); err != nil {
		fmt.Fprintf(stdout, "agents trace cache: migrating the previous cache: %v\n", err)
		return exitcode.NoRecord
	} else if mrep.Moved > 0 || mrep.Skipped > 0 || mrep.Failed > 0 {
		fmt.Fprintf(stdout, "migrated the previous cache: moved %d, already here %d, failed %d\n",
			mrep.Moved, mrep.Skipped, mrep.Failed)
		for _, d := range mrep.Details {
			fmt.Fprintln(stdout, "  "+tableCell(d))
		}
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
