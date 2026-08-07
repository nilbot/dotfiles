package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
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

func agentsDirHere(stdout io.Writer) (string, int) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stdout, "agents: %v\n", err)
		return "", exitcode.Malformed
	}
	rc, err := repo.Discover(cwd)
	if err != nil {
		fmt.Fprintln(stdout, "agents: not inside a git repository")
		return "", exitcode.Skip
	}
	return repo.AgentsDir(rc.Root), exitcode.OK
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
			r.When.Format("2006-01-02 15:04"), r.Harness, r.Event, r.Lane, r.Cwd, agent, ok, r.Description)
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

// runTraceCache is replaced in the task that builds the cache; it exists so the
// subcommand switch above compiles against a name that is already spoken for.
func runTraceCache(args []string, stdout io.Writer) int {
	fmt.Fprintln(stdout, "agents trace cache: not implemented")
	return exitcode.Skip
}
