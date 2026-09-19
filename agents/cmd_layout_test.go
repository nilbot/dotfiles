package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// newRepoWithAgents is the command-level v1 fixture: a git work tree with the
// v1 scaffold and no manifest, so the layout resolves implicitly.
func newRepoWithAgents(t *testing.T) string {
	t.Helper()
	root := newRepo(t)
	if err := scaffold.Create(root, false); err != nil {
		t.Fatal(err)
	}
	return root
}

// newV2RepoForCmd is the command-level v2 fixture: a git work tree with an
// active manifest, the four stores it declares, the v2 router, and a clean
// tree. It deliberately does not call scaffold.Create, which writes the v1
// docs/ stores a v2 repository never creates. storeRoot is repository-relative;
// a storeRoot of "../escape" is a deliberately invalid fixture.
func newV2RepoForCmd(t *testing.T, storeRoot string) string {
	t.Helper()
	root := newTestRepo(t)
	stores := make(map[string]string, len(layout.Roles()))
	for _, role := range layout.Roles() {
		stores[role] = storeRoot + "/" + role
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(storeRoot), role), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data, err := layout.MarshalManifest(layout.Manifest{
		Schema:         layout.SchemaV2,
		MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus:   layout.StatusActive,
		Archive:        storeRoot + "/archive",
		Stores:         stores,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, filepath.FromSlash(layout.ManifestRel))
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The router is the marker a v2 repository carries at the root; the layout
	// commands never write it, but a fixture that resolved v2 with a v1 router
	// would not be the state they are asked about.
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(layout.V2AgentsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	layoutGit(t, root, "add", "-A")
	layoutGit(t, root, "commit", "-m", "fixture")
	return root
}

// layoutGit runs one git command in a layout fixture and fails the test on
// error. It is local to this file -- the name says what it is for, so a later
// fixture helper with a general name cannot collide with it.
func layoutGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, root, err, out)
	}
	return string(out)
}

func TestLayoutShowResolvesV1AndV2(t *testing.T) {
	t.Chdir(newRepoWithAgents(t))
	var out bytes.Buffer
	if code := runLayoutShowWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("v1 exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"qna": "docs/qna"`) {
		t.Fatalf("v1 json = %s", out.String())
	}
	t.Chdir(newV2RepoForCmd(t, ".context"))
	out.Reset()
	if code := runLayoutShowWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("v2 exit = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), `"qna": ".context/qna"`) {
		t.Fatalf("v2 json = %s", out.String())
	}
}

func TestLayoutPathRefusesUnsupported(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, ".context"))
	var out bytes.Buffer
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.5.99"); code != exitcode.Skip {
		t.Fatalf("exit = %d, want Skip; output=%s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("unsupported path must print nothing: %q", out.String())
	}
	out.Reset()
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0"); code != exitcode.OK ||
		strings.TrimSpace(out.String()) != ".context/qna" {
		t.Fatalf("supported path = (%d, %q)", code, out.String())
	}
}

func TestLayoutValidateReportsProblems(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	var out bytes.Buffer
	if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), layout.ProblemPathEscapes) {
		t.Fatalf("json = %s", out.String())
	}
}

// The router is the migration skill's byte-restore path: it is captured and
// written to AGENTS.md, so a problem line printed first would be written into
// the router. The invalid fixture is the decisive half -- a valid layout would
// pass even if the router branch ran after the problem branch.
func TestLayoutShowRouterIsByteExact(t *testing.T) {
	t.Chdir(newRepoWithAgents(t))
	var out bytes.Buffer
	if code := runLayoutShowWithVersion([]string{"--router"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("v1 exit = %d: %s", code, out.String())
	}
	if out.String() != scaffold.DefaultAgentsMD {
		t.Fatalf("v1 --router printed %d bytes, want the %d-byte canonical v1 router",
			out.Len(), len(scaffold.DefaultAgentsMD))
	}

	t.Chdir(newV2RepoForCmd(t, "../escape"))
	out.Reset()
	if code := runLayoutShowWithVersion([]string{"--router"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("v2 exit = %d: %s", code, out.String())
	}
	if out.String() != layout.V2AgentsMD {
		t.Fatalf("v2 --router printed %d bytes, want the %d-byte canonical v2 router",
			out.Len(), len(layout.V2AgentsMD))
	}
}

// An invalid manifest is still reported: the reader sees why, and then sees
// what resolved. Returning after the problems alone would hide the layout.
func TestLayoutShowReportsProblemsAndStillResolves(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	var out bytes.Buffer
	if code := runLayoutShowWithVersion(nil, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	body := out.String()
	if !strings.Contains(body, layout.ProblemPathEscapes) {
		t.Errorf("human output does not name the problem:\n%s", body)
	}
	if !strings.Contains(body, "../escape/qna") {
		t.Errorf("human output does not show what resolved:\n%s", body)
	}
}

// Design 7.1 names the fields the human report carries.
func TestLayoutShowHumanOutputNamesEveryField(t *testing.T) {
	t.Chdir(newRepoWithAgents(t))
	var out bytes.Buffer
	if code := runLayoutShowWithVersion(nil, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	for _, want := range []string{
		"agents.layout/v1", "active", "min_mut_ver_floor", "Stores:", "Archive:",
		"design:", "plans:", "journal:", "qna:", "docs/qna",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show output does not name %q:\n%s", want, out.String())
		}
	}
}

// The JSON form is one object, so a consumer parses it rather than grepping
// it. The fixture is invalid but in the running binary's version range:
// supported folds validity in, the way drift's `unsupported` does, so a
// consumer reading the one boolean cannot be told to mutate a broken layout.
func TestLayoutValidateJSONNamesEveryField(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	var out bytes.Buffer
	if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	var report struct {
		ManifestPath string           `json:"manifest_path"`
		Problems     []layout.Problem `json:"problems"`
		Supported    bool             `json:"supported"`
		Reason       string           `json:"reason"`
		Schema       string           `json:"schema"`
		LayoutStatus string           `json:"layout_status"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("validate --json is not one JSON object: %v\n%s", err, out.String())
	}
	if report.ManifestPath != layout.ManifestRel {
		t.Errorf("manifest_path = %q, want %q", report.ManifestPath, layout.ManifestRel)
	}
	if report.Schema != layout.SchemaV2 || report.LayoutStatus != layout.StatusActive {
		t.Errorf("schema/status = %q/%q, want %q/%q",
			report.Schema, report.LayoutStatus, layout.SchemaV2, layout.StatusActive)
	}
	if report.Supported || report.Reason != "invalid" {
		t.Errorf("supported/reason = %v/%q, want false/invalid; running %s satisfies the floor, "+
			"so only the problem list can be the reason", report.Supported, report.Reason, layout.MinMutVerFloorV2)
	}
	if !layout.HasProblem(report.Problems, layout.ProblemPathEscapes) {
		t.Errorf("problems = %v, want %s", report.Problems, layout.ProblemPathEscapes)
	}
}

// Unsupported is its own exit and its own reason: a repository this binary may
// not mutate is not an invalid one. The same fixture with a supported version
// is the other half -- valid, supported, exit 0.
func TestLayoutValidateUnsupportedIsAdvisoryAndNamed(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, ".context"))
	var out bytes.Buffer
	if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.5.99"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	var report struct {
		Problems  []layout.Problem `json:"problems"`
		Supported bool             `json:"supported"`
		Reason    string           `json:"reason"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("validate --json is not one JSON object: %v\n%s", err, out.String())
	}
	if len(report.Problems) != 0 {
		t.Errorf("problems = %v; the fixture manifest is valid", report.Problems)
	}
	if report.Supported || report.Reason != "below_floor" {
		t.Errorf("supported/reason = %v/%q, want false/below_floor", report.Supported, report.Reason)
	}

	out.Reset()
	// json.Unmarshal leaves a field absent from the document at its previous
	// value, and `reason` is omitted when empty -- so the second parse starts
	// from a zeroed report rather than inheriting below_floor.
	report.Problems, report.Supported, report.Reason = nil, false, ""
	if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("supported exit = %d, want OK: %s", code, out.String())
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("validate --json is not one JSON object: %v\n%s", err, out.String())
	}
	if !report.Supported || report.Reason != "" || len(report.Problems) != 0 {
		t.Errorf("supported/reason/problems = %v/%q/%v, want true/\"\"/none",
			report.Supported, report.Reason, report.Problems)
	}
}

// Design 7.1: 4 outside a repository with .agents/. Both halves are checked,
// because "no repository" and "repository without .agents/" are different
// states that reach the same refusal.
func TestLayoutCommandsSkipOutsideARepositoryWithAgents(t *testing.T) {
	run := func(t *testing.T, label string) {
		t.Helper()
		var out bytes.Buffer
		if code := runLayoutShowWithVersion(nil, &out, "v0.6.0"); code != exitcode.Skip {
			t.Errorf("show: %s exit = %d, want Skip", label, code)
		}
		if strings.TrimSpace(out.String()) == "" {
			t.Errorf("show: %s refused without a reason", label)
		}
		out.Reset()
		if code := runLayoutValidateWithVersion(nil, &out, "v0.6.0"); code != exitcode.Skip {
			t.Errorf("validate: %s exit = %d, want Skip", label, code)
		}
		out.Reset()
		if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0"); code != exitcode.Skip {
			t.Errorf("path: %s exit = %d, want Skip", label, code)
		}
	}

	t.Chdir(t.TempDir())
	run(t, "not a repository")
	t.Chdir(newTestRepo(t))
	run(t, "repository without .agents/")
}

// The path command is a command substitution target: on refusal it must print
// nothing at all, and a malformed operand is its own code.
func TestLayoutPathRequiresOneKnownRole(t *testing.T) {
	t.Chdir(newRepoWithAgents(t))
	var out bytes.Buffer
	for _, args := range [][]string{nil, {"qna", "plans"}, {"bogus"}} {
		out.Reset()
		if code := runLayoutPathWithVersion(args, &out, "v0.6.0"); code != exitcode.Malformed {
			t.Errorf("path %v exit = %d, want Malformed", args, code)
		}
	}
	out.Reset()
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0"); code != exitcode.OK ||
		strings.TrimSpace(out.String()) != "docs/qna" {
		t.Fatalf("path qna = (%d, %q)", code, out.String())
	}
}

func TestLayoutPathRefusesInvalidLayoutSilently(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	var out bytes.Buffer
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory", code)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("an invalid layout must print nothing: %q", out.String())
	}
}

// The whole family is read-only. A clean v2 fixture that is still clean after
// every command -- manifest bytes unchanged and git reporting nothing -- is
// what "never creates, never writes" means mechanically.
func TestLayoutCommandsAreReadOnly(t *testing.T) {
	root := newV2RepoForCmd(t, ".context")
	t.Chdir(root)
	manifest := filepath.Join(root, filepath.FromSlash(layout.ManifestRel))
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	for _, args := range [][]string{nil, {"--json"}, {"--router"}} {
		out.Reset()
		runLayoutShowWithVersion(args, &out, "v0.6.0")
	}
	for _, args := range [][]string{nil, {"--json"}} {
		out.Reset()
		runLayoutValidateWithVersion(args, &out, "v0.6.0")
	}
	out.Reset()
	runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0")

	after, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("the manifest disappeared: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a read-only command rewrote the manifest")
	}
	if status := layoutGit(t, root, "status", "--porcelain"); status != "" {
		t.Errorf("a read-only command changed the working tree:\n%s", status)
	}
}
