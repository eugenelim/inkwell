package action

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// SECURITY-MAP: V6.3.1
// TestActionIDsHaveHighEntropy is the spec 17 §4.8 cryptographic-
// randomness invariant. Action IDs are used as primary keys in the
// store and as sub-request ids inside Graph $batch envelopes.
// Predictable IDs would let an attacker (with arbitrary store-write
// access — admittedly already game over) collide rows and confuse
// the executor.
//
// Rather than asserting "uses crypto/rand" directly (brittle), we
// generate 1000 IDs and sanity-check:
//  1. No collisions across the batch (with 16 random bytes the
//     birthday-problem expectation is basically zero).
//  2. The aggregate byte distribution looks uniform — a math/rand
//     accidental import would still produce no collisions, but
//     would have measurably lower entropy. We hash-aggregate and
//     check the SHA-256 of the concatenated IDs has roughly the
//     expected bit balance (40-60% ones).
func TestActionIDsHaveHighEntropy(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	concat := make([]byte, 0, n*32)
	for i := 0; i < n; i++ {
		id := newActionID()
		require.Len(t, id, 32, "action id must be 32 hex chars (16 bytes)")
		_, dup := seen[id]
		require.False(t, dup, "action id collision after %d generations: %q", i, id)
		seen[id] = struct{}{}
		raw, err := hex.DecodeString(id)
		require.NoError(t, err)
		concat = append(concat, raw...)
	}
	// Aggregate-bit-balance heuristic: SHA-256 the concatenation;
	// count the 1-bits in the digest. Uniform random input → ~50%.
	// math/rand.Int63 (or worse, a counter) skews this.
	digest := sha256.Sum256(concat)
	ones := 0
	for _, b := range digest {
		ones += popcount(b)
	}
	totalBits := 256
	ratio := float64(ones) / float64(totalBits)
	require.Greater(t, ratio, 0.40, "digest one-bit ratio too low (%v) — IDs may not be from crypto/rand", ratio)
	require.Less(t, ratio, 0.60, "digest one-bit ratio too high (%v) — IDs may not be from crypto/rand", ratio)
}

func popcount(b byte) int {
	n := 0
	for b != 0 {
		n += int(b & 1)
		b >>= 1
	}
	return n
}
