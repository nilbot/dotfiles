package lane

import (
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"SQ-123/payments":  "sq-123-payments",
		"feature/Add Auth": "feature-add-auth",
		"---weird---":      "weird",
		"":                 "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyTruncatesWithoutTrailingDash(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "a-"
	}
	got := Slugify(long)
	if len(got) > 64 {
		t.Fatalf("len = %d, want <= 64", len(got))
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("Slugify left a trailing dash: %q", got)
	}
}

func TestResolvePrecedence(t *testing.T) {
	rc := &repo.Context{Branch: "SQ-123/payments", Worktree: "myrepo"}

	if got := Resolve("Explicit Lane", rc); got != "explicit-lane" {
		t.Errorf("explicit should win: got %q", got)
	}
	if got := Resolve("", rc); got != "sq-123-payments" {
		t.Errorf("branch should be next: got %q", got)
	}

	detached := &repo.Context{Branch: "", Worktree: "My Repo"}
	if got := Resolve("", detached); got != "my-repo" {
		t.Errorf("worktree should be next: got %q", got)
	}

	nothing := &repo.Context{}
	if got := Resolve("", nothing); got != "default" {
		t.Errorf("default should be last: got %q", got)
	}
	if got := Resolve("", nil); got != "default" {
		t.Errorf("nil context should be default: got %q", got)
	}
}
