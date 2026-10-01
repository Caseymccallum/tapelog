package checkpoint

// RFC 6962 Merkle tree hashing + inclusion proofs — a faithful port of
// github.com/transparency-dev/merkle (rfc6962 hasher + proof.Verify).
//
//	HashLeaf(x)     = SHA256(0x00 || x)
//	HashChildren(l,r) = SHA256(0x01 || l || r)

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
)

const (
	leafPrefix     = 0x00
	nodePrefix     = 0x01
	hashOutputSize = sha256.Size
)

// hashLeaf returns the RFC 6962 leaf hash of an entry's raw bytes.
func hashLeaf(entry []byte) []byte {
	h := sha256.New()
	h.Write([]byte{leafPrefix})
	h.Write(entry)
	return h.Sum(nil)
}

// hashChildren returns the RFC 6962 interior node hash.
func hashChildren(l, r []byte) []byte {
	h := sha256.New()
	h.Write([]byte{nodePrefix})
	h.Write(l)
	h.Write(r)
	return h.Sum(nil)
}

// verifyInclusion recomputes the root from a leaf and its RFC 6962 proof
// (proof hashes ordered leaf→root) and compares it to root. Requires
// 0 <= index < size.
func verifyInclusion(index, size uint64, leafHash []byte, proof [][]byte, root []byte) error {
	if index >= size {
		return fmt.Errorf("index %d beyond tree size %d", index, size)
	}
	if len(leafHash) != hashOutputSize {
		return fmt.Errorf("leaf hash has size %d, want %d", len(leafHash), hashOutputSize)
	}
	inner := bits.Len64(index ^ (size - 1)) // divergence level of index vs size-1
	border := bits.OnesCount64(index >> uint(inner))
	if len(proof) != inner+border {
		return fmt.Errorf("proof has %d hashes, want %d", len(proof), inner+border)
	}

	// Inner levels: siblings left/right by the index bit.
	res := append([]byte(nil), leafHash...)
	for i, h := range proof[:inner] {
		if (index>>uint(i))&1 == 0 {
			res = hashChildren(res, h)
		} else {
			res = hashChildren(h, res)
		}
	}
	// Border levels: all siblings are left of the path.
	for _, h := range proof[inner:] {
		res = hashChildren(h, res)
	}

	if !bytes.Equal(res, root) {
		return fmt.Errorf("computed root %x does not match proof root %x", res, root)
	}
	return nil
}

// parseHexHash decodes a hex hash and enforces the hash output size.
func parseHexHash(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) != hashOutputSize {
		return nil, fmt.Errorf("hash %q has %d bytes, want %d", s, len(b), hashOutputSize)
	}
	return b, nil
}
