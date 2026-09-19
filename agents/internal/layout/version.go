package layout

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct {
	major, minor, patch int
	pre                 string
}

func parseVersion(s string) (semver, error) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "v"))
	if s == "" || strings.EqualFold(s, "dev") {
		return semver{}, fmt.Errorf("not a release version: %q", s)
	}
	main, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("not MAJOR.MINOR.PATCH: %q", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, fmt.Errorf("bad version component %q", p)
		}
		nums[i] = n
	}
	return semver{nums[0], nums[1], nums[2], pre}, nil
}

func compareVersions(a, b string) (int, error) {
	av, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]int{{av.major, bv.major}, {av.minor, bv.minor}, {av.patch, bv.patch}} {
		if pair[0] < pair[1] {
			return -1, nil
		}
		if pair[0] > pair[1] {
			return 1, nil
		}
	}
	switch {
	case av.pre == "" && bv.pre == "":
		return 0, nil
	case av.pre == "":
		return 1, nil
	case bv.pre == "":
		return -1, nil
	case av.pre < bv.pre:
		return -1, nil
	case av.pre > bv.pre:
		return 1, nil
	default:
		return 0, nil
	}
}

// Support reports whether this binary may mutate the repository. Reads are
// permitted regardless; callers that only display a layout must not use this
// as a read gate.
func Support(running string, l Layout) (bool, string) {
	if l.Schema == SchemaV1 {
		return true, ""
	}
	if l.Schema != SchemaV2 {
		return false, "unknown_schema"
	}
	if l.LayoutStatus == StatusMigrating {
		return false, "migrating"
	}
	if l.MinMutVerFloor == "" {
		return true, ""
	}
	got, err := parseVersion(running)
	if err != nil {
		return false, "unreleased" // fail closed: a source build cannot prove its release
	}
	want, err := parseVersion(l.MinMutVerFloor)
	if err != nil {
		return false, "invalid"
	}
	if compareValues(got, want) < 0 {
		return false, "below_floor"
	}
	return true, ""
}

func compareValues(a, b semver) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	switch {
	case a.pre == "" && b.pre == "":
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	case a.pre < b.pre:
		return -1
	case a.pre > b.pre:
		return 1
	default:
		return 0
	}
}
