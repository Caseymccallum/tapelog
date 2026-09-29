package replay

import (
	"encoding/json"
	"fmt"
	"sort"
)

// DiffResult summarizes differences between two cassettes (e.g. a replayed
// run vs. a fresh live run, or before/after a change).
type DiffResult struct {
	OnlyInA     []string `json:"only_in_a"`     // tool(args-hash) present only in A
	OnlyInB     []string `json:"only_in_b"`     // present only in B
	Changed     []string `json:"changed"`       // same call, different result
	OrderDiffers bool    `json:"order_differs"` // same calls, different sequence
	Same        bool     `json:"same"`
}

// Diff compares two cassettes interaction-by-interaction.
func Diff(a, b *Cassette) *DiffResult {
	res := &DiffResult{Same: true}

	key := func(it Interaction) string {
		return fmt.Sprintf("%s(%s)", it.Tool, it.ArgsHash[:12])
	}

	// Multiset of call keys per side, with canonical result hashes.
	type entry struct {
		results []string
	}
	aMap := map[string]*entry{}
	bMap := map[string]*entry{}
	var aSeq, bSeq []string

	for _, it := range a.Interactions {
		k := key(it)
		aSeq = append(aSeq, k)
		e := aMap[k]
		if e == nil {
			e = &entry{}
			aMap[k] = e
		}
		e.results = append(e.results, resultHash(it))
	}
	for _, it := range b.Interactions {
		k := key(it)
		bSeq = append(bSeq, k)
		e := bMap[k]
		if e == nil {
			e = &entry{}
			bMap[k] = e
		}
		e.results = append(e.results, resultHash(it))
	}

	all := map[string]bool{}
	for k := range aMap {
		all[k] = true
	}
	for k := range bMap {
		all[k] = true
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		ae, inA := aMap[k]
		be, inB := bMap[k]
		switch {
		case !inB:
			res.OnlyInA = append(res.OnlyInA, k)
		case !inA:
			res.OnlyInB = append(res.OnlyInB, k)
		default:
			as := append([]string(nil), ae.results...)
			bs := append([]string(nil), be.results...)
			sort.Strings(as)
			sort.Strings(bs)
			if fmt.Sprint(as) != fmt.Sprint(bs) {
				res.Changed = append(res.Changed, k)
			}
		}
	}

	res.OrderDiffers = fmt.Sprint(aSeq) != fmt.Sprint(bSeq)
	res.Same = len(res.OnlyInA) == 0 && len(res.OnlyInB) == 0 &&
		len(res.Changed) == 0 && !res.OrderDiffers
	return res
}

// resultHash canonically hashes an interaction's recorded outcome.
func resultHash(it Interaction) string {
	raw, _ := json.Marshal(map[string]any{
		"is_error": it.IsError,
		"result":   string(it.Result),
	})
	return hashArgs(json.RawMessage(raw))
}
