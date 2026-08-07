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
	return record.NewWriter(agentsDir).Append(record.Record{
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
	})
}
