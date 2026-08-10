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
		}
		if err != nil {
			return err
		}
	}
	return nil
}
