package checkpoint

import (
	"testing"
)

// TestVerifyInclusionMultiLeaf exercises the full RFC 6962 proof chain
// (inner + border levels) against hand-computed roots for a 5-leaf tree.
func TestVerifyInclusionMultiLeaf(t *testing.T) {
	// Leaves L0..L4.
	leaves := make([][]byte, 5)
	for i := range leaves {
		leaves[i] = hashLeaf([]byte{byte(i)})
	}
	// Build the RFC 6962 tree: level 0 = leaves, each level paired
	// left-to-right, odd node promoted unchanged.
	levels := [][][]byte{leaves}
	for len(levels[len(levels)-1]) > 1 {
		cur := levels[len(levels)-1]
		var next [][]byte
		for i := 0; i < len(cur); i += 2 {
			if i+1 < len(cur) {
				next = append(next, hashChildren(cur[i], cur[i+1]))
			} else {
				next = append(next, cur[i])
			}
		}
		levels = append(levels, next)
	}
	root := levels[len(levels)-1][0]

	// Compute the inclusion proof for leaf 3 mechanically: at each level,
	// if the path node has a sibling, that sibling is in the proof.
	idx := 3
	var proof [][]byte
	for l := 0; l < len(levels)-1; l++ {
		sib := idx ^ 1
		if sib < len(levels[l]) {
			proof = append(proof, levels[l][sib])
		}
		idx /= 2
	}

	if err := verifyInclusion(3, 5, leaves[3], proof, root); err != nil {
		t.Errorf("valid proof rejected: %v", err)
	}
	// Wrong leaf must fail.
	if err := verifyInclusion(3, 5, leaves[2], proof, root); err == nil {
		t.Error("wrong leaf verified")
	}
	// Wrong index must fail.
	if err := verifyInclusion(2, 5, leaves[3], proof, root); err == nil {
		t.Error("wrong index verified")
	}
	// Truncated proof must fail.
	if err := verifyInclusion(3, 5, leaves[3], proof[:1], root); err == nil {
		t.Error("truncated proof verified")
	}
	// Empty proof on a 1-leaf tree: root == leaf.
	if err := verifyInclusion(0, 1, leaves[0], nil, leaves[0]); err != nil {
		t.Errorf("1-leaf proof rejected: %v", err)
	}
}
