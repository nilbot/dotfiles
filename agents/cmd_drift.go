package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/registry"
)

func runDrift(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	fs.SetOutput(stdout)
	jsonOut := fs.Bool("json", false, "output full drift report JSON")
	repoPath := fs.String("repo", ".", "path to repository (defaults to \".\")")
	all := fs.Bool("all", false, "iterate over all registered repositories")

	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents drift: unexpected operand")
		}
		return exitcode.Malformed
	}

	repoVisited := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "repo" {
			repoVisited = true
		}
	})

	if *all && repoVisited {
		fmt.Fprintln(stdout, "agents drift: cannot specify both --all and --repo")
		return exitcode.Malformed
	}

	if *all {
		r, err := registry.Load()
		if err != nil {
			fmt.Fprintf(stdout, "agents drift: %v\n", err)
			return exitcode.NoRecord
		}
		present, missing, unknown := r.ReconcileDetailed()

		reports := make([]drift.DriftReport, 0, len(present))
		allClean := (len(missing) == 0 && len(unknown) == 0)

		for _, e := range present {
			rep, err := drift.InspectRepo(e.Path)
			if err != nil {
				allClean = false
				continue
			}
			reports = append(reports, rep)
			if !isDriftClean(rep) {
				allClean = false
			}
		}

		if *jsonOut {
			b, err := json.MarshalIndent(reports, "", "  ")
			if err != nil {
				fmt.Fprintf(stdout, "agents drift: %v\n", err)
				return exitcode.NoRecord
			}
			stdout.Write(append(b, '\n'))
			if allClean {
				return exitcode.OK
			}
			return exitcode.Advisory
		}

		if len(present) == 0 && len(missing) == 0 && len(unknown) == 0 {
			fmt.Fprintln(stdout, "no repositories registered")
			return exitcode.OK
		}

		for i, rep := range reports {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			printDriftReport(stdout, rep)
		}
		for _, e := range missing {
			fmt.Fprintf(stdout, "Repository: %s (missing -- no .agents/)\n", fleetPath(e.Path))
		}
		for _, e := range unknown {
			fmt.Fprintf(stdout, "Repository: %s (could not inspect .agents/)\n", fleetPath(e.Path))
		}

		if allClean {
			return exitcode.OK
		}
		return exitcode.Advisory
	}

	targetDir := *repoPath
	if targetDir == "" {
		targetDir = "."
	}
	report, err := drift.InspectRepo(targetDir)
	if err != nil {
		fmt.Fprintf(stdout, "agents drift: %v\n", err)
		return exitcode.NoRecord
	}

	if *jsonOut {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stdout, "agents drift: %v\n", err)
			return exitcode.NoRecord
		}
		stdout.Write(append(b, '\n'))
		if isDriftClean(report) {
			return exitcode.OK
		}
		return exitcode.Advisory
	}

	printDriftReport(stdout, report)
	if isDriftClean(report) {
		return exitcode.OK
	}
	return exitcode.Advisory
}

func isDriftClean(report drift.DriftReport) bool {
	if report.RouterState != drift.RouterCleanCurrent {
		return false
	}
	if report.SymlinkState != "ok" {
		return false
	}
	if report.DomainState != "ok" {
		return false
	}
	for _, state := range report.Skills {
		if state != string(drift.ComponentOK) {
			return false
		}
	}
	for _, ok := range report.DocsStores {
		if !ok {
			return false
		}
	}
	if len(report.MisplacedDocs) > 0 {
		return false
	}
	return true
}

func printDriftReport(w io.Writer, rep drift.DriftReport) {
	fmt.Fprintf(w, "Repository: %s\n", fleetPath(rep.RepoPath))
	fmt.Fprintf(w, "  Router:        %s\n", rep.RouterState)
	fmt.Fprintf(w, "  Symlink:       %s\n", rep.SymlinkState)
	fmt.Fprintf(w, "  Domain:        %s\n", rep.DomainState)
	fmt.Fprintln(w, "  Skills:")
	var skillNames []string
	for name := range rep.Skills {
		skillNames = append(skillNames, name)
	}
	sort.Strings(skillNames)
	for _, name := range skillNames {
		fmt.Fprintf(w, "    %-26s %s\n", name+":", rep.Skills[name])
	}
	if len(rep.LocalSkills) > 0 {
		fmt.Fprintln(w, "  Local skills (not managed by agents):")
		for _, name := range rep.LocalSkills {
			fmt.Fprintf(w, "    %s\n", name)
		}
	}
	fmt.Fprintln(w, "  Docs stores:")
	var storeNames []string
	for name := range rep.DocsStores {
		storeNames = append(storeNames, name)
	}
	sort.Strings(storeNames)
	for _, name := range storeNames {
		status := "missing"
		if rep.DocsStores[name] {
			status = "ok"
		}
		fmt.Fprintf(w, "    %-10s %s\n", name+":", status)
	}
	if len(rep.MisplacedDocs) > 0 {
		fmt.Fprintln(w, "  Misplaced docs:")
		for _, doc := range rep.MisplacedDocs {
			fmt.Fprintf(w, "    %s\n", doc)
		}
	}
	if rep.Diff != "" {
		fmt.Fprintln(w, "\nDiff against canonical AGENTS.md:")
		fmt.Fprint(w, rep.Diff)
	}
}
