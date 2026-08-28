package main

import (
	"os"
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
// A binary belongs to a dotfiles checkout if and only if:
//  1. Stamped at link time: go build -ldflags "-X main.dotfilesRoot=<path>"
//  2. Or explicitly configured via AGENTS_DOTFILES_ROOT.
//
// An unstamped binary without AGENTS_DOTFILES_ROOT operates in Standalone Mode
// and returns "" regardless of what exists in the user's home directory.
func DotfilesRoot() string {
	if dotfilesRoot != "" {
		return dotfilesRoot
	}
	return os.Getenv("AGENTS_DOTFILES_ROOT")
}
