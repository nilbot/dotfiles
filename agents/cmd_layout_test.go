package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/drift"
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
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "fixture")
	return root
}

// gitOutput runs one git command in a fixture and fails the test on error. It
// is the general helper the layout tests used to defer: the migration CLI's
// fixtures are here now, and two definitions in one package would collide.
func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, root, err, out)
	}
	return string(out)
}

// newManifestRepoForCmd is a git work tree with .agents/ and the manifest bytes
// written verbatim, so a test can pin what the commands do with a document that
// does not resolve at all -- broken JSON, or a schema from the future.
// newV2RepoForCmd cannot produce either state: it renders through
// layout.MarshalManifest.
func newManifestRepoForCmd(t *testing.T, body string) string {
	t.Helper()
	root := newTestRepo(t)
	manifest := filepath.Join(root, filepath.FromSlash(layout.ManifestRel))
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// newMigratingRepoForCmd is the v2 fixture mid-migration: status `migrating`
// with a complete journal (a frozen v1 source and one move in the `moved`
// phase), so V14 reports nothing and a refusal is attributable to the status
// rather than to a broken manifest. The name is deliberately not the migration
// CLI's own fixture name; that helper belongs to the migration tests, and two
// definitions in one package would collide.
func newMigratingRepoForCmd(t *testing.T) string {
	t.Helper()
	root := newTestRepo(t)
	stores := make(map[string]string, len(layout.Roles()))
	from := make(map[string]string, len(layout.Roles()))
	var moves []layout.Move
	for _, role := range layout.Roles() {
		stores[role] = "context/" + role
		from[role] = "docs/" + role
		if err := os.MkdirAll(filepath.Join(root, "context", role), 0o755); err != nil {
			t.Fatal(err)
		}
		moves = append(moves, layout.Move{
			Role: role, From: "docs/" + role, To: "context/" + role,
			State: layout.MoveDone, Files: 1, Bytes: 1, Digest: "sha256:" + strings.Repeat("0", 64),
		})
	}
	data, err := layout.MarshalManifest(layout.Manifest{
		Schema:         layout.SchemaV2,
		MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus:   layout.StatusMigrating,
		Stores:         stores,
		Migration: &layout.Migration{
			From:            layout.MigrationFrom{Schema: layout.SchemaV1, Stores: from},
			StartedAt:       "2026-09-19T00:00:00Z",
			BackupTag:       "agents-layout-migration",
			CreatedManifest: true,
			Phase:           layout.PhaseMoved,
			Moves:           moves,
		},
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
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "fixture")
	return root
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

// A manifest that exists but did not resolve must not answer with router bytes.
// The v1 router names docs/ stores such a repository may not have, and the
// migration skill writes whatever this prints into AGENTS.md -- so the implicit
// layout (no manifest at all) is the only state allowed to answer with v1
// bytes. `validate` on the same repository still names the reason, so the
// refusal is not silent.
func TestLayoutShowRouterRefusesAnUnresolvedManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantCode string
	}{
		{"malformed json", `{"schema":"agents.layout/v2", BROKEN`, layout.ProblemManifestJSON},
		{"unknown schema", `{"schema":"agents.layout/v3","layout_status":"active"}`, layout.ProblemSchemaUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(newManifestRepoForCmd(t, tc.body))
			var out bytes.Buffer
			if code := runLayoutShowWithVersion([]string{"--router"}, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Errorf("--router exit = %d, want Advisory", code)
			}
			if out.Len() != 0 {
				t.Errorf("--router printed %d bytes for a manifest that did not resolve, want nothing:\n%s",
					out.Len(), out.String())
			}

			out.Reset()
			if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Fatalf("validate exit = %d, want Advisory: %s", code, out.String())
			}
			var report struct {
				Problems []layout.Problem `json:"problems"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("validate --json is not one JSON object: %v\n%s", err, out.String())
			}
			if !layout.HasProblem(report.Problems, tc.wantCode) {
				t.Errorf("validate problems = %v, want %s", report.Problems, tc.wantCode)
			}
		})
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

// The JSON form is the machine surface, so an invalid manifest must not turn it
// into prose plus an object: a consumer parsing it fails on exactly the
// repositories it most needs to hear about. The verdict moves to the exit code
// and the problems stay inside the object, where the Layout already carries
// them. Only re-checking the exit code would not have caught the original bug,
// so this parses the bytes.
func TestLayoutShowJSONStaysMachineReadableWhenInvalid(t *testing.T) {
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	var out bytes.Buffer
	if code := runLayoutShowWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	var got layout.Layout
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("show --json is not one JSON object on an invalid manifest: %v\n%s", err, out.String())
	}
	if len(got.Problems) == 0 {
		t.Fatalf("the object carries no problems; the fixture manifest escapes the root:\n%s", out.String())
	}
	if !layout.HasProblem(got.Problems, layout.ProblemPathEscapes) {
		t.Errorf("problems = %v, want %s", got.Problems, layout.ProblemPathEscapes)
	}
	if path, ok := layout.Path(got, "qna"); !ok || path != "../escape/qna" {
		t.Errorf("stores[qna] = %q/%v, want ../escape/qna: the report must still resolve what it can", path, ok)
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

	// The same report on an invalid manifest must not say "yes". A V-rule makes
	// the layout unsupported for mutation (design §5.2), and `validate` on this
	// repository answers `invalid`; the two surfaces read from one helper, so
	// they cannot disagree.
	t.Chdir(newV2RepoForCmd(t, "../escape"))
	out.Reset()
	if code := runLayoutShowWithVersion(nil, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("invalid exit = %d, want Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Mutating:") || !strings.Contains(out.String(), "no (invalid") {
		t.Errorf("an invalid layout reports a mutating verdict that disagrees with validate:\n%s", out.String())
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

// Design §7.1 names the migrating disposition: a migration in progress is
// unsupported for mutation, so `path` prints nothing and exits 4 while
// `validate` exits 1 and says why. The fixture's journal is complete, so no
// V14 problem is present to explain the refusal -- the status alone does, which
// is the half nothing else pinned.
func TestLayoutMigratingLayoutRefusesByStatus(t *testing.T) {
	t.Chdir(newMigratingRepoForCmd(t))
	var out bytes.Buffer
	if code := runLayoutPathWithVersion([]string{"qna"}, &out, "v0.6.0"); code != exitcode.Skip {
		t.Fatalf("path exit = %d, want Skip: %s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("a migrating layout must print nothing: %q", out.String())
	}

	out.Reset()
	if code := runLayoutValidateWithVersion([]string{"--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("validate exit = %d, want Advisory: %s", code, out.String())
	}
	var report struct {
		Problems     []layout.Problem `json:"problems"`
		Supported    bool             `json:"supported"`
		Reason       string           `json:"reason"`
		LayoutStatus string           `json:"layout_status"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("validate --json is not one JSON object: %v\n%s", err, out.String())
	}
	if len(report.Problems) != 0 {
		t.Fatalf("problems = %v; the fixture journal is complete, so the status alone must explain the refusal",
			report.Problems)
	}
	if report.LayoutStatus != layout.StatusMigrating || report.Supported || report.Reason != "migrating" {
		t.Errorf("status/supported/reason = %q/%v/%q, want %q/false/migrating",
			report.LayoutStatus, report.Supported, report.Reason, layout.StatusMigrating)
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
	if status := gitOutput(t, root, "status", "--porcelain"); status != "" {
		t.Errorf("a read-only command changed the working tree:\n%s", status)
	}
}

// ---------------------------------------------------------------------------
// agents layout migrate: the command surface (design §7.2, §9.1, §9.2)
// ---------------------------------------------------------------------------

// writeFile writes one file, creating its parent directories, for a caller that
// already holds an absolute path. writeFileAt (cmd_save_test.go) is the same
// helper for a repository-relative path.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newGitV1RepoWithContent is the migrate fixture: a committed, clean v1
// repository on the non-protected branch `agents-test`, with content in two
// stores so the plan has something to carry. scaffold.Create writes exactly the
// layout `agents init` writes for a v1 repository and registers no fleet entry;
// XDG_STATE_HOME is isolated anyway, because a migration test must never touch
// this machine's real fleet registry.
func newGitV1RepoWithContent(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := newRepo(t)
	gitOutput(t, root, "branch", "-m", "agents-test")
	if err := scaffold.Create(root, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "docs/design/a-design.md"), "# a design\n")
	writeFile(t, filepath.Join(root, "docs/plans/a-plan.md"), "# a plan\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "v1 scaffold with content")
	if dirty := gitOutput(t, root, "status", "--porcelain"); dirty != "" {
		t.Fatalf("the v1 fixture is not committed clean:\n%s", dirty)
	}
	return root
}

// writeManifestForCmd marshals a manifest to .agents/layout.json, so a test can
// build the states the engine only reaches through a crash: a planned journal
// with real content identity, or a journal at `router` whose moves are done.
func writeManifestForCmd(t *testing.T, root string, m layout.Manifest) {
	t.Helper()
	data, err := layout.MarshalManifest(m)
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
}

// migratingManifest writes a `migrating` journal into a v1 repository, creates
// the backup tag a real apply would have left, and commits both. The digests
// are synthetic on purpose: abort and the refusal paths read the manifest and
// never the filesystem, and plannedJournalRepo freezes a real plan for the path
// that does reconcile.
func migratingManifest(t *testing.T, root, phase, moveState string, createdManifest bool) {
	t.Helper()
	stores := make(map[string]string, len(layout.Roles()))
	from := make(map[string]string, len(layout.Roles()))
	var moves []layout.Move
	for _, role := range layout.Roles() {
		stores[role] = "context/" + role
		from[role] = "docs/" + role
		moves = append(moves, layout.Move{
			Role: role, From: "docs/" + role, To: "context/" + role,
			State: moveState, Files: 1, Bytes: 1,
			Digest: "sha256:" + strings.Repeat("0", 64),
		})
	}
	writeManifestForCmd(t, root, layout.Manifest{
		Schema:         layout.SchemaV2,
		MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus:   layout.StatusMigrating,
		Stores:         stores,
		Migration: &layout.Migration{
			From:            layout.MigrationFrom{Schema: layout.SchemaV1, Stores: from},
			StartedAt:       "2026-09-19T00:00:00Z",
			BackupTag:       "pre-layout-v2-test",
			CreatedManifest: createdManifest,
			Phase:           phase,
			Moves:           moves,
		},
	})
	gitOutput(t, root, "tag", "-a", "pre-layout-v2-test", "-m", "pre-layout-v2 backup")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "migrating journal")
}

// plannedJournalRepo is the state ApplyMigration leaves when it crashes after
// writing the journal and before the first move: status migrating, phase
// planned, every move pending, and a real content identity per move, taken from
// PlanMigration itself. It is the fixture controller ruling R1 is about -- the
// manifest resolves as schema v2, so PlanMigration refuses the repository as a
// migration source, and only resume can carry it forward.
func plannedJournalRepo(t *testing.T) (string, layout.Plan) {
	t.Helper()
	root := newGitV1RepoWithContent(t)
	p, err := layout.PlanMigration(root, layout.MigrateOptions{
		Template:    layout.TemplateContentVault,
		Running:     "v0.6.0",
		RouterState: string(drift.RouterCurrent),
	})
	if err != nil {
		t.Fatalf("planning the fixture: %v", err)
	}
	if len(p.Blockers) > 0 {
		t.Fatalf("the fixture plan is blocked: %v", p.Blockers)
	}
	writeManifestForCmd(t, root, layout.Manifest{
		Schema:         layout.SchemaV2,
		MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus:   layout.StatusMigrating,
		Archive:        p.To.Archive,
		Stores:         p.To.Stores,
		Migration: &layout.Migration{
			From:            layout.MigrationFrom{Schema: p.From.Schema, Stores: p.From.Stores, Archive: p.From.Archive},
			StartedAt:       "2026-09-19T00:00:00Z",
			BackupTag:       "pre-layout-v2-test",
			CreatedManifest: true,
			Phase:           layout.PhasePlanned,
			Moves:           p.Moves,
		},
	})
	gitOutput(t, root, "tag", "-a", "pre-layout-v2-test", "-m", "pre-layout-v2 backup")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "planned journal")
	return root, p
}

func TestLayoutMigrateDryRunIsDefaultAndPrintsThePlan(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--dry-run",
	}, &out, "v0.6.0")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d, want Advisory: %s", code, out.String())
	}
	for _, want := range []string{"layout migrate (dry run)", "move    docs/design", "4 move"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".context")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the target")
	}
}

// Design §7.2: --json carries the migration phase. A fresh dry run reports the
// phase it would start from (`planned`) even though no journal exists yet.
func TestLayoutMigrateJSONCarriesThePhase(t *testing.T) {
	t.Chdir(newGitV1RepoWithContent(t))
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--dry-run", "--json",
	}, &out, "v0.6.0")
	if code != exitcode.Advisory {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	var payload struct {
		DryRun bool   `json:"dry_run"`
		Phase  string `json:"phase"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DryRun || payload.Phase != layout.PhasePlanned {
		t.Fatalf("payload = %+v, want dry_run=true phase=%q", payload, layout.PhasePlanned)
	}
}

// Design §9.1: a multi-step git operation is checked first, because a merge,
// rebase, cherry-pick, revert, am, or bisect moves or discards HEAD on its own
// and would strand the migration's backup tag. repo.InProgress already names
// all six (agents/internal/repo/repo.go:263); the handler must consume it.
func TestLayoutMigrateRefusesDirtyTreeBranchAndInProgressGit(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)

	writeFile(t, filepath.Join(root, "docs/design/scratch.md"), "dirty\n")
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory || !strings.Contains(out.String(), "not clean") {
		t.Fatalf("dirty tree = (%d, %s)", code, out.String())
	}
	if err := os.Remove(filepath.Join(root, "docs/design/scratch.md")); err != nil {
		t.Fatal(err)
	}

	gitOutput(t, root, "switch", "-c", "main")
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory || !strings.Contains(out.String(), "main") {
		t.Fatalf("protected branch = (%d, %s)", code, out.String())
	}
	gitOutput(t, root, "switch", "agents-test")

	// A real merge, not a marker file, so the guard is exercised against git's
	// own state the way cmd_save_test.go does for repo.InProgress.
	gitOutput(t, root, "switch", "-c", "side")
	writeFile(t, filepath.Join(root, "side.txt"), "side\n")
	gitOutput(t, root, "add", "side.txt")
	gitOutput(t, root, "commit", "-m", "side")
	gitOutput(t, root, "switch", "agents-test")
	writeFile(t, filepath.Join(root, "agents-side.txt"), "agents\n")
	gitOutput(t, root, "add", "agents-side.txt")
	gitOutput(t, root, "commit", "-m", "agents")
	gitOutput(t, root, "merge", "--no-ff", "--no-commit", "side")
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory || !strings.Contains(out.String(), "merge") {
		t.Fatalf("merge in progress = (%d, %s)", code, out.String())
	}
}

func TestLayoutMigrateApplyRequiresBackupTag(t *testing.T) {
	t.Chdir(newGitV1RepoWithContent(t))
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{
		"--template", "code-repo", "--apply",
	}, &out, "v0.6.0")
	if code != exitcode.Malformed || !strings.Contains(out.String(), "--backup-tag") {
		t.Fatalf("exit = %d, output = %s", code, out.String())
	}
}

// The planner names every offending entry, one line each, before anything runs.
//
// The stray file is committed first, because R2's clean-tree precondition runs
// before planning -- a tracked stray in docs/ is also the realistic shape of
// the blocker, and an uncommitted one is refused one step earlier with the
// dirtiness, which is what that refusal is for.
func TestLayoutMigrateDryRunNamesResidue(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(root, "docs/scratch.md"), "stray\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "stray")
	t.Chdir(root)
	var out bytes.Buffer
	code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0")
	if code != exitcode.Advisory || !strings.Contains(out.String(), "docs/scratch.md") {
		t.Fatalf("residue = (%d, %s)", code, out.String())
	}
}

// --abort is the only verb that deletes a manifest, and it succeeds only in the
// state design §0.5 allows: phase planned, every move pending, created_manifest.
func TestLayoutMigrateAbortDeletesAPlannedManifest(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhasePlanned, layout.MovePending, true)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("abort exit = %d, want OK: %s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agents/layout.json")); !os.IsNotExist(err) {
		t.Fatal("abort did not delete the manifest")
	}
	if got := gitOutput(t, root, "tag", "--list"); !strings.Contains(got, "pre-layout-v2-test") {
		t.Fatalf("abort must leave the backup tag: %s", got)
	}
}

func TestLayoutMigrateAbortRefusesAfterAMoveAndWithLayoutFlags(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhaseMoved, layout.MoveDone, true)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("abort after a move = %d, want Advisory: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "--resume --apply") {
		t.Fatalf("the refusal must name the remedy: %s", out.String())
	}
	out.Reset()
	code := runLayoutMigrateWithVersion([]string{"--abort", "--apply", "--template", "content-vault"}, &out, "v0.6.0")
	if code != exitcode.Malformed {
		t.Fatalf("abort with layout flags = %d, want Malformed: %s", code, out.String())
	}
}

// Design §9.2's shape, field by field: the header names the repository and the
// mode, then the from/to layouts, the router action, the archive, and the git
// branch and tree state; then the plan, the link count, the result counts, and
// the invocation that would apply it.
func TestLayoutMigrateDryRunNamesEveryReportField(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--template", "content-vault", "--dry-run"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	for _, want := range []string{
		"layout migrate (dry run)",
		"from    agents.layout/v1  docs/{design,plans,journal,qna}",
		"to      agents.layout/v2  stores=.context/{design,plans,journal,qna}",
		"router  current -> canonical v2",
		"archive none",
		"git     branch agents-test, tree clean",
		"move    docs/design -> .context/design",
		"links   0 markdown links point into the moved stores",
		"result  4 move, 0 keep, 0 blocked, 0 link(s)",
		"apply   agents layout migrate --template content-vault --apply --backup-tag",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not name %q:\n%s", want, out.String())
		}
	}
}

// Flag semantics (design §7.2, ruling R3): every conflicting or incomplete
// combination is malformed, and it is decided before the repository is read --
// which is why the matrix runs in a directory that is not a repository at all.
func TestLayoutMigrateRefusesMalformedFlagCombinations(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	for _, args := range [][]string{
		{"--resume", "--template", "content-vault"}, // --resume requires --apply
		{"--abort"},                        // --abort requires --apply
		{"--resume", "--apply", "--abort"}, // mutually exclusive
		{"--resume", "--apply", "--template", "content-vault"},  // resume takes the target from the manifest
		{"--abort", "--apply", "--stores", "design=x"},          // abort takes no layout flags
		{"--dry-run", "--apply", "--template", "content-vault"}, // dry run and apply are exclusive
		{"--template", "no-such-template"},                      // an unknown template is a typo, not a layout
		{"--template", "content-vault", "--stores", "design"},   // not role=path
		{"--template", "content-vault", "--apply"},              // --backup-tag is required
	} {
		out.Reset()
		if code := runLayoutMigrateWithVersion(args, &out, "v0.6.0"); code != exitcode.Malformed {
			t.Errorf("%v exit = %d, want Malformed:\n%s", args, code, out.String())
		}
	}
}

// The whole happy path: apply moves the four stores, writes the v2 router,
// retires the journal, removes the docs/ shell, and leaves a layout `validate`
// accepts.
func TestLayoutMigrateApplyMovesTheStoresAndValidates(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--apply", "--backup-tag", "pre-layout-v2-test",
	}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("apply exit = %d, want OK:\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".context/design/a-design.md")); err != nil {
		t.Errorf("the design store did not move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Error("the docs/ shell survived a completed migration (design §9.5)")
	}
	if got := gitOutput(t, root, "tag", "--list"); !strings.Contains(got, "pre-layout-v2-test") {
		t.Errorf("the required backup tag is missing: %s", got)
	}
	if router, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err != nil || string(router) != layout.V2AgentsMD {
		t.Errorf("AGENTS.md is not the v2 router (%v)", err)
	}
	if l := layout.Resolve(root); l.LayoutStatus != layout.StatusActive || l.Migration != nil {
		t.Errorf("status/journal = %q/%v, want active/nil", l.LayoutStatus, l.Migration)
	}
	out.Reset()
	if code := runLayoutValidateWithVersion(nil, &out, "v0.6.0"); code != exitcode.OK {
		t.Errorf("validate after apply = %d, want OK: %s", code, out.String())
	}
}

// A completed migration whose links still point at the old paths is advisory,
// not a failure: the layout is done and the migrating-fleet-context skill owns
// the rewrite (design §9.5).
func TestLayoutMigrateApplyWithLinkCandidatesIsAdvisory(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(root, "docs/plans/linked.md"), "See [a design](../design/a-design.md).\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "a link into a moving store")
	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--apply", "--backup-tag", "pre-layout-v2-test",
	}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("apply with link candidates = %d, want Advisory:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "migrating-fleet-context") {
		t.Errorf("the advisory must name the skill that rewrites the links:\n%s", out.String())
	}
	if l := layout.Resolve(root); l.LayoutStatus != layout.StatusActive {
		t.Errorf("status = %q; the moves completed, only the links did not", l.LayoutStatus)
	}
}

// A blocked plan is refused before anything is written: no tag, no journal, no
// moved store.
func TestLayoutMigrateApplyWithBlockersWritesNothing(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(root, "docs/scratch.md"), "stray\n")
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "stray")
	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--apply", "--backup-tag", "pre-layout-v2-test",
	}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("blocked apply = %d, want Advisory:\n%s", code, out.String())
	}
	if got := gitOutput(t, root, "tag", "--list"); strings.TrimSpace(got) != "" {
		t.Errorf("a blocked apply created the backup tag: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(layout.ManifestRel))); !os.IsNotExist(err) {
		t.Error("a blocked apply wrote a manifest")
	}
	if _, err := os.Stat(filepath.Join(root, "docs/design")); err != nil {
		t.Errorf("a blocked apply moved a store: %v", err)
	}
}

// Ruling R1: a `migrating` manifest resolves as schema v2, so PlanMigration
// refuses it as a v1 source with a message that names neither the half-finished
// migration nor its remedy. The handler routes it before planning: every verb
// but --resume/--abort refuses, naming the phase and the way forward, and the
// refusal writes nothing -- in particular it never creates the backup tag
// ApplyMigration's first act would.
func TestLayoutMigrateRoutesAMigratingManifestToResume(t *testing.T) {
	root, _ := plannedJournalRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	for _, args := range [][]string{
		{"--dry-run", "--template", "content-vault"},
		{"--apply", "--template", "content-vault", "--backup-tag", "second-tag"},
		{"--json", "--template", "content-vault"},
	} {
		out.Reset()
		if code := runLayoutMigrateWithVersion(args, &out, "v0.6.0"); code != exitcode.Advisory {
			t.Errorf("%v exit = %d, want Advisory:\n%s", args, code, out.String())
		}
		body := out.String()
		if !strings.Contains(body, "phase "+layout.PhasePlanned) || !strings.Contains(body, "agents layout migrate --resume --apply") {
			t.Errorf("%v does not name the phase and the remedy:\n%s", args, body)
		}
	}
	if tags := gitOutput(t, root, "tag", "--list"); strings.Contains(tags, "second-tag") {
		t.Errorf("a refused invocation created a backup tag:\n%s", tags)
	}
	if l := layout.Resolve(root); l.LayoutStatus != layout.StatusMigrating || l.Migration == nil || l.Migration.Phase != layout.PhasePlanned {
		t.Errorf("a refused invocation changed the journal: %+v", l.Migration)
	}
}

// --resume never re-plans: the journal's frozen `from`, its phase, and its
// per-move identity are what the report and the run both carry (design §7.2,
// §9.4). This resumes a real planned journal, so the migration runs to
// completion and the JSON must describe the run that was continued rather than
// a freshly planned one.
func TestLayoutMigrateResumeAppliesTheJournalAndKeepsItsPhase(t *testing.T) {
	root, planned := plannedJournalRepo(t)
	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--resume", "--apply", "--json"}, &out, "v0.6.0"); code != exitcode.OK {
		t.Fatalf("resume exit = %d, want OK:\n%s", code, out.String())
	}
	var got layout.Plan
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("resume --json is not one JSON object: %v\n%s", err, out.String())
	}
	if got.Phase != layout.PhasePlanned || got.DryRun {
		t.Errorf("phase/dry_run = %q/%v, want %q/false: the journal's own plan",
			got.Phase, got.DryRun, layout.PhasePlanned)
	}
	if from, _ := layout.Path(got.From, "design"); from != "docs/design" {
		t.Errorf("from.design = %q, want the journal's frozen docs/design", from)
	}
	if to, _ := layout.Path(got.To, "design"); to != ".context/design" {
		t.Errorf("to.design = %q, want the manifest's .context/design", to)
	}
	if len(got.Moves) != len(planned.Moves) {
		t.Fatalf("moves = %d, want the journal's %d", len(got.Moves), len(planned.Moves))
	}
	for i, m := range got.Moves {
		want := planned.Moves[i]
		if m.From != want.From || m.To != want.To || m.Files != want.Files || m.Bytes != want.Bytes || m.Digest != want.Digest {
			t.Errorf("move %d = %+v, want the journal's %+v unchanged", i, m, want)
		}
	}
	// The run itself happened: the stores moved, docs/ is gone, the layout is
	// active, and validate accepts it.
	if _, err := os.Stat(filepath.Join(root, ".context/design/a-design.md")); err != nil {
		t.Errorf("the resumed migration did not move the design store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Error("docs/ survived a completed resume")
	}
	if l := layout.Resolve(root); l.LayoutStatus != layout.StatusActive || l.Migration != nil {
		t.Errorf("status/journal after resume = %q/%v, want active/nil", l.LayoutStatus, l.Migration)
	}
	out.Reset()
	if code := runLayoutValidateWithVersion(nil, &out, "v0.6.0"); code != exitcode.OK {
		t.Errorf("validate after resume = %d, want OK: %s", code, out.String())
	}
}

// A mid-flight failure is NoRecord and leaves the journal in place for the next
// resume: the fixture records a digest the source cannot have, which is the
// shape a tree that changed between plan and apply produces.
func TestLayoutMigrateMoveErrorLeavesTheManifestMigrating(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhasePlanned, layout.MovePending, true)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--resume", "--apply"}, &out, "v0.6.0"); code != exitcode.NoRecord {
		t.Fatalf("move error exit = %d, want NoRecord:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "remedy") {
		t.Errorf("the per-move remedy is missing:\n%s", out.String())
	}
	if l := layout.Resolve(root); l.LayoutStatus != layout.StatusMigrating {
		t.Errorf("status = %q, want %s: a mid-flight failure stays resumable", l.LayoutStatus, layout.StatusMigrating)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/design")); err != nil {
		t.Errorf("a move ran despite the mismatch: %v", err)
	}
}

// docs_residue can appear after the plan was accepted. The layout is active --
// the moves did complete -- and the residue is a named advisory blocker, one
// line per entry (design §9.5).
func TestLayoutMigrateResidueAfterTheMovesLeavesTheLayoutActive(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	p, err := layout.PlanMigration(root, layout.MigrateOptions{
		Template: layout.TemplateContentVault, Running: "v0.6.0",
		RouterState: string(drift.RouterCurrent),
	})
	if err != nil || len(p.Blockers) > 0 {
		t.Fatalf("fixture plan = (%v, %v)", p.Blockers, err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".context"), 0o755); err != nil {
		t.Fatal(err)
	}
	moves := make([]layout.Move, len(p.Moves))
	for i, m := range p.Moves {
		gitOutput(t, root, "mv", m.From, m.To)
		m.State = layout.MoveDone
		moves[i] = m
	}
	// The journal is at `router`: every move is done and the v2 router is
	// written, so the only step left is retiring the journal -- which is where
	// apply re-checks docs/ and reports the shell it could not remove.
	writeFile(t, filepath.Join(root, "docs/scratch.md"), "stray\n")
	writeManifestForCmd(t, root, layout.Manifest{
		Schema: layout.SchemaV2, MinMutVerFloor: layout.MinMutVerFloorV2,
		LayoutStatus: layout.StatusMigrating, Archive: p.To.Archive, Stores: p.To.Stores,
		Migration: &layout.Migration{
			From:            layout.MigrationFrom{Schema: p.From.Schema, Stores: p.From.Stores, Archive: p.From.Archive},
			StartedAt:       "2026-09-19T00:00:00Z",
			BackupTag:       "pre-layout-v2-test",
			CreatedManifest: true,
			Phase:           layout.PhaseRouter,
			Moves:           moves,
		},
	})
	gitOutput(t, root, "add", "-A")
	gitOutput(t, root, "commit", "-m", "journal at router")

	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--resume", "--apply"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("residue exit = %d, want Advisory:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "docs/scratch.md") {
		t.Errorf("the residue entry is not named:\n%s", out.String())
	}
	l := layout.Resolve(root)
	if l.LayoutStatus != layout.StatusActive || l.Migration != nil {
		t.Errorf("status/journal = %q/%v, want active/nil: the moves completed", l.LayoutStatus, l.Migration)
	}
}

// Ruling R4: --resume and --abort act on a journal, not on a plan, so a
// repository without a `migrating` manifest refuses both with Advisory -- and
// the refusal names that reason even on a protected branch with a dirty tree,
// which is the state a resume is normally run in.
func TestLayoutMigrateResumeAndAbortWithoutAManifestRefuse(t *testing.T) {
	check := func(t *testing.T, label string) {
		t.Helper()
		var out bytes.Buffer
		for _, args := range [][]string{{"--resume", "--apply"}, {"--abort", "--apply"}} {
			out.Reset()
			if code := runLayoutMigrateWithVersion(args, &out, "v0.6.0"); code != exitcode.Advisory {
				t.Errorf("%s: %v exit = %d, want Advisory:\n%s", label, args, code, out.String())
			}
			if !strings.Contains(out.String(), "no migrating manifest") {
				t.Errorf("%s: %v must name the missing manifest rather than anything about the tree:\n%s",
					label, args, out.String())
			}
		}
	}
	t.Chdir(newGitV1RepoWithContent(t))
	check(t, "v1 repository")
	t.Chdir(newV2RepoForCmd(t, ".context"))
	check(t, "active v2 repository")
}

// Ruling R4: --abort --apply is a mutation, so it passes the version guard, and
// a state that does not prove nothing moved refuses it (design §0.5).
func TestLayoutMigrateAbortRefusalsAndTheVersionGuard(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhasePlanned, layout.MovePending, true)

	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply"}, &out, "v0.5.99"); code != exitcode.Advisory {
		t.Fatalf("below-floor abort = %d, want Advisory:\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(layout.ManifestRel))); err != nil {
		t.Errorf("a below-floor abort deleted the manifest: %v", err)
	}
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--resume", "--apply"}, &out, "v0.5.99"); code != exitcode.Advisory {
		t.Errorf("below-floor resume = %d, want Advisory:\n%s", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "docs/design")); err != nil {
		t.Errorf("a below-floor resume moved a store: %v", err)
	}

	// created_manifest false: this run did not write the manifest, so it is not
	// this binary's to delete, whatever the phase says.
	other := newGitV1RepoWithContent(t)
	t.Chdir(other)
	migratingManifest(t, other, layout.PhasePlanned, layout.MovePending, false)
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply"}, &out, "v0.6.0"); code != exitcode.Advisory ||
		!strings.Contains(out.String(), "did not create") {
		t.Errorf("abort of a manifest this migration did not create = (%d, %s)", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(other, filepath.FromSlash(layout.ManifestRel))); err != nil {
		t.Errorf("abort deleted a manifest it did not create: %v", err)
	}
}

// Design §7.1: 4 outside a repository with .agents/, for the whole family.
func TestLayoutMigrateSkipsOutsideARepositoryWithAgents(t *testing.T) {
	run := func(t *testing.T, label string) {
		t.Helper()
		var out bytes.Buffer
		if code := runLayoutMigrateWithVersion([]string{"--dry-run", "--template", "content-vault"}, &out, "v0.6.0"); code != exitcode.Skip {
			t.Errorf("%s: exit = %d, want Skip: %s", label, code, out.String())
		}
	}
	t.Chdir(t.TempDir())
	run(t, "not a repository")
	t.Chdir(newTestRepo(t))
	run(t, "repository without .agents/")
}

// A subcommand that is not declared in commands.go is unreachable however well
// it works: `agents layout migrate` outside a repository must reach the
// handler's own Skip, not an unknown-command Malformed.
func TestMainRegistersLayoutMigrate(t *testing.T) {
	t.Chdir(t.TempDir())
	var code int
	out := captureStdout(t, func() { code = run([]string{"layout", "migrate"}) })
	if code != exitcode.Skip {
		t.Fatalf("run(layout migrate) = %d, want Skip (%d); stdout:\n%s", code, exitcode.Skip, out)
	}
}

// Ruling R4's table is the exit code of the invocation, not of the surface that
// printed it: --json changes the shape of the report, never the disposition.
// Every row that can be reached through a journal is pinned here, because the
// first implementation returned the emit helper's own OK on all of them.
func TestLayoutMigrateJSONKeepsTheTableExitCodes(t *testing.T) {
	// A move error: NoRecord, and the object carries the remedy as a blocker so
	// a --json consumer can read it without the prose.
	root := newGitV1RepoWithContent(t)
	t.Chdir(root)
	migratingManifest(t, root, layout.PhasePlanned, layout.MovePending, true)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{"--resume", "--apply", "--json"}, &out, "v0.6.0"); code != exitcode.NoRecord {
		t.Errorf("move error --json exit = %d, want NoRecord:\n%s", code, out.String())
	}
	var failed layout.Plan
	if err := json.Unmarshal(out.Bytes(), &failed); err != nil {
		t.Fatalf("move error --json is not one JSON object on its own: %v\n%s", err, out.String())
	}
	if len(failed.Blockers) != 1 || !strings.Contains(failed.Blockers[0].Detail, "remedy") {
		t.Errorf("the object does not carry the remedy:\n%s", out.String())
	}

	// A refused abort: Advisory, and the object names the refusal. The fixture
	// is past `planned` -- the journal that just refused the resume is still
	// abortable by design, so the refusal needs a journal that is not.
	moved := newGitV1RepoWithContent(t)
	t.Chdir(moved)
	migratingManifest(t, moved, layout.PhaseMoved, layout.MoveDone, true)
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{"--abort", "--apply", "--json"}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Errorf("refused abort --json exit = %d, want Advisory:\n%s", code, out.String())
	}
	if err := json.Unmarshal(out.Bytes(), &failed); err != nil {
		t.Fatalf("refused abort --json is not one JSON object: %v\n%s", err, out.String())
	}
	if len(failed.Blockers) != 1 || failed.Blockers[0].Code != "abort_refused" {
		t.Errorf("abort blockers = %+v, want one abort_refused", failed.Blockers)
	}

	// Applied, but links remain: Advisory, not OK.
	linked := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(linked, "docs/plans/linked.md"), "See [a design](../design/a-design.md).\n")
	gitOutput(t, linked, "add", "-A")
	gitOutput(t, linked, "commit", "-m", "a link into a moving store")
	t.Chdir(linked)
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--apply", "--backup-tag", "pre-layout-v2-test", "--json",
	}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Errorf("applied with link candidates --json exit = %d, want Advisory:\n%s", code, out.String())
	}
	var applied layout.Plan
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatalf("apply --json is not one JSON object: %v\n%s", err, out.String())
	}
	if applied.DryRun || applied.Phase != layout.PhasePlanned || applied.Counts.Links == 0 {
		t.Errorf("applied payload = dry_run %v, phase %q, links %d", applied.DryRun, applied.Phase, applied.Counts.Links)
	}

	// Applied cleanly: OK.
	clean := newGitV1RepoWithContent(t)
	t.Chdir(clean)
	out.Reset()
	if code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--apply", "--backup-tag", "pre-layout-v2-test", "--json",
	}, &out, "v0.6.0"); code != exitcode.OK {
		t.Errorf("clean apply --json exit = %d, want OK:\n%s", code, out.String())
	}
}

// Ruling R2: the preconditions run before any write, so a refused --apply
// leaves no tag, no journal, and no moved store behind it.
func TestLayoutMigratePreconditionsRefuseBeforeAnyWrite(t *testing.T) {
	root := newGitV1RepoWithContent(t)
	writeFile(t, filepath.Join(root, "docs/design/scratch.md"), "dirty\n")
	t.Chdir(root)
	var out bytes.Buffer
	if code := runLayoutMigrateWithVersion([]string{
		"--template", "content-vault", "--apply", "--backup-tag", "pre-layout-v2-test",
	}, &out, "v0.6.0"); code != exitcode.Advisory {
		t.Fatalf("dirty apply = %d, want Advisory:\n%s", code, out.String())
	}
	if got := gitOutput(t, root, "tag", "--list"); strings.TrimSpace(got) != "" {
		t.Errorf("a refused apply created the backup tag: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".context")); !os.IsNotExist(err) {
		t.Error("a refused apply wrote the target")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(layout.ManifestRel))); !os.IsNotExist(err) {
		t.Error("a refused apply wrote a manifest")
	}
}
