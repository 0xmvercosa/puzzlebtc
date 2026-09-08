// Package kangaroo recovers a private key from a public key when the key is
// known to lie inside a bounded interval.
//
// It exists for two reasons, and the second one is why it earns its place here.
//
// The first is offensive: puzzles whose public key is already exposed cannot be
// attacked by sweeping HASH160 at all — the only tractable route is this
// algorithm, which costs about 2*sqrt(W) group operations over an interval of
// width W instead of W derivations. For puzzle #140 that is the difference
// between 10^41 and 10^21.
//
// The second is defensive, and it turns the project's worst threat around. A
// participant who modifies the client to keep a prize must eventually spend the
// coins, and any transaction that spends them publishes the public key in its
// signature. From that moment the key sits inside a known interval — the
// campaign's own range — and this package recovers it in seconds. A thief who
// broadcasts through the public mempool can therefore be outbid and displaced
// before the transaction confirms.
//
// That asymmetry is the point. Keeping a stolen prize requires submitting
// privately to a miner, which requires a commercial relationship an anonymous
// defector does not have and a pool operator does. See docs/DISSUASAO.md.
//
// # Algorithm
//
// Pollard's kangaroo with distinguished points. A tame kangaroo starts at a
// known scalar in the middle of the interval; a wild kangaroo starts at the
// unknown target. Both take pseudorandom jumps whose size depends only on the
// point they are standing on, so once the two land on the same point they follow
// identical paths forever. Recording only distinguished points — those whose x
// coordinate ends in a run of zero bits — makes the collision detectable with a
// table proportional to the square root of the interval rather than to the
// interval itself.
package kangaroo

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/bits"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Params tune the search. Zero values are filled in from the interval width by
// NewSolver, which is what callers should normally use.
type Params struct {
	// JumpCount is how many distinct jump sizes the kangaroos choose between.
	// Around log2(W)/2 is the usual choice: too few and the walks correlate, too
	// many and the mean jump overshoots the interval.
	JumpCount int
	// DPBits is how many trailing zero bits in a point's x coordinate make it
	// distinguished. Larger means a smaller table and a longer tail after the
	// real collision, so it trades memory against wasted work.
	DPBits uint
	// MaxSteps bounds the search so a caller cannot hang forever on a target
	// that is not in the interval at all.
	MaxSteps uint64
}

// Solver recovers keys inside one fixed interval.
type Solver struct {
	lo, hi *big.Int
	width  *big.Int
	params Params

	jumpScalars []*big.Int
	jumpPoints  []secp256k1.JacobianPoint
}

// ErrNotFound is returned when the budget runs out. It does not prove the key is
// outside the interval, only that this run did not find it.
var ErrNotFound = errors.New("kangaroo: key not found within the step budget")

// NewSolver builds a solver for the closed interval [lo, hi].
func NewSolver(lo, hi *big.Int, p Params) (*Solver, error) {
	if lo == nil || hi == nil {
		return nil, errors.New("kangaroo: nil interval bound")
	}
	if lo.Cmp(hi) > 0 {
		return nil, fmt.Errorf("kangaroo: interval [%s, %s] is inverted", lo, hi)
	}
	width := new(big.Int).Sub(hi, lo)
	width.Add(width, big.NewInt(1))

	w := uint(width.BitLen())
	if p.JumpCount == 0 {
		p.JumpCount = int(w/2) + 1
		if p.JumpCount < 4 {
			p.JumpCount = 4
		}
	}
	if p.DPBits == 0 {
		// Aim for roughly sqrt(W)/2^DPBits stored points. Keeping DPBits near
		// w/4 keeps the table small without letting the post-collision tail
		// dominate the run.
		p.DPBits = w / 4
		if p.DPBits < 2 {
			p.DPBits = 2
		}
		if p.DPBits > 24 {
			p.DPBits = 24
		}
	}
	if p.MaxSteps == 0 {
		// The algorithm needs about 2*sqrt(W) steps; ten times that is a budget
		// that a correct run never reaches and a wrong interval always does.
		sqrt := new(big.Int).Sqrt(width)
		budget := new(big.Int).Mul(sqrt, big.NewInt(20))
		if !budget.IsUint64() {
			p.MaxSteps = ^uint64(0)
		} else {
			p.MaxSteps = budget.Uint64() + 1024
		}
	}

	s := &Solver{
		lo:     new(big.Int).Set(lo),
		hi:     new(big.Int).Set(hi),
		width:  width,
		params: p,
	}
	s.buildJumps()
	return s, nil
}

// buildJumps precomputes the jump table. Jump i moves by 2^i, so the mean jump
// is about sqrt(W) — large enough to cross the interval quickly, small enough
// that the two kangaroos cannot leap past each other.
func (s *Solver) buildJumps() {
	s.jumpScalars = make([]*big.Int, s.params.JumpCount)
	s.jumpPoints = make([]secp256k1.JacobianPoint, s.params.JumpCount)
	for i := range s.jumpScalars {
		d := new(big.Int).Lsh(big.NewInt(1), uint(i))
		s.jumpScalars[i] = d
		s.jumpPoints[i] = scalarBaseMult(d)
	}
}

// jumpIndex picks a jump from the point alone, so two kangaroos on the same
// point always make the same move. That is what makes paths merge.
func jumpIndex(x *secp256k1.FieldVal, n int) int {
	var b [32]byte
	x.PutBytes(&b)
	return int(binary.BigEndian.Uint64(b[24:]) % uint64(n))
}

// isDistinguished reports whether a point is one of the rare ones worth storing.
func isDistinguished(x *secp256k1.FieldVal, dpBits uint) bool {
	var b [32]byte
	x.PutBytes(&b)
	low := binary.BigEndian.Uint64(b[24:])
	return uint(bits.TrailingZeros64(low)) >= dpBits
}

// Solve recovers the scalar k such that k*G equals target and lo <= k <= hi.
//
// It runs the tame and wild walks in lockstep in one goroutine. Splitting them
// across cores is the obvious speedup and is deliberately left out: this
// implementation exists to prove the mechanism and to be checkable, and a
// production recovery would use a GPU walker anyway.
func (s *Solver) Solve(ctx context.Context, target *secp256k1.JacobianPoint) (*big.Int, error) {
	if target == nil {
		return nil, errors.New("kangaroo: nil target point")
	}

	// The tame kangaroo starts in the middle of the interval, where it is
	// closest to wherever the wild one is.
	mid := new(big.Int).Rsh(s.width, 1)
	mid.Add(mid, s.lo)

	tameDist := new(big.Int)
	tamePt := scalarBaseMult(mid)

	wildDist := new(big.Int)
	wildPt := *target
	toAffine(&wildPt)

	// Distinguished points seen by each herd, mapped to the distance travelled.
	tameSeen := map[[32]byte]*big.Int{}
	wildSeen := map[[32]byte]*big.Int{}

	for step := uint64(0); step < s.params.MaxSteps; step++ {
		if step&0xFFF == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		// Tame hop.
		if isDistinguished(&tamePt.X, s.params.DPBits) {
			key := pointKey(&tamePt)
			if wd, ok := wildSeen[key]; ok {
				// tame scalar == wild scalar: mid + tameDist == k + wd
				k := new(big.Int).Add(mid, tameDist)
				k.Sub(k, wd)
				if s.verify(k, target) {
					return k, nil
				}
			}
			if _, ok := tameSeen[key]; !ok {
				tameSeen[key] = new(big.Int).Set(tameDist)
			}
		}
		s.hop(&tamePt, tameDist)

		// Wild hop.
		if isDistinguished(&wildPt.X, s.params.DPBits) {
			key := pointKey(&wildPt)
			if td, ok := tameSeen[key]; ok {
				k := new(big.Int).Add(mid, td)
				k.Sub(k, wildDist)
				if s.verify(k, target) {
					return k, nil
				}
			}
			if _, ok := wildSeen[key]; !ok {
				wildSeen[key] = new(big.Int).Set(wildDist)
			}
		}
		s.hop(&wildPt, wildDist)
	}
	return nil, ErrNotFound
}

// hop advances a kangaroo one jump, updating both its point and the distance it
// has travelled.
func (s *Solver) hop(p *secp256k1.JacobianPoint, dist *big.Int) {
	i := jumpIndex(&p.X, s.params.JumpCount)
	var out secp256k1.JacobianPoint
	secp256k1.AddNonConst(p, &s.jumpPoints[i], &out)
	toAffine(&out)
	*p = out
	dist.Add(dist, s.jumpScalars[i])
}

// verify checks a candidate before returning it. A collision can be spurious —
// two different scalars reaching the same distinguished point — and returning a
// wrong key would be worse than returning none.
func (s *Solver) verify(k *big.Int, target *secp256k1.JacobianPoint) bool {
	if k.Sign() <= 0 || k.Cmp(s.lo) < 0 || k.Cmp(s.hi) > 0 {
		return false
	}
	got := scalarBaseMult(k)
	want := *target
	toAffine(&want)
	return got.X.Equals(&want.X) && got.Y.Equals(&want.Y)
}

func pointKey(p *secp256k1.JacobianPoint) [32]byte {
	var b [32]byte
	p.X.PutBytes(&b)
	return b
}

func scalarBaseMult(k *big.Int) secp256k1.JacobianPoint {
	var s secp256k1.ModNScalar
	var buf [32]byte
	k.FillBytes(buf[:])
	s.SetBytes(&buf)
	var p secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(&s, &p)
	toAffine(&p)
	return p
}

func toAffine(p *secp256k1.JacobianPoint) {
	if !p.Z.IsOne() {
		p.ToAffine()
	}
}

// PublicKeyFromScalar is a helper for callers holding a key as a big.Int.
func PublicKeyFromScalar(k *big.Int) secp256k1.JacobianPoint { return scalarBaseMult(k) }

// ExpectedSteps is the mean number of group operations a run should need over an
// interval of the given width: about 2*sqrt(W).
func ExpectedSteps(width *big.Int) *big.Int {
	return new(big.Int).Mul(new(big.Int).Sqrt(width), big.NewInt(2))
}
