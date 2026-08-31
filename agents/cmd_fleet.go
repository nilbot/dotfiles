package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/registry"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func runFleetLS(args []string, stdout io.Writer) int {
	return runFleetLSWithBeforePrune(args, stdout, func() {})
}

// The callback is the test seam at the only stale-snapshot boundary: after the
// read-only listing and before prune obtains the locked, fresh snapshot. The
// production callback is empty.
func runFleetLSWithBeforePrune(args []string, stdout io.Writer, beforePrune func()) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(stdout)
	prune := fs.Bool("prune", false, "drop registered entries whose .agents/ is gone")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents ls: unexpected operand")
		}
		return exitcode.Malformed
	}

	if *prune {
		beforePrune()
		pruned := 0
		var present, missing, unknown []registry.Entry
		_, err := registry.Update(func(fresh *registry.Registry) (bool, error) {
			present, missing, unknown = fresh.ReconcileDetailed()
			for _, e := range missing {
				if fresh.Remove(e.Path) {
					pruned++
				}
			}
			return pruned > 0, nil
		})
		if err != nil {
			fmt.Fprintf(stdout, "agents ls: could not prune registry: %v\n", err)
			return exitcode.NoRecord
		}
		printFleetEntries(stdout, present, missing, unknown)
		fmt.Fprintf(stdout, "pruned %d registered repo(s)\n", pruned)
		if len(unknown) > 0 {
			fmt.Fprintf(stdout, "%d registered repo(s) could not be inspected and were left unchanged\n", len(unknown))
			return exitcode.Advisory
		}
		return exitcode.OK
	}

	r, err := registry.Load()
	if err != nil {
		fmt.Fprintf(stdout, "agents ls: %v\n", err)
		return exitcode.NoRecord
	}
	present, missing, unknown := r.ReconcileDetailed()
	printFleetEntries(stdout, present, missing, unknown)
	if len(missing) > 0 || len(unknown) > 0 {
		fmt.Fprintf(stdout, "%d registered repo(s) missing; %d could not be inspected; `agents ls --prune` forgets only confirmed missing entries\n", len(missing), len(unknown))
		return exitcode.Advisory
	}
	return exitcode.OK
}

func printFleetEntries(stdout io.Writer, present, missing, unknown []registry.Entry) {
	for _, e := range present {
		marker := ""
		if e.Local {
			marker = "  (local)"
		}
		fmt.Fprintf(stdout, "%s%s\n", fleetPath(e.Path), marker)
	}
	for _, e := range missing {
		fmt.Fprintf(stdout, "%s  -- no .agents/ here any more\n", fleetPath(e.Path))
	}
	for _, e := range unknown {
		fmt.Fprintf(stdout, "%s  -- could not inspect .agents/; left unchanged\n", fleetPath(e.Path))
	}
}

func runFleetUpdate(args []string, stdout io.Writer) int {
	return runFleetUpdateWithWire(args, stdout, wireAll)
}

func runFleetUpdateWithWire(args []string, stdout io.Writer, wire func(string, io.Writer) int) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stdout)
	all := fs.Bool("all", false, "update every registered repository")
	apply := fs.Bool("apply", false, "rewrite repository wiring")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents update: unexpected operand")
		}
		return exitcode.Malformed
	}
	if !*all {
		fmt.Fprintln(stdout, "agents update: --all is required; use `agents wire` for one repository")
		return exitcode.Malformed
	}

	r, err := registry.Load()
	if err != nil {
		fmt.Fprintf(stdout, "agents update: %v\n", err)
		return exitcode.NoRecord
	}
	present, missing, unknown := r.ReconcileDetailed()
	if !*apply {
		fmt.Fprintf(stdout, "would rewire %d registered repo(s); re-run with --apply\n", len(present))
		for _, e := range present {
			fmt.Fprintf(stdout, "  %s\n", fleetPath(e.Path))
		}
		for _, e := range missing {
			fmt.Fprintf(stdout, "  skip (missing): %s\n", fleetPath(e.Path))
		}
		for _, e := range unknown {
			fmt.Fprintf(stdout, "  skip (unknown): %s -- could not inspect .agents/\n", fleetPath(e.Path))
		}
		return exitcode.Advisory
	}

	for _, e := range missing {
		fmt.Fprintf(stdout, "skip (missing): %s -- no .agents/ here any more\n", fleetPath(e.Path))
	}
	for _, e := range unknown {
		fmt.Fprintf(stdout, "skip (unknown): %s -- could not inspect .agents/; left unchanged\n", fleetPath(e.Path))
	}
	failed := 0
	drifted := 0
	for _, e := range present {
		var detail bytes.Buffer
		if code := wire(e.Path, &detail); code != exitcode.OK {
			failed++
			fmt.Fprintf(stdout, "failed to rewire %s (exit %d)", fleetPath(e.Path), code)
			if text := strings.TrimSpace(detail.String()); text != "" {
				fmt.Fprintf(stdout, ": %s", strconv.QuoteToASCII(text))
			}
			fmt.Fprintln(stdout)
			continue
		}
		if err := scaffold.RefreshInfrastructuralSkills(e.Path); err != nil {
			failed++
			fmt.Fprintf(stdout, "failed to refresh skills in %s: %v\n", fleetPath(e.Path), err)
			continue
		}
		fmt.Fprintf(stdout, "rewired %s\n", fleetPath(e.Path))
		rep, err := drift.InspectRepo(e.Path)
		if err != nil || !isDriftClean(rep) {
			drifted++
			fmt.Fprintf(stdout, "notice: %s has context drift; run 'migrating-fleet-context' agent skill to migrate\n", fleetPath(e.Path))
		}
	}

	if failed > 0 || len(missing) > 0 || len(unknown) > 0 {
		fmt.Fprintf(stdout, "%d repo(s) failed; %d registered repo(s) missing; %d could not be inspected\n", failed, len(missing), len(unknown))
		return exitcode.Advisory
	}
	fmt.Fprintf(stdout, "rewired %d registered repo(s)\n", len(present))
	if drifted > 0 {
		return exitcode.Advisory
	}
	return exitcode.OK
}

func fleetPath(path string) string { return strconv.QuoteToASCII(path) }
