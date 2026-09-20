package drift

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/layout"
)

// DriftReport encapsulates the deterministic drift inspection findings for a repository.
type DriftReport struct {
	RepoPath      string            `json:"repo_path"`
	RouterState   RouterState       `json:"router_state"`
	SymlinkState  string            `json:"symlink_state"`  // "ok" | "broken" | "not_symlink" | "missing"
	DomainState   string            `json:"domain_state"`   // "ok" | "missing"
	Skills        map[string]string `json:"skills"`         // embedded skill_name -> ComponentState
	LocalSkills   []string          `json:"local_skills"`   // repo-specific skills: listed, never judged
	DocsStores    map[string]bool   `json:"docs_stores"`    // deprecated role presence; removed no earlier than v0.8.0
	MisplacedDocs []string          `json:"misplaced_docs"` // e.g. plans living in docs/journal/
	Diff          string            `json:"diff,omitempty"` // Unified diff against canonical router

	LayoutVersion     string            `json:"layout_version"`               // "v1" | "v2" | "unknown"
	MinMutVerFloor    string            `json:"min_mut_ver_floor,omitempty"`  // manifest value; empty on v1
	LayoutStatus      string            `json:"layout_status,omitempty"`      // "active" | "migrating"
	Stores            map[string]string `json:"stores"`                       // resolved role -> repository-relative path
	Unsupported       string            `json:"unsupported,omitempty"`        // "" | unknown_schema | below_floor | unreleased | invalid
	UnsupportedDetail string            `json:"unsupported_detail,omitempty"` // human-readable reason
}

// InspectRepo performs deterministic inspection of the repository context layout and contents.
//
// runningVersion is the version of the running binary, compared against the
// resolved layout's min_mut_ver_floor: reads are always permitted, but a report
// on a layout this binary may not mutate says why (design §7.3).
func InspectRepo(root, runningVersion string) (DriftReport, error) {
	report := DriftReport{
		RepoPath:      root,
		Skills:        make(map[string]string),
		LocalSkills:   []string{},
		DocsStores:    map[string]bool{"design": false, "plans": false, "journal": false, "qna": false},
		MisplacedDocs: []string{},
	}

	// The resolved layout is the one source of physical paths (design §5.1).
	// Resolved once: it selects the canonical router and skill text, the stores
	// the presence check and the misplacement walk read, and the support
	// verdict. The currency predicate and the state names stay layout-blind.
	l := layout.Resolve(root)
	report.LayoutVersion = "v1"
	if l.Schema == layout.SchemaV2 {
		report.LayoutVersion = "v2"
	} else if l.Schema != layout.SchemaV1 {
		report.LayoutVersion = "unknown"
	}
	report.MinMutVerFloor = l.MinMutVerFloor
	report.LayoutStatus = l.LayoutStatus
	if len(l.Problems) > 0 {
		if layout.HasProblem(l.Problems, layout.ProblemSchemaUnknown) {
			report.Unsupported = "unknown_schema"
		} else {
			report.Unsupported = "invalid"
		}
		report.UnsupportedDetail = ProblemsText(l.Problems)
	} else if ok, reason := layout.Support(runningVersion, l); !ok {
		if reason == "migrating" {
			report.LayoutStatus = layout.StatusMigrating
		} else {
			report.Unsupported = reason
			report.UnsupportedDetail = fmt.Sprintf("requires agents >= %s", l.MinMutVerFloor)
		}
	}

	// 1. Router inspection (AGENTS.md)
	agentsPath := filepath.Join(root, "AGENTS.md")
	agentsData, err := os.ReadFile(agentsPath)
	if err != nil {
		if os.IsNotExist(err) {
			report.RouterState = RouterMissing
		} else {
			return report, err
		}
	} else {
		digest := DigestBytes(agentsData)
		if digest == CanonicalRouterDigestFor(l) {
			report.RouterState = RouterCurrent
		} else if IsLegacyRouterDigest(digest) {
			report.RouterState = RouterKnownLegacy
		} else {
			report.RouterState = RouterDiverged
			report.Diff = unifiedDiff("canonical/AGENTS.md", "repo/AGENTS.md", canonicalRouterText(l), string(agentsData))
		}
	}

	// 2. Symlink inspection (CLAUDE.md)
	claudePath := filepath.Join(root, "CLAUDE.md")
	info, err := os.Lstat(claudePath)
	if err != nil {
		if os.IsNotExist(err) {
			report.SymlinkState = "missing"
		} else {
			report.SymlinkState = "broken"
		}
	} else if info.Mode()&os.ModeSymlink == 0 {
		report.SymlinkState = "not_symlink"
	} else {
		target, err := os.Readlink(claudePath)
		if err != nil || target != "AGENTS.md" {
			report.SymlinkState = "broken"
		} else {
			if _, err := os.Stat(claudePath); err != nil {
				report.SymlinkState = "broken"
			} else {
				report.SymlinkState = "ok"
			}
		}
	}

	// 3. Domain context (.agents/AGENTS.md)
	domainPath := filepath.Join(root, ".agents", "AGENTS.md")
	if info, err := os.Stat(domainPath); err == nil && !info.IsDir() {
		report.DomainState = "ok"
	} else {
		report.DomainState = "missing"
	}

	// 4. Skills inspection (.agents/skills/)
	trackedSkills := []string{"recording-what-you-learn", "migrating-fleet-context"}
	for _, skillName := range trackedSkills {
		skillFile := filepath.Join(root, ".agents", "skills", skillName, "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				report.Skills[skillName] = string(ComponentMissing)
			} else {
				return report, err
			}
		} else {
			h := DigestBytes(data)
			canDigest, err := CanonicalSkillDigestFor(skillName, l)
			if err == nil && h == canDigest {
				report.Skills[skillName] = string(ComponentCurrent)
			} else if IsLegacySkillDigest(skillName, h) {
				report.Skills[skillName] = string(ComponentKnownLegacy)
			} else {
				report.Skills[skillName] = string(ComponentDiverged)
			}
		}
	}

	// Repository-specific skills are listed, never classified. `.agents/skills/`
	// is where design section 2 says they belong, so `agents` owning the whole
	// directory made a repository dirty for using the feature as intended --
	// and no migration could clear it, because there is nothing to fix. The
	// tool judges only what it embeds.
	skillsDir := filepath.Join(root, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if _, tracked := report.Skills[name]; tracked {
				continue
			}
			skillFile := filepath.Join(skillsDir, name, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				report.LocalSkills = append(report.LocalSkills, name)
			}
		}
	}
	sort.Strings(report.LocalSkills)

	// 5. Stores inspection. The resolved layout's role -> path map is the
	// answer design §7.3 reports as `stores`; docs_stores stays populated
	// beside it as the deprecated presence alias for v1 consumers, on both
	// layouts, until v0.8.0 at the earliest.
	report.Stores = make(map[string]string, len(l.Stores))
	for role, path := range l.Stores {
		report.Stores[role] = path
		sInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil && sInfo.IsDir() {
			report.DocsStores[role] = true
		}
	}

	// 6. Misplaced docs inspection
	if l.Schema == layout.SchemaV2 {
		report.MisplacedDocs = append(report.MisplacedDocs, misplacedDocsV2(root, l)...)
	} else {
		report.MisplacedDocs = append(report.MisplacedDocs, misplacedDocsV1(root)...)
	}
	sort.Strings(report.MisplacedDocs)

	return report, nil
}

// misplacedDocsV1 is the v1 walker, unchanged: docs/ is the only tree, and
// docs/archive/ is immutable by repository rule, so nothing in it can be
// "misplaced": a report here is an instruction to move a file that must not
// move. Excluding it keeps this classifier and the migrating-fleet-context
// skill agreeing on one definition.
func misplacedDocsV1(root string) []string {
	var misplaced []string
	docsRoot := filepath.Join(root, "docs")
	if dInfo, err := os.Stat(docsRoot); err == nil && dInfo.IsDir() {
		_ = filepath.WalkDir(docsRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			name := d.Name()

			if strings.HasPrefix(relSlash, "docs/archive/") {
				return nil
			}

			if strings.HasSuffix(name, "-plan.md") && !strings.HasPrefix(relSlash, "docs/plans/") {
				misplaced = append(misplaced, relSlash)
			}
			if strings.HasSuffix(name, "-design.md") && !strings.HasPrefix(relSlash, "docs/design/") {
				misplaced = append(misplaced, relSlash)
			}
			return nil
		})
	}
	return misplaced
}

// misplacedDocsV2 walks the declared stores, never the vault: a `*-plan.md`
// note in a content vault is not an agents plan artifact (design §7.3). The
// archive is excluded by the manifest's own path, because a v2 repository may
// put it anywhere; a file inside a store that is not that store's own kind is
// misplaced.
func misplacedDocsV2(root string, l layout.Layout) []string {
	var misplaced []string
	archive := ""
	if l.Archive != "" {
		archive = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(l.Archive)), "/")
	}
	for role, storePath := range l.Stores {
		if storePath == "" {
			continue
		}
		storeRoot := filepath.Join(root, filepath.FromSlash(storePath))
		sInfo, err := os.Stat(storeRoot)
		if err != nil || !sInfo.IsDir() {
			continue
		}
		_ = filepath.WalkDir(storeRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			if archive != "" && (relSlash == archive || strings.HasPrefix(relSlash, archive+"/")) {
				return nil
			}
			name := d.Name()
			if strings.HasSuffix(name, "-plan.md") && role != layout.RolePlans {
				misplaced = append(misplaced, relSlash)
			}
			if strings.HasSuffix(name, "-design.md") && role != layout.RoleDesign {
				misplaced = append(misplaced, relSlash)
			}
			return nil
		})
	}
	return misplaced
}

// ProblemsText renders a layout's validation problems as one line, so an
// invalid manifest's `unsupported_detail` names every rule it broke rather
// than only the first.
//
// Exported because `doctor` prints the same list in its own one-line detail.
// The two callers differ only in what they add around it, and doctor's private
// copy of this renderer was a copy that drifted.
func ProblemsText(problems []layout.Problem) string {
	parts := make([]string, 0, len(problems))
	for _, p := range problems {
		text := p.Code
		if p.Path != "" {
			text += " " + p.Path
		}
		if p.Detail != "" {
			text += " (" + p.Detail + ")"
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "; ")
}

// isCurrent is the currency predicate (design §0.2, §7.3): strict equality
// between the repository's embedded assets and the running binary's canonical
// assets for the resolved layout, and nothing else. It does not consider
// ownership, layout preference, or health -- `doctor` asks those questions.
func isCurrent(report DriftReport) bool {
	if report.Unsupported != "" || report.LayoutStatus == layout.StatusMigrating {
		return false
	}
	if report.RouterState != RouterCurrent {
		return false
	}
	if report.SymlinkState != "ok" {
		return false
	}
	if report.DomainState != "ok" {
		return false
	}
	for _, state := range report.Skills {
		if state != string(ComponentCurrent) {
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

// IsCurrent is the exported spelling of isCurrent, for package main where the
// `drift` and fleet commands decide their exit codes from it.
func IsCurrent(report DriftReport) bool { return isCurrent(report) }

func unifiedDiff(oldName, newName, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)

	m, n := len(oldLines), len(newLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			if oldLines[i] == newLines[j] {
				dp[i+1][j+1] = dp[i][j] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i+1][j+1] = dp[i+1][j]
			} else {
				dp[i+1][j+1] = dp[i][j+1]
			}
		}
	}

	type editOp struct {
		op   byte // '=', '-', '+'
		line string
	}
	var ops []editOp
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			ops = append(ops, editOp{op: '=', line: oldLines[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, editOp{op: '+', line: newLines[j-1]})
			j--
		} else if i > 0 && (j == 0 || dp[i][j-1] < dp[i-1][j]) {
			ops = append(ops, editOp{op: '-', line: oldLines[i-1]})
			i--
		}
	}
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("--- %s\n", oldName))
	b.WriteString(fmt.Sprintf("+++ %s\n", newName))
	b.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines)))
	for _, op := range ops {
		switch op.op {
		case '=':
			b.WriteString(" " + op.line + "\n")
		case '-':
			b.WriteString("-" + op.line + "\n")
		case '+':
			b.WriteString("+" + op.line + "\n")
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
