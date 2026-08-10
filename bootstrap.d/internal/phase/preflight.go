package phase

import "fmt"

func Preflight(c Context) error {
	c.logf("== preflight")
	c.logf("   platform    %s", c.Platform)
	c.logf("   repository  %s", c.Root)
	c.logf("   home        %s", c.Home)
	c.logf("   profile     %s", c.Profile)

	for _, tool := range []string{"git", "awk", "sed"} {
		if _, err := c.Change.LookPath(tool); err != nil {
			return fmt.Errorf("required stage-zero tool %q is not on PATH", tool)
		}
	}

	if c.Profile == "workstation" {
		c.logf("   needs sudo    (login shell change, /etc/shells)")
		c.logf("   needs network (Homebrew, packages, fisher plugins)")
	} else {
		c.logf("   needs neither sudo nor network")
	}
	return nil
}
