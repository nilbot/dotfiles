// Package scaffold creates the tracked .agents/ layout in a repository and the
// two thin files that make a harness notice it.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/layout"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// ErrLocalInLinkedWorktree refuses --local where it would silently delete work.
//
// --local's only mechanism is an exclude entry, and a linked worktree shares
// info/exclude with its main checkout. Measured: already-tracked .agents/ files
// stay visible, but every new one is ignored everywhere -- `git check-ignore`
// matches it, `git status --untracked-files=all` omits it, `git add .` skips it.
// The handoffs, memory notes and traces this tool exists to preserve would be
// dropped without a message. A warning in scrollback is not a defence against
// that, so this is an error.
var ErrLocalInLinkedWorktree = errors.New("--local is not supported inside a linked worktree: info/exclude is shared with the main checkout, so excluding /.agents/ here would hide every new file under .agents/ from git in every worktree of this repo, including files already written. Run `agents init --local` in the main checkout instead, or drop --local here")

// LegacyDoctorInstruction is preserved for backwards compatibility with repositories scaffolded under earlier versions.
const LegacyDoctorInstruction = "Run `agents doctor` early and report any warnings before relying on this context."

// DoctorInstruction is part of newly generated context only. Create never
// rewrites an existing instruction file, so restoring this marker is not a migration.
const DoctorInstruction = "- If the `agents` CLI is installed, run `agents doctor` early and report any warnings before relying on this context.\n- If `agents` is not installed on this machine, skip machine wiring checks and adhere directly to the repository instructions above."

// DefaultAgentsMD is the root instruction file for coding agent harnesses.
const DefaultAgentsMD = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/plans/`" + ` — implementation plans
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself
and a missing hook fails silently.
` + DoctorInstruction + `

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + `
skill; it is not repo-specific and is not restated here.
`

// gitattributesLines are tracked on purpose: they are a statement about how this
// repository merges and renders, which belongs to the repository.
//
// merge=union used to be here and is gone with the thing it protected. It
// existed because two branches appending to the same tracked daily trace file
// produced conflict markers that are not valid JSON, which a line-oriented
// reader silently drops. The index is machine-local now, so nothing tracked
// appends concurrently and the attribute has nothing to defend.
var gitattributesLines = []string{
	".agents/** linguist-generated=true",
}

// v2GitattributesLines adds the manifest exception after the blanket rule.
// gitattributes is last-match-wins, so the one .agents/ file a maintainer has to
// read in a diff stops being linguist-generated while every other one stays
// hidden. It is deliberately not part of gitattributesLines: a v1 repository has
// no manifest, and `init` must not edit its tracked .gitattributes to describe a
// file that is not there.
var v2GitattributesLines = append(append([]string{}, gitattributesLines...),
	".agents/layout.json -linguist-generated")

// excludeLines are machine-specific generated paths. They go in
// .git/info/exclude rather than the repo's tracked .gitignore: an ignore list
// belongs to the repository's maintainers, not to this tool.
var excludeLines = []string{
	"/.claude/settings.json",
	"/.claude/.agents-wire.lock",
	"/.claude/skills",
	"/.codex/hooks.json",
	"/.codex/.agents-wire.lock",
	"/.codex/skills",
	"/.agents/hooks.json",
	"/.agents/.agents-wire.lock",
}

// dirs is what .agents/ is for after the 2026-08-19 redesign: machine wiring
// and repo-specific procedure, nothing else.
//
// memory/ and reports/handoff/ held knowledge and were retired with the
// apparatus that filled them. reports/{specs,plans,analysis} went with them for
// the same reason -- they duplicated a docs/ directory every repository already
// has, which is the tier error the redesign exists to correct.
var dirs = []string{
	"skills",
}

// embeddedAssets are the layout-independent files CreateWithLayout installs.
// Neither the four docs/<role>/README.md files nor the two bundled skills are
// here: both travel with the store the resolved layout puts them in (design
// §0.8), so CreateWithLayout writes them from the layout it was handed.
var embeddedAssets = []struct {
	relPath   string
	assetPath string
}{
	{".agents/AGENTS.md", "assets/dotagents/AGENTS.md"},
}

// bundledSkills are the skills embedded in the binary and refreshed by it.
// Which text is canonical for each is a function of the resolved layout.
var bundledSkills = []string{
	"recording-what-you-learn",
	"migrating-fleet-context",
}

// skillAssets maps schema -> skill name -> embedded asset path. The flat path
// holds the v2 text; v1/SKILL.md holds the frozen v1 text. Both exist for both
// skills: the only mechanical remedy for a non-current bundled skill is a
// fleet-wide `agents update --all --apply`, and the playbook forbids exactly
// that during the pilot window, so freezing one more prose asset is cheaper
// than leaving every v1 repository red for the length of the pilot (design
// §0.8).
var skillAssets = map[string]map[string]string{
	layout.SchemaV1: {
		"recording-what-you-learn": "assets/skills/recording-what-you-learn/v1/SKILL.md",
		"migrating-fleet-context":  "assets/skills/migrating-fleet-context/v1/SKILL.md",
	},
	layout.SchemaV2: {
		"recording-what-you-learn": "assets/skills/recording-what-you-learn/SKILL.md",
		"migrating-fleet-context":  "assets/skills/migrating-fleet-context/SKILL.md",
	},
}

// SkillAssetPath resolves one embedded skill asset for a schema.
func SkillAssetPath(schema, skillName string) (string, error) {
	byName, ok := skillAssets[schema]
	if !ok {
		return "", fmt.Errorf("no skill assets for schema %q", schema)
	}
	path, ok := byName[skillName]
	if !ok {
		return "", fmt.Errorf("no asset for skill %q", skillName)
	}
	return path, nil
}

// Create is the v1-compatible entry point: a caller that has not resolved a
// layout gets the implicit one, exactly as before layouts existed. Like
// CreateWithLayout it is idempotent -- running it on an initialized repository
// changes nothing.
func Create(root string, local bool) error {
	return CreateWithLayout(root, local, layout.V1ForRoot(root))
}

// CreateWithLayout scaffolds .agents/ plus the stores the resolved layout
// declares, and writes the router that layout selects. It never creates docs/
// for a v2 layout: docs/{design,plans,journal,qna} are v1's stores, and an
// older binary recreating them on a v2 repository is the anti-shell failure
// design §1.2 exists to prevent.
//
// A layout whose manifest is already in the repository is that manifest's own
// declaration, so this writes no layout for it (design §7.5): the machine
// exclude file is still updated, because it is machine state about wiring rather
// than layout data, but no store, router, or gitattributes line is added. It
// deliberately does not create a store the manifest declares and the tree lacks:
// the manifest is the authority, and a half-created store is Task 10's
// `store_missing` blocker rather than init's remedy. Support and validity are
// the caller's gate (layoutRefusal); ManifestPath is what tells a manifest read
// back from the repository apart from a layout a caller constructed in order to
// create it.
func CreateWithLayout(root string, local bool, l layout.Layout) error {
	// First, before anything is written: a refusal that leaves a half-scaffolded
	// repo behind is a worse outcome than the one it is refusing.
	if local {
		linked, err := repo.IsLinkedWorktree(root)
		if err != nil {
			return err
		}
		if linked {
			return ErrLocalInLinkedWorktree
		}
	}

	// The machine exclude file is written before the layout question is asked,
	// and for every layout: it is machine state about wiring, not layout data
	// (design §0.7 -- info/exclude is "never read as a layout input, never
	// written from layout data"). Suppressing it under the §7.5 no-op below
	// would leave a supported v2 repository with untracked .claude/, .codex/,
	// .agents/hooks.json and .agents/.agents-wire.lock, so a `git add .` there
	// would commit one machine's settings.json.
	lines := excludeLines
	if local {
		// --local: the whole directory stays out of the repo, for repos where
		// committing agent artifacts is not acceptable. Same layout either way.
		lines = append(append([]string{}, excludeLines...), "/.agents/")
	}
	// Ask git where the exclude file is rather than assuming <root>/.git/info:
	// in a linked worktree .git is a regular file and that path cannot exist.
	exclude, err := repo.InfoExcludePath(root)
	if err != nil {
		return err
	}
	if err := appendMissingLines(exclude, lines); err != nil {
		return err
	}

	// design §7.5: a manifest already in this repository is the authority, so
	// `init` is a no-op for it -- not even a declared store the tree lacks, and
	// not a missing router either. A half-created store is the migration
	// command's `store_missing` blocker; a missing router is what `drift` and
	// `doctor` report. Adding either here would be this function inventing a
	// layout the manifest does not describe. This suppresses the layout writes
	// only; the exclude write above has already happened.
	if l.Schema == layout.SchemaV2 && l.ManifestPath != "" &&
		l.LayoutStatus == layout.StatusActive && len(l.Problems) == 0 {
		return nil
	}

	agents := filepath.Join(root, ".agents")
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(agents, d), 0o755); err != nil {
			return err
		}
	}
	// git does not track empty directories, and an .agents/ that vanishes on
	// clone is worse than one with placeholder files.
	for _, d := range dirs {
		keep := filepath.Join(agents, d, ".gitkeep")
		if _, err := os.Stat(keep); os.IsNotExist(err) {
			if err := os.WriteFile(keep, nil, 0o644); err != nil {
				return err
			}
		}
	}

	// One loop for both schemas: v1's stores are the synthesized docs/<role>,
	// a v2 manifest's are wherever it says. The README that explains a store
	// travels with the store, so a repository never gets a bare directory whose
	// purpose is only in the binary.
	for _, role := range layout.Roles() {
		store, ok := layout.Path(l, role)
		if !ok {
			return fmt.Errorf("layout declares no %s store", role)
		}
		if err := os.MkdirAll(filepath.Join(root, store), 0o755); err != nil {
			return err
		}
		assetPath := "assets/docs/" + role + "/README.md"
		if err := writeIfAbsentFromFS(filepath.Join(root, store, "README.md"), AssetsFS, assetPath); err != nil {
			return err
		}
	}

	for _, a := range embeddedAssets {
		if err := writeIfAbsentFromFS(filepath.Join(root, a.relPath), AssetsFS, a.assetPath); err != nil {
			return err
		}
	}

	// The resolved layout selects each bundled skill's canonical text (design
	// §0.8), from the layout the caller handed us rather than a second read of
	// the manifest. This is what keeps a fresh v1 repository byte-identical to
	// what v0.5.1 wrote: the flat asset is now the v2 text, and installing it
	// into a v1 repository would make `agents init` produce a repository that
	// `agents drift` immediately calls customized.
	for _, skillName := range bundledSkills {
		assetPath, err := SkillAssetPath(l.Schema, skillName)
		if err != nil {
			return err
		}
		relPath := filepath.Join(".agents", "skills", skillName, "SKILL.md")
		if err := writeIfAbsentFromFS(filepath.Join(root, relPath), AssetsFS, assetPath); err != nil {
			return err
		}
	}

	router := DefaultAgentsMD
	if l.Schema == layout.SchemaV2 {
		router = layout.V2AgentsMD
	}
	if err := writeIfAbsent(filepath.Join(root, "AGENTS.md"), router); err != nil {
		return err
	}
	if err := linkIfAbsent(filepath.Join(root, "CLAUDE.md"), "AGENTS.md"); err != nil {
		return err
	}

	attributes := gitattributesLines
	if l.Schema == layout.SchemaV2 {
		attributes = v2GitattributesLines
	}
	return appendMissingLines(filepath.Join(root, ".gitattributes"), attributes)
}

func writeIfAbsentFromFS(path string, fs embed.FS, assetPath string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	content, err := fs.ReadFile(assetPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func writeIfAbsent(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func linkIfAbsent(path, target string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

// appendMissingLines adds only the lines that are not already present, so
// running init twice does not duplicate anything and a hand-edited file keeps
// its edits.
func appendMissingLines(path string, want []string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(l)] = true
	}

	var add []string
	for _, l := range want {
		if !have[l] {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	var b strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	for _, l := range add {
		b.WriteString(l + "\n")
	}
	_, err = f.WriteString(b.String())
	return err
}

// RefreshInfrastructuralSkills refreshes the 100% agents-owned infrastructural
// skill (migrating-fleet-context) to the text for the repository's resolved
// layout. It never writes recording-what-you-learn or a repository-specific
// skill (design §0.8).
//
// It reads first and writes only when the bytes differ, so an unmodified v1
// repository is a literal no-op rather than an identical rewrite: the design
// promises "byte-identical", and an unconditional write would leave mtime churn
// in every v1 repository on every `agents update --all --apply`.
//
// This is not how a repository becomes v2. A v1 repository's skill copy is the
// frozen v1 text and does not know `agents layout migrate`; the CLI performs
// the flip, and this refresh only keeps the agents-owned skill current once the
// layout is v2.
func RefreshInfrastructuralSkills(root, layoutVersion string) error {
	assetPath, err := SkillAssetPath(layoutVersion, "migrating-fleet-context")
	if err != nil {
		return err
	}
	content, err := AssetsFS.ReadFile(assetPath)
	if err != nil {
		return err
	}
	target := filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md")
	if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, content) {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, content, 0o644)
}
