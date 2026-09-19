package drift

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/layout"
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

// skillAssetBytes reads one embedded skill asset through the layout selector,
// so a test can install the exact bytes a layout treats as canonical instead of
// trusting whatever the writer happens to write.
func skillAssetBytes(t *testing.T, schema, skill string) []byte {
	t.Helper()
	path, err := scaffold.SkillAssetPath(schema, skill)
	if err != nil {
		t.Fatal(err)
	}
	data, err := scaffold.AssetsFS.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writeV2Layout writes a valid v2 manifest at .agents/layout.json together with
// the four store directories it declares. storeRoot is repository-relative
// (".context" in the tests). The archive is declared too, so a v2 walker is
// pinned to the manifest's own archive rather than a hardcoded docs/archive.
func writeV2Layout(t *testing.T, dir, storeRoot string) {
	t.Helper()
	writeV2LayoutWithArchive(t, dir, storeRoot, storeRoot+"/archive")
}

// writeV2LayoutWithArchive is writeV2Layout with the archive path chosen by the
// caller. A test that pins the archive exclusion needs an archive the walk
// would otherwise visit -- one nested inside a declared store -- which V12
// rejects as archive_overlap but which the walker, a separate concern from
// validation, still has to handle.
func writeV2LayoutWithArchive(t *testing.T, dir, storeRoot, archive string) {
	t.Helper()
	for _, role := range layout.Roles() {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(storeRoot), role), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := fmt.Sprintf(`{
  "schema": %q,
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "archive": %q,
  "stores": {
    "design": %q,
    "plans": %q,
    "journal": %q,
    "qna": %q
  }
}`, layout.SchemaV2, archive,
		storeRoot+"/design", storeRoot+"/plans", storeRoot+"/journal", storeRoot+"/qna")
	path := filepath.Join(dir, ".agents", "layout.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFile writes one fixture file, creating its parent directories. The
// layout tests need paths the scaffold writer would never create -- a stray
// plan in a journal store, a vault note outside every declared store.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectCurrentRepo(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	report, err := InspectRepo(dir, "v0.6.0")
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	if report.RepoPath != dir {
		t.Errorf("got repo path %q, want %q", report.RepoPath, dir)
	}
	if report.RouterState != RouterCurrent {
		t.Errorf("got router state %q, want %q", report.RouterState, RouterCurrent)
	}
	if report.SymlinkState != "ok" {
		t.Errorf("got symlink state %q, want ok", report.SymlinkState)
	}
	if report.DomainState != "ok" {
		t.Errorf("got domain state %q, want ok", report.DomainState)
	}
	if report.Skills["recording-what-you-learn"] != string(ComponentCurrent) {
		t.Errorf("got recording-what-you-learn skill state %q, want current", report.Skills["recording-what-you-learn"])
	}
	if report.Skills["migrating-fleet-context"] != string(ComponentCurrent) {
		t.Errorf("got migrating-fleet-context skill state %q, want current", report.Skills["migrating-fleet-context"])
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

// installFrozenV1Layout builds a v1 repository (no .agents/layout.json) by
// hand: the frozen v0.5.1 skill texts, the v1 docs/ stores, and the v1 router.
// It deliberately does not call scaffold.Create, so the test below pins the
// classifier rather than whatever bytes the writer happens to install.
func installFrozenV1Layout(t *testing.T, dir string) {
	t.Helper()
	for skill, text := range map[string]string{
		"recording-what-you-learn": LegacyRecordingSkillV051,
		"migrating-fleet-context":  LegacyMigratingSkillV051,
	} {
		path := filepath.Join(dir, ".agents", "skills", skill, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".agents", "AGENTS.md"), []byte("# Domain rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, store := range []string{"design", "plans", "journal", "qna"} {
		if err := os.MkdirAll(filepath.Join(dir, "docs", store), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(scaffold.DefaultAgentsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
}

// TestFrozenV1SkillTextsAreCurrentOnAV1Repository pins design §0.8 for the
// bundled skills: the resolved layout selects the canonical text, so a v1
// repository carrying the frozen v0.5.1 texts is current — not diverged.
// That is this release's central no-op promise for v1 repositories, and the
// fixture is hand-built so a writer installing the wrong layout's bytes cannot
// make the assertion pass by agreeing with the classifier.
func TestFrozenV1SkillTextsAreCurrentOnAV1Repository(t *testing.T) {
	dir := newRepo(t)
	installFrozenV1Layout(t, dir)

	report, err := InspectRepo(dir, "v0.6.0")
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	if report.RouterState != RouterCurrent {
		t.Errorf("router_state = %q, want %q", report.RouterState, RouterCurrent)
	}
	if report.SymlinkState != "ok" {
		t.Errorf("symlink_state = %q, want ok", report.SymlinkState)
	}
	if report.DomainState != "ok" {
		t.Errorf("domain_state = %q, want ok", report.DomainState)
	}
	for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
		if got := report.Skills[skill]; got != string(ComponentCurrent) {
			t.Errorf("skills[%s] = %q, want %q: the frozen v1 text is this layout's canonical text", skill, got, ComponentCurrent)
		}
	}
	for _, store := range []string{"design", "plans", "journal", "qna"} {
		if !report.DocsStores[store] {
			t.Errorf("docs_stores[%s] = false, want true", store)
		}
	}
	if len(report.MisplacedDocs) != 0 {
		t.Errorf("misplaced_docs = %v, want none", report.MisplacedDocs)
	}
	if report.Diff != "" {
		t.Errorf("diff = %q, want empty", report.Diff)
	}
}

// The report keeps the layout-blind field names v1 consumers already read and
// adds the resolved layout's own fields (design §7.3). docs_stores is
// deprecated but still populated, for both layouts, until v0.8.0 at the
// earliest.
func TestInspectV1ReportKeepsLegacyFieldsAndAddsLayoutFields(t *testing.T) {
	// newRepo, not a bare t.TempDir(): scaffold.Create asks git for the
	// repository's info/exclude path, so a v1 fixture must be a repository.
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	rep, err := InspectRepo(dir, "v0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	if rep.LayoutVersion != "v1" || rep.Stores["qna"] != "docs/qna" {
		t.Fatalf("layout fields = %+v", rep)
	}
	if !rep.DocsStores["qna"] {
		t.Fatal("docs_stores must remain populated for v1 consumers")
	}
	if rep.Unsupported != "" {
		t.Fatalf("v1 must be supported, got %q", rep.Unsupported)
	}
}

func TestInspectV2ReportResolvesStoresAndRefusesBelowFloor(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	rep, err := InspectRepo(dir, "v0.5.99")
	if err != nil {
		t.Fatal(err)
	}
	if rep.LayoutVersion != "v2" || rep.Stores["qna"] != ".context/qna" {
		t.Fatalf("layout fields = %+v", rep)
	}
	if rep.Unsupported != "below_floor" {
		t.Fatalf("unsupported = %q, want below_floor", rep.Unsupported)
	}
	if rep.UnsupportedDetail != "requires agents >= 0.6.0" {
		t.Fatalf("unsupported_detail = %q", rep.UnsupportedDetail)
	}
	// docs_stores is deprecated, not dropped: it stays populated on both
	// layouts until v0.8.0 at the earliest (design §7.3).
	if !rep.DocsStores["qna"] {
		t.Fatal("docs_stores must remain populated for v2 consumers")
	}
	if isCurrent(rep) {
		t.Fatal("an unsupported layout must not report clean")
	}
}

// The other half of the [below_floor] control: a v2 repository this binary may
// mutate, carrying the v2 texts, is current -- which is what makes the v2 path
// reachable at all.
func TestInspectCleanV2RepoIsCurrent(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
		writeFile(t, filepath.Join(dir, ".agents/skills", skill, "SKILL.md"),
			string(skillAssetBytes(t, layout.SchemaV2, skill)))
	}
	writeFile(t, filepath.Join(dir, ".agents/AGENTS.md"), "# Domain rules\n")
	writeFile(t, filepath.Join(dir, "AGENTS.md"), layout.V2AgentsMD)
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	rep, err := InspectRepo(dir, "v0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	if !isCurrent(rep) {
		t.Fatalf("a v2 repository with the v2 texts must be current: %+v", rep)
	}
}

// The v2 walk follows the manifest's declared stores. A vault note that merely
// ends in -plan.md is not an agents plan artifact (design §7.3), and the
// archive is the manifest's archive, not docs/archive.
func TestInspectV2MisplacedDocsWalksDeclaredStoresNotTheVault(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	writeFile(t, filepath.Join(dir, ".context/journal/wrong-plan.md"), "# wrong\n")
	writeFile(t, filepath.Join(dir, "protein/a-plan.md"), "# vault note\n") // not an agents plan
	rep, _ := InspectRepo(dir, "v0.6.0")
	if len(rep.MisplacedDocs) != 1 || rep.MisplacedDocs[0] != ".context/journal/wrong-plan.md" {
		t.Fatalf("misplaced = %v", rep.MisplacedDocs)
	}
}

// v1 keeps its walker exactly: docs/ is the only tree, and docs/archive/ is
// immutable so nothing in it can be misplaced.
func TestInspectV1MisplacedDocsStillWalkDocsAndExcludeArchive(t *testing.T) {
	dir := newRepo(t)
	if err := scaffold.Create(dir, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "docs/journal/stray-plan.md"), "# stray\n")
	writeFile(t, filepath.Join(dir, "docs/archive/plans/old-plan.md"), "# archived\n")
	rep, _ := InspectRepo(dir, "v0.6.0")
	if !slices.Contains(rep.MisplacedDocs, "docs/journal/stray-plan.md") {
		t.Fatalf("live stray plan not reported: %v", rep.MisplacedDocs)
	}
	for _, m := range rep.MisplacedDocs {
		if strings.HasPrefix(m, "docs/archive/") {
			t.Errorf("archive file reported as misplaced: %s", m)
		}
	}
}

// The v2 archive may be anywhere, so the exclusion is the manifest's archive
// path -- and it is load-bearing only where the walk would otherwise reach the
// archive. This fixture therefore nests the archive INSIDE the journal store:
// V12 rejects that as archive_overlap (so `unsupported` is `invalid`, which is
// a separate concern and does not stop the walk), and without the exclusion the
// two archived markup files below are exactly what the classifier would report.
// The live plan is the control: the same shape outside the archive must appear,
// or this test could not tell "exclusion works" from "walker is broken".
func TestInspectV2MisplacedDocsExcludesTheManifestArchive(t *testing.T) {
	dir := t.TempDir()
	writeV2LayoutWithArchive(t, dir, ".context", ".context/journal/archive")
	writeFile(t, filepath.Join(dir, ".context/journal/archive/old-plan.md"), "# archived\n")
	writeFile(t, filepath.Join(dir, ".context/journal/archive/old-design.md"), "# archived\n")
	writeFile(t, filepath.Join(dir, ".context/journal/live-plan.md"), "# live\n")

	rep, _ := InspectRepo(dir, "v0.6.0")
	if rep.Unsupported != "invalid" {
		t.Fatalf("unsupported = %q, want invalid: a store nested in the archive is V12", rep.Unsupported)
	}
	for _, m := range rep.MisplacedDocs {
		if strings.HasPrefix(m, ".context/journal/archive/") {
			t.Errorf("the manifest's archive was walked: %s", m)
		}
	}
	if slices.Contains(rep.MisplacedDocs, ".context/journal/archive/old-plan.md") ||
		slices.Contains(rep.MisplacedDocs, ".context/journal/archive/old-design.md") {
		t.Fatalf("archived documents reported as misplaced: %v", rep.MisplacedDocs)
	}
	if !slices.Contains(rep.MisplacedDocs, ".context/journal/live-plan.md") {
		t.Fatalf("live misplaced plan not reported: %v", rep.MisplacedDocs)
	}
}

// Both misplacement axes on v2: a -plan.md outside the plans store and a
// -design.md outside the design store. The journals and the qna store are the
// non-owning stores here; the files' own stores stay clean.
func TestInspectV2FlagsBothDocumentKindsOutsideTheirStores(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	writeFile(t, filepath.Join(dir, ".context/qna/stray-design.md"), "# stray\n")
	writeFile(t, filepath.Join(dir, ".context/plans/valid-plan.md"), "# fine\n")
	writeFile(t, filepath.Join(dir, ".context/design/valid-design.md"), "# fine\n")

	rep, _ := InspectRepo(dir, "v0.6.0")
	if !slices.Contains(rep.MisplacedDocs, ".context/qna/stray-design.md") {
		t.Fatalf("a -design.md outside the design store was not reported: %v", rep.MisplacedDocs)
	}
	if slices.Contains(rep.MisplacedDocs, ".context/plans/valid-plan.md") ||
		slices.Contains(rep.MisplacedDocs, ".context/design/valid-design.md") {
		t.Fatalf("documents in their own stores were reported: %v", rep.MisplacedDocs)
	}
}

// An unknown schema is not guessed at: the report says `unknown`, names the
// reason, and resolves no stores (design §7.3, §5.1).
func TestInspectUnknownSchemaIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/layout.json"),
		`{"schema":"agents.layout/v9","layout_status":"active","stores":{}}`)

	rep, _ := InspectRepo(dir, "v0.6.0")
	if rep.LayoutVersion != "unknown" {
		t.Fatalf("layout_version = %q, want unknown", rep.LayoutVersion)
	}
	if rep.Unsupported != "unknown_schema" {
		t.Fatalf("unsupported = %q, want unknown_schema", rep.Unsupported)
	}
	if !strings.Contains(rep.UnsupportedDetail, "schema_unknown") {
		t.Fatalf("unsupported_detail does not name the problem: %q", rep.UnsupportedDetail)
	}
	if len(rep.Stores) != 0 {
		t.Fatalf("an unknown schema must not resolve stores: %v", rep.Stores)
	}
	if isCurrent(rep) {
		t.Fatal("an unknown schema must not report current")
	}
}

// A v2 manifest that breaks a validation rule is `invalid`, and the detail
// names the rule and the offending role rather than only the first problem.
func TestInspectInvalidManifestIsUnsupportedWithDetail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/layout.json"), `{
  "schema": "agents.layout/v2",
  "min_mut_ver_floor": "0.6.0",
  "layout_status": "active",
  "stores": {
    "design": ".context/design",
    "plans": ".context/plans",
    "journal": ".context/journal"
  }
}`)

	rep, _ := InspectRepo(dir, "v0.6.0")
	if rep.LayoutVersion != "v2" {
		t.Fatalf("layout_version = %q, want v2", rep.LayoutVersion)
	}
	if rep.Unsupported != "invalid" {
		t.Fatalf("unsupported = %q, want invalid", rep.Unsupported)
	}
	if !strings.Contains(rep.UnsupportedDetail, "role_missing") || !strings.Contains(rep.UnsupportedDetail, "qna") {
		t.Fatalf("unsupported_detail does not name the broken rule: %q", rep.UnsupportedDetail)
	}
	if isCurrent(rep) {
		t.Fatal("an invalid manifest must not report current")
	}
}

// The resolved layout selects the canonical router (design §8.1), which is what
// makes a v2 repository carrying the v2 router current instead of diverged; the
// v1 router stays a known template there, never an unclassifiable drift.
func TestInspectV2RepoAcceptsTheV2Router(t *testing.T) {
	dir := t.TempDir()
	writeV2Layout(t, dir, ".context")
	writeFile(t, filepath.Join(dir, "AGENTS.md"), layout.V2AgentsMD)
	rep, _ := InspectRepo(dir, "v0.6.0")
	if rep.RouterState != RouterCurrent {
		t.Fatalf("router_state = %q, want %q", rep.RouterState, RouterCurrent)
	}
	writeFile(t, filepath.Join(dir, "AGENTS.md"), scaffold.DefaultAgentsMD)
	rep, _ = InspectRepo(dir, "v0.6.0")
	if rep.RouterState != RouterKnownLegacy {
		t.Fatalf("v1 router on v2 = %q, want %q", rep.RouterState, RouterKnownLegacy)
	}
	// The diff is against the layout's own canonical router, not the v1 one:
	// V2AgentsMD names the manifest and hardcodes no store path.
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "# local router\n")
	rep, _ = InspectRepo(dir, "v0.6.0")
	if rep.RouterState != RouterDiverged {
		t.Fatalf("local router on v2 = %q, want %q", rep.RouterState, RouterDiverged)
	}
	if !strings.Contains(rep.Diff, "`agents.layout/v2`") {
		t.Errorf("the diff is not against the v2 canonical router: %s", rep.Diff)
	}
	if strings.Contains(rep.Diff, "docs/") {
		t.Errorf("the diff carries v1 store paths: %s", rep.Diff)
	}
}

// The canonical text is chosen by the resolved layout (design §0.8). Task 3
// already split the bytes, so this proves the selection path end to end; Task 13
// adds the cross-layout `known_legacy` classification, which needs the legacy
// catalog wired there.
func TestSkillCurrencyUsesTheLayoutSelectedCanonical(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/skills/recording-what-you-learn/SKILL.md"),
		string(skillAssetBytes(t, layout.SchemaV1, "recording-what-you-learn")))
	if rep, _ := InspectRepo(dir, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "current" {
		t.Fatalf("canonical bytes = %q, want current", rep.Skills["recording-what-you-learn"])
	}
	writeFile(t, filepath.Join(dir, ".agents/skills/recording-what-you-learn/SKILL.md"), "# local edit\n")
	if rep, _ := InspectRepo(dir, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "diverged" {
		t.Fatalf("local edit = %q, want diverged", rep.Skills["recording-what-you-learn"])
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

		report, err := InspectRepo(dir, "v0.6.0")
		if err != nil {
			t.Fatalf("template %d: InspectRepo failed: %v", i, err)
		}
		if report.RouterState != RouterKnownLegacy {
			t.Errorf("template %d: got router state %q, want %q", i, report.RouterState, RouterKnownLegacy)
		}
		if report.Diff != "" {
			t.Errorf("template %d: expected empty diff for known_legacy, got %q", i, report.Diff)
		}
	}
}

func TestInspectDivergedRepo(t *testing.T) {
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

	report, err := InspectRepo(dir, "v0.6.0")
	if err != nil {
		t.Fatalf("InspectRepo failed: %v", err)
	}
	if report.RouterState != RouterDiverged {
		t.Errorf("got router state %q, want %q", report.RouterState, RouterDiverged)
	}
	if report.Diff == "" {
		t.Error("expected non-empty diff for a diverged repo")
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

	report, err := InspectRepo(dir, "v0.6.0")
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
		report, err := InspectRepo(dir, "v0.6.0")
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
		report, err := InspectRepo(dir, "v0.6.0")
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
		report, err := InspectRepo(dir, "v0.6.0")
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
		report, err := InspectRepo(dir, "v0.6.0")
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

	report, err := InspectRepo(dir, "v0.6.0")
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
	t.Run("diverged skill", func(t *testing.T) {
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

		report, err := InspectRepo(dir, "v0.6.0")
		if err != nil {
			t.Fatal(err)
		}
		if report.Skills["recording-what-you-learn"] != string(ComponentDiverged) {
			t.Errorf("got recording skill state %q, want diverged", report.Skills["recording-what-you-learn"])
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

		report, err := InspectRepo(dir, "v0.6.0")
		if err != nil {
			t.Fatal(err)
		}
		if report.Skills["recording-what-you-learn"] != string(ComponentKnownLegacy) {
			t.Errorf("got recording skill state %q, want known_legacy", report.Skills["recording-what-you-learn"])
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

		report, err := InspectRepo(dir, "v0.6.0")
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

		report, err := InspectRepo(dir, "v0.6.0")
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

	report, err := InspectRepo(dir, "v0.6.0")
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
	report, err := InspectRepo(dir, "v0.6.0")
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

	report, err := InspectRepo(dir, "v0.6.0")
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
// other directory as `diverged` made the currency predicate report a repository as
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

	report, err := InspectRepo(dir, "v0.6.0")
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
	if report.Skills["recording-what-you-learn"] != string(ComponentCurrent) {
		t.Errorf("embedded skill state = %q, want current", report.Skills["recording-what-you-learn"])
	}
	// Listed: a migrating agent needs to know they exist so it leaves them alone.
	if !slices.Contains(report.LocalSkills, "human-ranked-sft") ||
		!slices.Contains(report.LocalSkills, "llm-in-the-loop-rl-discovery") {
		t.Errorf("local_skills = %v, want both repo-specific skills listed", report.LocalSkills)
	}
}
