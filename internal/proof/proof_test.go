package proof

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
)

// Test blocks are tiny so the CPU sweeper can finish them in milliseconds. The
// difficulty is scaled down to match, keeping the witness density — and so the
// statistics every test relies on — the same as a production campaign.
const (
	testBlockBits   = 12 // 4096 keys per block
	testWitnessBits = 4  // ~256 witnesses per block
)

var testSecret = []byte("test-secret-that-is-at-least-32-bytes-long!!")

func testParams() Params {
	p := DefaultParams(testBlockBits)
	p.WitnessBits = testWitnessBits
	p.Buckets = 16 // 256 keys per bucket, ~16 witnesses each
	p.Canaries = 3
	p.SampleSize = 32
	return p
}

// harness builds a campaign whose block 0 provably contains a known key, so the
// jackpot path can be exercised without solving anything.
func harness(t *testing.T) (*keyspace.Campaign, *Verifier, keyspace.Block) {
	t.Helper()

	// Puzzle #24's range is small enough that a 4096-key block is a meaningful
	// slice of it, and its solved key is public.
	min, max, err := keyspace.PuzzleRange(24)
	if err != nil {
		t.Fatal(err)
	}
	// Target: the key sitting at offset 100 of block 0. Using a real derived
	// hash160 (rather than a placeholder) means the jackpot check is exercised
	// against the same code path a real hit would take.
	targetKey := new(big.Int).Add(min, big.NewInt(100))
	th := btc.PubKeyHash160(targetKey)

	c, err := keyspace.NewCampaign("test", 24, hexOf(th), min, max, testBlockBits)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifier(c, testParams(), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := c.BlockAt(0)
	if err != nil {
		t.Fatal(err)
	}
	return c, v, blk
}

func hexOf(h btc.Hash160) string { return hex.EncodeToString(h[:]) }

// sweepBlock runs the honest reference worker over a block.
func sweepBlock(t *testing.T, c *keyspace.Campaign, v *Verifier, blk keyspace.Block) Submission {
	t.Helper()
	wl, err := v.Watchlist(blk)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := NewSweeper(v.Params(), c.TargetHash160, wl)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sw.Sweep(context.Background(), blk)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

// An honest sweep must pass, and must find the planted target.
func TestHonestSweepAccepted(t *testing.T) {
	c, v, blk := harness(t)
	sub := sweepBlock(t, c, v, blk)

	res, err := v.Verify(blk, sub, false)
	if err != nil {
		t.Fatalf("honest sweep rejected: %v", err)
	}
	t.Logf("witnesses=%d expected=%.1f verified=%d emptiest_bucket=%d",
		res.Witnesses, res.Expected, res.Verified, res.EmptiestBucket)

	if res.TargetKey == nil {
		t.Fatal("honest sweep did not report the planted target")
	}
	want := new(big.Int).Add(blk.Lo, big.NewInt(100))
	if res.TargetKey.Cmp(want) != 0 {
		t.Errorf("target key = %s, want %s", res.TargetKey, want)
	}
	if res.Witnesses < 100 {
		t.Errorf("suspiciously few witnesses (%d); the difficulty may be misconfigured", res.Witnesses)
	}
}

// Deep verification must reach the same verdict as sampling, checking every
// witness rather than a subset.
func TestDeepVerifyAcceptsHonestSweep(t *testing.T) {
	c, v, blk := harness(t)
	sub := sweepBlock(t, c, v, blk)

	res, err := v.Verify(blk, sub, true)
	if err != nil {
		t.Fatalf("deep verify rejected an honest sweep: %v", err)
	}
	if res.Verified != len(sub.Witnesses) {
		t.Errorf("deep verify checked %d of %d witnesses", res.Verified, len(sub.Witnesses))
	}
}

// The headline claim: a worker that skips part of its block is caught. Sweeping
// the first 90% leaves the last buckets empty.
func TestPartialSweepRejected(t *testing.T) {
	c, v, blk := harness(t)
	full := sweepBlock(t, c, v, blk)

	cut := blk.Len.Uint64() * 90 / 100
	var partial Submission
	partial.BlockIndex = blk.Index
	for _, w := range full.Witnesses {
		if w < cut {
			partial.Witnesses = append(partial.Witnesses, w)
		}
	}
	for _, cn := range full.Canaries {
		if cn < cut {
			partial.Canaries = append(partial.Canaries, cn)
		}
	}

	_, err := v.Verify(blk, partial, false)
	if err == nil {
		t.Fatal("a 90% sweep was accepted; the coverage test is not working")
	}
	t.Logf("rejected as expected: %v", err)
}

// A worker that reports nothing at all must fail on volume, not crash.
func TestEmptySubmissionRejected(t *testing.T) {
	_, v, blk := harness(t)
	_, err := v.Verify(blk, Submission{BlockIndex: blk.Index}, false)
	if err == nil {
		t.Fatal("an empty submission was accepted")
	}
	r, ok := err.(*Rejection)
	if !ok {
		t.Fatalf("want a *Rejection, got %T: %v", err, err)
	}
	if r.Code != "too_few_witnesses" {
		t.Errorf("code = %q, want too_few_witnesses", r.Code)
	}
}

// Inventing offsets is the obvious attack: fabricate a well-spread list without
// doing any hashing. The cryptographic sample must catch it.
func TestFabricatedWitnessesRejected(t *testing.T) {
	_, v, blk := harness(t)

	// Evenly spaced offsets: perfect coverage, perfect count, zero work.
	var fake Submission
	fake.BlockIndex = blk.Index
	step := blk.Len.Uint64() / 300
	for off := uint64(0); off < blk.Len.Uint64(); off += step {
		fake.Witnesses = append(fake.Witnesses, off)
	}
	fake.Canaries = v.CanaryOffsets(blk) // assume the cheater even guessed these

	_, err := v.Verify(blk, fake, false)
	if err == nil {
		t.Fatal("fabricated witnesses were accepted")
	}
	r, ok := err.(*Rejection)
	if !ok {
		t.Fatalf("want a *Rejection, got %T: %v", err, err)
	}
	if r.Code != "witness_forged" {
		t.Errorf("code = %q, want witness_forged", r.Code)
	}
	t.Logf("rejected as expected: %v", err)
}

// Padding a real sweep with repeats of one valid witness must not inflate the
// count past the volume test.
func TestDuplicateWitnessesRejected(t *testing.T) {
	c, v, blk := harness(t)
	full := sweepBlock(t, c, v, blk)

	padded := full
	padded.Witnesses = append([]uint64{full.Witnesses[0], full.Witnesses[0]}, full.Witnesses...)

	_, err := v.Verify(blk, padded, false)
	if err == nil {
		t.Fatal("duplicated witnesses were accepted")
	}
	r, _ := err.(*Rejection)
	if r == nil || r.Code != "witnesses_unordered" {
		t.Errorf("got %v, want a witnesses_unordered rejection", err)
	}
}

// A worker that sweeps honestly but never reports matches — a stubbed or broken
// comparison — collects witnesses fine and must still be rejected.
func TestBrokenReportingPathRejected(t *testing.T) {
	c, v, blk := harness(t)
	sub := sweepBlock(t, c, v, blk)

	sub.Canaries = nil
	sub.FoundOffset = nil

	_, err := v.Verify(blk, sub, false)
	if err == nil {
		t.Fatal("a submission with no canaries was accepted")
	}
	r, _ := err.(*Rejection)
	if r == nil || r.Code != "canary_missed" {
		t.Errorf("got %v, want a canary_missed rejection", err)
	}
}

// A false jackpot claim must be rejected outright — this is the one check that
// guards real money, so it is never sampled.
func TestFalseJackpotClaimRejected(t *testing.T) {
	c, v, blk := harness(t)
	sub := sweepBlock(t, c, v, blk)

	wrong := uint64(7) // not the planted offset
	sub.FoundOffset = &wrong

	_, err := v.Verify(blk, sub, false)
	if err == nil {
		t.Fatal("a false jackpot claim was accepted")
	}
	r, _ := err.(*Rejection)
	if r == nil || r.Code != "found_mismatch" {
		t.Errorf("got %v, want a found_mismatch rejection", err)
	}
}

// Canaries must be a deterministic function of (secret, campaign, block): the
// coordinator stores none and must recompute the same set after a restart.
func TestCanariesAreDeterministicAndSecretDependent(t *testing.T) {
	c, v, blk := harness(t)
	a := v.CanaryOffsets(blk)
	b := v.CanaryOffsets(blk)
	if len(a) != testParams().Canaries {
		t.Fatalf("got %d canaries, want %d", len(a), testParams().Canaries)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("canary offsets are not reproducible")
		}
	}
	// Distinct, or "all canaries returned" would be trivially satisfiable.
	seen := map[uint64]bool{}
	for _, o := range a {
		if seen[o] {
			t.Fatal("duplicate canary offset")
		}
		seen[o] = true
	}

	other, err := NewVerifier(c, testParams(), []byte("a completely different secret, also 32+ bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if equalU64(a, other.CanaryOffsets(blk)) {
		t.Fatal("canary offsets do not depend on the secret")
	}

	// Different blocks must get different canaries.
	blk1, err := c.BlockAt(1)
	if err != nil {
		t.Fatal(err)
	}
	if equalU64(a, v.CanaryOffsets(blk1)) {
		t.Fatal("canary offsets do not depend on the block index")
	}
}

func equalU64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The sampled indices must not be guessable from public data alone, or a worker
// could fake every witness outside the sample.
func TestSampleDependsOnSecret(t *testing.T) {
	c, v, blk := harness(t)
	a := v.sampleIndices(blk, 1000, false)
	other, err := NewVerifier(c, testParams(), []byte("yet another secret long enough to pass!!"))
	if err != nil {
		t.Fatal(err)
	}
	if equalInt(a, other.sampleIndices(blk, 1000, false)) {
		t.Fatal("witness sampling does not depend on the secret")
	}
}

func equalInt(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParamsThresholds(t *testing.T) {
	p := DefaultParams(33)
	if p.WitnessBits != 21 {
		t.Errorf("witness_bits = %d, want 21 for 2^33 blocks", p.WitnessBits)
	}
	if p.Buckets != 128 {
		t.Errorf("buckets = %d, want 128", p.Buckets)
	}
	blockLen := new(big.Int).Lsh(big.NewInt(1), 33)
	if got := p.ExpectedWitnesses(blockLen); got != TargetWitnesses {
		t.Errorf("expected witnesses = %v, want %d", got, TargetWitnesses)
	}
	// 5 sigma below 4096 is ~3776; the per-bucket mean is 32.
	if min := p.MinTotal(blockLen); min < 3700 || min > 4000 {
		t.Errorf("min total = %d, outside the expected 5-sigma band", min)
	}
	if min := p.MinPerBucket(blockLen); min < 1 || min > 10 {
		t.Errorf("min per bucket = %d, outside the expected band", min)
	}
}

// A worker that reports the jackpot as just another watchlist hit — because it
// was configured with the wrong target, or reports every hit uniformly — must
// not lose the find. The coordinator re-derives it from the extra offset.
func TestJackpotRecoveredFromUnflaggedHit(t *testing.T) {
	c, v, blk := harness(t)
	sub := sweepBlock(t, c, v, blk)

	if sub.FoundOffset == nil {
		t.Fatal("harness precondition: block 0 should contain the planted target")
	}
	// Downgrade the jackpot to a plain watchlist hit, as a misconfigured worker
	// would report it.
	sub.Canaries = append(sub.Canaries, *sub.FoundOffset)
	sub.FoundOffset = nil

	res, err := v.Verify(blk, sub, false)
	if err != nil {
		t.Fatalf("submission rejected: %v", err)
	}
	if res.TargetKey == nil {
		t.Fatal("the coordinator dropped a real hit that was not flagged as found")
	}
	want := new(big.Int).Add(blk.Lo, big.NewInt(100))
	if res.TargetKey.Cmp(want) != 0 {
		t.Errorf("recovered key = %s, want %s", res.TargetKey, want)
	}
}

// The safety net must not fire on ordinary junk: an extra offset that is not the
// target is accepted and simply produces no solution.
func TestExtraNonTargetHitIsHarmless(t *testing.T) {
	c, v, blk := harness(t)
	sub := sweepBlock(t, c, v, blk)
	sub.FoundOffset = nil
	sub.Canaries = append(sub.Canaries, 4095) // not the target, not a canary

	res, err := v.Verify(blk, sub, false)
	if err != nil {
		t.Fatalf("an extra non-target hit was rejected: %v", err)
	}
	if res.TargetKey != nil {
		t.Error("a non-target offset was mistaken for the jackpot")
	}
}

// DefaultParams must produce a usable configuration for every legal block size.
// The first version did not: blocks of 2^14 keys or fewer got a difficulty of
// zero, and the coordinator refused to start. A smoke run caught it, so the
// whole range is pinned here.
func TestDefaultParamsValidForEveryBlockSize(t *testing.T) {
	for bits := uint(1); bits <= 63; bits++ {
		p := DefaultParams(bits)
		if err := p.Validate(); err != nil {
			t.Errorf("block_bits=%d: %v", bits, err)
			continue
		}
		blockLen := new(big.Int).Lsh(big.NewInt(1), bits)
		// Every bucket must expect enough witnesses that an empty one is real
		// evidence, not variance.
		if lam := p.ExpectedWitnesses(blockLen) / float64(p.Buckets); bits >= 20 && lam < 16 {
			t.Errorf("block_bits=%d: only %.1f witnesses per bucket; the coverage test would false-reject", bits, lam)
		}
		if got := p.ExpectedWitnesses(blockLen); got < 1 {
			t.Errorf("block_bits=%d: expects %.2f witnesses per block, too few to police", bits, got)
		}
		if uint64(p.Buckets) > blockLen.Uint64() && blockLen.IsUint64() {
			t.Errorf("block_bits=%d: %d buckets over %s keys", bits, p.Buckets, blockLen)
		}
	}
}
