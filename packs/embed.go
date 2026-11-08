// Package packs ships batteries-included policy packs. Since M5 the YAML
// files are also embedded at build time, so the Session Workbench Policy
// Studio can show — and extend — exactly the pack an operator would copy,
// comments included, without needing the source tree.
package packs

import (
	"embed"
	"sort"
	"strings"
)

//go:embed *.yaml
var files embed.FS

// Pack is one embedded policy pack.
type Pack struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Bytes       int    `json:"bytes"`
}

// All returns the embedded packs, ordered by name.
func All() []Pack {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}
	out := make([]Pack, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := files.ReadFile(e.Name())
		if err != nil {
			continue
		}
		title, desc := describe(string(data))
		out = append(out, Pack{
			Name:        strings.TrimSuffix(e.Name(), ".yaml"),
			Title:       title,
			Description: desc,
			Source:      string(data),
			Bytes:       len(data),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns one pack by name (file stem, e.g. "starter").
func Get(name string) (Pack, bool) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".yaml")
	for _, p := range All() {
		if p.Name == name {
			return p, true
		}
	}
	return Pack{}, false
}

// Names lists the available pack names.
func Names() []string {
	all := All()
	out := make([]string, 0, len(all))
	for _, p := range all {
		out = append(out, p.Name)
	}
	return out
}

// describe reads the pack's own header comment. The packs explain themselves
// in prose; repeating it here keeps the Studio honest without a second copy.
func describe(src string) (title, desc string) {
	var prose []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if title == "" {
			if rest, ok := strings.CutPrefix(body, "tapelog policy pack:"); ok {
				title = strings.TrimSpace(rest)
			}
			continue
		}
		if body == "" {
			if len(prose) > 0 {
				break
			}
			continue
		}
		prose = append(prose, body)
	}
	desc = strings.Join(prose, " ")
	if len(desc) > 400 {
		desc = strings.TrimSpace(desc[:400]) + "…"
	}
	return title, desc
}
