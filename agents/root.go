package main

import (
	"os"
	"path/filepath"
)

// dotfilesRoot is set at link time by every builder that knows the checkout:
//
//	go build -ldflags "-X main.dotfilesRoot=<root>"
//
// It is deliberately NOT inferred from core.hooksPath. doctor's git-hooks:global
// check compares the configured hooksPath against the root's git/hooks.d, so a
// root derived from that value would make the check pass by construction -- a
// guard that cannot fail is worse than no guard, because it reports success.
var dotfilesRoot string

// DotfilesRoot answers which checkout this binary belongs to.
//
// The fallback to ~/dotfiles is the historical assumption, kept last so an
// unstamped binary behaves as it always did rather than failing outright.
func DotfilesRoot() string {
	if dotfilesRoot != "" {
		return dotfilesRoot
	}
	if fromEnv := os.Getenv("AGENTS_DOTFILES_ROOT"); fromEnv != "" {
		return fromEnv
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	fallback := filepath.Join(home, "dotfiles")
	if info, err := os.Stat(fallback); err == nil && info.IsDir() {
		return fallback
	}
	return ""
}
