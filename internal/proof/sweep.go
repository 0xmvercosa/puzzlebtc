package proof

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/0xmvercosa/puzzlebtc/internal/blind"
	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
)

// Sweeper produces a Submission by actually walking a block on the CPU.
//
// It is the reference implementation of the worker side: correct, obvious, and
// far too slow for real campaigns (a few tens of thousands of keys per second
// per core, against hundreds of millions on a GPU). Its jobs are to define the
// protocol unambiguously, to back the tests, and to give a GPU worker something
// to diff against on a small block before it is trusted with a real one.
type Sweeper struct {
	Params    Params
	Watchlist map[btc.Hash160]struct{}
	// Target is the campaign's real target. A hit on it is reported as
	// FoundOffset; hits on anything else in Watchlist are canaries.
	Target btc.Hash160
}

// NewSweeper builds a sweeper from the hex watchlist a lease hands out.
func NewSweeper(p Params, targetHex string, watchlistHex []string) (*Sweeper, error) {
	target, err := parseHash160(targetHex)
	if err != nil {
		return nil, fmt.Errorf("proof: target: %w", err)
	}
	s := &Sweeper{Params: p, Target: target, Watchlist: make(map[btc.Hash160]struct{}, len(watchlistHex))}
	for _, h := range watchlistHex {
		v, err := parseHash160(h)
		if err != nil {
			return nil, fmt.Errorf("proof: watchlist: %w", err)
		}
		s.Watchlist[v] = struct{}{}
	}
	return s, nil
}

// NewBlindSweeper builds a sweeper for a blinded campaign, which is told the
// watchlist but not which entry is the target.
//
// A blinded worker does not need to know: it reports every watchlist hit the
// same way and the coordinator re-derives them, so a real find is recognized on
// the server. Withholding the target from the client is not secrecy — the
// puzzle address is public and a modified client can hardcode it — it is the
// removal of a step the honest client no longer has to get right, and one fewer
// place for a misconfigured worker to drop a prize.
func NewBlindSweeper(p Params, watchlistHex []string) (*Sweeper, error) {
	s := &Sweeper{Params: p, Watchlist: make(map[btc.Hash160]struct{}, len(watchlistHex))}
	for _, h := range watchlistHex {
		v, err := parseHash160(h)
		if err != nil {
			return nil, fmt.Errorf("proof: watchlist: %w", err)
		}
		s.Watchlist[v] = struct{}{}
	}
	return s, nil
}

func parseHash160(s string) (btc.Hash160, error) {
	var h btc.Hash160
	if len(s) != 2*btc.Hash160Len {
		return h, fmt.Errorf("hash160 %q must be %d hex chars, got %d", s, 2*btc.Hash160Len, len(s))
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("hash160 %q is not valid hex: %w", s, err)
	}
	copy(h[:], raw)
	return h, nil
}

// Sweep walks every key in blk and returns the submission for it.
//
// Witnesses come out strictly ascending because the walk is ascending — a GPU
// worker whose kernel emits out of order must sort before submitting, or the
// coordinator will reject the result as unordered.
func (s *Sweeper) Sweep(ctx context.Context, blk keyspace.Block) (Submission, error) {
	sub := Submission{BlockIndex: blk.Index}

	key := new(big.Int).Set(blk.Lo)
	one := big.NewInt(1)
	length := blk.Len.Uint64()
	if !blk.Len.IsUint64() {
		return sub, fmt.Errorf("proof: block %d has %s keys, too many for a single CPU sweep", blk.Index, blk.Len)
	}

	for off := uint64(0); off < length; off++ {
		// Cancellation is checked on a coarse stride: the check costs more than
		// the hash otherwise.
		if off&0xFFFF == 0 {
			select {
			case <-ctx.Done():
				return sub, ctx.Err()
			default:
			}
		}

		h := btc.PubKeyHash160(key)

		if h.LeadingZeroBits() >= s.Params.WitnessBits {
			sub.Witnesses = append(sub.Witnesses, off)
		}
		if _, hit := s.Watchlist[h]; hit {
			if h == s.Target {
				o := off
				sub.FoundOffset = &o
			} else {
				sub.Canaries = append(sub.Canaries, off)
			}
		}

		key.Add(key, one)
	}
	return sub, nil
}

// SweepLot walks a blinded lot and returns the submission for it.
//
// It is the same sweep as Sweep, driven by curve points instead of by private
// keys: the worker starts at the lot's public start point and adds G once per
// key. The offsets it reports mean exactly what they mean in Sweep, so the
// coordinator verifies both the same way — the only difference is that this
// worker never held a key and so has nothing to keep.
//
// This is also the faster of the two. Sweep derives a public key from scratch
// for every offset; here each key costs one point addition, which is what every
// real searcher does and what a GPU kernel batches.
func (s *Sweeper) SweepLot(ctx context.Context, lot blind.Lot) (Submission, error) {
	var sub Submission

	length, ok := new(big.Int).SetString(lot.Length, 10)
	if !ok || !length.IsUint64() {
		return sub, fmt.Errorf("proof: lot length %q is not a usable count", lot.Length)
	}
	w, err := blind.NewWalker(lot.StartPoint)
	if err != nil {
		return sub, err
	}

	n := length.Uint64()
	for off := uint64(0); off < n; off++ {
		if off&0xFFFF == 0 {
			select {
			case <-ctx.Done():
				return sub, ctx.Err()
			default:
			}
		}

		h := w.Hash160()

		if h.LeadingZeroBits() >= s.Params.WitnessBits {
			sub.Witnesses = append(sub.Witnesses, off)
		}
		if _, hit := s.Watchlist[h]; hit {
			if h == s.Target {
				o := off
				sub.FoundOffset = &o
			} else {
				sub.Canaries = append(sub.Canaries, off)
			}
		}

		w.Next()
	}
	return sub, nil
}
