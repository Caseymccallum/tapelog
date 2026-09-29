//go:build linux

package sandbox

import (
	"path/filepath"

	landlock "github.com/landlock-lsm/go-landlock/landlock"
)

// restrictPlatform applies Landlock filesystem restrictions to the
// current process (best-effort in lenient mode).
func (o Options) restrictPlatform() error {
	var rules []landlock.Rule
	add := func(paths []string, rw bool) error {
		for _, p := range paths {
			abs, err := filepath.Abs(p)
			if err != nil {
				return err
			}
			if rw {
				rules = append(rules, landlock.RWDirs(abs))
			} else {
				rules = append(rules, landlock.RODirs(abs))
			}
		}
		return nil
	}
	if err := add(baselineRO(), false); err != nil {
		return err
	}
	if err := add(baselineRW(), true); err != nil {
		return err
	}
	if err := add(o.ReadOnly, false); err != nil {
		return err
	}
	if err := add(o.ReadWrite, true); err != nil {
		return err
	}

	cfg := landlock.V3
	if o.Lenient {
		cfg = cfg.BestEffort()
	}
	return cfg.RestrictPaths(rules...)
}
