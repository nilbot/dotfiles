package main

import (
	"fmt"
	"io"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
)

// runVersion prints the binary version and build provenance.
func runVersion(args []string, stdout io.Writer) int {
	fmt.Fprintf(stdout, "agents %s (commit: %s, built: %s)\n", version, commit, date)
	return exitcode.OK
}
