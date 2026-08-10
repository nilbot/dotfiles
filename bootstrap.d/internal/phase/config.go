package phase

import (
	"fmt"
	"path/filepath"

	"github.com/nilbot/dotfiles/bootstrap/internal/manifest"
)

// Config reconciles every applicable row of links.manifest.
func Config(c Context) error {
	c.logf("== config")

	path := filepath.Join(c.Root, "bootstrap.d", "links.manifest")
	data, err := c.Change.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	rows, err := manifest.Parse(data)
	if err != nil {
		return err
	}
	rows = manifest.For(rows, c.Platform)

	if dupes := manifest.DuplicateTargets(rows); len(dupes) > 0 {
		return fmt.Errorf("the manifest claims these targets more than once: %v", dupes)
	}

	for _, row := range rows {
		target := filepath.Join(c.Home, row.Target)
		source := filepath.Join(c.Root, row.Source)
		switch row.Kind {
		case manifest.KindLink:
			err = c.Change.Link(source, target)
		case manifest.KindSeed:
			err = c.Change.Seed(source, target)
		case manifest.KindDir:
			err = c.Change.Dir(target)
		default:
			// Unreachable while manifest.Parse rejects unknown kinds at load --
			// but that guard lives in another package. A kind added there and
			// not here would otherwise skip the row in silence, which is the one
			// failure mode this design refuses to accept anywhere else.
			err = fmt.Errorf("unknown kind %q for target %s", row.Kind, row.Target)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
