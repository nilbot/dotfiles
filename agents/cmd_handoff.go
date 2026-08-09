package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/lane"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func runHandoff(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: agents handoff write|prune [flags]")
		return exitcode.Malformed
	}
	switch args[0] {
	case "write":
		return runHandoffWrite(args[1:], stdin, stdout)
	case "prune":
		return runHandoffPrune(args[1:], stdout)
	default:
		fmt.Fprintf(stdout, "agents handoff: unknown subcommand %q\n", args[0])
		return exitcode.Malformed
	}
}

func runHandoffWrite(args []string, stdin io.Reader, stdout io.Writer) int {
	fs := flag.NewFlagSet("handoff write", flag.ContinueOnError)
	fs.SetOutput(stdout)
	laneFlag := fs.String("lane", "", "override lane resolution")
	session := fs.String("session", "", "session id (required)")
	draft := fs.Bool("draft", false, "mark as an unreviewed auto-draft")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if *session == "" {
		fmt.Fprintln(stdout, "agents handoff write: --session is required; it is what keeps concurrent agents from clobbering each other")
		return exitcode.Malformed
	}
	// Checked here as well as in handoff.Write so a bad flag reports as
	// malformed input rather than as a failure to record: the caller is a
	// harness, and the two codes ask it for different things.
	if err := handoff.CheckSession(*session); err != nil {
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.Malformed
	}

	body, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.Malformed
	}
	if len(body) == 0 {
		fmt.Fprintln(stdout, "agents handoff write: refusing to write an empty handoff")
		return exitcode.Malformed
	}

	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}
	// agentsDirHere has already established that this is a repository with a
	// .agents/ directory; discovery runs again only for the branch that lane
	// resolution reads.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents handoff write: not inside a git repository")
		return exitcode.Skip
	}

	status := handoff.StatusReviewed
	if *draft {
		status = handoff.StatusDraft
	}
	path, err := handoff.Write(dir, lane.Resolve(*laneFlag, rc),
		*session, status, string(body), time.Now().UTC())
	// "The handoff is on disk and the index is stale" is not "wanted to record
	// and could not". WriteIndex re-parses the whole tree, so one conflicted or
	// hand-broken handoff anywhere -- a steady state for files designed to be
	// merged across branches -- would otherwise make every later write report
	// NoRecord and suppress the path, telling a session-end hook a handoff was
	// lost while it is sitting in the tree. The path goes out first either way,
	// so a caller reading the first line gets it.
	var stale *handoff.IndexError
	switch {
	case errors.As(err, &stale):
		fmt.Fprintln(stdout, path)
		fmt.Fprintf(stdout, "agents handoff write: %v; fix that file and run `agents index`\n", err)
		return exitcode.Advisory
	case err != nil:
		fmt.Fprintf(stdout, "agents handoff write: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, path)
	return exitcode.OK
}

func runHandoffPrune(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("handoff prune", flag.ContinueOnError)
	fs.SetOutput(stdout)
	keep := fs.Int("keep", 5, "handoffs to keep per lane")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}

	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}
	removed, err := handoff.Prune(dir, *keep)
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff prune: %v\n", err)
		return exitcode.NoRecord
	}
	for _, r := range removed {
		fmt.Fprintln(stdout, "removed "+r)
	}
	fmt.Fprintf(stdout, "kept the newest %d per lane\n", *keep)
	return exitcode.OK
}
