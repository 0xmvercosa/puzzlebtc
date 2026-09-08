// Package proof implements the coordinator's answer to the only question that
// makes a distributed key search worth paying for: did this worker actually
// sweep the block it was given?
//
// The scheme has two independent halves, both O(1) for the coordinator to check.
//
// # Witnesses — proof of volume and of coverage
//
// A key is a "witness" when its HASH160 has at least WitnessBits leading zero
// bits. Witnesses are rare (one in 2^WitnessBits keys) and there is no way to
// produce one except by hashing keys until one turns up. A worker that sweeps
// its block finds them for free — the test is the same comparison the search
// kernel already performs against the target — while a worker that fakes the
// sweep must spend, on average, exactly the honest cost per witness it invents.
// Forging the proof is therefore never cheaper than doing the work.
//
// Counting witnesses bounds how much of the block was swept; requiring them in
// every bucket bounds *where*. With the defaults below, a gap as small as one
// bucket (0.2% of the block) leaves a bucket empty and is rejected outright,
// while an honest submission is rejected with probability under 1e-8.
//
// The coordinator verifies a random sample of the witnesses cryptographically
// (a handful of scalar multiplications), so submitting fabricated offsets that
// merely *look* well-distributed fails too.
//
// # Canaries — proof that the reporting path works
//
// Witnesses prove keys were hashed. They do not prove the worker would have told
// anyone about a hit. So the coordinator derives a few canary offsets from a
// server-side secret (statelessly — they are recomputed at verification time,
// never stored) and includes their HASH160 in the watchlist it hands the worker
// alongside the real target. A worker whose match-reporting path is broken,
// disabled, or stubbed out collects witnesses happily and still fails to return
// the canaries.
//
// # What this cannot do
//
// Nothing here compels a worker that finds the real key to report it. The target
// address is public, so a worker can always tell the real hit from a canary. See
// the "Trust model" section of the README: that risk is handled by incentive
// design and after-the-fact chain observation, not by cryptography.
package proof

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand"

	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
)

// TargetWitnesses is how many witnesses DefaultParams aims for per block.
//
// 4096 is a deliberate step down from an earlier 16384. Four separate costs fall
// with it at once — wire size, JSON parse, bucket counting and the deep audit —
// and the only thing bought by the larger figure was resolution: the smallest
// detectable coverage gap moves from 0.2% to 0.8% of a block. A cheater who
// trims 0.8% of their sweep saves 0.8% of their electricity, so that resolution
// was never worth four times the cost of every submission.
const TargetWitnesses = 4096

// Params are the per-campaign proof settings. Workers must be told all of them:
// a worker using a different WitnessBits produces a submission that cannot pass.
type Params struct {
	// WitnessBits is the required number of leading zero bits in a witness's
	// HASH160. Higher means rarer witnesses: less bandwidth, coarser resolution.
	WitnessBits uint `json:"witness_bits"`
	// Buckets is how many equal slices the block is cut into for the coverage
	// test. It sets the smallest detectable gap: one block in Buckets.
	Buckets uint32 `json:"buckets"`
	// Canaries is how many planted keys the worker must report back.
	Canaries int `json:"canaries"`
	// SampleSize is how many submitted witnesses are verified with real curve
	// math. Zero means verify every one (see DeepVerify).
	SampleSize int `json:"sample_size"`
	// Sigmas sets both count thresholds, as standard deviations below the
	// Poisson mean. 5 keeps false rejections below ~1e-6 per submission.
	Sigmas float64 `json:"sigmas"`
}

// DefaultParams derives settings for a block of 2^blockBits keys, targeting
// TargetWitnesses witnesses per block.
//
// Small blocks — the ones used in tests and local smoke runs — cannot hit that
// target at any sane difficulty, so both the difficulty and the bucket count are
// clamped to stay valid rather than degenerating. The result is always usable:
// Validate accepts DefaultParams(n) for every n in 1..keyspace.MaxBlockBits.
func DefaultParams(blockBits uint) Params {
	// 2^blockBits / 2^(blockBits-14) = 2^14 witnesses, floored at difficulty 1
	// so a witness is never simply "every key".
	witnessBits := uint(1)
	if blockBits > 13 {
		witnessBits = blockBits - 12 // 2^blockBits / 2^(blockBits-12) = 4096
	}

	// Buckets must fall with the witness count. Holding 512 buckets at 4096
	// witnesses puts the mean at 8 per bucket, which collapses the Poisson floor
	// to 1 and false-rejects roughly 17% of honest submissions. At 128 buckets
	// the mean is 32 and an empty bucket is a 1-in-10^14 event.
	buckets := uint32(128)
	if blockBits < 12 {
		buckets = uint32(1) << (blockBits / 2)
	}

	return Params{
		WitnessBits: witnessBits,
		Buckets:     buckets,
		Canaries:    4,
		SampleSize:  64,
		Sigmas:      5,
	}
}

// Validate rejects settings that would make verification meaningless.
func (p Params) Validate() error {
	switch {
	case p.WitnessBits == 0:
		return errors.New("proof: witness_bits must be positive; a difficulty of 0 makes every key a witness")
	case p.WitnessBits > 160:
		return fmt.Errorf("proof: witness_bits %d exceeds the 160-bit digest", p.WitnessBits)
	case p.Buckets == 0:
		return errors.New("proof: buckets must be positive")
	case p.Canaries < 0:
		return errors.New("proof: canaries must not be negative")
	case p.SampleSize < 0:
		return errors.New("proof: sample_size must not be negative")
	case p.Sigmas <= 0:
		return errors.New("proof: sigmas must be positive")
	}
	return nil
}

// ExpectedWitnesses is the mean number of witnesses in a block of blockLen keys.
func (p Params) ExpectedWitnesses(blockLen *big.Int) float64 {
	density := math.Ldexp(1, -int(p.WitnessBits)) // 2^-WitnessBits
	return bigToFloat(blockLen) * density
}

// MinTotal is the smallest witness count a submission may carry, set Sigmas
// standard deviations below the Poisson mean. It never drops below 1: a block
// that expects fewer than a handful of witnesses cannot be policed by counting,
// and Validate-time configuration should have avoided that.
func (p Params) MinTotal(blockLen *big.Int) uint64 {
	return poissonFloor(p.ExpectedWitnesses(blockLen), p.Sigmas)
}

// MinPerBucket is the same threshold applied to a single bucket.
func (p Params) MinPerBucket(blockLen *big.Int) uint64 {
	return poissonFloor(p.ExpectedWitnesses(blockLen)/float64(p.Buckets), p.Sigmas)
}

// poissonFloor is max(1, floor(lambda - sigmas*sqrt(lambda))).
func poissonFloor(lambda, sigmas float64) uint64 {
	t := lambda - sigmas*math.Sqrt(lambda)
	if t < 1 {
		return 1
	}
	return uint64(math.Floor(t))
}

func bigToFloat(x *big.Int) float64 {
	f, _ := new(big.Float).SetInt(x).Float64()
	return f
}

// Submission is what a worker returns when it finishes a block. Offsets are
// measured from the block's first key, so they stay small regardless of how
// deep into the keyspace the block sits.
type Submission struct {
	CampaignID string `json:"campaign_id"`
	BlockIndex uint64 `json:"block_index"`
	LeaseToken string `json:"lease_token"`

	// Witnesses lists every offset whose HASH160 met the difficulty, strictly
	// ascending. Strict ordering is required, not merely conventional: it makes
	// duplicates — the cheapest way to inflate a count — impossible to express.
	Witnesses []uint64 `json:"witnesses"`

	// Canaries lists the offsets of watchlist keys the worker matched, excluding
	// any real-target hit. Order does not matter.
	Canaries []uint64 `json:"canaries"`

	// FoundOffset, when non-nil, is an offset the worker claims hashes to the
	// campaign target. This is the jackpot claim and is always verified in full.
	FoundOffset *uint64 `json:"found_offset,omitempty"`
}

// Rejection explains why a submission failed. It is deliberately specific: a
// worker with a genuine bug deserves to be able to fix it, and a cheater learns
// nothing useful from knowing which test caught them.
type Rejection struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (r *Rejection) Error() string { return r.Code + ": " + r.Detail }

func reject(code, format string, args ...any) *Rejection {
	return &Rejection{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Result describes an accepted submission.
type Result struct {
	// Witnesses is how many the worker submitted.
	Witnesses uint64 `json:"witnesses"`
	// Expected is the Poisson mean for this block, for operator dashboards.
	Expected float64 `json:"expected"`
	// Verified is how many witnesses were checked with real curve math.
	Verified int `json:"verified"`
	// EmptiestBucket is the smallest per-bucket witness count observed. A value
	// hovering near MinPerBucket across many submissions is the early warning
	// that a worker is trimming its sweep.
	EmptiestBucket uint64 `json:"emptiest_bucket"`
	// TargetKey is set only when the submission carried a verified jackpot hit.
	TargetKey *big.Int `json:"-"`
}

// Verifier checks submissions against a campaign. The zero value is not usable;
// construct one with NewVerifier.
type Verifier struct {
	campaign *keyspace.Campaign
	params   Params
	secret   []byte
}

// NewVerifier binds a campaign, its proof params and the canary secret.
//
// secret must be high-entropy and must never reach a worker: anyone holding it
// can predict the canary offsets for every block and satisfy the canary test
// without sweeping anything.
func NewVerifier(c *keyspace.Campaign, p Params, secret []byte) (*Verifier, error) {
	if c == nil {
		return nil, errors.New("proof: nil campaign")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(secret) < 32 {
		return nil, errors.New("proof: canary secret must be at least 32 bytes")
	}
	return &Verifier{campaign: c, params: p, secret: append([]byte(nil), secret...)}, nil
}

// Params returns the settings workers must mirror.
func (v *Verifier) Params() Params { return v.params }

// CanaryOffsets derives this block's canary offsets from the server secret.
// They are a pure function of (secret, campaign, block), so the coordinator
// stores nothing and a restart cannot forget what it planted.
func (v *Verifier) CanaryOffsets(blk keyspace.Block) []uint64 {
	n := v.params.Canaries
	if n <= 0 {
		return nil
	}
	out := make([]uint64, 0, n)
	seen := make(map[uint64]struct{}, n)

	// Draw until n distinct offsets land. Collisions are vanishingly rare for
	// real block sizes but certain for the tiny blocks used in tests, and a
	// duplicated canary would make the "all canaries returned" test unfalsifiable.
	for i := 0; len(out) < n; i++ {
		if i > 64*n {
			break // block too small to hold n distinct canaries; use what we have
		}
		mac := hmac.New(sha256.New, v.secret)
		fmt.Fprintf(mac, "puzzlepool/canary/v1\x00%s\x00", v.campaign.ID)
		var buf [16]byte
		binary.BigEndian.PutUint64(buf[0:8], blk.Index)
		binary.BigEndian.PutUint64(buf[8:16], uint64(i))
		mac.Write(buf[:])

		off := new(big.Int).SetBytes(mac.Sum(nil))
		off.Mod(off, blk.Len)
		u := off.Uint64()
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

// Watchlist is what a worker compares every key against: the real target plus
// this block's canaries, shuffled so their positions carry no signal.
//
// For a public puzzle the target's HASH160 is already published, so shuffling
// hides nothing from a determined worker. It is done anyway because it costs
// nothing and does hide the canaries from a *casual* one — and the canaries'
// job is to catch broken reporting, not to trap a deliberate defector.
func (v *Verifier) Watchlist(blk keyspace.Block) ([]string, error) {
	target := v.campaign.TargetHash160
	list := make([]string, 0, v.params.Canaries+1)
	list = append(list, target)
	for _, off := range v.CanaryOffsets(blk) {
		key, ok := blk.KeyAt(off)
		if !ok {
			return nil, fmt.Errorf("proof: canary offset %d outside block %d", off, blk.Index)
		}
		h := btc.PubKeyHash160(key)
		list = append(list, fmt.Sprintf("%x", h))
	}
	// Deterministic shuffle: the same block always yields the same order, so a
	// worker retrying a lease sees a stable watchlist.
	seed := hmac.New(sha256.New, v.secret)
	fmt.Fprintf(seed, "puzzlepool/shuffle/v1\x00%s\x00%d", v.campaign.ID, blk.Index)
	r := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(seed.Sum(nil)[:8])))) //nolint:gosec // shuffling only
	r.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	return list, nil
}

// Verify runs every check against a submission. It returns a *Rejection for a
// failed proof and a plain error only for a caller mistake (bad block index).
//
// deep forces every witness to be verified instead of a random sample. Run it
// on a small random fraction of submissions, and on every submission from a
// worker whose reputation is not yet established.
func (v *Verifier) Verify(blk keyspace.Block, sub Submission, deep bool) (*Result, error) {
	if sub.BlockIndex != blk.Index {
		return nil, fmt.Errorf("proof: submission is for block %d, verifying %d", sub.BlockIndex, blk.Index)
	}
	p := v.params

	// --- Structure: ascending, distinct, in range. ------------------------
	blockLen := blk.Len
	var prev uint64
	for i, off := range sub.Witnesses {
		if i > 0 && off <= prev {
			return nil, reject("witnesses_unordered",
				"witness %d (offset %d) does not exceed its predecessor %d; witnesses must be strictly ascending", i, off, prev)
		}
		if new(big.Int).SetUint64(off).Cmp(blockLen) >= 0 {
			return nil, reject("witness_out_of_range",
				"witness %d has offset %d, past the block length %s", i, off, blockLen)
		}
		prev = off
	}

	// --- Volume: enough witnesses overall. --------------------------------
	total := uint64(len(sub.Witnesses))
	expected := p.ExpectedWitnesses(blockLen)
	if min := p.MinTotal(blockLen); total < min {
		return nil, reject("too_few_witnesses",
			"got %d witnesses, need at least %d (expected ~%.0f)", total, min, expected)
	}

	// --- Coverage: no bucket left empty. ----------------------------------
	counts, err := bucketCounts(sub.Witnesses, blockLen, p.Buckets)
	if err != nil {
		return nil, err
	}
	minPer := p.MinPerBucket(blockLen)
	emptiest := ^uint64(0)
	for i, c := range counts {
		if c < emptiest {
			emptiest = c
		}
		if c < minPer {
			return nil, reject("coverage_gap",
				"bucket %d of %d holds %d witnesses, need at least %d — that stretch of the block was not swept",
				i, p.Buckets, c, minPer)
		}
	}
	if len(counts) == 0 {
		emptiest = 0
	}

	// --- Canaries: the reporting path works. ------------------------------
	reported := make(map[uint64]struct{}, len(sub.Canaries))
	for _, off := range sub.Canaries {
		reported[off] = struct{}{}
	}
	planted := make(map[uint64]struct{}, p.Canaries)
	for _, want := range v.CanaryOffsets(blk) {
		planted[want] = struct{}{}
		if _, ok := reported[want]; !ok {
			return nil, reject("canary_missed",
				"the worker did not report a planted watchlist key; its match-reporting path did not run over the whole block")
		}
	}

	// A watchlist hit the coordinator did not plant is either junk or the prize.
	// Checking it costs one curve operation and only runs on the extras, so the
	// common case pays nothing — but a worker configured with the wrong target,
	// or one that reports every hit uniformly rather than singling out the
	// jackpot, cannot lose a real find to a misconfiguration.
	var recovered *big.Int
	for off := range reported {
		if _, isPlanted := planted[off]; isPlanted {
			continue
		}
		key, ok := blk.KeyAt(off)
		if !ok {
			return nil, reject("canary_out_of_range",
				"reported watchlist hit at offset %d is past the block length %s", off, blockLen)
		}
		h := btc.PubKeyHash160(key)
		if fmt.Sprintf("%x", h) == v.campaign.TargetHash160 {
			recovered = key
		}
	}

	// --- Cryptography: sampled witnesses are real. ------------------------
	verified, err := v.checkWitnesses(blk, sub.Witnesses, deep)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Witnesses:      total,
		Expected:       expected,
		Verified:       verified,
		EmptiestBucket: emptiest,
		TargetKey:      recovered,
	}

	// --- Jackpot claim, always verified in full. --------------------------
	if sub.FoundOffset != nil {
		key, ok := blk.KeyAt(*sub.FoundOffset)
		if !ok {
			return nil, reject("found_out_of_range",
				"claimed hit at offset %d, past the block length %s", *sub.FoundOffset, blockLen)
		}
		h := btc.PubKeyHash160(key)
		if fmt.Sprintf("%x", h) != v.campaign.TargetHash160 {
			return nil, reject("found_mismatch",
				"claimed hit at offset %d hashes to %x, not the campaign target", *sub.FoundOffset, h)
		}
		res.TargetKey = key
	}
	return res, nil
}

// checkWitnesses derives HASH160 for a sample (or all) of the offsets and
// confirms each really meets the difficulty.
func (v *Verifier) checkWitnesses(blk keyspace.Block, witnesses []uint64, deep bool) (int, error) {
	idx := v.sampleIndices(blk, len(witnesses), deep)
	for _, i := range idx {
		off := witnesses[i]
		key, ok := blk.KeyAt(off)
		if !ok {
			return 0, reject("witness_out_of_range", "witness %d has offset %d, past the block", i, off)
		}
		h := btc.PubKeyHash160(key)
		if got := h.LeadingZeroBits(); got < v.params.WitnessBits {
			return 0, reject("witness_forged",
				"witness %d (offset %d) hashes to %x, which has %d leading zero bits, not the required %d",
				i, off, h, got, v.params.WitnessBits)
		}
	}
	return len(idx), nil
}

// sampleIndices picks which witnesses to verify. The choice is seeded from the
// server secret and the block, so it is reproducible during a dispute yet
// unpredictable to a worker deciding which offsets it can afford to fake.
func (v *Verifier) sampleIndices(blk keyspace.Block, n int, deep bool) []int {
	if n == 0 {
		return nil
	}
	if deep || v.params.SampleSize == 0 || v.params.SampleSize >= n {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		return all
	}
	seed := hmac.New(sha256.New, v.secret)
	fmt.Fprintf(seed, "puzzlepool/sample/v1\x00%s\x00%d", v.campaign.ID, blk.Index)
	r := rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(seed.Sum(nil)[:8])))) //nolint:gosec // sampling only
	return r.Perm(n)[:v.params.SampleSize]
}

// bucketCounts tallies witnesses per bucket. Buckets partition the block evenly;
// integer division puts any remainder keys in the last bucket, which only ever
// makes that bucket's threshold easier to meet.
func bucketCounts(witnesses []uint64, blockLen *big.Int, buckets uint32) ([]uint64, error) {
	if buckets == 0 {
		return nil, errors.New("proof: zero buckets")
	}
	nb := new(big.Int).SetUint64(uint64(buckets))
	width := new(big.Int).Div(blockLen, nb)
	if width.Sign() == 0 {
		// Fewer keys than buckets: every key is its own bucket and the coverage
		// test degenerates. Only reachable in tests.
		width = big.NewInt(1)
	}
	counts := make([]uint64, buckets)
	last := uint64(buckets) - 1
	for _, off := range witnesses {
		b := new(big.Int).Div(new(big.Int).SetUint64(off), width).Uint64()
		if b > last {
			b = last
		}
		counts[b]++
	}
	return counts, nil
}
