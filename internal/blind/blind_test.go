package blind

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"

	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/kangaroo"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
)

const testTarget = "0000000000000000000000000000000000000000"

// campaign builds a small campaign of the right shape to run real attacks
// against: puzzle #26 is 2^25 keys, small enough that a full discrete log
// finishes in a test and large enough that the two attacks differ by orders of
// magnitude.
func campaign(t *testing.T, blockBits uint, shift uint64) *keyspace.Campaign {
	t.Helper()
	min, max, err := keyspace.PuzzleRange(26)
	if err != nil {
		t.Fatalf("puzzle range: %v", err)
	}
	c, err := keyspace.NewShiftedCampaign("t", 26, testTarget, min, max, blockBits, shift)
	if err != nil {
		t.Fatalf("campaign: %v", err)
	}
	return c
}

// The walk over points must produce exactly the hashes a walk over keys would.
// If it does not, a blinded worker and the coordinator disagree about what was
// swept, and every proof breaks.
func TestWalkerMatchesKeyDerivation(t *testing.T) {
	start := big.NewInt(0).Lsh(big.NewInt(1), 25)
	start.Add(start, big.NewInt(1234567))

	lot, err := LotFor(keyspace.Block{Index: 0, Lo: start, Len: big.NewInt(64)})
	if err != nil {
		t.Fatalf("lot: %v", err)
	}
	w, err := NewWalker(lot.StartPoint)
	if err != nil {
		t.Fatalf("walker: %v", err)
	}

	key := new(big.Int).Set(start)
	for off := 0; off < 64; off++ {
		want := btc.PubKeyHash160(key)
		if got := w.Hash160(); got != want {
			t.Fatalf("offset %d: point walk gave %x, key derivation gave %x", off, got, want)
		}
		w.Next()
		key.Add(key, big.NewInt(1))
	}
}

// The lot handed to a worker must contain no key material at all. This is the
// property the whole package exists for, so it is asserted on the serialized
// form a worker actually receives, not on the struct.
func TestLotCarriesNoKeyMaterial(t *testing.T) {
	c := campaign(t, 10, 777)
	blk, err := c.BlockAt(12345)
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	lot, err := LotFor(blk)
	if err != nil {
		t.Fatalf("lot: %v", err)
	}
	raw, err := json.Marshal(lot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := strings.ToLower(string(raw))

	for name, v := range map[string]*big.Int{
		"first key": blk.Lo,
		"last key":  blk.Hi,
		"base":      c.Base(),
	} {
		for _, form := range []string{v.Text(16), v.String()} {
			if strings.Contains(wire, strings.ToLower(form)) {
				t.Fatalf("the %s (%s) appears in the lot handed to the worker: %s", name, form, wire)
			}
		}
	}
	if n := len(lot.StartPoint); n != 66 {
		t.Fatalf("start point should be a 33-byte compressed pubkey in hex, got %d chars", n)
	}
}

// This is the attack that would have made the whole scheme worthless, run for
// real against the naive tiling.
//
// With no shift, lot starts sit on the lattice Min + j*blockSize. A worker with
// only the curve point subtracts Min*G, sets Q = blockSize*G, and solves
// A - Min*G = j*Q with baby-step/giant-step over the block count — square root
// of the number of blocks, not of the key range. Here that is a few hundred
// operations; on puzzle #71 with 2^33-key lots it is about 2^19, which is
// milliseconds.
func TestLatticeAttackBreaksUnshiftedLots(t *testing.T) {
	c := campaign(t, 10, 0)
	const index = 9999

	blk, err := c.BlockAt(index)
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	lot, err := LotFor(blk)
	if err != nil {
		t.Fatalf("lot: %v", err)
	}
	pt, err := DecodePoint(lot.StartPoint)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	j, ok, ops := latticeAttack(&pt, c.Base(), c.BlockSize(), c.NumBlocksU64())
	if !ok {
		t.Fatal("the lattice attack failed against an unshifted tiling; if this is now safe the shift is redundant and the claim in the package doc is wrong")
	}
	if j != index {
		t.Fatalf("lattice attack recovered block %d, want %d", j, index)
	}

	// Recovering the index recovers the key, which is the whole prize.
	recovered, err := c.BlockAt(j)
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if recovered.Lo.Cmp(blk.Lo) != 0 {
		t.Fatalf("recovered lot start %s, want %s", recovered.Lo, blk.Lo)
	}
	t.Logf("unshifted tiling broken in %d group operations over %d blocks (full discrete log would need ~%s)",
		ops, c.NumBlocksU64(), kangaroo.ExpectedSteps(new(big.Int).Sub(c.Max, c.Min)))
}

// With the shift in place the same attack finds nothing, because there is no
// longer a lattice to walk: the lot start is Min - shift + j*blockSize and the
// attacker knows neither term. What remains is a discrete log over the whole
// campaign range, which the second half of this test runs to completion so the
// cost claim is measured rather than asserted.
func TestShiftDefeatsLatticeAttack(t *testing.T) {
	shift, err := DeriveShift([]byte("operator secret, never leaves the coordinator"), "t", 10)
	if err != nil {
		t.Fatalf("shift: %v", err)
	}
	if shift == 0 {
		t.Fatal("derived shift is zero, which is the unshifted tiling; pick a different campaign id for this test")
	}

	c := campaign(t, 10, shift)
	blk, err := c.BlockAt(9999)
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	lot, err := LotFor(blk)
	if err != nil {
		t.Fatalf("lot: %v", err)
	}
	pt, err := DecodePoint(lot.StartPoint)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The cheap attack, mounted exactly as before but against Min rather than
	// the base it cannot know.
	if _, ok, ops := latticeAttack(&pt, c.Min, c.BlockSize(), c.NumBlocksU64()); ok {
		t.Fatal("the lattice attack still works with a shifted tiling; the shift is not doing its job")
	} else {
		t.Logf("lattice attack exhausted after %d operations", ops)
	}

	// The expensive one, which does work — and is the honest bound on what the
	// blind lot buys.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	got, err := AuditLot(ctx, c, lot)
	if err != nil {
		t.Fatalf("discrete log over the campaign range: %v", err)
	}
	if got.Cmp(blk.Lo) != 0 {
		t.Fatalf("recovered %s, want %s", got, blk.Lo)
	}

	width := new(big.Int).Sub(c.Max, c.Base())
	t.Logf("shifted lot recovered only by full discrete log: ~%s group operations against %d for the lattice attack",
		kangaroo.ExpectedSteps(width), 2*isqrt(c.NumBlocksU64()))
}

// A shift out of range must be refused rather than silently wrapped: a shift at
// or above the block size would re-align the tiling onto the lattice it exists
// to break.
func TestShiftIsBoundedByBlockSize(t *testing.T) {
	min, max, err := keyspace.PuzzleRange(26)
	if err != nil {
		t.Fatalf("puzzle range: %v", err)
	}
	if _, err := keyspace.NewShiftedCampaign("t", 26, testTarget, min, max, 10, 1<<10); err == nil {
		t.Fatal("a shift equal to the block size was accepted")
	}
}

// The shift must be stable: a coordinator restarted from an empty database has
// to tile the campaign the same way, or it hands out ground that was already
// swept under a different alignment.
func TestShiftIsDeterministicAndSecretBound(t *testing.T) {
	a, err := DeriveShift([]byte("secret"), "campaign-1", 33)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	b, err := DeriveShift([]byte("secret"), "campaign-1", 33)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if a != b {
		t.Fatalf("same secret and campaign gave %d then %d", a, b)
	}
	c, err := DeriveShift([]byte("secret"), "campaign-2", 33)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if a == c {
		t.Fatal("two campaigns under one secret share a shift")
	}
	d, err := DeriveShift([]byte("other"), "campaign-1", 33)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if a == d {
		t.Fatal("two secrets gave the same shift")
	}
	if a >= 1<<33 {
		t.Fatalf("shift %d is not below the block size", a)
	}
	if _, err := DeriveShift(nil, "c", 33); err == nil {
		t.Fatal("an empty secret was accepted")
	}
}

// Every block index must resolve back from any key inside it, because that is
// what names the participant who was holding the ground a stolen key sat on.
func TestBlockIndexOfIsTheAttributionPath(t *testing.T) {
	c := campaign(t, 10, 615)
	for _, idx := range []uint64{0, 1, 4242, c.NumBlocksU64() - 1} {
		blk, err := c.BlockAt(idx)
		if err != nil {
			t.Fatalf("block %d: %v", idx, err)
		}
		for _, k := range []*big.Int{blk.Lo, blk.Hi, new(big.Int).Add(blk.Lo, big.NewInt(1))} {
			if k.Cmp(blk.Hi) > 0 {
				continue
			}
			got, ok := c.BlockIndexOf(k)
			if !ok || got != idx {
				t.Fatalf("key %s in block %d resolved to (%d, %v)", k, idx, got, ok)
			}
		}
	}
	if _, ok := c.BlockIndexOf(big.NewInt(1)); ok {
		t.Fatal("a key far below the campaign resolved to a block")
	}
	if _, ok := c.BlockIndexOf(new(big.Int).Add(c.Max, big.NewInt(1))); ok {
		t.Fatal("a key above Max resolved to a block")
	}
}

// latticeAttack is the cheap break the shift exists to prevent, implemented so
// the tests above can run it rather than describe it.
//
// It assumes lot starts are base + j*size for j < n and recovers j with
// baby-step/giant-step in about 2*sqrt(n) group operations. It returns the
// operation count so the tests can report what the attack actually costs.
func latticeAttack(a *secp256k1.JacobianPoint, base, size *big.Int, n uint64) (uint64, bool, int) {
	ops := 0

	// A' = A - base*G, so A' = j*Q with Q = size*G.
	baseG := kangaroo.PublicKeyFromScalar(base)
	var target secp256k1.JacobianPoint
	secp256k1.AddNonConst(a, negated(&baseG), &target)
	affine(&target)
	ops++

	if target.Z.IsZero() { // j = 0
		return 0, true, ops
	}

	q := kangaroo.PublicKeyFromScalar(size)
	m := isqrt(n) + 1

	// Baby steps: i*Q for i in [1, m].
	baby := make(map[[33]byte]uint64, m)
	cur := q
	for i := uint64(1); i <= m; i++ {
		baby[pointKey(&cur)] = i
		var next secp256k1.JacobianPoint
		secp256k1.AddNonConst(&cur, &q, &next)
		affine(&next)
		cur = next
		ops++
	}

	// Giant steps: A' - g*m*Q, looking for a baby step. Written as
	// A' + Q = (j+1)*Q so the j = 0 case is already handled above and every
	// lookup is against a stored i >= 1.
	var walk secp256k1.JacobianPoint
	secp256k1.AddNonConst(&target, &q, &walk)
	affine(&walk)

	stride := kangaroo.PublicKeyFromScalar(new(big.Int).Mul(size, new(big.Int).SetUint64(m)))
	negStride := negated(&stride)

	for g := uint64(0); g*m <= n+m; g++ {
		if i, ok := baby[pointKey(&walk)]; ok {
			j := g*m + i - 1
			if j < n {
				return j, true, ops
			}
		}
		var next secp256k1.JacobianPoint
		secp256k1.AddNonConst(&walk, negStride, &next)
		affine(&next)
		walk = next
		ops++
	}
	return 0, false, ops
}

func negated(p *secp256k1.JacobianPoint) *secp256k1.JacobianPoint {
	q := *p
	affine(&q)
	q.Y.Negate(1)
	q.Y.Normalize()
	return &q
}

func affine(p *secp256k1.JacobianPoint) {
	if !p.Z.IsOne() {
		p.ToAffine()
	}
}

func pointKey(p *secp256k1.JacobianPoint) [33]byte {
	q := *p
	affine(&q)
	var out [33]byte
	out[0] = 0x02
	if q.Y.IsOdd() {
		out[0] = 0x03
	}
	var xb [32]byte
	q.X.PutBytes(&xb)
	copy(out[1:], xb[:])
	return out
}

func isqrt(n uint64) uint64 {
	return new(big.Int).Sqrt(new(big.Int).SetUint64(n)).Uint64()
}
