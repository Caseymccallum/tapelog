package scenario

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/replay"
)

// matching returns interactions whose tool matches the glob.
func matching(inter []replay.Interaction, glob string) []replay.Interaction {
	re := globToRegex(glob)
	if re == nil {
		return nil
	}
	var out []replay.Interaction
	for _, it := range inter {
		if re.MatchString(it.Tool) {
			out = append(out, it)
		}
	}
	return out
}

// anyVerdict reports whether some matching call carries one of verdicts.
func anyVerdict(inter []replay.Interaction, glob string, verdicts ...string) bool {
	for _, it := range matching(inter, glob) {
		for _, v := range verdicts {
			if it.Verdict == v {
				return true
			}
		}
	}
	return false
}

// isSubsequence reports whether wanted appears in order in the trajectory.
func isSubsequence(inter []replay.Interaction, wanted []string) bool {
	i := 0
	for _, it := range inter {
		if i < len(wanted) {
			if re := globToRegex(wanted[i]); re != nil && re.MatchString(it.Tool) {
				i++
			}
		}
	}
	return i == len(wanted)
}

// argsSubset reports whether every wanted key/value appears in args.
func argsSubset(args json.RawMessage, want map[string]any) bool {
	if len(want) == 0 {
		return true
	}
	var have map[string]any
	if err := json.Unmarshal(args, &have); err != nil {
		return false
	}
	for k, v := range want {
		hv, ok := have[k]
		if !ok || fmt.Sprintf("%v", hv) != fmt.Sprintf("%v", v) {
			return false
		}
	}
	return true
}

func compact(raw json.RawMessage) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// globToRegex converts a tool glob (* and ?) to an anchored regexp.
func globToRegex(pat string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pat {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteString(regexp.QuoteMeta(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// ResolveFixture makes a fixture path absolute against the scenario file.
func ResolveFixture(sc *Scenario, scenarioPath string) string {
	if filepath.IsAbs(sc.Fixture) {
		return sc.Fixture
	}
	return filepath.Join(filepath.Dir(scenarioPath), sc.Fixture)
}
