package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/record"
)

// CacheReport separates the two reasons a transcript is not in the cache,
// because they call for different actions: Unreachable means it was here and
// is gone, Elsewhere means go to that machine (or wait for `agents distill`).
type CacheReport struct {
	Copied      int
	Skipped     int // already cached
	Unreachable int // this machine, but the file is not there
	Elsewhere   int // another machine holds it
	Details     []string
}

// Cache copies transcripts that are reachable from here into a git-ignored
// directory inside .agents/. It never copies a transcript belonging to another
// machine, and never one whose pointer did not verify.
func Cache(agentsDir, thisMachine string, recs []record.Record) (CacheReport, error) {
	var rep CacheReport
	seen := map[string]bool{}

	for _, r := range recs {
		if r.Transcript == "" || !r.PointerVerified {
			continue
		}
		// One session yields many records naming one transcript -- a stop plus
		// a subagent-stop per subagent. Cleaning first so that two spellings of
		// one path are one transcript, here and in the destination name.
		src := filepath.Clean(r.Transcript)
		if seen[src] {
			continue
		}
		seen[src] = true

		if r.Machine != thisMachine {
			rep.Elsewhere++
			rep.Details = append(rep.Details,
				fmt.Sprintf("elsewhere (%s): %s", r.Machine, r.Transcript))
			continue
		}
		if _, err := os.Stat(src); err != nil {
			rep.Unreachable++
			rep.Details = append(rep.Details, "unreachable: "+r.Transcript)
			continue
		}

		dst := filepath.Join(agentsDir, ".trace-cache", harnessDir(r.Harness), cacheName(src))
		if _, err := os.Stat(dst); err == nil {
			rep.Skipped++
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return rep, err
		}
		rep.Copied++
	}
	return rep, nil
}

// harnessDir reduces the harness name to exactly one path component.
//
// Harness is the only field of a record that becomes a directory name, and
// records are read out of a tracked file that anyone with commit access can
// write. Left as typed, a harness of "../../.." makes `agents trace cache`
// write outside the repository -- a pull would become a write primitive aimed
// anywhere the user can write. filepath.Base is not enough on its own, because
// Base("..") is "..".
func harnessDir(name string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
	// Trimming dots removes "." and ".." along with them, and leaves nothing
	// that filepath.Join could read as a traversal.
	if mapped = strings.Trim(mapped, "."); mapped == "" {
		return "unknown"
	}
	return mapped
}

// cacheName names the copy after the whole source path, not just its basename.
//
// Nothing in a record guarantees that basenames are unique: two sessions in two
// directories can arrive carrying one name. Keyed on the basename alone the
// second copy either overwrites the first or -- worse, because it reports
// success -- is counted as already cached, leaving one session's transcript
// filed under a name that says it is the other's. Keyed on the full path, two
// sources can never claim one name, and the same source always claims the same
// one, which is what makes a second run a no-op.
func cacheName(src string) string {
	sum := sha256.Sum256([]byte(src))
	tag := hex.EncodeToString(sum[:])[:12]

	base := filepath.Base(src)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		base = "transcript"
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // a dotfile such as ".jsonl": all extension, no stem
		stem, ext = base, ""
	}
	return stem + "-" + tag + ext
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Write to a temporary name and rename, so an interrupted copy never leaves
	// a truncated transcript behind under a name that says it is complete.
	// O_TRUNC because the temporary from such an interruption is still there:
	// without it the tail of the longer old file survives past the new content.
	tmp := dst + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
