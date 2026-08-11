package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nilbot/dotfiles/agents/internal/doctor"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

type doctorCommandDependencies struct {
	Getwd      func() (string, error)
	Discover   func(string) (*repo.Context, error)
	ReadID     func() (string, error)
	BinaryPath func() (string, error)
	Now        func() time.Time
	DoctorDeps doctor.Dependencies
	Run        func(string, string, string, string, doctor.Thresholds, time.Time, doctor.Dependencies) ([]doctor.Check, error)
}

func defaultDoctorCommandDependencies() doctorCommandDependencies {
	return doctorCommandDependencies{
		Getwd:      os.Getwd,
		Discover:   repo.Discover,
		ReadID:     machine.ReadID,
		BinaryPath: binaryPath,
		Now:        func() time.Time { return time.Now().UTC() },
		DoctorDeps: doctor.DependenciesFor(DotfilesRoot()),
		Run:        doctor.RunWithDeps,
	}
}

func runDoctor(args []string, stdout io.Writer) int {
	return runDoctorWithDependencies(args, stdout, defaultDoctorCommandDependencies())
}

func runDoctorWithDependencies(args []string, stdout io.Writer, deps doctorCommandDependencies) int {
	thresholds := doctor.DefaultThresholds()
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.DurationVar(&thresholds.Window, "lane-window", thresholds.Window, "recent window used for lane health")
	fs.IntVar(&thresholds.Modules, "lane-modules", thresholds.Modules, "distinct top-level modules before a lane is flagged")
	fs.IntVar(&thresholds.Days, "lane-days", thresholds.Days, "days of span before a lane is flagged")
	fs.IntVar(&thresholds.Sessions, "lane-sessions", thresholds.Sessions, "sessions before a lane is flagged")
	fs.DurationVar(&thresholds.RecordingFreshness, "recording-freshness", thresholds.RecordingFreshness, "maximum age of a recent harness record")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || thresholds.Window <= 0 || thresholds.Modules <= 0 || thresholds.Days <= 0 || thresholds.Sessions <= 0 || thresholds.RecordingFreshness <= 0 {
		if err == nil {
			fmt.Fprintln(stdout, "agents doctor: flags require positive thresholds and no positional arguments")
		}
		return exitcode.Malformed
	}

	cwd, err := deps.Getwd()
	if err != nil {
		fmt.Fprintln(stdout, "agents doctor: could not resolve the current directory")
		return exitcode.NoRecord
	}
	rc, err := deps.Discover(cwd)
	if err != nil {
		if errors.Is(err, repo.ErrNotARepo) {
			fmt.Fprintln(stdout, "agents doctor: not inside a Git repository")
			return exitcode.Skip
		}
		fmt.Fprintln(stdout, "agents doctor: could not inspect the Git repository")
		return exitcode.NoRecord
	}
	machineID, _ := deps.ReadID()
	binary, err := deps.BinaryPath()
	if err != nil {
		fmt.Fprintln(stdout, "agents doctor: could not resolve the running executable")
		return exitcode.NoRecord
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		fmt.Fprintln(stdout, "agents doctor: could not normalize the running executable")
		return exitcode.NoRecord
	}
	checks, err := deps.Run(rc.Root, repo.AgentsDir(rc.Root), machineID, binary, thresholds, deps.Now(), deps.DoctorDeps)
	if err != nil {
		fmt.Fprintln(stdout, "agents doctor: could not complete the diagnostic")
		return exitcode.NoRecord
	}
	worst := exitcode.OK
	for _, check := range checks {
		mark := map[string]string{doctor.OK: "ok  ", doctor.Warn: "warn", doctor.Fail: "FAIL"}[check.Status]
		if mark == "" {
			fmt.Fprintln(stdout, "agents doctor: diagnostic returned an invalid status")
			return exitcode.NoRecord
		}
		fmt.Fprintf(stdout, "%s  %-28s %s\n", mark, safeDoctorField(check.Name), safeDoctorField(check.Detail))
		if check.Status != doctor.OK && check.Remedy != "" {
			fmt.Fprintf(stdout, "      -> %s\n", safeDoctorField(check.Remedy))
		}
		if check.Status != doctor.OK {
			worst = exitcode.Advisory
		}
	}
	return worst
}

func safeDoctorField(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			quoted := strconv.QuoteRuneToASCII(r)
			out.WriteString(quoted[1 : len(quoted)-1])
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
