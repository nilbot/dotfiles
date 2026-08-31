package drift

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/scaffold"
)

// DriftReport encapsulates the deterministic drift inspection findings for a repository.
type DriftReport struct {
	RepoPath      string            `json:"repo_path"`
	RouterState   RouterState       `json:"router_state"`
	SymlinkState  string            `json:"symlink_state"`  // "ok" | "broken" | "not_symlink" | "missing"
	DomainState   string            `json:"domain_state"`   // "ok" | "missing"
	Skills        map[string]string `json:"skills"`         // skill_name -> ComponentState
	DocsStores    map[string]bool   `json:"docs_stores"`    // design, plans, journal, qna
	MisplacedDocs []string          `json:"misplaced_docs"` // e.g. plans living in docs/journal/
	Diff          string            `json:"diff,omitempty"` // Unified diff against canonical router
}

// InspectRepo performs deterministic inspection of the repository context layout and contents.
func InspectRepo(root string) (DriftReport, error) {
	report := DriftReport{
		RepoPath:      root,
		Skills:        make(map[string]string),
		DocsStores:    map[string]bool{"design": false, "plans": false, "journal": false, "qna": false},
		MisplacedDocs: []string{},
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
		if digest == CanonicalRouterDigest() {
			report.RouterState = RouterCleanCurrent
		} else if IsLegacyRouterDigest(digest) {
			report.RouterState = RouterCleanLegacy
		} else {
			report.RouterState = RouterDrifted
			report.Diff = unifiedDiff("canonical/AGENTS.md", "repo/AGENTS.md", scaffold.DefaultAgentsMD, string(agentsData))
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
			canDigest, err := CanonicalSkillDigest(skillName)
			if err == nil && h == canDigest {
				report.Skills[skillName] = string(ComponentOK)
			} else if IsLegacySkillDigest(skillName, h) {
				report.Skills[skillName] = string(ComponentCleanLegacy)
			} else {
				report.Skills[skillName] = string(ComponentCustomized)
			}
		}
	}

	skillsDir := filepath.Join(root, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if _, already := report.Skills[name]; already {
				continue
			}
			skillFile := filepath.Join(skillsDir, name, "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				report.Skills[name] = string(ComponentCustomized)
			}
		}
	}

	// 5. Docs stores inspection
	for store := range report.DocsStores {
		dir := filepath.Join(root, "docs", store)
		if sInfo, err := os.Stat(dir); err == nil && sInfo.IsDir() {
			report.DocsStores[store] = true
		}
	}

	// 6. Misplaced docs inspection
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

			if strings.HasSuffix(name, "-plan.md") && !strings.HasPrefix(relSlash, "docs/plans/") {
				report.MisplacedDocs = append(report.MisplacedDocs, relSlash)
			}
			if strings.HasSuffix(name, "-design.md") && !strings.HasPrefix(relSlash, "docs/design/") {
				report.MisplacedDocs = append(report.MisplacedDocs, relSlash)
			}
			return nil
		})
	}
	sort.Strings(report.MisplacedDocs)

	return report, nil
}

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
