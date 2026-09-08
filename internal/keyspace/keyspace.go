// Package keyspace maps a puzzle's private-key range onto the fixed-size blocks
// that workers lease, sweep and prove.
//
// The block table is never materialized. Puzzle #71 with 2^40-key blocks has
// 2^30 blocks; #135 would have 2^94. Only blocks that have actually been leased
// get a row in the store — "available" means "no row exists". Allocation picks a
// uniformly random index and relies on a uniqueness constraint to reject the
// astronomically rare collision, so the cost of handing out a block does not
// grow with the size of the campaign.
package keyspace

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// MaxBlockIndexBits caps NumBlocks at 2^63 so a block index always fits in a
// uint64 (and in a SQLite INTEGER). MaxBlockBits caps the block size for the
// same reason: an offset within a block is a uint64 too.
//
// Together they bound how deep a campaign can reach: 2^63 blocks of 2^63 keys is
// 2^126 keys, so puzzles up to #126 are addressable and #127 and beyond are not.
// That is far past anything a real pool will attempt — puzzle #71 needs 2^30
// blocks — but the constructor rejects an over-large campaign explicitly rather
// than truncating the index and handing out the same blocks forever.
const (
	MaxBlockIndexBits = 63
	MaxBlockBits      = 63
)

// Campaign is one puzzle being searched: a target, a key range, and the block
// geometry every worker and the coordinator must agree on.
type Campaign struct {
	// ID is the stable identifier workers quote in every request.
	ID string
	// PuzzleNum is the Bitcoin Puzzle index (1..160), for display only.
	PuzzleNum int
	// TargetHash160 is the HASH160 of the address being searched, hex-encoded.
	TargetHash160 string
	// Min and Max bound the search, inclusive on both ends.
	Min, Max *big.Int
	// BlockBits sets the block size to 2^BlockBits keys.
	BlockBits uint
	// Shift slides the tiling down by this many keys, so block 0 starts at
	// Min-Shift. It must be less than the block size. Zero is the plain tiling.
	Shift uint64

	base      *big.Int
	numBlocks *big.Int
}

// NewCampaign validates the geometry and precomputes the block count.
//
// The range is NOT required to be a whole number of blocks: the final block is
// truncated at Max. Workers learn each block's true length from the lease, so a
// short tail block is proved on its real width, not padded.
func NewCampaign(id string, puzzleNum int, targetHash160 string, min, max *big.Int, blockBits uint) (*Campaign, error) {
	return NewShiftedCampaign(id, puzzleNum, targetHash160, min, max, blockBits, 0)
}

// NewShiftedCampaign is NewCampaign with the tiling slid down by shift keys.
//
// shift must be smaller than the block size, and the range must sit far enough
// above zero that Min-shift is still a valid scalar. A campaign on a real puzzle
// satisfies that by a margin of 2^37 or more; the check exists for the tiny
// ranges used in tests.
func NewShiftedCampaign(id string, puzzleNum int, targetHash160 string, min, max *big.Int, blockBits uint, shift uint64) (*Campaign, error) {
	switch {
	case strings.TrimSpace(id) == "":
		return nil, errors.New("keyspace: campaign id is empty")
	case min == nil || max == nil:
		return nil, errors.New("keyspace: nil range bound")
	case min.Sign() <= 0:
		return nil, errors.New("keyspace: min must be positive (key 0 is not a valid scalar)")
	case min.Cmp(max) > 0:
		return nil, fmt.Errorf("keyspace: min %s exceeds max %s", min, max)
	case blockBits == 0 || blockBits > MaxBlockBits:
		return nil, fmt.Errorf("keyspace: block_bits %d out of range (1..%d)", blockBits, MaxBlockBits)
	case len(targetHash160) != 40:
		return nil, fmt.Errorf("keyspace: target_hash160 must be 40 hex chars, got %d", len(targetHash160))
	}

	c := &Campaign{
		ID:            id,
		PuzzleNum:     puzzleNum,
		TargetHash160: strings.ToLower(targetHash160),
		Min:           new(big.Int).Set(min),
		Max:           new(big.Int).Set(max),
		BlockBits:     blockBits,
		Shift:         shift,
	}

	size := c.BlockSize()
	shiftBig := new(big.Int).SetUint64(shift)
	if shiftBig.Cmp(size) >= 0 {
		return nil, fmt.Errorf("keyspace: shift %d must be below the block size 2^%d", shift, blockBits)
	}
	c.base = new(big.Int).Sub(c.Min, shiftBig)
	if c.base.Sign() <= 0 {
		return nil, fmt.Errorf("keyspace: shift %d pushes the first block to %s, at or below zero; the range is too close to the bottom of the keyspace to be shifted", shift, c.base)
	}

	// numBlocks = ceil(coveredLen / 2^blockBits) where coveredLen spans from the
	// shifted base to Max, so the truncated tail still gets an index of its own
	// instead of being dropped.
	rangeLen := new(big.Int).Sub(c.Max, c.base)
	rangeLen.Add(rangeLen, big.NewInt(1))
	n := new(big.Int).Add(rangeLen, new(big.Int).Sub(size, big.NewInt(1)))
	n.Div(n, size)

	if n.BitLen() > MaxBlockIndexBits {
		return nil, fmt.Errorf("keyspace: this range needs a %d-bit block index, over the %d-bit limit; raise block_bits (max %d, giving a reach of 2^%d keys)",
			n.BitLen(), MaxBlockIndexBits, MaxBlockBits, MaxBlockIndexBits+MaxBlockBits)
	}
	c.numBlocks = n
	return c, nil
}

// BlockSize is the nominal number of keys per block, 2^BlockBits.
func (c *Campaign) BlockSize() *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), c.BlockBits)
}

// NumBlocks is how many blocks the range divides into, tail included.
func (c *Campaign) NumBlocks() *big.Int { return new(big.Int).Set(c.numBlocks) }

// NumBlocksU64 returns NumBlocks as a uint64. Safe by construction: the
// constructor rejects campaigns whose block count would not fit.
func (c *Campaign) NumBlocksU64() uint64 { return c.numBlocks.Uint64() }

// Base is the key the tiling starts at: Min-Shift. Blocks below Min and above
// Max exist only at the two ends and cost at most one block of wasted work each.
func (c *Campaign) Base() *big.Int { return new(big.Int).Set(c.base) }

// BlockIndexOf reports which block covers key, and whether key is covered at all.
//
// This is what makes a swept prize attributable: the coordinator signed a lease
// naming a worker for this index before anyone could have found the key, so a
// key that surfaces on chain names the participant who was holding the ground it
// sat on.
func (c *Campaign) BlockIndexOf(key *big.Int) (uint64, bool) {
	if key == nil || key.Cmp(c.base) < 0 || key.Cmp(c.Max) > 0 {
		return 0, false
	}
	idx := new(big.Int).Sub(key, c.base)
	idx.Div(idx, c.BlockSize())
	if !idx.IsUint64() || idx.Cmp(c.numBlocks) >= 0 {
		return 0, false
	}
	return idx.Uint64(), true
}

// Block is one leasable unit of work: a contiguous, half-open-at-the-top-in-
// spirit but inclusive key interval, plus its length.
type Block struct {
	Index uint64
	Lo    *big.Int // first key, inclusive
	Hi    *big.Int // last key, inclusive
	Len   *big.Int // Hi - Lo + 1; equals BlockSize except for the tail block
}

// ErrBlockOutOfRange is returned for an index at or past NumBlocks.
var ErrBlockOutOfRange = errors.New("keyspace: block index out of range")

// BlockAt resolves an index to its key interval.
func (c *Campaign) BlockAt(index uint64) (Block, error) {
	if new(big.Int).SetUint64(index).Cmp(c.numBlocks) >= 0 {
		return Block{}, fmt.Errorf("%w: %d (have %s)", ErrBlockOutOfRange, index, c.numBlocks)
	}
	size := c.BlockSize()
	lo := new(big.Int).Mul(size, new(big.Int).SetUint64(index))
	lo.Add(lo, c.base)

	hi := new(big.Int).Add(lo, size)
	hi.Sub(hi, big.NewInt(1))
	if hi.Cmp(c.Max) > 0 { // truncate the tail block
		hi.Set(c.Max)
	}

	length := new(big.Int).Sub(hi, lo)
	length.Add(length, big.NewInt(1))
	return Block{Index: index, Lo: lo, Hi: hi, Len: length}, nil
}

// Contains reports whether key falls inside b.
func (b Block) Contains(key *big.Int) bool {
	return key.Cmp(b.Lo) >= 0 && key.Cmp(b.Hi) <= 0
}

// KeyAt returns the key at a zero-based offset within b, or false if the offset
// runs past the block's end.
func (b Block) KeyAt(offset uint64) (*big.Int, bool) {
	off := new(big.Int).SetUint64(offset)
	if off.Cmp(b.Len) >= 0 {
		return nil, false
	}
	return new(big.Int).Add(b.Lo, off), true
}

// PuzzleRange returns the canonical Bitcoin Puzzle range for puzzle n:
// [2^(n-1), 2^n - 1].
func PuzzleRange(n int) (min, max *big.Int, err error) {
	if n < 1 || n > 160 {
		return nil, nil, fmt.Errorf("keyspace: puzzle number %d out of range (1..160)", n)
	}
	min = new(big.Int).Lsh(big.NewInt(1), uint(n-1))
	max = new(big.Int).Lsh(big.NewInt(1), uint(n))
	max.Sub(max, big.NewInt(1))
	return min, max, nil
}
