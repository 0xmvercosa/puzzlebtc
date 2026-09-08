// Package btc holds the reference (CPU) Bitcoin key derivation used by the
// coordinator to verify worker submissions. It is deliberately simple and
// unoptimized: the coordinator only ever derives a handful of keys per
// submission, so clarity matters more than throughput here. Workers use GPU
// kernels that must agree with this implementation bit for bit.
package btc

import (
	"crypto/sha256"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // Bitcoin requires RIPEMD-160.
)

// Hash160Len is the length in bytes of a HASH160 digest.
const Hash160Len = 20

// Hash160 is RIPEMD160(SHA256(compressed_pubkey(priv))) — the 20 bytes that a
// P2PKH address encodes. Comparing these directly avoids Base58 on the hot path.
type Hash160 [Hash160Len]byte

// PubKeyHash160 derives the HASH160 of the compressed public key for priv.
//
// priv must be in [1, n-1]; values outside that range are the caller's problem
// (secp256k1 reduces them mod n, which would silently alias two scalars to one
// key). Keyspace blocks are always far below n, so callers in this repo are safe.
func PubKeyHash160(priv *big.Int) Hash160 {
	var buf [32]byte
	priv.FillBytes(buf[:])

	key := secp256k1.PrivKeyFromBytes(buf[:])
	compressed := key.PubKey().SerializeCompressed()

	sum := sha256.Sum256(compressed)
	r := ripemd160.New()
	_, _ = r.Write(sum[:])

	var out Hash160
	copy(out[:], r.Sum(nil))
	return out
}

// LeadingZeroBits counts the leading zero bits of h, capped at 8*Hash160Len.
// The proof-of-sweep scheme in internal/proof declares a key a "witness" when
// this count reaches the campaign's difficulty.
func (h Hash160) LeadingZeroBits() uint {
	var n uint
	for _, b := range h {
		if b != 0 {
			return n + uint(leadingZeros8(b))
		}
		n += 8
	}
	return n
}

func leadingZeros8(b byte) int {
	n := 0
	for mask := byte(0x80); mask != 0 && b&mask == 0; mask >>= 1 {
		n++
	}
	return n
}
