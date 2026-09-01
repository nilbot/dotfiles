package drift

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	return dir
}

func TestInspectCleanCurrentRepo(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	if report.RepoPath != dir {
		t.Errorf("got repo path %q, want %q", report.RepoPath, dir)
	}
	if report.RouterState != RouterCleanCurrent {
		t.Errorf("got router state %q, want %q", report.RouterState, RouterCleanCurrent)
	}
	if report.SymlinkState != "ok" {
		t.Errorf("got symlink state %q, want ok", report.SymlinkState)
	}
	if report.DomainState != "ok" {
		t.Errorf("got domain state %q, want ok", report.DomainState)
	}
	if report.Skills["recording-what-you-learn"] != string(ComponentOK) {
		t.Errorf("got recording-what-you-learn skill state %q, want ok", report.Skills["recording-what-you-learn"])
	}
	if report.Skills["migrating-fleet-context"] != string(ComponentOK) {
		t.Errorf("got migrating-fleet-context skill state %q, want ok", report.Skills["migrating-fleet-context"])
	}
	for _, store := range []string{"design", "plans", "journal", "qna"} {
		if !report.DocsStores[store] {
			t.Errorf("expected docs store %q to be true", store)
		}
	}
	if len(report.MisplacedDocs) != 0 {
		t.Errorf("expected no misplaced docs, got %v", report.MisplacedDocs)
	}
	if report.Diff != "" {
		t.Errorf("expected empty diff, got %q", report.Diff)
	}
}

func TestInspectLegacyRouterRepo(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	legacyTemplates := []string{
		LegacySingleBulletRouter,
		LegacySingleBulletRouterTrimmed,
		LegacyPrePlansRouter,
		LegacyPrePlansRouterTrimmed,
		LegacyCaptureRouter,
		LegacyInitialClaudeMDRouter,
	}

	for i, tmpl := range legacyTemplates {
		agentsPath := filepath.Join(dir, "AGENTS.md")
		if err := os.WriteFile(agentsPath, []byte(tmpl), 0o644); err != nil {
			t.Fatalf("failed to write legacy template %d: %v", i, err)
		}

		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatalf("template %d: InspectRepo failed: %v", i, err)
		}
		if report.RouterState != RouterCleanLegacy {
			t.Errorf("template %d: got router state %q, want %q", i, report.RouterState, RouterCleanLegacy)
		}
		if report.Diff != "" {
			t.Errorf("template %d: expected empty diff for clean_legacy, got %q", i, report.Diff)
		}
	}
}

func TestInspectDriftedRepo(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	// Append custom rule to root AGENTS.md
	agentsPath := filepath.Join(dir, "AGENTS.md")
	f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n## Custom Domain Rules\n- Use Python uv\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	if report.RouterState != RouterDrifted {
		t.Errorf("got router state %q, want %q", report.RouterState, RouterDrifted)
	}
	if report.Diff == "" {
		t.Error("expected non-empty diff for drifted repo")
	}
	if !strings.Contains(report.Diff, "+## Custom Domain Rules") {
		t.Errorf("diff does not contain added lines: %s", report.Diff)
	}
}

func TestInspectMissingDomain(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	domainPath := filepath.Join(dir, ".agents", "AGENTS.md")
	if err := os.Remove(domainPath); err != nil {
		t.Fatal(err)
	}

	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	if report.DomainState != "missing" {
		t.Errorf("got domain state %q, want missing", report.DomainState)
	}
}

func TestInspectSymlinkStates(t *testing.T) {
	t.Run("missing symlink", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "CLAUDE.md")); err != nil {
			t.Fatal(err)
		}
		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.SymlinkState != "missing" {
			t.Errorf("got symlink state %q, want missing", report.SymlinkState)
		}
	})

	t.Run("not symlink (regular file)", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		claudePath := filepath.Join(dir, "CLAUDE.md")
		if err := os.Remove(claudePath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(claudePath, []byte("regular file"), 0o644); err != nil {
			t.Fatal(err)
		}
		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.SymlinkState != "not_symlink" {
			t.Errorf("got symlink state %q, want not_symlink", report.SymlinkState)
		}
	})

	t.Run("broken symlink target missing", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
			t.Fatal(err)
		}
		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.SymlinkState != "broken" {
			t.Errorf("got symlink state %q, want broken", report.SymlinkState)
		}
		if report.RouterState != RouterMissing {
			t.Errorf("got router state %q, want missing", report.RouterState)
		}
	})

	t.Run("broken symlink wrong target", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		claudePath := filepath.Join(dir, "CLAUDE.md")
		if err := os.Remove(claudePath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("OTHER.md", claudePath); err != nil {
			t.Fatal(err)
		}
		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.SymlinkState != "broken" {
			t.Errorf("got symlink state %q, want broken", report.SymlinkState)
		}
	})
}

func TestInspectMisplacedDocs(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	// Create valid files
	if err := os.WriteFile(filepath.Join(dir, "docs", "plans", "2026-08-30-valid-plan.md"), []byte("# Valid Plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "design", "2026-08-30-valid-design.md"), []byte("# Valid Design"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create misplaced files
	if err := os.WriteFile(filepath.Join(dir, "docs", "journal", "2026-08-30-test-plan.md"), []byte("# Misplaced Plan in Journal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "archive", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Archived, and therefore NOT misplaced: docs/archive/ is immutable, so
	// this file is deliberately kept out of wantMisplaced below. See
	// Amendment 1 of the 2026-08-29 two-tier design.
	if err := os.WriteFile(filepath.Join(dir, "docs", "archive", "plans", "2026-08-30-old-plan.md"), []byte("# Archived Plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "journal", "2026-08-30-test-design.md"), []byte("# Misplaced Design in Journal"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}

	wantMisplaced := []string{
		"docs/journal/2026-08-30-test-design.md",
		"docs/journal/2026-08-30-test-plan.md",
	}

	if len(report.MisplacedDocs) != len(wantMisplaced) {
		t.Fatalf("got %d misplaced docs %v, want %d %v", len(report.MisplacedDocs), report.MisplacedDocs, len(wantMisplaced), wantMisplaced)
	}
	for i, want := range wantMisplaced {
		if report.MisplacedDocs[i] != want {
			t.Errorf("misplaced doc %d: got %q, want %q", i, report.MisplacedDocs[i], want)
		}
	}
}

func TestInspectSkillStates(t *testing.T) {
	t.Run("customized skill", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		skillPath := filepath.Join(dir, ".agents", "skills", "recording-what-you-learn", "SKILL.md")
		f, err := os.OpenFile(skillPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString("\n## Custom local note\n")
		f.Close()

		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.Skills["recording-what-you-learn"] != string(ComponentCustomized) {
			t.Errorf("got recording skill state %q, want customized", report.Skills["recording-what-you-learn"])
		}
	})

	t.Run("legacy recording skill", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		skillPath := filepath.Join(dir, ".agents", "skills", "recording-what-you-learn", "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(LegacyRecordingSkill), 0o644); err != nil {
			t.Fatal(err)
		}

		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.Skills["recording-what-you-learn"] != string(ComponentCleanLegacy) {
			t.Errorf("got recording skill state %q, want clean_legacy", report.Skills["recording-what-you-learn"])
		}
	})

	t.Run("missing migrating skill", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		skillDir := filepath.Join(dir, ".agents", "skills", "migrating-fleet-context")
		if err := os.RemoveAll(skillDir); err != nil {
			t.Fatal(err)
		}

		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		if report.Skills["migrating-fleet-context"] != string(ComponentMissing) {
			t.Errorf("got migrating skill state %q, want missing", report.Skills["migrating-fleet-context"])
		}
	})

	t.Run("user custom skill directory", func(t *testing.T) {
		dir := newRepo(t)
		if err := scaffold.Create(dir, false); err != nil {
			t.Fatal(err)
		}
		customSkillDir := filepath.Join(dir, ".agents", "skills", "my-custom-skill")
		if err := os.MkdirAll(customSkillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(customSkillDir, "SKILL.md"), []byte("# custom skill"), 0o644); err != nil {
			t.Fatal(err)
		}

		report, err := InspectRepo(dir)
		if err != nil {
			t.Fatal(err)
		}
		// A repository-specific skill is listed, not classified: the tool owns
		// only the skills it embeds. See TestRepoSpecificSkillsAreListedNotJudged.
		if _, judged := report.Skills["my-custom-skill"]; judged {
			t.Errorf("my-custom-skill was classified as %q; it should only be listed", report.Skills["my-custom-skill"])
		}
		if !slices.Contains(report.LocalSkills, "my-custom-skill") {
			t.Errorf("local_skills = %v, want my-custom-skill listed", report.LocalSkills)
		}
	})
}

func TestDriftReportJSONSerialization(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}

	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed DriftReport
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed.RouterState != report.RouterState {
		t.Errorf("got router_state %q, want %q", parsed.RouterState, report.RouterState)
	}
	if parsed.SymlinkState != report.SymlinkState {
		t.Errorf("got symlink_state %q, want %q", parsed.SymlinkState, report.SymlinkState)
	}
	if parsed.DomainState != report.DomainState {
		t.Errorf("got domain_state %q, want %q", parsed.DomainState, report.DomainState)
	}
	if len(parsed.MisplacedDocs) != 0 {
		t.Errorf("expected empty misplaced docs in JSON, got %v", parsed.MisplacedDocs)
	}
}

func TestInspectEmptyRepo(t *testing.T) {
	dir := newRepo(t)
	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if report.RouterState != RouterMissing {
		t.Errorf("got router state %q, want missing", report.RouterState)
	}
	if report.SymlinkState != "missing" {
		t.Errorf("got symlink state %q, want missing", report.SymlinkState)
	}
	if report.DomainState != "missing" {
		t.Errorf("got domain state %q, want missing", report.DomainState)
	}
	for _, store := range []string{"design", "plans", "journal", "qna"} {
		if report.DocsStores[store] {
			t.Errorf("expected store %s to be false in empty repo", store)
		}
	}
}

// docs/archive/ is strictly immutable: `.agents/AGENTS.md` says so, and
// docs/design/README.md says nothing in it is rewritten to stay true. A file
// reported as misplaced is a file the migration skill is told to `git mv`, so
// reporting an archived plan is an instruction to violate that rule.
func TestMisplacedDocsExcludesArchive(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Archived: immutable, must never be reported.
	mustWrite("docs/archive/plans/2026-08-01-old-plan.md", "# old\n")
	mustWrite("docs/archive/specs/2026-08-01-old-design.md", "# old\n")
	// Live stores: genuinely misplaced, must still be reported.
	mustWrite("docs/journal/2026-08-30-stray-plan.md", "# stray\n")

	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	for _, m := range report.MisplacedDocs {
		if strings.HasPrefix(m, "docs/archive/") {
			t.Errorf("archive file reported as misplaced: %s", m)
		}
	}
	// The positive control: without this the check passes on a classifier that
	// reports nothing at all.
	if !slices.Contains(report.MisplacedDocs, "docs/journal/2026-08-30-stray-plan.md") {
		t.Errorf("live-store misplaced plan was not reported; got %v", report.MisplacedDocs)
	}
}

// `.agents/skills/` is where repository-specific skills are supposed to live
// (design section 2). The tool owns only the skills it embeds; classifying every
// other directory as `customized` made isDriftClean report a repository as
// dirty for using the feature exactly as designed, and no migration could ever
// clear it. playground/autogo-mlx carries two such skills and could not report
// clean on 2026-09-01.
func TestRepoSpecificSkillsAreListedNotJudged(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"human-ranked-sft", "llm-in-the-loop-rl-discovery"} {
		p := filepath.Join(dir, ".agents", "skills", name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := InspectRepo(dir)
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}

	// Not judged: absent from the state map the cleanliness check reads.
	for _, name := range []string{"human-ranked-sft", "llm-in-the-loop-rl-discovery"} {
		if state, ok := report.Skills[name]; ok {
			t.Errorf("repo-specific skill %s classified as %q; the tool owns only the skills it embeds", name, state)
		}
	}
	// Still tracked: the embedded skills are judged as before. Without this the
	// test would pass on an inspector that classified nothing at all.
	if report.Skills["recording-what-you-learn"] != string(ComponentOK) {
		t.Errorf("embedded skill state = %q, want ok", report.Skills["recording-what-you-learn"])
	}
	// Listed: a migrating agent needs to know they exist so it leaves them alone.
	if !slices.Contains(report.LocalSkills, "human-ranked-sft") ||
		!slices.Contains(report.LocalSkills, "llm-in-the-loop-rl-discovery") {
		t.Errorf("local_skills = %v, want both repo-specific skills listed", report.LocalSkills)
	}
}
