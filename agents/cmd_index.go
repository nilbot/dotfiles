package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/memory"
)

// runIndex regenerates every generated file under .agents/.
//
// The normal path never needs it: every command that writes memory or a handoff
// regenerates the relevant index in the same operation. This exists for
// hand-edits and for out-of-band writes.
func runIndex(args []string, stdout io.Writer) int {
	dir, code := agentsDirHere(stdout)
	if code != exitcode.OK {
		return code
	}
	if err := memory.WriteIndex(filepath.Join(dir, "memory")); err != nil {
		fmt.Fprintf(stdout, "agents index: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, "regenerated memory/INDEX.md")

	// handoff.WriteIndex takes the .agents/ directory and finds reports/handoff
	// under it itself, the same way handoff.Write and handoff.Prune do.
	if err := handoff.WriteIndex(dir); err != nil {
		fmt.Fprintf(stdout, "agents index: %v\n", err)
		return exitcode.NoRecord
	}
	fmt.Fprintln(stdout, "regenerated reports/handoff/INDEX.md")
	return exitcode.OK
}
