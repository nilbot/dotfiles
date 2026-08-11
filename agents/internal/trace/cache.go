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

// CacheReport separates the reasons a transcript is not in the cache, because
// they call for different actions: Unreachable means it was here and is gone or
// cannot be read, Elsewhere means go to that machine (or wait for `agents
// distill`), Unverified means the record cannot say which session the path
// belongs to. None of them is silent -- a run that copies nothing must say why,
// or it reads exactly like a run with nothing to do.
type CacheReport struct {
	Copied      int
	Skipped     int // already cached
	Unreachable int // this machine, but the file is not there or cannot be read
	Elsewhere   int // another machine holds it
	Unverified  int // the pointer never verified, so the path is only a lead
	Details     []string
}

// Cache copies transcripts that are reachable from here into root, which the
// caller resolves with repo.TraceCacheDir. It never copies a transcript
// belonging to another machine, and never one whose pointer did not verify.
//
// root is passed in rather than derived from .agents/ because the cache is not
// tracked context: it is machine-local content whose only copy may be the one
// here, and it belongs in the git common directory, shared by every worktree
// and outside all of them. See repo.TraceCacheDir for why.
//
// One unusable record is not a failed run. Anything that stops this one
// transcript -- another machine, a deleted file, a permission, a source that is
// not a regular file, a copy that fails halfway -- is counted, detailed, and the
// run continues. The error return is reserved for what stops every record, so a
// caller that gets one knows the report is not merely incomplete but absent.
func Cache(root, thisMachine string, recs []record.Record) (CacheReport, error) {
	var rep CacheReport

	if err := os.MkdirAll(root, 0o755); err != nil {
		return rep, err
	}

	for _, t := range dedupe(recs) {
		r := t.rec
		if !r.PointerVerified {
			// Not copied, but not invisible either: an unverified pointer is a
			// normal state (see package pointer), so this is the largest class
			// of transcript that is on this disk and still not in the cache.
			rep.Unverified++
			rep.Details = append(rep.Details, "unverified pointer: "+r.Transcript)
			continue
		}
		if r.Machine != thisMachine {
			rep.Elsewhere++
			rep.Details = append(rep.Details,
				fmt.Sprintf("elsewhere (%s): %s", r.Machine, r.Transcript))
			continue
		}
		// Lstat, not Stat: Stat follows the last component, so a record naming a
		// symlink would materialise the target's bytes under a name that says it
		// is a transcript. Transcript comes out of a tracked file that anyone
		// with commit access can write, exactly as Harness does, and a symlink to
		// ~/.ssh/id_ed25519 is a one-line commit away from being copied into the
		// working tree of everyone who runs this.
		fi, err := os.Lstat(t.src)
		if err != nil {
			rep.Unreachable++
			rep.Details = append(rep.Details, "unreachable: "+r.Transcript)
			continue
		}
		if !fi.Mode().IsRegular() {
			rep.Unreachable++
			rep.Details = append(rep.Details,
				fmt.Sprintf("unreachable (not a regular file: %s): %s", kindOf(fi.Mode()), r.Transcript))
			continue
		}

		dst := filepath.Join(root, harnessDir(r.Harness), cacheName(t.src))
		if _, err := os.Stat(dst); err == nil {
			rep.Skipped++
			continue
		}
		if err := copyFile(t.src, dst); err != nil {
			rep.Unreachable++
			rep.Details = append(rep.Details,
				fmt.Sprintf("unreachable (%v): %s", err, r.Transcript))
			continue
		}
		rep.Copied++
	}
	return rep, nil
}

// target is one transcript to consider, and the record that speaks for it.
type target struct {
	rec record.Record
	src string
}

// dedupe reduces the records to one entry per transcript, keyed on machine and
// path together.
//
// Path alone is wrong: identical transcript paths across machines are the
// expected condition, not an edge case -- both harnesses write $HOME-relative
// paths that look the same everywhere, which is why records carry a machine at
// all (see package machine). Keyed on the path alone, a local record and a
// remote one naming it collapse into whichever the query happened to return
// first, so the same repository reports "on another machine" or "copied 1" for
// the same pair depending on which session stopped last.
//
// Within one (machine, path) a verified sighting outranks an unverified one:
// one session writes many records for one transcript, and if any of them
// confirmed the pointer then the pointer is confirmed. Otherwise the first
// record wins, so a query's ordering never decides an outcome.
func dedupe(recs []record.Record) []*target {
	var out []*target
	at := map[string]int{}
	for _, r := range recs {
		if r.Transcript == "" {
			continue
		}
		// Cleaning first so that two spellings of one path are one transcript,
		// here and in the destination name.
		src := filepath.Clean(r.Transcript)
		key := r.Machine + "\x00" + src
		if i, ok := at[key]; ok {
			if r.PointerVerified && !out[i].rec.PointerVerified {
				out[i].rec = r
			}
			continue
		}
		at[key] = len(out)
		out = append(out, &target{rec: r, src: src})
	}
	return out
}

// kindOf names what a non-regular source is, because "unreachable" alone does
// not tell the reader whether to go looking for the file or fix the record.
func kindOf(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m&os.ModeSymlink != 0:
		return "symlink"
	default:
		return m.Type().String()
	}
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
