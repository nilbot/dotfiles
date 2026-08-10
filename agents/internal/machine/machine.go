// Package machine resolves this computer's stable identity.
//
// Every trace record carries it, because both harnesses write $HOME-relative
// transcript paths that look identical on every machine you own. A record
// without provenance is not merely incomplete elsewhere -- it resolves to a
// different session that happens to occupy the same path.
package machine

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// StateDir is where machine-local, never-tracked state lives.
func StateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "agents")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "agents")
	}
	return filepath.Join(home, ".local", "state", "agents")
}

// ReadID observes an existing machine identity without creating machine state.
func ReadID() (string, error) {
	return ReadIDAt(filepath.Join(StateDir(), "machine-id"))
}

// ReadIDAt observes the identity at path without creating or repairing it.
func ReadIDAt(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", errors.New("machine id is empty")
	}
	return id, nil
}

// ID returns the stable identifier for this machine, generating it on first
// call. It is deliberately NOT derived from the live hostname: hostnames change,
// and a changed hostname would split one machine's history into two identities
// with no way to notice after the fact.
func ID() (string, error) {
	// If $HOME is unset and XDG_STATE_HOME is unset, we cannot proceed safely.
	// We would fall back to os.TempDir(), which is not stable across invocations.
	if os.Getenv("XDG_STATE_HOME") == "" {
		_, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}

	path := filepath.Join(StateDir(), "machine-id")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// Any error other than "file doesn't exist" is a problem we should
		// report (EACCES, I/O error, etc). We must not silently mint a new id.
		return "", err
	}
	if err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}

	id, err := generate()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func generate() (string, error) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	host, _, _ = strings.Cut(host, ".") // strip .local and any domain
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(host), "-"), "-")
	if slug == "" {
		slug = "unknown"
	}

	// Short hostnames repeat across machines; two random bytes make the id
	// unique without making it unreadable.
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return slug + "-" + hex.EncodeToString(b[:]), nil
}
