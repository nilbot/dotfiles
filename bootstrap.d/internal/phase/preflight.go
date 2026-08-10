package phase

import "fmt"

func Preflight(c Context) error {
	// The two load-bearing inputs, checked in the phase whose entire job is
	// checking. HOME unset with XDG_CACHE_HOME set is a normal container shape
	// -- and containers are exactly why the dotfiles profile exists -- which
	// would otherwise resolve every managed path against "/".
	if c.Root == "" {
		return fmt.Errorf("repository root is empty; the shim exports BOOTSTRAP_ROOT")
	}
	if c.Home == "" {
		return fmt.Errorf("HOME is empty; every managed path is resolved against it")
	}

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
