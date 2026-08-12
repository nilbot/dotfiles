package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/lane"
	"github.com/nilbot/dotfiles/agents/internal/queue"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// runHandoffDraft writes an unreviewed note into the machine-local queue.
//
// The counterpart to `handoff write`, differing in exactly one thing: where it
// lands. `write` puts a reviewed note in the tracked tree; this puts an
// unreviewed one somewhere git cannot see, to be promoted by `agents review` or
// binned. That separation is what makes it safe for a model to draft at all --
// spec 1 rejected aiming unreviewed model output at a tracked directory, and
// the queue is that objection answered rather than argued with.
//
// Drafting is meant to be cheap. It commits the author to nothing, which is
// what the instruction in CLAUDE.md promises, and the promise has to be true or
// the instruction is asking for something more expensive than it admits.
func runHandoffDraft(args []string, stdin io.Reader, stdout io.Writer) int {
	fs := flag.NewFlagSet("handoff draft", flag.ContinueOnError)
	fs.SetOutput(stdout)
	laneFlag := fs.String("lane", "", "override lane resolution")
	session := fs.String("session", "", "session id (required)")
	kind := fs.String("kind", queue.KindHandoff, "handoff or memory")
	subject := fs.String("subject", "", "one-line summary, used as the commit subject at promotion")
	name := fs.String("name", "", "memory only: the entry slug")
	description := fs.String("description", "", "memory only: the one-line description the index renders")
	entryType := fs.String("type", "", "memory only: user | feedback | project | reference")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if *session == "" {
		fmt.Fprintln(stdout, "agents handoff draft: --session is required; it is what keeps concurrent agents from clobbering each other")
		return exitcode.Malformed
	}

	body, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff draft: %v\n", err)
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff draft: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents handoff draft: not inside a git repository")
		return exitcode.Skip
	}
	store, err := repo.StoreDir(rc.Root)
	if err != nil {
		fmt.Fprintf(stdout, "agents handoff draft: %v\n", err)
		return exitcode.NoRecord
	}

	d, err := queue.Write(store, queue.Draft{
		Kind:        *kind,
		Lane:        lane.Resolve(*laneFlag, rc),
		Session:     *session,
		When:        time.Now().UTC(),
		Subject:     *subject,
		Name:        *name,
		Description: *description,
		Type:        *entryType,
		Body:        string(body),
	})
	if err != nil {
		// Malformed rather than NoRecord: every way this fails is the caller
		// having described the draft wrongly, and the two codes ask a harness
		// for different things.
		fmt.Fprintf(stdout, "agents handoff draft: %v\n", err)
		return exitcode.Malformed
	}

	// The id first and alone on the line, so a caller can name this draft back
	// at review time without parsing prose.
	fmt.Fprintln(stdout, d.ID)
	fmt.Fprintln(stdout, "queued, untracked; `agents review` to promote or bin it")
	return exitcode.OK
}
