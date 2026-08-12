package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/lane"
	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

// runHook records one hook firing.
//
// It returns 0 on every path, deliberately. A harness is blocked on this
// process; a trace record is worth strictly less than the dispatch it would
// interrupt. Every reason for not recording is reported on stderr instead,
// where the user can see it.
func runHook(args []string, stdin io.Reader, stderr io.Writer) int {
	if err := recordHook(args, stdin); err != nil {
		fmt.Fprintf(stderr, "agents hook: not recorded: %v\n", err)
	}
	return 0
}

var validEvents = map[string]bool{
	harness.SessionStart:  true,
	harness.SubagentStart: true,
	harness.SubagentStop:  true,
	harness.Stop:          true,
}

func recordHook(args []string, stdin io.Reader) error {
	if len(args) == 0 {
		return errors.New("usage: agents hook <event> --harness <name>")
	}
	event := args[0]
	if !validEvents[event] {
		return fmt.Errorf("unknown event %q", event)
	}

	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	harnessName := fs.String("harness", "", "harness that is calling (required)")
	laneFlag := fs.String("lane", "", "override lane resolution")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *harnessName == "" {
		return errors.New("--harness is required; harness identity is never inferred")
	}
	adapter, ok := harness.Get(*harnessName)
	if !ok {
		return fmt.Errorf("unknown harness %q", *harnessName)
	}

	p, err := harness.Decode(stdin)
	if err != nil {
		return err
	}

	// The payload's cwd is the harness's view; fall back to ours when absent.
	cwd := p.Cwd
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return err
		}
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		return err
	}
	agentsDir := repo.AgentsDir(rc.Root)
	if fi, err := os.Stat(agentsDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("no %s; run `agents init` here first", filepath.Base(agentsDir))
	}

	mid, err := machine.ID()
	if err != nil {
		return err
	}

	tr := harness.Build(adapter, event, p)
	rec := record.Record{
		When:            time.Now().UTC(),
		Harness:         adapter.Name(),
		Machine:         mid,
		Event:           tr.Event,
		Lane:            lane.Resolve(*laneFlag, rc),
		Cwd:             rc.RelCwd,
		SessionID:       tr.SessionID,
		TurnID:          tr.TurnID,
		AgentID:         tr.AgentID,
		AgentType:       tr.AgentType,
		Description:     tr.Description,
		Transcript:      tr.Transcript,
		PointerVerified: tr.PointerVerified,
	}
	store, err := repo.StoreDir(rc.Root)
	if err != nil {
		return err
	}
	if err := record.NewWriter(store).Append(rec); err != nil {
		return err
	}
	return cacheSubagentTranscript(rc.Root, mid, event, rec)
}

// cacheSubagentTranscript copies the child transcript this hook just recorded.
//
// The pointer alone is not enough. A subagent transcript is complete when the
// child stops and is deleted later, part-way through the session that made it:
// 25 of 111 in one measured session, scattered rather than oldest-first, so no
// schedule can anticipate which. `agents trace cache` run afterwards is
// therefore already too late for whatever has gone, and this hook is the
// earliest moment the finished file is on disk.
//
// Only for subagent-stop. A session transcript is 12.9 MB against a subagent's
// 424 KB mean, is still being appended to, and is named by every stop event --
// roughly thirty a day. Copying it here would be quadratic in session length on
// a path a harness is blocked on, and skip-if-exists would freeze the first
// partial copy forever.
//
// The error is returned so runHook can print it, never so it can fail: the
// caller turns every error into a line on stderr and exit 0, because a trace is
// worth strictly less than the dispatch it would interrupt. The record is
// already written by the time this runs, so a failure here costs the copy and
// not the pointer.
func cacheSubagentTranscript(root, mid, event string, rec record.Record) error {
	if event != harness.SubagentStop {
		return nil
	}
	// Writing to disk on every subagent completion is a trade, not a given.
	if os.Getenv("AGENTS_NO_AUTO_CACHE") != "" {
		return nil
	}
	cacheRoot, err := repo.TraceCacheDir(root)
	if err != nil {
		return err
	}
	// Through Cache with a single record rather than a copy of its own: every
	// rule that makes copying safe -- Lstat rather than Stat so a symlinked
	// pointer cannot materialise its target, the regular-file check, the
	// harness-name sanitising, the write-then-rename -- lives there, and a
	// second implementation here would be a second place for them to be wrong.
	rep, err := trace.Cache(cacheRoot, mid, []record.Record{rec})
	if err != nil {
		return err
	}
	if rep.Copied == 0 && len(rep.Details) > 0 {
		return errors.New(rep.Details[0])
	}
	return nil
}
