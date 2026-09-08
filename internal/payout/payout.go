// Package payout splits a prize across the finder, the platform and everyone
// who swept a verified block.
//
// Every amount is in satoshis and every operation is exact integer arithmetic.
// Floating point never touches money here: a float64 cannot represent all
// satoshi values above 2^53, and a rounding drift of one satoshi per payee is
// the kind of bug that destroys trust in a pool permanently.
//
// The split is exhaustive by construction — Finder + Platform + sum(Helpers)
// equals the prize exactly, with the remainder from integer division handed out
// by largest remainder rather than dropped.
package payout

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

// Split is the pool's revenue policy, in percent. The three must total 100.
type Split struct {
	FinderPct   int64 `json:"finder_pct"`
	PlatformPct int64 `json:"platform_pct"`
	HelperPct   int64 `json:"helper_pct"`

	// FinderKeepsTickets decides whether the finder's own tickets also earn a
	// share of the helper pool. True is the fairer reading — the finder swept
	// blocks like everyone else — and is the default.
	FinderKeepsTickets bool `json:"finder_keeps_tickets"`
}

// DefaultSplit is 50% finder / 30% platform / 20% helpers.
func DefaultSplit() Split {
	return Split{FinderPct: 50, PlatformPct: 30, HelperPct: 20, FinderKeepsTickets: true}
}

// Validate rejects a policy that would not distribute exactly the prize.
func (s Split) Validate() error {
	if s.FinderPct < 0 || s.PlatformPct < 0 || s.HelperPct < 0 {
		return errors.New("payout: percentages must not be negative")
	}
	if total := s.FinderPct + s.PlatformPct + s.HelperPct; total != 100 {
		return fmt.Errorf("payout: percentages total %d, must total 100", total)
	}
	return nil
}

// Holder is one participant's verified contribution.
type Holder struct {
	WorkerID string `json:"worker_id"`
	Tickets  int64  `json:"tickets"`
}

// Award is one participant's payout.
type Award struct {
	WorkerID string   `json:"worker_id"`
	Tickets  int64    `json:"tickets"`
	Satoshis *big.Int `json:"satoshis"`
}

// Distribution is the full, exact breakdown of a prize.
type Distribution struct {
	PrizeSat    *big.Int `json:"prize_sat"`
	FinderID    string   `json:"finder_id"`
	FinderSat   *big.Int `json:"finder_sat"`
	PlatformSat *big.Int `json:"platform_sat"`
	HelperSat   *big.Int `json:"helper_sat"`
	Helpers     []Award  `json:"helpers"`
}

// Compute divides prizeSat according to s.
//
// holders should be every worker with at least one verified block, the finder
// included. If nobody but the finder holds tickets — or the policy excludes the
// finder and no one else qualifies — the helper pool folds into the finder's
// share rather than being stranded.
func Compute(prizeSat *big.Int, s Split, finderID string, holders []Holder) (*Distribution, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if prizeSat == nil || prizeSat.Sign() < 0 {
		return nil, errors.New("payout: prize must be non-negative")
	}
	if finderID == "" {
		return nil, errors.New("payout: finder id is empty")
	}

	hundred := big.NewInt(100)
	platform := new(big.Int).Mul(prizeSat, big.NewInt(s.PlatformPct))
	platform.Div(platform, hundred)

	finder := new(big.Int).Mul(prizeSat, big.NewInt(s.FinderPct))
	finder.Div(finder, hundred)

	// The helper pool absorbs the division remainder, so the three parts always
	// re-add to exactly prizeSat.
	helperPool := new(big.Int).Sub(prizeSat, platform)
	helperPool.Sub(helperPool, finder)

	eligible := make([]Holder, 0, len(holders))
	var totalTickets int64
	for _, h := range holders {
		if h.Tickets <= 0 {
			continue
		}
		if !s.FinderKeepsTickets && h.WorkerID == finderID {
			continue
		}
		eligible = append(eligible, h)
		totalTickets += h.Tickets
	}

	d := &Distribution{
		PrizeSat:    new(big.Int).Set(prizeSat),
		FinderID:    finderID,
		FinderSat:   finder,
		PlatformSat: platform,
		HelperSat:   new(big.Int).Set(helperPool),
	}

	if totalTickets == 0 {
		// Nobody to share with: the finder takes the pool.
		d.FinderSat = new(big.Int).Add(d.FinderSat, helperPool)
		d.HelperSat = big.NewInt(0)
		return d, nil
	}

	d.Helpers = largestRemainder(helperPool, eligible, totalTickets)
	return d, nil
}

// largestRemainder distributes pool pro rata by tickets, giving the leftover
// satoshis to the holders with the largest fractional entitlement. Ties break on
// worker ID so the result is identical on every node that computes it.
func largestRemainder(pool *big.Int, holders []Holder, totalTickets int64) []Award {
	total := big.NewInt(totalTickets)

	type share struct {
		Award
		remainder *big.Int
	}
	shares := make([]share, len(holders))
	distributed := new(big.Int)

	for i, h := range holders {
		num := new(big.Int).Mul(pool, big.NewInt(h.Tickets))
		q, r := new(big.Int).QuoRem(num, total, new(big.Int))
		shares[i] = share{
			Award:     Award{WorkerID: h.WorkerID, Tickets: h.Tickets, Satoshis: q},
			remainder: r,
		}
		distributed.Add(distributed, q)
	}

	leftover := new(big.Int).Sub(pool, distributed)

	// Rank by remainder descending, then by worker ID ascending.
	order := make([]int, len(shares))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if c := shares[ia].remainder.Cmp(shares[ib].remainder); c != 0 {
			return c > 0
		}
		return shares[ia].WorkerID < shares[ib].WorkerID
	})

	one := big.NewInt(1)
	for _, idx := range order {
		if leftover.Sign() <= 0 {
			break
		}
		shares[idx].Satoshis.Add(shares[idx].Satoshis, one)
		leftover.Sub(leftover, one)
	}

	out := make([]Award, len(shares))
	for i, sh := range shares {
		out[i] = sh.Award
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].WorkerID < out[b].WorkerID })
	return out
}

// Total re-adds every part of d. Callers should assert it equals PrizeSat before
// paying anyone; a mismatch means a bug in this package, not in the caller.
func (d *Distribution) Total() *big.Int {
	sum := new(big.Int).Add(d.FinderSat, d.PlatformSat)
	for _, h := range d.Helpers {
		sum.Add(sum, h.Satoshis)
	}
	return sum
}
