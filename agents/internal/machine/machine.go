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
		return filepath.Join(".local", "state", "agents")
	}
	return filepath.Join(home, ".local", "state", "agents")
}

// ID returns the stable identifier for this machine, generating it on first
// call. It is deliberately NOT derived from the live hostname: hostnames change,
// and a changed hostname would split one machine's history into two identities
// with no way to notice after the fact.
func ID() (string, error) {
	path := filepath.Join(StateDir(), "machine-id")
	if b, err := os.ReadFile(path); err == nil {
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
