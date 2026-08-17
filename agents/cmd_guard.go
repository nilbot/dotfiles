package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/guard"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func runGuard(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	fs.SetOutput(stdout)
	staged := fs.Bool("staged", false, "check what is staged for commit")
	if err := fs.Parse(args); err != nil {
		return exitcode.Malformed
	}
	if !*staged || fs.NArg() != 0 {
		fmt.Fprintln(stdout, usageFor("guard"))
		return exitcode.Malformed
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stdout, "agents guard: could not resolve the current directory")
		return exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents guard: not inside a git repository")
		return exitcode.Skip
	}

	findings, err := guard.Staged(rc.Root)
	blocked := false
	for _, finding := range findings {
		label := "warning"
		if finding.Blocking {
			label = "BLOCKED"
			blocked = true
		}
		if finding.Path == "" {
			fmt.Fprintf(stdout, "%s [%s] %s\n", label, finding.Rule, finding.Detail)
			continue
		}
		fmt.Fprintf(stdout, "%s %s:%d [%s] %s\n",
			label, strconv.QuoteToASCII(finding.Path), finding.Line, finding.Rule, finding.Detail)
	}
	if err != nil {
		fmt.Fprintf(stdout, "agents guard: could not complete operation: %v\n", err)
		return exitcode.Block
	}

	switch {
	case blocked:
		return exitcode.Block
	case len(findings) > 0:
		return exitcode.Advisory
	default:
		return exitcode.OK
	}
}
