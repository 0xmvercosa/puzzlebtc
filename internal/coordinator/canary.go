package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// ChainObserver answers the one question a claim canary asks: did the money
// move?
//
// It is an interface because the coordinator must not care how the answer is
// obtained — an operator's own node, a block explorer, or a hand-run check
// during a pilot. What matters is that the answer comes from the chain, which
// no client can forge, rather than from anything the client says about itself.
type ChainObserver interface {
	// Spent reports whether address has been spent from, and the txid if so.
	Spent(ctx context.Context, address string) (spent bool, txid string, err error)
}

// CanaryAuditor settles dealt canaries once their deadline passes, and bans the
// workers whose clients did not perform the rescue.
type CanaryAuditor struct {
	co  *Coordinator
	obs ChainObserver
	log *slog.Logger
}

// NewCanaryAuditor wires an auditor. A nil observer disables auditing, which is
// the correct behaviour for a pilot with no chain access: canaries are still
// planted and dealt, they simply are not settled, and nobody is banned on
// evidence nobody looked at.
func NewCanaryAuditor(co *Coordinator, obs ChainObserver, log *slog.Logger) *CanaryAuditor {
	if log == nil {
		log = slog.Default()
	}
	return &CanaryAuditor{co: co, obs: obs, log: log}
}

// ErrNoObserver is returned when auditing runs without a chain observer.
var ErrNoObserver = errors.New("coordinator: no chain observer configured; canaries cannot be settled")

// RunOnce settles every canary whose deadline has passed. It returns how many
// were settled and how many failed.
//
// A failure bans the worker. That is a heavy action taken on automated evidence,
// so it is deliberately narrow: the ban says only that this identity held a
// block containing spendable coins, was told to sweep them, and did not. Honest
// software does that every time. It is also the reason the deadline is generous
// — a machine that was simply switched off mid-block must not be mistaken for a
// modified one, which is why the deadline is measured from the lease and set far
// beyond how long the sweep itself takes.
func (a *CanaryAuditor) RunOnce(ctx context.Context) (settled, failed int, err error) {
	if a.obs == nil {
		return 0, 0, ErrNoObserver
	}
	due, err := a.co.db.CanariesDue(ctx, a.co.now())
	if err != nil {
		return 0, 0, err
	}

	for _, c := range due {
		spent, txid, err := a.obs.Spent(ctx, c.Address)
		if err != nil {
			// An observer that cannot answer must not be read as a failure: that
			// would ban honest workers whenever the operator's node is down.
			a.log.Error("canary check failed; leaving it open", "block", c.BlockIndex, "address", c.Address, "err", err)
			continue
		}

		if err := a.co.db.SettleCanary(ctx, a.co.campaign.ID, c.BlockIndex, spent, txid); err != nil {
			return settled, failed, err
		}
		settled++

		if spent {
			a.log.Info("canary claimed", "worker", c.WorkerID, "block", c.BlockIndex, "txid", txid)
			continue
		}

		failed++
		evidence := fmt.Sprintf("canary at block %d (address %s, %d sat) was not swept by the deadline",
			c.BlockIndex, c.Address, c.FundedSat)
		if err := a.co.db.Ban(ctx, c.WorkerID, "claim_canary_missed", evidence); err != nil {
			return settled, failed, err
		}
		a.log.Warn("worker banned: rescue path did not run",
			"worker", c.WorkerID, "block", c.BlockIndex, "address", c.Address)
	}
	return settled, failed, nil
}

// Run audits on a ticker until ctx is cancelled.
func (a *CanaryAuditor) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			settled, failed, err := a.RunOnce(ctx)
			if err != nil && !errors.Is(err, ErrNoObserver) {
				a.log.Error("canary audit", "err", err)
			}
			if settled > 0 {
				a.log.Info("canary audit", "settled", settled, "failed", failed)
			}
		}
	}
}

// CanaryPlan describes a canary the operator must fund before it can be used.
//
// The coordinator produces the plan; funding happens outside, from the
// operator's own wallet. The private key is derived here and returned to the
// operator once, and is never written to the database: a canary's key can move
// its coins, and a coordinator that stored them would put every canary one
// breach away.
type CanaryPlan struct {
	BlockIndex uint64 `json:"block_index"`
	KeyOffset  uint64 `json:"key_offset"`
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
	Hash160    string `json:"hash160"`
}

// PlanCanary derives a fresh canary for a block that has never been leased.
//
// The offset is derived from the campaign secret, so the same block always
// yields the same canary and an operator who loses the plan can regenerate it.
func (co *Coordinator) PlanCanary(ctx context.Context, blockIndex uint64) (*CanaryPlan, error) {
	blk, err := co.campaign.BlockAt(blockIndex)
	if err != nil {
		return nil, err
	}
	offsets := co.verifier.CanaryOffsets(blk)
	if len(offsets) == 0 {
		return nil, errors.New("coordinator: campaign params plant no canaries")
	}
	// The last reporting canary doubles as the claim canary, so a client cannot
	// tell the two apart from the watchlist alone.
	off := offsets[len(offsets)-1]

	key, ok := blk.KeyAt(off)
	if !ok {
		return nil, fmt.Errorf("coordinator: canary offset %d outside block %d", off, blockIndex)
	}
	h := btc.PubKeyHash160(key)
	return &CanaryPlan{
		BlockIndex: blockIndex,
		KeyOffset:  off,
		PrivateKey: fmt.Sprintf("%064x", key),
		Address:    btc.Hash160ToAddress(h),
		Hash160:    fmt.Sprintf("%x", h),
	}, nil
}

// ArmCanary records a canary the operator has funded on chain.
func (co *Coordinator) ArmCanary(ctx context.Context, p *CanaryPlan, fundedSat uint64, fundingTxid string) error {
	return co.db.ArmCanary(ctx, co.campaign.ID, p.BlockIndex, p.KeyOffset, fundedSat, p.Address, fundingTxid)
}

// canaryLeaseWindow is how long after leasing a canary block the rescue must
// have appeared on chain. It is deliberately many times the sweep itself: a
// laptop closed mid-block, a lease left to expire, or a slow miner must never
// be mistaken for a modified client.
const canaryLeaseWindow = 24 * time.Hour

// tryDealCanary offers a canary block instead of a random one, at the campaign's
// configured rate. It returns 0, false when no canary is dealt.
func (co *Coordinator) tryDealCanary(ctx context.Context, workerID string) (uint64, bool) {
	if co.cfg.CanaryRate <= 0 {
		return 0, false
	}
	if mrandFloat() >= co.cfg.CanaryRate {
		return 0, false
	}
	c, err := co.db.TakeArmedCanary(ctx, co.campaign.ID, workerID, co.now(), canaryLeaseWindow)
	if err != nil || c == nil {
		return 0, false
	}
	return c.BlockIndex, true
}

var _ = store.CanaryArmed // keep the state constants referenced from this file's docs
