package policy

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse reads a policy document from memory. It is the in-memory twin of
// Load: the same YAML decoding and the same normalize() compile step
// (patterns, expires, Cedar `where` conditions, flows, limits, injection).
//
// It exists so a *candidate* policy can be validated without ever being
// written to disk — the Policy Studio has no file-writing endpoint and must
// not create scratch files next to an operator's active configuration.
func Parse(data []byte, name string) (*Policy, error) {
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		if name == "" {
			return nil, fmt.Errorf("parse policy YAML: %w", err)
		}
		return nil, fmt.Errorf("parse policy YAML (%s): %w", name, err)
	}
	if err := p.normalize(); err != nil {
		return nil, err
	}
	return &p, nil
}
