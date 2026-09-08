package keyspace

import (
	"fmt"
	"math/big"
	"sort"
)

// BlockRange is a closed interval of block indices.
type BlockRange struct {
	Lo uint64 `json:"lo"`
	Hi uint64 `json:"hi"`
}

// Len is how many blocks the range covers.
func (r BlockRange) Len() uint64 { return r.Hi - r.Lo + 1 }

// Contains reports whether index falls inside r.
func (r BlockRange) Contains(index uint64) bool { return index >= r.Lo && index <= r.Hi }

// BlockRangeForKeys converts a key interval into the block indices that overlap
// it. A partially covered block counts as covered: the claim is that someone
// swept those keys, and a block only half-covered by the claim still has keys
// nobody has proven, so treating it as claimed is the conservative direction
// only if claims are trusted. It is not — claimed blocks are still swept, just
// last, so erring toward "claimed" costs nothing but ordering.
//
// Keys outside the campaign are clamped. Returns false if the interval misses
// the campaign entirely.
func (c *Campaign) BlockRangeForKeys(lo, hi *big.Int) (BlockRange, bool) {
	if lo.Cmp(hi) > 0 {
		lo, hi = hi, lo
	}
	if hi.Cmp(c.Min) < 0 || lo.Cmp(c.Max) > 0 {
		return BlockRange{}, false
	}
	if lo.Cmp(c.Min) < 0 {
		lo = c.Min
	}
	if hi.Cmp(c.Max) > 0 {
		hi = c.Max
	}
	size := c.BlockSize()
	loIdx := new(big.Int).Div(new(big.Int).Sub(lo, c.Min), size)
	hiIdx := new(big.Int).Div(new(big.Int).Sub(hi, c.Min), size)
	return BlockRange{Lo: loIdx.Uint64(), Hi: hiIdx.Uint64()}, true
}

// ClaimSet is a campaign's third-party scan claims: ranges somebody says they
// already searched, without proof this project would accept.
//
// Claims are never treated as swept. They only change the ORDER blocks go out:
// unclaimed ground first, claimed ground after it runs out. If the claims are
// honest that ordering raises the odds per block by 1/(1-f), where f is the
// claimed fraction, because the key cannot be in ground already searched. If the
// claims are dishonest nothing is lost — those blocks are still handed out, just
// later. The asymmetry is the whole point: excluding them outright would risk
// skipping the key forever to save some duplicated work.
type ClaimSet struct {
	ranges []BlockRange // sorted by Lo, non-overlapping
	blocks uint64       // total blocks covered, overlaps merged
}

// NewClaimSet merges overlapping and adjacent ranges into a canonical set.
func NewClaimSet(in []BlockRange) *ClaimSet {
	cs := &ClaimSet{}
	if len(in) == 0 {
		return cs
	}
	sorted := append([]BlockRange(nil), in...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Lo != sorted[j].Lo {
			return sorted[i].Lo < sorted[j].Lo
		}
		return sorted[i].Hi < sorted[j].Hi
	})

	cur := sorted[0]
	for _, r := range sorted[1:] {
		// Merge when overlapping or touching. The +1 guards against overflow at
		// the very top of the index space.
		if r.Lo <= cur.Hi || (cur.Hi != ^uint64(0) && r.Lo == cur.Hi+1) {
			if r.Hi > cur.Hi {
				cur.Hi = r.Hi
			}
			continue
		}
		cs.ranges = append(cs.ranges, cur)
		cs.blocks += cur.Len()
		cur = r
	}
	cs.ranges = append(cs.ranges, cur)
	cs.blocks += cur.Len()
	return cs
}

// Contains reports whether a block index falls inside any claim.
func (cs *ClaimSet) Contains(index uint64) bool {
	if cs == nil || len(cs.ranges) == 0 {
		return false
	}
	// First range whose Hi is at or past index; if it also starts at or before
	// index, the block is inside it.
	i := sort.Search(len(cs.ranges), func(i int) bool { return cs.ranges[i].Hi >= index })
	return i < len(cs.ranges) && cs.ranges[i].Lo <= index
}

// Blocks is how many blocks the claims cover in total, overlaps merged.
func (cs *ClaimSet) Blocks() uint64 {
	if cs == nil {
		return 0
	}
	return cs.blocks
}

// Ranges returns the canonical merged ranges.
func (cs *ClaimSet) Ranges() []BlockRange {
	if cs == nil {
		return nil
	}
	return append([]BlockRange(nil), cs.ranges...)
}

// Fraction is the share of the campaign the claims cover, in [0,1].
func (cs *ClaimSet) Fraction(total *big.Int) float64 {
	if cs == nil || cs.blocks == 0 || total.Sign() == 0 {
		return 0
	}
	f, _ := new(big.Float).Quo(
		new(big.Float).SetUint64(cs.blocks),
		new(big.Float).SetInt(total),
	).Float64()
	if f > 1 {
		return 1
	}
	return f
}

// String renders the set for operator output.
func (cs *ClaimSet) String() string {
	if cs == nil || len(cs.ranges) == 0 {
		return "no third-party claims"
	}
	return fmt.Sprintf("%d claimed range(s) covering %d blocks", len(cs.ranges), cs.blocks)
}
