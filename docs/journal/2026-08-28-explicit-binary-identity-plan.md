# Explicit Binary Identity & Standalone Resolution Implementation Plan

**Date:** 2026-08-28  
**Status:** Ready to Execute  
**Spec:** [`docs/design/2026-08-28-binary-identity-and-standalone-resolution.md`](../design/2026-08-28-binary-identity-and-standalone-resolution.md)  

**Goal:** Eliminate the `$HOME/dotfiles` existence fallback in `DotfilesRoot()`, establishing explicit binary identity resolution (link-time stamp > `AGENTS_DOTFILES_ROOT` > Standalone `""`), updating tests, and refreshing documentation.

## Plan Summary
1. **Task 1**: Update `DotfilesRoot()` in `agents/root.go` and unit tests in `agents/root_test.go`.
2. **Task 2**: Update integration test fixtures in `agents/cmd_doctor_test.go` to test standalone and operator modes.
3. **Task 3**: Update comments in `Makefile` and `devtools.go`, update Spec 6, and add Q&A document.
4. **Task 4**: Full test suite verification across `./...`.
