package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

const (
	beginMarker = "<!-- BEGIN GENERATED: agents help --render=markdown -->"
	endMarker   = "<!-- END GENERATED -->"
)

// readmeBlock splits README.md around the generated command table and returns
// the three parts. Task 10 reads the surrounding prose through it.
//
// The checkout is located with task18RepoRoot, not by resolving ".." here:
// TestMain chdirs out of the checkout before any test runs, so cwd is not the
// repository, and packageDir's own comment says to go through that helper
// rather than each caller doing its own arithmetic on the path. Eight call
// sites already do.
func readmeBlock(t *testing.T) (before, block, after string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(task18RepoRoot(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, beginMarker)
	j := strings.Index(text, endMarker)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("README.md is missing the generated-block markers")
	}
	return text[:i+len(beginMarker)], text[i+len(beginMarker) : j], text[j:]
}

// The README block is derived. If it drifts, the fix is to regenerate it, not
// to edit it -- which is the whole reason it is generated.
//
// This is what stops the README from describing a command set the binary does
// not have. The previous arrangement had no command reference at all, which is
// the only reason it had never gone stale.
func TestReadmeCommandBlockIsCurrent(t *testing.T) {
	_, block, _ := readmeBlock(t)
	var want bytes.Buffer
	RenderMarkdown(rootCommand(), &want)
	if strings.TrimSpace(block) != strings.TrimSpace(want.String()) {
		t.Errorf("README command block is stale. Regenerate it:\n"+
			"  agents help --render=markdown\n\ngot:\n%s\nwant:\n%s", block, want.String())
	}
}

// commandSpan matches an inline code span naming an agents command, and only
// that. Prose mentions of the word "agents" are not command references, and
// flagging them would produce findings nobody would act on. The character
// class stops at the first `<`, `=` or `|`, so a span carrying an argument
// placeholder -- `agents handoff draft --lane <lane>` -- is skipped rather
// than half-matched.
var commandSpan = regexp.MustCompile("`agents ([a-z][a-z -]*)`")

// livingDocuments are the files that describe how to use this repository
// today, as opposed to what was true when they were written.
//
// Plans and specs are deliberately excluded. They are dated records: the
// executed bootstrap plan legitimately names `make githooks`, a target that no
// longer exists, and a record silently rewritten to stay true is not a record.
func livingDocuments(t *testing.T, root string) []string {
	t.Helper()
	targets := []string{"README.md", filepath.Join("agents", "README.md"), "CLAUDE.md", filepath.Join("claude", "CLAUDE.md")}
	for _, dir := range []string{filepath.Join("claude", "skills"), filepath.Join(".agents", "skills")} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
				rel, relErr := filepath.Rel(root, p)
				if relErr == nil {
					targets = append(targets, rel)
				}
			}
			return nil
		})
	}
	return targets
}

// No living document may name an `agents` subcommand the registry does not
// define.
//
// This is the direction the README block cannot cover. Generating the table
// keeps the reference complete; it does nothing about a skill or a CLAUDE.md
// that tells its reader to run a command which was renamed two specs ago. The
// reader in that case is usually an agent, and it will run what it is told.
func TestLivingDocumentsNameOnlyRealCommands(t *testing.T) {
	root := task18RepoRoot(t)

	known := map[string]bool{}
	rootCommand().Walk(func(path []string, _ *Command) { known[strings.Join(path, " ")] = true })
	if !known["trace cache prune"] {
		t.Fatal("the tree walk found no three-level command; this check would prove little")
	}

	spans := 0
	for _, rel := range livingDocuments(t, root) {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // an optional document that does not exist yet
		}
		for _, m := range commandSpan.FindAllStringSubmatch(string(data), -1) {
			spans++
			// Longest match wins: `agents trace cache prune` is a command, and
			// so is the `agents trace` inside it. Checking the longest form
			// first means a real leaf is never reported because its parent
			// happened to match a shorter prefix.
			words := strings.Fields(m[1])
			matched := false
			for n := len(words); n > 0 && !matched; n-- {
				matched = known[strings.Join(words[:n], " ")]
			}
			if !matched {
				t.Errorf("%s names `agents %s`, which the registry does not define", rel, m[1])
			}
		}
	}
	// A pattern that matched nothing would pass every document silently.
	if spans == 0 {
		t.Fatal("no `agents ...` code spans found in any living document; the scan is broken")
	}
}

// A command an agent may invoke must appear in the fleet-wide guidance, or a
// harness never learns to reach for it.
//
// Spec 7 measured this exact shape: the instruction said HOW to write a handoff
// and never THAT one should, and twenty sessions produced none. A generated
// reference answers "what is this command"; only the skill answers "which
// command is this situation", and that is judgment, so it cannot be generated
// -- which is precisely why it needs a check that it stayed complete.
func TestHarnessSkillCoversAgentCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(task18RepoRoot(t),
		"claude", "skills", "agents-tool", "SKILL.md"))
	if err != nil {
		t.Fatalf("the fleet-wide agents skill is missing: %v", err)
	}
	text := string(data)

	var missing []string
	agentFacing := 0
	rootCommand().Walk(func(path []string, c *Command) {
		for _, a := range c.Audience {
			if a != Agent {
				continue
			}
			agentFacing++
			if !strings.Contains(text, "`agents "+strings.Join(path, " ")+"`") {
				missing = append(missing, strings.Join(path, " "))
			}
			return
		}
	})
	if agentFacing == 0 {
		t.Fatal("no agent-facing commands found; this check would prove nothing")
	}
	if len(missing) > 0 {
		t.Errorf("agent-facing commands absent from the skill:\n  %s", strings.Join(missing, "\n  "))
	}
}

// The migrating-fleet-context skill is the one deliverable of the two-tier work
// that is prose rather than code, and it shipped contradicting the tooling it
// drives: no fleet mode, one procedure for four router states, an unconditional
// `rm -f CLAUDE.md`, and a commit with no approval gate.
//
// TestLivingDocumentsNameOnlyRealCommands already scans this file, but only in
// one direction -- it catches a command that does not exist, never a required
// command that is absent. That asymmetry is exactly why the omissions survived
// review. This is the other direction, the way TestHarnessSkillCoversAgentCommands
// is the other direction for claude/skills/agents-tool.
//
// See docs/journal/2026-09-01-why-the-migration-skill-shipped-hollow.md and
// Amendment 1 of docs/design/2026-08-29-two-tier-context-and-llm-migration-architecture.md.
func TestMigrationSkillCoversItsSpecifiedProtocol(t *testing.T) {
	root := task18RepoRoot(t)
	rel := filepath.Join(".agents", "skills", "migrating-fleet-context", "SKILL.md")
	repoCopy, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("the migration skill is missing: %v", err)
	}

	// The test reads three texts: the repository copy, the v1 asset it must
	// equal, and the v2 asset the layout-era requirements live in (design §0.8,
	// §8.2). No single text can satisfy both tables: the legacy vocabulary is
	// v1's canonical vocabulary and the forbidden list below is v2's.
	v1 := skillAssetBytes(t, layout.SchemaV1, "migrating-fleet-context")
	v2 := skillAssetBytes(t, layout.SchemaV2, "migrating-fleet-context")
	if string(repoCopy) != v1 {
		t.Fatal("the repository copy is not the v1 canonical text")
	}

	// Required by the amended design section 7, asserted against the v1 asset
	// because every row is true of the frozen v0.5.1 prose -- the repository
	// copy is that text, and dotfiles is a v1 repository. Each entry names the
	// section that requires it, so a future edit that drops one can find out
	// why.
	legacyRequired := []struct{ substr, why string }{
		{"agents ls", "7.1.2 target discovery over the registered fleet"},
		{"agents drift --json", "7.1.2 single-repository inspection"},
		{"agents drift --all --json", "7.6 fleet inspection, which returns an array"},
		{"agents update --all --apply", "7.1.1 self-currency check; --all is required or the CLI rejects it"},
		{"agents doctor", "7.1.7 verification gate"},
		{"clean_current", "7.3 router state table"},
		{"clean_legacy", "7.3 router state table"},
		{"drifted", "7.3 router state table"},
		{"missing", "7.3 router state table"},
		{"upstream", "7.3 named merge sources"},
		{"base", "7.3 named merge sources"},
		{"local", "7.3 named merge sources"},
		{"stop and ask", "7.5 unclassifiable blocks are not a judgement call"},
		{"traceability", "7.5 evidence for zero rule dropping"},
		{"gh pr create", "7.1.9 the migration ends in a pull request"},
	}

	// The layout-era requirements, asserted against the v2 asset. None of these
	// commands or states exists in v0.5.1, so they belong to the text Task 3
	// authored; a v1 copy that lost the rows above and gained these would be
	// wrong in both directions, which is why the two tables are scoped.
	layoutRequired := []struct{ substr, why string }{
		{"agents layout show", "manifest-first resolution"},
		{"agents layout migrate", "the deterministic migration command"},
		{"--dry-run", "plan first, apply only after approval"},
		{"--apply", "the only mutation path"},
		{"--resume", "resumable migration"},
		{"--abort", "the nothing-moved escape from a planned migration"},
		{"min_mut_ver_floor", "below-floor refusal"},
		{"layout_status", "migrating vs active"},
		{"unsupported", "stop instead of guessing"},
		{"archive", "immutable archive handling"},
		{"--local", "v2 is unavailable where .agents/ is ignored"},
		{"the other layout's canonical text", "a copy from another layout is replaced with this layout's canonical text, never merged"},
		{"cannot perform the flip", "the v1 text is frozen; only `agents layout migrate` flips a layout"},
	}
	for _, tc := range []struct {
		name     string
		text     string
		required []struct{ substr, why string }
	}{
		{"v1", v1, legacyRequired},
		{"v2", v2, layoutRequired},
	} {
		lower := strings.ToLower(tc.text)
		for _, r := range tc.required {
			if !strings.Contains(lower, strings.ToLower(r.substr)) {
				t.Errorf("the %s asset does not mention %q, required by %s", tc.name, r.substr, r.why)
			}
		}
	}

	// The v2 text uses the v0.6.0 state vocabulary; the old backticked names are
	// forbidden there. The v1 text keeps them -- that is its canonical
	// vocabulary and this release does not touch it (design §0.3, §2.1). Both
	// directions are asserted here so a reader sees the scoping in one place;
	// the byte pin in TestFrozenV1AssetsEqualTheRecordedV051Bytes already
	// freezes the v1 side transitively.
	for _, old := range []string{"`clean_current`", "`clean_legacy`", "`drifted`", "`customized`"} {
		if !strings.Contains(v1, old) {
			t.Errorf("the v1 skill lost the v0.5.1 state name %s; v1 is frozen and keeps its canonical vocabulary", old)
		}
		if strings.Contains(v2, old) {
			t.Errorf("the v2 skill still uses the v0.5.1 state name %s", old)
		}
	}

	// Forbidden in both texts: the unconditional symlink replacement. On the
	// pre-2026-08-19 topology (AGENTS.md -> CLAUDE.md, content in CLAUDE.md)
	// this deletes the only real file and leaves AGENTS.md -> CLAUDE.md ->
	// AGENTS.md, a symlink loop with every line of repository context gone.
	// playground/desktop_pet was in exactly that state on 2026-09-01.
	//
	// Forbidden in both texts: a command that relocates out of the immutable
	// archive. Prose forbidding the move is fine and expected; a `git mv` with
	// an archive source is not.
	for _, tc := range []struct{ name, text string }{{"v1", v1}, {"v2", v2}} {
		if strings.Contains(tc.text, "rm -f CLAUDE.md") {
			t.Errorf("the %s skill still carries the unconditional `rm -f CLAUDE.md`; design 7.4 requires "+
				"stat-ing both root paths and preserving content before the symlink", tc.name)
		}
		for _, line := range strings.Split(tc.text, "\n") {
			if strings.Contains(line, "git mv") && strings.Contains(line, "docs/archive/") {
				t.Errorf("the %s skill relocates out of docs/archive/, which is immutable: %q", tc.name, strings.TrimSpace(line))
			}
		}
	}
}

// skillAssetBytes resolves one embedded skill asset for a resolved layout and
// returns its bytes. The flat path holds the v2 text; v1/SKILL.md holds the
// frozen v1 text (design §0.8).
func skillAssetBytes(t *testing.T, schema, skill string) string {
	t.Helper()
	path, err := scaffold.SkillAssetPath(schema, skill)
	if err != nil {
		t.Fatal(err)
	}
	data, err := scaffold.AssetsFS.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The v2 recording skill resolves its stores through the binary rather than
// naming them, which is what lets one text serve a repository whose stores are
// anywhere (design §8.3). This is the v2 half of the prose gate; the v1 half is
// TestFrozenV1SkillAssetsStillSpeakV1Paths.
func TestRecordingSkillNamesRolesNotDocsPaths(t *testing.T) {
	text := skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn")
	for _, want := range []string{
		"agents layout path qna", "agents layout path journal", ".agents/layout.json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the v2 recording skill does not name %q", want)
		}
	}
	if strings.Contains(text, "docs/") {
		t.Error("the v2 recording skill hardcodes a docs/ path")
	}
}

// The other half of the gate: the v1 assets must keep speaking v1, so a v1
// repository never silently loses the paths its layout uses. The repository
// copy of each skill is this frozen text (TestMigrationSkillMatchesEmbeddedAsset
// pins the migrating one), so "no v2 prose in the v1 asset" is also "no v2
// prose in dotfiles".
func TestFrozenV1SkillAssetsStillSpeakV1Paths(t *testing.T) {
	for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
		v1 := skillAssetBytes(t, layout.SchemaV1, skill)
		if !strings.Contains(v1, "docs/") {
			t.Errorf("the frozen v1 %s asset no longer names the v1 docs/ paths", skill)
		}
		if strings.Contains(v1, "agents layout path") {
			t.Errorf("the frozen v1 %s asset carries v2-only prose", skill)
		}
	}
}

// The v1 constants are not merely legacy: they are the v1 canonical text, so the
// pin doubles as the freeze. Nobody "improves" a v1 repository by accident.
//
// TestSkillAssetsSplitByLayout asserts the split (the halves differ); this test
// owns the byte pin against the recorded v0.5.1 constants, so the two cannot
// assert the same thing in two places.
func TestFrozenV1AssetsEqualTheRecordedV051Bytes(t *testing.T) {
	for _, tc := range []struct{ skill, want string }{
		{"recording-what-you-learn", drift.LegacyRecordingSkillV051},
		{"migrating-fleet-context", drift.LegacyMigratingSkillV051},
	} {
		path, err := scaffold.SkillAssetPath(layout.SchemaV1, tc.skill)
		if err != nil {
			t.Fatal(err)
		}
		data, err := scaffold.AssetsFS.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != tc.want {
			t.Fatalf("the v1 asset for %s no longer equals the recorded v0.5.1 bytes; v1 is frozen", tc.skill)
		}
	}
}

// The split itself: the flat assets are the v2 texts and v1/SKILL.md is the
// frozen v0.5.1 bytes.
func TestSkillAssetsSplitByLayout(t *testing.T) {
	v1Recording := skillAssetBytes(t, layout.SchemaV1, "recording-what-you-learn")
	v2Recording := skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn")
	v1Migrating := skillAssetBytes(t, layout.SchemaV1, "migrating-fleet-context")
	v2Migrating := skillAssetBytes(t, layout.SchemaV2, "migrating-fleet-context")

	if v1Recording == v2Recording || v1Migrating == v2Migrating {
		t.Fatal("the flat (v2) and v1 assets must differ after the split")
	}
	// The frozen half is byte-identical to the recorded v0.5.1 bytes; that pin
	// lives in TestFrozenV1AssetsEqualTheRecordedV051Bytes.
	//
	// The v2 half is the new prose.
	if strings.Contains(v2Recording, "docs/") {
		t.Error("the v2 recording text still hardcodes a docs/ path")
	}
	for _, want := range []string{"agents layout path qna", "agents layout path journal", ".agents/layout.json"} {
		if !strings.Contains(v2Recording, want) {
			t.Errorf("the v2 recording text does not name %q", want)
		}
	}
	for _, want := range []string{
		"agents layout migrate", "--dry-run", "--apply", "--resume", "--abort",
		"--local", "min_mut_ver_floor", "layout_status",
		"the other layout's canonical text", "cannot perform the flip",
		// Design §8.4 names both of these as part of the v2 text's surface.
		"agents layout show", "agents layout path",
	} {
		if !strings.Contains(v2Migrating, want) {
			t.Errorf("the v2 migrating text does not name %q", want)
		}
	}
}

// The canonical text is selected by the resolved layout, so the same bytes are
// current in one layout and known_legacy in the other (design §0.8, §8.2). The
// known_legacy half is what Task 13's catalog wiring adds: before it, a copy
// from the other layout reported diverged -- the 2026-09-01 accident, where a
// recognizable shared copy had no exit.
//
// The v2 fixture goes through newV2RepoForCmd rather than the drift package's
// unexported writeV2Layout, which package main cannot see; it resolves v2 for
// the same reason (a complete, active manifest).
func TestSkillCurrencyIsSelectedByResolvedLayout(t *testing.T) {
	v1Text := skillAssetBytes(t, layout.SchemaV1, "recording-what-you-learn")
	v2Text := skillAssetBytes(t, layout.SchemaV2, "recording-what-you-learn")
	if v1Text == v2Text {
		t.Fatal("the v1 and v2 texts must differ after the Task 3 split; the selection is untestable otherwise")
	}
	write := func(root, text string) {
		t.Helper()
		writeFile(t, filepath.Join(root, ".agents/skills/recording-what-you-learn/SKILL.md"), text)
	}

	v1 := t.TempDir()
	write(v1, v1Text)
	if rep, _ := drift.InspectRepo(v1, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "current" {
		t.Fatalf("v1 repo + v1 text = %q, want current", rep.Skills["recording-what-you-learn"])
	}
	write(v1, v2Text)
	if rep, _ := drift.InspectRepo(v1, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "known_legacy" {
		t.Fatalf("v1 repo + v2 text = %q, want known_legacy", rep.Skills["recording-what-you-learn"])
	}

	v2 := newV2RepoForCmd(t, ".context")
	write(v2, v1Text)
	if rep, _ := drift.InspectRepo(v2, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "known_legacy" {
		t.Fatalf("v2 repo + v1 text = %q, want known_legacy", rep.Skills["recording-what-you-learn"])
	}
	write(v2, v2Text)
	if rep, _ := drift.InspectRepo(v2, "v0.6.0"); rep.Skills["recording-what-you-learn"] != "current" {
		t.Fatalf("v2 repo + v2 text = %q, want current", rep.Skills["recording-what-you-learn"])
	}
}

// The v2 prose resolves roles through the binary, and on a v1 repository the
// binary answers the v1 paths. This is the positive control that lets one text
// serve both layouts; without it "the v2 skill also works on v1" is an assertion
// (design §8.3). The text is not installed here: what is under test is the
// fallback's answer, not the copy.
func TestV2RecordingSkillFallbackResolvesV1Paths(t *testing.T) {
	t.Chdir(newRepoWithAgents(t))
	for role, want := range map[string]string{"qna": "docs/qna", "journal": "docs/journal"} {
		var out bytes.Buffer
		if code := runLayoutPathWithVersion([]string{role}, &out, "v0.6.0"); code != exitcode.OK ||
			strings.TrimSpace(out.String()) != want {
			t.Fatalf("layout path %s on a v1 fixture = (%d, %q), want %q", role, code, out.String(), want)
		}
	}
}

// Every layout's canonical text is in the other layout's legacy catalog
// (design §8.2: the catalogs are closed over both). Without that, a repository
// carrying the other layout's copy reports diverged -- an unclassifiable local
// edit rather than a recognizable shared copy -- and the skill's replacement
// rule has no state to act on.
//
// The union is layout-blind on purpose: each layout's effective legacy set is
// the union minus its own canonical text, which is exactly what the two
// assertions below pin (this layout's canonical is absent; the other's is
// present). The currency predicate stays layout-blind too.
func TestChangedSkillAssetsHaveLegacyDigests(t *testing.T) {
	for _, schema := range []string{layout.SchemaV1, layout.SchemaV2} {
		other := layout.SchemaV2
		if schema == layout.SchemaV2 {
			other = layout.SchemaV1
		}
		for _, skill := range []string{"recording-what-you-learn", "migrating-fleet-context"} {
			current, err := drift.CanonicalSkillDigestFor(skill, layout.Layout{Manifest: layout.Manifest{Schema: schema}})
			if err != nil {
				t.Fatal(err)
			}
			otherCanonical, err := drift.CanonicalSkillDigestFor(skill, layout.Layout{Manifest: layout.Manifest{Schema: other}})
			if err != nil {
				t.Fatal(err)
			}
			legacy := drift.LegacySkillDigests(skill)
			if len(legacy) == 0 {
				t.Fatalf("%s/%s has no legacy digest; the other layout's copies would report diverged", schema, skill)
			}
			// The union contains this layout's canonical text too, because it
			// is the *other* layout's catalog entry for this skill: a v2
			// repository carrying the v1 text must report known_legacy, and
			// vice versa. Canonical-first precedence shadows that entry, so
			// carrying this layout's own text still classifies current rather
			// than known_legacy -- pinned end to end by
			// TestSkillCurrencyIsSelectedByResolvedLayout.
			contains := func(want string) bool {
				for _, d := range legacy {
					if d == want {
						return true
					}
				}
				return false
			}
			if !contains(current) {
				t.Fatalf("%s/%s: this layout's canonical text is missing from the union; the other layout's catalog would not recognize a shared copy", schema, skill)
			}
			// Each layout's effective legacy set is the union minus its own
			// canonical text, so the other layout's canonical text must be there:
			// a repository that flips layout without the skill reports
			// known_legacy, never diverged.
			if !contains(otherCanonical) {
				t.Fatalf("%s/%s: the other layout's canonical text is missing from the legacy catalog", schema, skill)
			}
		}
	}
}

// A v1 repository's copy is the v1 asset, not the flat one: that is what keeps
// this release a no-op for dotfiles and every other v1 repository.
func TestMigrationSkillMatchesEmbeddedAsset(t *testing.T) {
	root := task18RepoRoot(t)
	l := layout.Resolve(root)
	asset, err := scaffold.SkillAssetPath(l.Schema, "migrating-fleet-context")
	if err != nil {
		t.Fatal(err)
	}
	want, err := scaffold.AssetsFS.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf(".agents/skills/migrating-fleet-context/SKILL.md is not the %s asset", l.Schema)
	}
}

// The skill's v1 text pastes the canonical router so a migrating agent can
// restore it without a second tool. That is a second copy of DefaultAgentsMD,
// and the two ship in the same binary -- so nothing except this test stops a
// change to one from silently leaving the other behind, telling every migrated
// v1 repository to adopt a router the tool then reports as diverged.
//
// It reads the v1 asset rather than the repository copy: dotfiles is a v1
// repository today, but the binding is to the v1 canonical text, and the v2
// text deliberately pastes no router at all (it restores the bytes from
// `agents layout show --router`).
func TestMigrationSkillPastesTheCanonicalRouter(t *testing.T) {
	data := skillAssetBytes(t, layout.SchemaV1, "migrating-fleet-context")

	const fence = "```markdown\n# Agent context\n"
	i := strings.Index(data, fence)
	if i < 0 {
		t.Fatal("the skill no longer pastes a canonical router block; if that is deliberate, " +
			"delete this test, and if it is not, restore the block")
	}
	body := data[i+len("```markdown\n"):]
	j := strings.Index(body, "\n```")
	if j < 0 {
		t.Fatal("unterminated router code fence in the skill")
	}
	pasted := body[:j+1]

	if pasted != scaffold.DefaultAgentsMD {
		t.Errorf("the router pasted into the skill does not match scaffold.DefaultAgentsMD\n"+
			"pasted %d bytes, canonical %d bytes", len(pasted), len(scaffold.DefaultAgentsMD))
	}
}

// The skill is scaffolded into other people's repositories, which do not have
// this repository's documents. A "where this comes from" list of dated design
// and Q&A paths reads as a working reference and resolves to nothing there --
// worse than no pointer, because an agent will try to follow it.
//
// Bare dates describing an era ("the pre-2026-08-19 topology") are fine; a dated
// *filename* is a path into this repository and is not.
//
// Both layout assets are read, not the repository copy: whichever text a
// repository receives depends on its resolved layout (design §0.8), so gating
// only the v1 asset would leave the text every v2 repository gets unchecked.
// The recording skill is deliberately not scanned here: its "where this comes
// from" section cites the design document by name in both of its texts.
func TestMigrationSkillNamesNoRepoLocalDocuments(t *testing.T) {
	datedDoc := regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}-[A-Za-z0-9-]+\.md`)
	for _, schema := range []string{layout.SchemaV1, layout.SchemaV2} {
		data := skillAssetBytes(t, schema, "migrating-fleet-context")
		for _, m := range datedDoc.FindAllString(data, -1) {
			t.Errorf("the %s migrating skill names %q, a document that exists only in this repository; "+
				"the skill ships into repositories that have no copy of it", schema, m)
		}
	}
}

// `agents update` rejects any invocation without --all: "--all is required; use
// `agents wire` for one repository". A living document that says
// `agents update --apply` is telling its reader -- usually an agent -- to run a
// command that cannot work, and both the skill and the doctor remedy said
// exactly that until it was run against a real repository.
//
// TestLivingDocumentsNameOnlyRealCommands does not catch this: the command
// exists and --apply is a registered flag. What is wrong is the absent required
// flag, which no existence check can see.
func TestLivingDocumentsSpellUpdateWithAll(t *testing.T) {
	root := task18RepoRoot(t)
	// Every mention of `agents update`, to the end of the line or the closing
	// backtick -- whichever comes first. Scanning only inline code spans would
	// miss fenced ```bash blocks, which is precisely where a reader copies the
	// command from: the first version of this check passed while the runnable
	// command in the skill's own fence was wrong.
	updateSpan := regexp.MustCompile("agents update([^\n`]*)")
	checked := 0
	for _, rel := range livingDocuments(t, root) {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for _, m := range updateSpan.FindAllStringSubmatch(string(data), -1) {
			// A bare `agents update` names the command; that is prose, and the
			// skills use it that way. A span carrying flags is an invocation,
			// and an invocation without --all does not run.
			if !strings.Contains(m[1], "--") {
				continue
			}
			checked++
			if !strings.Contains(m[1], "--all") {
				t.Errorf("%s names `agents update%s`, which the CLI rejects; --all is required", rel, m[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no flag-bearing `agents update` spans found in any living document; the scan is broken")
	}
}
