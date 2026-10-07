package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The verification gate exists twice on purpose, and this file stops the two
// copies drifting apart in the parts that have to agree.
//
// `.github/workflows/verify.yml` is the gate this repository runs: eight job
// definitions that provision Linux and macOS runners, run the doctest list, and
// assert the suites keep out of $HOME. `template/ci/verify.yml` is the generic
// starter other repositories copy, so it carries four jobs and none of the
// repository-specific ones. Comparing them job for job would be wrong.
//
// What must not drift is what the template's README calls the hardening rules:
// the action pins, the shape of the aggregate gate, the absence of path
// filters, and the fail-closed quality placeholder. Nothing read `template/ci/`
// before this file existed, so every one of those drifted unobserved between
// ef4cdb7 (2026-08-29), the last change to the template, and f274bc8
// (2026-09-24), the last change to the reference workflow.
//
// The template's README is read too. Its Go example is the only place in the
// repository that pins `actions/setup-go` and `actions/cache` outside the
// reference workflow, and those pins were already a release behind when the
// template was one day old.
//
// Each check below is written to fail on the mutation it names, not to observe
// the current text: change one pin, drop one job from `needs`, add a `paths:`
// filter, and the matching test goes red. All four were run that way.
const (
	referenceWorkflow = ".github/workflows/verify.yml"
	templateWorkflow  = "template/ci/verify.yml"
	templateReadme    = "template/ci/README.md"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var (
	// fencedYAML matches a ```yaml block in a markdown file.
	fencedYAML = regexp.MustCompile("(?s)```yaml\n(.*?)```")

	// actionUse matches a `uses:` reference and the ref after the `@`.
	actionUse = regexp.MustCompile(`(?m)^[ \t]*(?:-[ \t]+)?uses:[ \t]*([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)@(\S+)`)

	// commitSHA is the only ref these files may use.
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

	// jobHeader matches a top-level job id: exactly two spaces of indentation.
	// Keys inside a job are indented four or more, and a `run:` block's lines are
	// deeper still, so two spaces is the job level. Verified against both files:
	// the only other two-space keys are `on:`'s triggers, which sit above
	// `jobs:` and are never reached.
	jobHeader = regexp.MustCompile(`^  ([a-z][a-z0-9-]*):[ \t]*$`)

	// needsLine matches the single-line `needs: [...]` the gate job uses.
	needsLine = regexp.MustCompile(`(?m)^    needs:[ \t]*\[(.*)\][ \t]*$`)

	// jobNameLine matches a job-level `name:` -- four spaces, not the six a
	// step's `- name:` carries.
	jobNameLine = regexp.MustCompile(`(?m)^    name:[ \t]`)

	// pathsFilter matches a `paths:` or `paths-ignore:` key at any depth.
	pathsFilter = regexp.MustCompile(`(?m)^[ \t]*paths(-ignore)?:`)
)

// yamlExamples returns the concatenated ```yaml blocks of a markdown file, so
// the pins in documentation are read by the same code as the pins in workflows.
func yamlExamples(markdown string) string {
	var out strings.Builder
	for _, m := range fencedYAML.FindAllStringSubmatch(markdown, -1) {
		out.WriteString(m[1])
		out.WriteString("\n")
	}
	return out.String()
}

// workflowJobs returns the top-level job ids of a workflow, in file order.
func workflowJobs(t *testing.T, text, source string) []string {
	t.Helper()
	var jobs []string
	inJobs := false
	for _, line := range strings.Split(text, "\n") {
		if line == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if m := jobHeader.FindStringSubmatch(line); m != nil {
			jobs = append(jobs, m[1])
		}
	}
	if len(jobs) == 0 {
		t.Fatalf("%s: found no jobs under `jobs:`; this check would pass without looking at anything", source)
	}
	return jobs
}

// jobBlock returns one job's text, from its header to the next top-level job or
// to the end of the file. It returns "" when the job is not defined.
func jobBlock(text, job string) string {
	header := regexp.MustCompile(`^  ` + regexp.QuoteMeta(job) + `:[ \t]*$`)
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if header.MatchString(line) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	for i := start + 1; i < len(lines); i++ {
		if jobHeader.MatchString(lines[i]) {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// gateActionFiles returns the three files whose action pins have to be read
// together: the reference workflow, the template workflow, and the template
// README's examples.
func gateActionFiles(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		referenceWorkflow: readRepoFile(t, referenceWorkflow),
		templateWorkflow:  readRepoFile(t, templateWorkflow),
		templateReadme:    yamlExamples(readRepoFile(t, templateReadme)),
	}
}

// A tag is a mutable pointer: `@v4` resolves to whatever the action's
// maintainer moved it to since the last run, so a tag reference lets the gate's
// own definition change without a commit here. Every `uses:` in these files is a
// step in a verification gate, so every one is pinned to a commit.
func TestGateActionReferencesAreCommitPinned(t *testing.T) {
	for rel, text := range gateActionFiles(t) {
		found := 0
		for _, m := range actionUse.FindAllStringSubmatch(text, -1) {
			found++
			if !commitSHA.MatchString(m[2]) {
				t.Errorf("%s pins %s to %q, which is not a 40-character commit SHA", rel, m[1], m[2])
			}
		}
		if found == 0 {
			t.Errorf("%s: found no `uses:` references, so this check proved nothing about it", rel)
		}
	}
}

type actionPin struct {
	file string
	ref  string
}

// describePins renders one action's pins for a failure message, one file per
// line so the file that was missed is visible without reading a diff.
func describePins(pins []actionPin) string {
	byFile := map[string]string{}
	for _, p := range pins {
		byFile[p.file] = p.ref
	}
	var parts []string
	for file, ref := range byFile {
		parts = append(parts, file+" = "+ref)
	}
	sort.Strings(parts)
	return "\n  " + strings.Join(parts, "\n  ")
}

// Where the same action is pinned in more than one of these files, the pins
// must agree. This is the mechanical half of the template README's rule that a
// generic change lands in the template in the same pull request: bumping
// `actions/checkout` in the reference workflow and not in the template fails
// here rather than in an adopter's repository months later.
//
// It has teeth today: `actions/checkout`, `actions/setup-go` and
// `actions/cache` are each pinned in two of the three files.
func TestSharedActionPinsAgree(t *testing.T) {
	pins := map[string][]actionPin{}
	for rel, text := range gateActionFiles(t) {
		for _, m := range actionUse.FindAllStringSubmatch(text, -1) {
			pins[m[1]] = append(pins[m[1]], actionPin{rel, m[2]})
		}
	}

	shared := 0
	for action, list := range pins {
		files := map[string]bool{}
		for _, p := range list {
			files[p.file] = true
		}
		if len(files) < 2 {
			continue
		}
		shared++
		first := list[0].ref
		for _, p := range list[1:] {
			if p.ref != first {
				t.Errorf("%s is pinned to different commits, so a version bump reached one file and not the other:%s",
					action, describePins(list))
				break
			}
		}
	}
	if shared == 0 {
		t.Fatal("no action is named by more than one of the three files; this check compared nothing")
	}
}

// The required status check's context is the job id, `gate` -- that is the
// exact string this repository's ruleset names, and the same one the template's
// README tells an adopter to require
// (docs/qna/how-do-github-rulesets-map-to-classic-branch-protection.md). A
// `name:` on the gate job would change the context, and a ruleset naming a
// context no job produces waits forever.
func TestGateHasNoDisplayName(t *testing.T) {
	for _, rel := range []string{referenceWorkflow, templateWorkflow} {
		gate := jobBlock(readRepoFile(t, rel), "gate")
		if gate == "" {
			t.Fatalf("%s: no `gate` job; the check a ruleset requires has to exist", rel)
		}
		if jobNameLine.MatchString(gate) {
			t.Errorf("%s: the gate job carries a display name, which moves the required check's context off `gate`", rel)
		}
	}
}

// Every job in a workflow must appear in the gate's `needs`, or it runs without
// gating the merge: `gate` can go green while the job that was supposed to block
// the pull request is still running or has already failed.
//
// `if: always()` is the other half. Without it a failed dependency skips the
// gate, and a skipped required check reports "Expected" rather than failing.
func TestGateAggregatesEveryJob(t *testing.T) {
	for _, rel := range []string{referenceWorkflow, templateWorkflow} {
		text := readRepoFile(t, rel)
		jobs := workflowJobs(t, text, rel)
		gate := jobBlock(text, "gate")
		if gate == "" {
			t.Fatalf("%s: no `gate` job", rel)
		}
		if !strings.Contains(gate, "if: always()") {
			t.Errorf("%s: the gate job does not run `if: always()`, so a failed dependency skips it instead of failing it", rel)
		}
		m := needsLine.FindStringSubmatch(gate)
		if m == nil {
			t.Fatalf("%s: the gate job has no single-line `needs: [...]`, so this check cannot see what it aggregates", rel)
		}
		var want []string
		for _, job := range jobs {
			if job != "gate" {
				want = append(want, job)
			}
		}
		got := strings.Split(strings.ReplaceAll(m[1], " ", ""), ",")
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: gate needs %v, but the workflow defines %v; a job outside `needs` runs without gating the merge",
				rel, got, want)
		}
	}
}

// There are deliberately no `paths:` filters. A required check that a filter
// skips stays "Expected" on the pull request and blocks it rather than passing
// it, and the filter also skips verification for exactly the edits a cached test
// run is least likely to notice.
func TestGateWorkflowsHaveNoPathsFilter(t *testing.T) {
	for _, rel := range []string{referenceWorkflow, templateWorkflow} {
		if m := pathsFilter.FindString(readRepoFile(t, rel)); m != "" {
			t.Errorf("%s carries a %q filter; a required check skipped by a path filter blocks the pull request instead of passing it",
				rel, strings.TrimSuffix(m, ":"))
		}
	}
}

// qualityShell returns the shell the template's quality job runs, dedented out
// of its `run: |` block so it can be executed.
func qualityShell(t *testing.T, quality string) string {
	t.Helper()
	lines := strings.Split(quality, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " \t") == "        run: |" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the template's quality job has no `run: |` block:\n%s", quality)
	}
	var body []string
	for _, line := range lines[start+1:] {
		if strings.TrimSpace(line) == "" {
			body = append(body, "")
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		body = append(body, strings.TrimPrefix(line, "          "))
	}
	if len(body) == 0 {
		t.Fatal("the template's quality job has an empty `run:` block")
	}
	return strings.Join(body, "\n")
}

// The template's quality job must fail closed. A placeholder that prints a
// message and exits 0 makes `gate` green on a repository where nothing was
// verified, which is a check that cannot fail -- the defect
// docs/qna/tests-that-pass-no-matter-what.md records, and what the first
// version of the template shipped.
//
// The script is RUN, not searched for `exit 1`. Reading for the literal was the
// first version of this check, and it has both failure modes at once: it passes
// for an `exit 1` in a branch that is never taken, and it fails for a script
// that exits non-zero some other way (`false`, a failing linter run last). What
// the property needs is the exit status, so that is what is asserted.
func TestTemplateQualityJobFailsClosed(t *testing.T) {
	quality := jobBlock(readRepoFile(t, templateWorkflow), "quality")
	if quality == "" {
		t.Fatal("the template has no quality job")
	}
	script := qualityShell(t, quality)
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("the template's quality job exits 0, so an adopter who copies the template before configuring anything gets a green gate; it printed:\n%s", out)
	}
}
