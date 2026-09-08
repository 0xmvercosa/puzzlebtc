// Package blind implements the blind-lot protocol: a worker searches a range of
// private keys without ever being told which keys they are.
//
// # Why
//
// The pool's hardest problem is a participant who edits the client to keep the
// prize instead of reporting it. Nothing detects that edit — a modified client
// behaves identically to an honest one until the single moment it finds the key,
// and at that moment it is holding the key in its own memory on its own machine.
// Every approach that tries to *detect* the modification is ruled out with
// reasons in docs/CLIENTE_MODIFICADO.md.
//
// So this package attacks the other end. It does not try to detect the modified
// client. It arranges for the client not to have the key in the first place.
//
// # How
//
// A worker's lot is handed over as a curve point, not as a number:
//
//	A = a*G          the public key of the lot's first private key
//	n                how many keys the lot covers
//
// The worker walks A, A+G, A+2G, ... hashing each point exactly as it would have
// hashed the public key derived from a private key. Nothing else about the
// search changes; this is the same sequential point addition every fast searcher
// already does, and it is faster than deriving each key from scratch.
//
// When a point's HASH160 lands on the watchlist, the worker reports the offset it
// was standing at. The coordinator holds a, so it computes a+offset and has the
// key. The worker holds only points, and a point is not a key.
//
// # What it costs an attacker
//
// A worker who wants the key must solve a discrete logarithm: recover a from
// A = a*G. Pollard's kangaroo does that in about 2*sqrt(W) group operations for
// an interval of width W, which for puzzle #71 is roughly 2^36 — about eight
// times the work of sweeping a whole lot, and a different program from the one
// the pool ships. See internal/kangaroo.
//
// That is a speed bump, not a wall: on a fast GPU 2^36 group operations is under
// a minute. Three things still change, and they are the point:
//
//  1. The honest client cannot leak what it does not have. A memory dump, a
//     compromised machine, a curious participant reading the source — none of
//     them produce a key. The promise that a participant never sees the prize
//     key stops being a policy and becomes a property.
//  2. Theft stops being passive. "I just kept what my computer found" is no
//     longer available: extracting the key requires deliberately running a
//     second, different cryptanalytic attack on data the pool handed over
//     blinded. That is what makes the attribution in docs/DISSUASAO.md bite.
//  3. On campaigns with a large range the speed bump becomes a real wall. The
//     kangaroo cost grows as sqrt of the range: 2^36 for #71, but 2^68 for #135
//     — more than the whole pool will ever compute.
//
// # The lattice trap
//
// The obvious version of this is broken, and the break is cheap enough that
// shipping it would have been worse than shipping nothing.
//
// If lots simply tile the range from Min, then every lot's first key is
// Min + j*2^blockBits for some index j. A worker holding A can subtract Min*G,
// set Q = (2^blockBits)*G, and solve A - Min*G = j*Q with baby-step/giant-step
// over the block count alone. For puzzle #71 that is about 2^19 operations and a
// table of half a million points: milliseconds, on a laptop, in a few lines.
//
// The fix is the campaign shift in internal/keyspace: the tiling starts at
// Min - shift for a secret shift drawn under the block size, so lot starts lie
// on no lattice the worker can name, and the cheapest attack goes back to the
// full-range discrete log. TestLatticeAttackBreaksUnshiftedLots and
// TestShiftDefeatsLatticeAttack in this package run both attacks for real.
//
// # What the worker gives up
//
// Honesty about the trade: a blinded worker can no longer check for itself that
// the ground it was handed lies inside the campaign's range. Verifying that
// costs exactly what attacking it costs — one discrete log per lot audited —
// which is affordable for an auditor spot-checking a handful of lots and
// pointless for a thief who must first be the one to find the key. AuditLot
// below is that check, and it is the same kangaroo either way.
package blind

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"

	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/kangaroo"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
)

// DeriveShift returns the tiling shift for a campaign, in [0, 2^blockBits).
//
// It is derived rather than stored so the operator holds one secret instead of
// two, and so a coordinator restarted from an empty database still tiles the
// campaign the same way — handing out a differently-aligned lot after a restart
// would silently re-issue ground that was already swept.
//
// The secret must never reach a worker. With it, every lot's first key follows
// from the block index in one subtraction.
func DeriveShift(secret []byte, campaignID string, blockBits uint) (uint64, error) {
	if len(secret) == 0 {
		return 0, errors.New("blind: campaign secret is empty")
	}
	if blockBits == 0 || blockBits > 63 {
		return 0, fmt.Errorf("blind: block_bits %d out of range (1..63)", blockBits)
	}
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte("puzzlebtc/blind-shift/v1\x00"))
	_, _ = m.Write([]byte(campaignID))
	sum := m.Sum(nil)

	v := binary.BigEndian.Uint64(sum[:8])
	if blockBits < 64 {
		v &= (uint64(1) << blockBits) - 1
	}
	return v, nil
}

// Lot is the whole of what a blinded worker receives. There is no key in it.
type Lot struct {
	// StartPoint is the compressed public key of the lot's first private key,
	// 33 bytes hex-encoded.
	StartPoint string `json:"start_point"`
	// Length is how many consecutive points the worker must walk, decimal.
	Length string `json:"length"`
}

// LotFor builds the client view of a block.
//
// The block index is deliberately not part of it: the index plus the campaign
// geometry would give away where the lot sits even without the first key, and a
// worker addresses its lot by lease token instead.
func LotFor(blk keyspace.Block) (Lot, error) {
	if blk.Lo == nil || blk.Len == nil {
		return Lot{}, errors.New("blind: block is missing its bounds")
	}
	if blk.Lo.Sign() <= 0 {
		return Lot{}, fmt.Errorf("blind: block %d starts at %s, which is not a valid scalar", blk.Index, blk.Lo)
	}
	pt := kangaroo.PublicKeyFromScalar(blk.Lo)
	return Lot{
		StartPoint: EncodePoint(&pt),
		Length:     blk.Len.String(),
	}, nil
}

// EncodePoint serializes an affine point as a compressed public key in hex.
func EncodePoint(p *secp256k1.JacobianPoint) string {
	q := *p
	if !q.Z.IsOne() {
		q.ToAffine()
	}
	var out [33]byte
	out[0] = 0x02
	if q.Y.IsOdd() {
		out[0] = 0x03
	}
	var xb [32]byte
	q.X.PutBytes(&xb)
	copy(out[1:], xb[:])
	return hex.EncodeToString(out[:])
}

// DecodePoint parses a compressed public key in hex back to an affine point.
func DecodePoint(s string) (secp256k1.JacobianPoint, error) {
	var p secp256k1.JacobianPoint
	raw, err := hex.DecodeString(s)
	if err != nil {
		return p, fmt.Errorf("blind: start point is not valid hex: %w", err)
	}
	pub, err := secp256k1.ParsePubKey(raw)
	if err != nil {
		return p, fmt.Errorf("blind: start point is not a curve point: %w", err)
	}
	pub.AsJacobian(&p)
	if !p.Z.IsOne() {
		p.ToAffine()
	}
	return p, nil
}

// Walker walks the points of a lot: P, P+G, P+2G, ...
//
// This is the reference implementation — one field inversion per step, which a
// GPU kernel replaces with a batched Montgomery inversion amortizing to about
// five multiplications per key. See docs/OTIMIZACOES.md. The arithmetic it
// performs is the same, and a kernel that disagrees with it on a small lot is
// wrong.
type Walker struct {
	p secp256k1.JacobianPoint
	g secp256k1.JacobianPoint
}

// NewWalker starts a walk at the lot's first point.
func NewWalker(startPoint string) (*Walker, error) {
	p, err := DecodePoint(startPoint)
	if err != nil {
		return nil, err
	}
	return &Walker{p: p, g: generator()}, nil
}

// Point is the point the walker is standing on.
func (w *Walker) Point() *secp256k1.JacobianPoint { return &w.p }

// Hash160 is the HASH160 of the current point's compressed public key — the
// value compared against the watchlist.
func (w *Walker) Hash160() btc.Hash160 { return btc.PointHash160(&w.p) }

// Next advances one key.
func (w *Walker) Next() {
	var out secp256k1.JacobianPoint
	secp256k1.AddNonConst(&w.p, &w.g, &out)
	if !out.Z.IsOne() {
		out.ToAffine()
	}
	w.p = out
}

func generator() secp256k1.JacobianPoint {
	return kangaroo.PublicKeyFromScalar(big.NewInt(1))
}

// AuditLot recovers the private key behind a lot's start point, proving the lot
// really lies inside the campaign it was issued for.
//
// It is the participant's check on the operator and the thief's attack, in one
// function, because they are the same computation. Publishing it is deliberate:
// a defence whose strength depends on the attacker not thinking of the attack is
// not a defence, and a participant who cannot audit the lots has to trust the
// operator on the one thing the whole protocol is supposed to remove trust from.
//
// The cost is about 2*sqrt(campaign range) group operations. Budget accordingly,
// and cancel through ctx.
func AuditLot(ctx context.Context, c *keyspace.Campaign, lot Lot) (*big.Int, error) {
	if c == nil {
		return nil, errors.New("blind: nil campaign")
	}
	pt, err := DecodePoint(lot.StartPoint)
	if err != nil {
		return nil, err
	}
	// Lots may start one block below Min and end one block above Max, because
	// the tiling is shifted; the audit interval has to allow for that or an
	// honest lot at either end would look forged.
	lo := new(big.Int).Sub(c.Base(), big.NewInt(1))
	if lo.Sign() <= 0 {
		lo = big.NewInt(1)
	}
	hi := new(big.Int).Add(c.Max, c.BlockSize())

	s, err := kangaroo.NewSolver(lo, hi, kangaroo.Params{})
	if err != nil {
		return nil, err
	}
	return s.Solve(ctx, &pt)
}
