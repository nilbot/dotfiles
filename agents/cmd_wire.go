package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// runWire regenerates harness configs without touching .agents/ content. It is
// the command to run after `make agents` moves the binary, or after a harness
// changes its schema.
func runWire(args []string, stdout io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents wire: %v\n", err)
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents wire: not inside a git repository")
		return exitcode.Skip
	}
	return wireAll(rc.Root, stdout)
}
