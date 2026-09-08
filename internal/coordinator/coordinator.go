// Package coordinator ties the pieces together: it hands out random blocks that
// nobody has taken, verifies the proof that came back, and mints the ticket that
// entitles a worker to a slice of the prize.
package coordinator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	mrand "math/rand"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/payout"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// leaseAttempts bounds the random-index retry loop. Each attempt is one indexed
// INSERT, and for any campaign with meaningful headroom the first one succeeds;
// exhausting the budget is the signal that the keyspace is effectively used up.
const leaseAttempts = 32

// Config holds the operator's choices for one coordinator process.
type Config struct {
	// LeaseTTL is how long a worker has to return a block before it goes back
	// into circulation. Set it to a few times the expected sweep duration:
	// too short and slow workers lose finished work, too long and a crashed
	// worker parks a block for hours.
	LeaseTTL time.Duration
	// DeepAuditRate is the fraction of submissions verified in full rather than
	// sampled, in [0,1]. Deep audits cost one curve operation per witness.
	DeepAuditRate float64
	// Split is the prize distribution policy.
	Split payout.Split
	// CanarySecret seeds canary placement and witness sampling. Losing it
	// invalidates every outstanding lease; leaking it lets a worker fake the
	// canary test.
	CanarySecret []byte
}

// DefaultConfig returns sane starting values. CanarySecret must still be set.
func DefaultConfig() Config {
	return Config{
		LeaseTTL:      2 * time.Hour,
		DeepAuditRate: 0.02,
		Split:         payout.DefaultSplit(),
	}
}

// Coordinator serves one campaign.
type Coordinator struct {
	cfg      Config
	db       *store.DB
	campaign *keyspace.Campaign
	verifier *proof.Verifier
	rng      *mrand.Rand
	now      func() time.Time
}

// New builds a coordinator and persists the campaign if it is new.
func New(ctx context.Context, db *store.DB, c *keyspace.Campaign, p proof.Params, cfg Config) (*Coordinator, error) {
	if db == nil || c == nil {
		return nil, errors.New("coordinator: nil db or campaign")
	}
	if err := cfg.Split.Validate(); err != nil {
		return nil, err
	}
	if cfg.LeaseTTL <= 0 {
		return nil, errors.New("coordinator: lease_ttl must be positive")
	}
	if cfg.DeepAuditRate < 0 || cfg.DeepAuditRate > 1 {
		return nil, errors.New("coordinator: deep_audit_rate must be in [0,1]")
	}
	v, err := proof.NewVerifier(c, p, cfg.CanarySecret)
	if err != nil {
		return nil, err
	}
	paramsJSON, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("coordinator: encode params: %w", err)
	}
	if err := db.PutCampaign(ctx, store.CampaignRow{
		ID:            c.ID,
		PuzzleNum:     c.PuzzleNum,
		TargetHash160: c.TargetHash160,
		MinKeyHex:     c.Min.Text(16),
		MaxKeyHex:     c.Max.Text(16),
		BlockBits:     c.BlockBits,
		ParamsJSON:    string(paramsJSON),
	}); err != nil {
		return nil, err
	}

	// Seeded from the OS: block selection must not be predictable, or a worker
	// could pre-compute the block it is about to be handed.
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, fmt.Errorf("coordinator: seed rng: %w", err)
	}
	return &Coordinator{
		cfg:      cfg,
		db:       db,
		campaign: c,
		verifier: v,
		rng:      mrand.New(mrand.NewSource(int64(bigEndianU64(seed[:])))), //nolint:gosec // block choice, not a secret
		now:      time.Now,
	}, nil
}

func bigEndianU64(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

// Campaign exposes the campaign being served.
func (co *Coordinator) Campaign() *keyspace.Campaign { return co.campaign }

// Lease is what a worker receives when it asks for work.
type Lease struct {
	CampaignID string       `json:"campaign_id"`
	BlockIndex uint64       `json:"block_index"`
	LoKeyHex   string       `json:"lo_key_hex"`
	HiKeyHex   string       `json:"hi_key_hex"`
	Length     string       `json:"length"`
	Token      string       `json:"lease_token"`
	ExpiresAt  int64        `json:"expires_at"`
	Params     proof.Params `json:"proof_params"`
	// Watchlist is every HASH160 the worker must report a match on: the real
	// target plus this block's canaries, shuffled.
	Watchlist []string `json:"watchlist"`
}

// LeaseBlock hands a worker a uniformly random block that nobody holds.
//
// Randomness is the point, not an implementation detail: sequential handout
// would let a worker predict its next block and pre-compute it, and would make
// the pool's progress trivially observable to an outside competitor.
func (co *Coordinator) LeaseBlock(ctx context.Context, workerID string) (*Lease, error) {
	if workerID == "" {
		return nil, errors.New("coordinator: worker id is empty")
	}
	now := co.now()

	// Expired leases return to the pool before we look for a free index.
	if _, err := co.db.ReclaimExpired(ctx, co.campaign.ID, now); err != nil {
		return nil, err
	}

	total := co.campaign.NumBlocksU64()
	if total == 0 {
		return nil, store.ErrNoBlockAvailable
	}

	for attempt := 0; attempt < leaseAttempts; attempt++ {
		index := uint64(co.rng.Int63n(int64(total))) //nolint:gosec // uniform over the campaign
		token, err := newToken()
		if err != nil {
			return nil, err
		}

		taken, err := co.db.TryLease(ctx, co.campaign.ID, index, workerID, token, now, co.cfg.LeaseTTL)
		if err != nil {
			return nil, err
		}
		if taken {
			continue // already leased or already swept; roll again
		}

		blk, err := co.campaign.BlockAt(index)
		if err != nil {
			return nil, err
		}
		watchlist, err := co.verifier.Watchlist(blk)
		if err != nil {
			return nil, err
		}
		return &Lease{
			CampaignID: co.campaign.ID,
			BlockIndex: index,
			LoKeyHex:   blk.Lo.Text(16),
			HiKeyHex:   blk.Hi.Text(16),
			Length:     blk.Len.String(),
			Token:      token,
			ExpiresAt:  now.Add(co.cfg.LeaseTTL).Unix(),
			Params:     co.verifier.Params(),
			Watchlist:  watchlist,
		}, nil
	}
	return nil, fmt.Errorf("%w after %d attempts: the campaign keyspace is effectively exhausted",
		store.ErrNoBlockAvailable, leaseAttempts)
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("coordinator: generate lease token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Receipt is the coordinator's answer to an accepted submission.
type Receipt struct {
	BlockIndex uint64 `json:"block_index"`
	Witnesses  uint64 `json:"witnesses"`
	Verified   int    `json:"witnesses_verified"`
	DeepAudit  bool   `json:"deep_audit"`
	TicketID   string `json:"ticket_id"`
	// Solved is true when this submission carried the winning key.
	Solved bool `json:"solved"`
}

// Submit verifies a worker's proof and, if it holds, mints the ticket.
//
// A rejected proof leaves the lease in place: the worker may have a fixable bug
// and can resubmit until the lease expires. Nothing is recorded against it here
// — reputation and stake slashing belong to a later layer, and hanging a
// permanent penalty on an unreviewed heuristic would punish honest bugs.
func (co *Coordinator) Submit(ctx context.Context, workerID string, sub proof.Submission) (*Receipt, error) {
	if workerID == "" {
		return nil, errors.New("coordinator: worker id is empty")
	}
	if sub.CampaignID != "" && sub.CampaignID != co.campaign.ID {
		return nil, fmt.Errorf("coordinator: submission is for campaign %q, this is %q", sub.CampaignID, co.campaign.ID)
	}
	blk, err := co.campaign.BlockAt(sub.BlockIndex)
	if err != nil {
		return nil, err
	}

	deep := co.rng.Float64() < co.cfg.DeepAuditRate
	res, err := co.verifier.Verify(blk, sub, deep)
	if err != nil {
		return nil, err // *proof.Rejection for a failed proof
	}

	now := co.now()
	if err := co.db.CompleteBlock(ctx, co.campaign.ID, sub.BlockIndex, workerID, sub.LeaseToken, res.Witnesses, now); err != nil {
		return nil, err
	}

	receipt := &Receipt{
		BlockIndex: sub.BlockIndex,
		Witnesses:  res.Witnesses,
		Verified:   res.Verified,
		DeepAudit:  deep,
		TicketID:   fmt.Sprintf("%s/%d", co.campaign.ID, sub.BlockIndex),
	}

	if res.TargetKey != nil {
		if err := co.db.RecordSolution(ctx, co.campaign.ID, sub.BlockIndex, workerID, res.TargetKey.Text(16), now); err != nil {
			return nil, err
		}
		receipt.Solved = true
	}
	return receipt, nil
}

// Progress reports how much of the campaign is done.
type Progress struct {
	CampaignID      string  `json:"campaign_id"`
	PuzzleNum       int     `json:"puzzle_num"`
	TotalBlocks     string  `json:"total_blocks"`
	CompletedBlocks int64   `json:"completed_blocks"`
	LeasedBlocks    int64   `json:"leased_blocks"`
	Tickets         int64   `json:"tickets"`
	Workers         int64   `json:"workers"`
	FractionSwept   float64 `json:"fraction_swept"`
}

// Progress summarizes the campaign. FractionSwept will be indistinguishable
// from zero for any unsolved puzzle; that is the honest number, not a bug.
func (co *Coordinator) Progress(ctx context.Context) (*Progress, error) {
	s, err := co.db.Stats(ctx, co.campaign.ID)
	if err != nil {
		return nil, err
	}
	total := co.campaign.NumBlocks()
	frac, _ := new(big.Float).Quo(
		new(big.Float).SetInt64(s.Completed),
		new(big.Float).SetInt(total),
	).Float64()

	return &Progress{
		CampaignID:      co.campaign.ID,
		PuzzleNum:       co.campaign.PuzzleNum,
		TotalBlocks:     total.String(),
		CompletedBlocks: s.Completed,
		LeasedBlocks:    s.Leased,
		Tickets:         s.Tickets,
		Workers:         s.Workers,
		FractionSwept:   frac,
	}, nil
}

// Distribution computes what each participant would receive for a prize of
// prizeSat, using the tickets recorded so far. Run it before paying anything —
// and check Distribution.Total() against the prize.
func (co *Coordinator) Distribution(ctx context.Context, prizeSat *big.Int, finderID string) (*payout.Distribution, error) {
	holders, err := co.db.TicketHolders(ctx, co.campaign.ID)
	if err != nil {
		return nil, err
	}
	ph := make([]payout.Holder, len(holders))
	for i, h := range holders {
		ph[i] = payout.Holder{WorkerID: h.WorkerID, Tickets: h.Tickets}
	}
	return payout.Compute(prizeSat, co.cfg.Split, finderID, ph)
}
