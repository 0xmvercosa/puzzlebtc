package keyspace

import (
	"math/big"
	"testing"
)

const target = "751e76e8199196d454941c45d1b3a323f1433bd6"

func TestPuzzleRange(t *testing.T) {
	cases := []struct {
		n        int
		min, max string
	}{
		{1, "1", "1"},
		{2, "2", "3"},
		{38, "2000000000", "3fffffffff"},
		{71, "400000000000000000", "7fffffffffffffffff"},
	}
	for _, c := range cases {
		min, max, err := PuzzleRange(c.n)
		if err != nil {
			t.Fatalf("puzzle %d: %v", c.n, err)
		}
		if min.Text(16) != c.min || max.Text(16) != c.max {
			t.Errorf("puzzle %d: got [%s,%s], want [%s,%s]", c.n, min.Text(16), max.Text(16), c.min, c.max)
		}
	}
	if _, _, err := PuzzleRange(0); err == nil {
		t.Error("puzzle 0 was accepted")
	}
	if _, _, err := PuzzleRange(161); err == nil {
		t.Error("puzzle 161 was accepted")
	}
}

// Blocks must tile the range exactly: no gap between one block's end and the
// next block's start, and no key past Max.
func TestBlocksTileTheRangeExactly(t *testing.T) {
	min, max, _ := PuzzleRange(20)
	c, err := NewCampaign("c", 20, target, min, max, 8) // 256-key blocks
	if err != nil {
		t.Fatal(err)
	}

	n := c.NumBlocksU64()
	if n != 1<<11 { // 2^19 keys / 2^8 per block
		t.Fatalf("got %d blocks, want %d", n, 1<<11)
	}

	var prevHi *big.Int
	covered := new(big.Int)
	for i := uint64(0); i < n; i++ {
		blk, err := c.BlockAt(i)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && blk.Lo.Cmp(min) != 0 {
			t.Fatalf("block 0 starts at %s, want %s", blk.Lo, min)
		}
		if prevHi != nil {
			want := new(big.Int).Add(prevHi, big.NewInt(1))
			if blk.Lo.Cmp(want) != 0 {
				t.Fatalf("block %d starts at %s, leaving a gap after %s", i, blk.Lo, prevHi)
			}
		}
		if blk.Hi.Cmp(max) > 0 {
			t.Fatalf("block %d ends at %s, past the campaign max %s", i, blk.Hi, max)
		}
		covered.Add(covered, blk.Len)
		prevHi = blk.Hi
	}

	if prevHi.Cmp(max) != 0 {
		t.Errorf("last block ends at %s, want %s", prevHi, max)
	}
	rangeLen := new(big.Int).Sub(max, min)
	rangeLen.Add(rangeLen, big.NewInt(1))
	if covered.Cmp(rangeLen) != 0 {
		t.Errorf("blocks cover %s keys, range holds %s", covered, rangeLen)
	}
}

// A range that is not a whole number of blocks must still be fully covered, with
// the final block truncated rather than overrunning Max or being dropped.
func TestTailBlockIsTruncatedNotDropped(t *testing.T) {
	min := big.NewInt(100)
	max := big.NewInt(1000) // 901 keys
	c, err := NewCampaign("c", 1, target, min, max, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.NumBlocksU64(); got != 4 { // ceil(901/256)
		t.Fatalf("got %d blocks, want 4", got)
	}
	tail, err := c.BlockAt(3)
	if err != nil {
		t.Fatal(err)
	}
	if tail.Hi.Cmp(max) != 0 {
		t.Errorf("tail block ends at %s, want %s", tail.Hi, max)
	}
	if want := big.NewInt(901 - 3*256); tail.Len.Cmp(want) != 0 {
		t.Errorf("tail block holds %s keys, want %s", tail.Len, want)
	}
}

func TestBlockAtRejectsOutOfRange(t *testing.T) {
	min, max, _ := PuzzleRange(16)
	c, err := NewCampaign("c", 16, target, min, max, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.BlockAt(c.NumBlocksU64()); err == nil {
		t.Error("an index one past the end was accepted")
	}
}

func TestKeyAtAndContains(t *testing.T) {
	min, max, _ := PuzzleRange(16)
	c, _ := NewCampaign("c", 16, target, min, max, 8)
	blk, err := c.BlockAt(2)
	if err != nil {
		t.Fatal(err)
	}
	k, ok := blk.KeyAt(0)
	if !ok || k.Cmp(blk.Lo) != 0 {
		t.Errorf("offset 0 = %v (ok=%v), want %s", k, ok, blk.Lo)
	}
	if _, ok := blk.KeyAt(blk.Len.Uint64()); ok {
		t.Error("an offset one past the block end was accepted")
	}
	if !blk.Contains(blk.Hi) {
		t.Error("the block does not contain its own last key")
	}
	if blk.Contains(new(big.Int).Add(blk.Hi, big.NewInt(1))) {
		t.Error("the block claims a key past its end")
	}
}

// A campaign whose block count would not fit a uint64 index must be rejected up
// front, not silently truncated into handing out the same blocks forever.
//
// This also pins the documented reach of the design: MaxBlockIndexBits blocks of
// MaxBlockBits keys is 2^126 keys, so puzzle #126 is the deepest addressable one.
// Everything a real pool would run — #71 needs 2^30 blocks — is far inside it.
func TestOversizedCampaignRejected(t *testing.T) {
	min, max, _ := PuzzleRange(160)
	if _, err := NewCampaign("c", 160, target, min, max, 8); err == nil {
		t.Fatal("puzzle 160 with 256-key blocks was accepted; the index would overflow")
	}
	// Even the largest legal block size cannot bring #160 into range.
	if _, err := NewCampaign("c", 160, target, min, max, MaxBlockBits); err == nil {
		t.Error("puzzle 160 was accepted at the maximum block size; it needs a 97-bit index")
	}

	// #126 is the deepest that fits, and #127 is the first that does not.
	min, max, _ = PuzzleRange(126)
	if _, err := NewCampaign("c", 126, target, min, max, MaxBlockBits); err != nil {
		t.Errorf("puzzle 126 rejected at the maximum block size: %v", err)
	}
	min, max, _ = PuzzleRange(127)
	if _, err := NewCampaign("c", 127, target, min, max, MaxBlockBits); err == nil {
		t.Error("puzzle 127 was accepted; it is one bit past the documented reach")
	}

	// The campaign a pool would actually run is comfortably inside the limit.
	min, max, _ = PuzzleRange(71)
	c, err := NewCampaign("c", 71, target, min, max, 40)
	if err != nil {
		t.Fatalf("puzzle 71 with 2^40 blocks rejected: %v", err)
	}
	if got := c.NumBlocksU64(); got != 1<<30 {
		t.Errorf("puzzle 71 has %d blocks, want 2^30", got)
	}
}

func TestBadCampaignsRejected(t *testing.T) {
	one, ten := big.NewInt(1), big.NewInt(10)
	cases := []struct {
		name string
		fn   func() error
	}{
		{"empty id", func() error { _, err := NewCampaign("", 1, target, one, ten, 8); return err }},
		{"min > max", func() error { _, err := NewCampaign("c", 1, target, ten, one, 8); return err }},
		{"zero min", func() error { _, err := NewCampaign("c", 1, target, big.NewInt(0), ten, 8); return err }},
		{"short target", func() error { _, err := NewCampaign("c", 1, "abcd", one, ten, 8); return err }},
		{"zero block bits", func() error { _, err := NewCampaign("c", 1, target, one, ten, 0); return err }},
		{"huge block bits", func() error { _, err := NewCampaign("c", 1, target, one, ten, 64); return err }},
	}
	for _, c := range cases {
		if err := c.fn(); err == nil {
			t.Errorf("%s: accepted, want an error", c.name)
		}
	}
}
