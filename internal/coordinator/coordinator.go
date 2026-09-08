// Package coordinator ties the pieces together: it hands out random blocks that
// nobody has taken, verifies the proof that came back, and mints the ticket that
// entitles a worker to a slice of the prize.
package coordinator

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	mrand "math/rand/v2"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/blind"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/payout"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// leaseAttempts bounds the random-index retry loop. Each attempt is one indexed
// INSERT, and for any campaign with meaningful headroom the first one succeeds;
// exhausting the budget is the signal that the keyspace is effectively used up.
const leaseAttempts = 32

// claimSkipAttempts bounds how many times allocation rerolls to avoid ground a
// third party claims to have searched. Rerolling costs one in-memory lookup, and
// the expected number of tries is 1/(1-f) for a claimed fraction f — under 10
// even at 90% claimed. Past this budget the unclaimed ground is either exhausted
// or too sparse to find by sampling, and allocation falls through to claimed
// blocks rather than refusing to hand out work.
const claimSkipAttempts = 64

// MaxLeaseBatch bounds how many blocks one request may lease.
//
// Batching exists because the block is sized for a CPU, not for a GPU. A block
// that takes a four-core CPU about 48 minutes takes a high-end card 1.4 seconds,
// so a card would otherwise open 62,000 connections a day and ten thousand cards
// would mean seven thousand lease requests per second. At fifty blocks a request
// that falls to a hundred and forty, which is an ordinary web service.
//
// Keeping the block small and batching the requests is what lets one block size
// serve both, without making a ticket mean different amounts of work for
// different people.
const MaxLeaseBatch = 200

// Config holds the operator's choices for one coordinator process.
type Config struct {
	// LeaseTTL is how long a worker has to return a block before it goes back
	// into circulation. Set it to a few times the expected sweep duration:
	// too short and slow workers lose finished work, too long and a crashed
	// worker parks a block for hours.
	LeaseTTL time.Duration

	// AttestKey signs the commitments in internal/attest: the record of who was
	// handed which ground, published before anyone could have found anything.
	// Nil disables attestation, and a campaign running without it has no way to
	// name whoever sweeps a prize outside the protocol.
	AttestKey ed25519.PrivateKey

	// Blind hands lots out as curve points instead of key ranges, so a worker
	// never holds the private keys it is sweeping and a modified client cannot
	// simply keep the prize it stumbles on. It costs the worker nothing and the
	// coordinator one scalar multiplication per lease. The campaign must have
	// been built with a secret shift (see internal/blind) or the blinding is
	// worthless: without it a worker recovers its lot start from the point in
	// milliseconds.
	Blind bool
	// DeepAuditRate is the fraction of submissions verified in full rather than
	// sampled, in [0,1]. Deep audits cost one curve operation per witness.
	DeepAuditRate float64
	// Split is the prize distribution policy.
	Split payout.Split
	// CanaryEvery is how often each participant is given a funded claim canary.
	//
	// The cadence is per participant and per unit of time, deliberately not per
	// block. A high-end card closes a block every 1.4 seconds and a laptop takes
	// most of an hour, so a per-block probability would test the card hundreds of
	// times a day while the laptop went a month untested — paying a fortune to
	// over-test exactly the machines least likely to be someone's only one.
	//
	// Shorter means a modified client is caught sooner and costs more in fees.
	//
	// It is OFF by default, and that is a decision rather than an oversight. The
	// mechanism requires the operator to front working capital and to keep paying
	// transaction fees, and it only meaningfully protects against participants
	// large enough for the attack to be worth building — who are few, and better
	// handled by knowing who they are. See docs/CLIENTE_MODIFICADO.md.
	//
	// Zero disables it.
	CanaryEvery time.Duration
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
		CanaryEvery:   0, // off; see the field comment
		Split:         payout.DefaultSplit(),
	}
}

// Coordinator serves one campaign.
type Coordinator struct {
	cfg      Config
	db       *store.DB
	campaign *keyspace.Campaign
	verifier *proof.Verifier
	claims   *keyspace.ClaimSet
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
	if cfg.CanaryEvery < 0 {
		return nil, errors.New("coordinator: canary_every must not be negative")
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
	// Third-party claims only reorder allocation, so a failure to read them is not
	// fatal to the campaign — but it silently changes behaviour, so it is an error.
	stored, err := db.ExternalClaims(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	brs := make([]keyspace.BlockRange, 0, len(stored))
	for _, cl := range stored {
		brs = append(brs, keyspace.BlockRange{Lo: cl.Lo, Hi: cl.Hi})
	}
	claims := keyspace.NewClaimSet(brs)

	return &Coordinator{
		cfg:      cfg,
		db:       db,
		campaign: c,
		verifier: v,
		claims:   claims,
		now:      time.Now,
	}, nil
}

// Campaign exposes the campaign being served.
func (co *Coordinator) Campaign() *keyspace.Campaign { return co.campaign }

// Lease is what a worker receives when it asks for work.
//
// On a blinded campaign three of these fields are absent, and their absence is
// the security property: BlockIndex, LoKeyHex and HiKeyHex are all ways of
// naming where the lot sits, and a worker that knows where its lot sits can
// derive the keys in it. Lot carries the curve point instead. See
// internal/blind.
type Lease struct {
	CampaignID string `json:"campaign_id"`
	// BlockIndex is nil on a blinded campaign. Knowing it would reduce the
	// discrete log that protects the lot to a search over the tiling shift
	// alone — about 2^17 operations instead of 2^36.
	BlockIndex *uint64 `json:"block_index,omitempty"`
	// LoKeyHex and HiKeyHex are empty on a blinded campaign.
	LoKeyHex string `json:"lo_key_hex,omitempty"`
	HiKeyHex string `json:"hi_key_hex,omitempty"`
	// Lot is set only on a blinded campaign: the lot's start point and length,
	// with no key in it.
	Lot       *blind.Lot   `json:"lot,omitempty"`
	Length    string       `json:"length"`
	Token     string       `json:"lease_token"`
	ExpiresAt int64        `json:"expires_at"`
	Params    proof.Params `json:"proof_params"`
	// Watchlist is every HASH160 the worker must report a match on: the real
	// target plus this block's canaries, shuffled.
	Watchlist []string `json:"watchlist"`
	// Tier is "fresh" for ground nobody claims to have searched, or "reclaim"
	// for a block inside a third-party claim. Reclaim blocks are handed out only
	// once fresh ground runs out. Workers do not need to treat them differently;
	// it is shown so a participant can see what they are sweeping.
	Tier string `json:"tier"`
}

// Allocation tiers.
const (
	TierFresh   = "fresh"
	TierReclaim = "reclaim"
)

// LeaseBatch hands a worker up to n random blocks in one call.
//
// Blocks are drawn independently, so a batch is not contiguous — a participant
// works scattered ground, which is what keeps allocation unpredictable. Partial
// success is normal and not an error: the caller gets what was available, and an
// empty result means the campaign is exhausted.
func (co *Coordinator) LeaseBatch(ctx context.Context, workerID string, n int) ([]*Lease, error) {
	switch {
	case workerID == "":
		return nil, errors.New("coordinator: worker id is empty")
	case n <= 0:
		return nil, fmt.Errorf("coordinator: batch size %d must be positive", n)
	case n > MaxLeaseBatch:
		return nil, fmt.Errorf("coordinator: batch size %d exceeds the limit of %d", n, MaxLeaseBatch)
	}

	// One reclaim for the whole batch rather than one per block.
	if _, err := co.db.ReclaimExpired(ctx, co.campaign.ID, co.now()); err != nil {
		return nil, err
	}

	out := make([]*Lease, 0, n)
	for i := 0; i < n; i++ {
		lease, err := co.leaseOne(ctx, workerID)
		if errors.Is(err, store.ErrNoBlockAvailable) {
			break // hand back what we have; the caller decides whether to retry
		}
		if err != nil {
			if len(out) > 0 {
				// Blocks already leased are real work the worker can do. Losing
				// them to an error on a later draw would strand them until the
				// lease expires.
				return out, nil
			}
			return nil, err
		}
		out = append(out, lease)
	}
	if len(out) == 0 {
		return nil, store.ErrNoBlockAvailable
	}
	return out, nil
}

// ErrBanned is returned when a worker excluded from the pool asks for work.
var ErrBanned = errors.New("coordinator: worker is excluded from this pool")

// LeaseBlock hands a worker a uniformly random block that nobody holds.
//
// Randomness is the point, not an implementation detail: sequential handout
// would let a worker predict its next block and pre-compute it, and would make
// the pool's progress trivially observable to an outside competitor.
func (co *Coordinator) LeaseBlock(ctx context.Context, workerID string) (*Lease, error) {
	if workerID == "" {
		return nil, errors.New("coordinator: worker id is empty")
	}
	// Expired leases return to the pool before we look for a free index.
	if _, err := co.db.ReclaimExpired(ctx, co.campaign.ID, co.now()); err != nil {
		return nil, err
	}
	return co.leaseOne(ctx, workerID)
}

// leaseOne draws and records a single block. It does not reclaim expired leases;
// callers do that once per request so a batch pays for it only once.
func (co *Coordinator) leaseOne(ctx context.Context, workerID string) (*Lease, error) {
	// A banned worker gets no blocks. It is free to keep searching the keyspace
	// on its own, which is exactly the intended outcome: it loses the pool's
	// coordination — the guarantee that no ground is repeated — and goes back to
	// competing against the whole space alone.
	if banned, reason, err := co.db.IsBanned(ctx, workerID); err != nil {
		return nil, err
	} else if banned {
		return nil, fmt.Errorf("%w: %s", ErrBanned, reason)
	}

	now := co.now()
	total := co.campaign.NumBlocksU64()
	if total == 0 {
		return nil, store.ErrNoBlockAvailable
	}

	// A funded canary block, when one is dealt, replaces the random draw.
	if idx, ok := co.tryDealCanary(ctx, workerID); ok {
		if lease, err := co.recordLease(ctx, workerID, idx, TierFresh, now); err == nil {
			return lease, nil
		}
		// The canary block was already taken; fall through to a normal draw.
	}

	for attempt := 0; attempt < leaseAttempts; attempt++ {
		index, tier := co.pickIndex(total)
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
		return co.buildLease(ctx, workerID, index, token, tier, now)
	}
	return nil, fmt.Errorf("%w after %d attempts: the campaign keyspace is effectively exhausted",
		store.ErrNoBlockAvailable, leaseAttempts)
}

// pickIndex draws a block, preferring ground no third party claims to have
// searched.
//
// The preference is an ordering, never an exclusion. If the claims are honest,
// sweeping unclaimed ground first raises the odds per block by 1/(1-f), because
// the key cannot be sitting where someone already looked. If the claims are
// dishonest, nothing is lost: those blocks still go out, just after the fresh
// ground. Excluding them instead would trade a permanent risk of skipping the
// key for a temporary saving in duplicated work, which is the wrong side of that
// trade for a search that will never exhaust its space anyway.
func (co *Coordinator) pickIndex(total uint64) (uint64, string) {
	draw := func() uint64 {
		return mrand.Uint64N(total)
	}
	if co.claims.Blocks() == 0 {
		return draw(), TierFresh
	}
	for i := 0; i < claimSkipAttempts; i++ {
		if index := draw(); !co.claims.Contains(index) {
			return index, TierFresh
		}
	}
	// Fresh ground is exhausted or too sparse to hit by sampling. Fall through
	// rather than refuse work.
	return draw(), TierReclaim
}

// recordLease claims a specific index for a worker and builds its lease.
func (co *Coordinator) recordLease(ctx context.Context, workerID string, index uint64, tier string, now time.Time) (*Lease, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	taken, err := co.db.TryLease(ctx, co.campaign.ID, index, workerID, token, now, co.cfg.LeaseTTL)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, store.ErrNoBlockAvailable
	}
	return co.buildLease(ctx, workerID, index, token, tier, now)
}

// buildLease assembles the lease a worker receives for an already-claimed index.
func (co *Coordinator) buildLease(ctx context.Context, workerID string, index uint64, token, tier string, now time.Time) (*Lease, error) {
	blk, err := co.campaign.BlockAt(index)
	if err != nil {
		return nil, err
	}
	// Record who was handed this ground, before they have it. The lease log is
	// append-only and is what internal/attest commits to; a record written after
	// a theft would prove nothing, so it has to be written now and it has to
	// fail the lease if it cannot be.
	expires := now.Add(co.cfg.LeaseTTL)
	if err := co.db.LogLease(ctx, co.campaign.ID, index, workerID, now, expires); err != nil {
		return nil, err
	}
	watchlist, err := co.verifier.Watchlist(blk)
	if err != nil {
		return nil, err
	}
	lease := &Lease{
		CampaignID: co.campaign.ID,
		Length:     blk.Len.String(),
		Token:      token,
		ExpiresAt:  expires.Unix(),
		Params:     co.verifier.Params(),
		Watchlist:  watchlist,
		Tier:       tier,
	}
	if co.cfg.Blind {
		lot, err := blind.LotFor(blk)
		if err != nil {
			return nil, err
		}
		lease.Lot = &lot
		return lease, nil
	}
	idx := index
	lease.BlockIndex = &idx
	lease.LoKeyHex = blk.Lo.Text(16)
	lease.HiKeyHex = blk.Hi.Text(16)
	return lease, nil
}

// mrandFloat is a package-level draw, safe for concurrent use.
func mrandFloat() float64 { return mrand.Float64() }

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("coordinator: generate lease token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Receipt is the coordinator's answer to an accepted submission.
type Receipt struct {
	// BlockIndex is nil on a blinded campaign, for the same reason it is absent
	// from the lease.
	BlockIndex *uint64 `json:"block_index,omitempty"`
	Witnesses  uint64  `json:"witnesses"`
	Verified   int     `json:"witnesses_verified"`
	DeepAudit  bool    `json:"deep_audit"`
	// TicketID names the block, so on a blinded campaign it is an opaque handle
	// the worker quotes back rather than a coordinate it can use.
	TicketID string `json:"ticket_id"`
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
	index := sub.BlockIndex
	var err error
	if co.cfg.Blind {
		// A blinded worker was never told its index, so it quotes the lease
		// token. The token is a bearer credential for exactly one block: it has
		// to resolve, and it has to resolve to this worker, or a participant
		// could claim a ticket for somebody else's work.
		var holder string
		index, holder, err = co.db.BlockByToken(ctx, co.campaign.ID, sub.LeaseToken)
		if err != nil {
			return nil, err
		}
		if holder != workerID {
			return nil, fmt.Errorf("coordinator: lease token belongs to another worker")
		}
		// The token is the binding on a blinded campaign, so the index it
		// resolves to is authoritative. Stamping it here keeps the verifier's
		// block-agreement check meaningful on the unblinded path, where the
		// worker does quote an index and quoting the wrong one is a real bug.
		sub.BlockIndex = index
	}
	blk, err := co.campaign.BlockAt(index)
	if err != nil {
		return nil, err
	}

	deep := mrand.Float64() < co.cfg.DeepAuditRate
	res, err := co.verifier.Verify(blk, sub, deep)
	if err != nil {
		return nil, err // *proof.Rejection for a failed proof
	}

	now := co.now()
	// The ticket's weight is the block's true length. Blocks are uniform, so this
	// only differs for the truncated final block of a campaign — which is exactly
	// the case that a plain row count would over-pay.
	if !blk.Len.IsUint64() {
		return nil, fmt.Errorf("coordinator: block %d has %s keys, too many to weight a ticket", blk.Index, blk.Len)
	}
	if err := co.db.CompleteBlock(ctx, co.campaign.ID, index, workerID, sub.LeaseToken, res.Witnesses, blk.Len.Uint64(), now); err != nil {
		return nil, err
	}

	receipt := &Receipt{
		Witnesses: res.Witnesses,
		Verified:  res.Verified,
		DeepAudit: deep,
		TicketID:  co.ticketID(index),
	}
	if !co.cfg.Blind {
		idx := index
		receipt.BlockIndex = &idx
	}

	if res.TargetKey != nil {
		if err := co.db.RecordSolution(ctx, co.campaign.ID, index, workerID, res.TargetKey.Text(16), now); err != nil {
			return nil, err
		}
		receipt.Solved = true
	}
	return receipt, nil
}

// ticketID names a settled block in a form the worker can quote back.
//
// On a blinded campaign it is a keyed digest of the index rather than the index
// itself, and that is not cosmetic. A worker that learns its block index knows
// its lot starts at Base + index*BlockSize, which reduces the discrete log
// protecting the lot from the width of the whole campaign to the width of the
// tiling shift — for puzzle #71, from about 2^36 operations to about 2^17. The
// index would have leaked here, in a display string, after being kept out of the
// lease and out of the receipt.
//
// The coordinator's own tables are keyed by index, so nothing is lost operator-side.
func (co *Coordinator) ticketID(index uint64) string {
	if !co.cfg.Blind {
		return fmt.Sprintf("%s/%d", co.campaign.ID, index)
	}
	m := hmac.New(sha256.New, co.cfg.CanarySecret)
	fmt.Fprintf(m, "puzzlebtc/ticket/v1\x00%s\x00%d", co.campaign.ID, index)
	return co.campaign.ID + "/" + hex.EncodeToString(m.Sum(nil)[:8])
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
	// ClaimedByOthers is the share of the campaign a third party says it already
	// searched. Unverified, and swept anyway once fresh ground runs out.
	ClaimedByOthers float64 `json:"claimed_by_others"`
	// OddsPerBlock is the current chance that the next block handed out holds
	// the key, given everything proven swept so far came back empty. It rises as
	// the campaign progresses, because blocks are never reissued.
	OddsPerBlock float64 `json:"odds_per_block"`
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

	// Sampling without replacement: with the key uniform over N blocks and k
	// proven empty, the next block holds it with probability 1/(N-k). This is
	// what the no-reissue rule buys — a searcher that reshuffles instead is stuck
	// at 1/N forever.
	remaining := new(big.Int).Sub(total, big.NewInt(s.Completed))
	odds := 0.0
	if remaining.Sign() > 0 {
		odds, _ = new(big.Float).Quo(
			big.NewFloat(1),
			new(big.Float).SetInt(remaining),
		).Float64()
	}

	return &Progress{
		CampaignID:      co.campaign.ID,
		PuzzleNum:       co.campaign.PuzzleNum,
		TotalBlocks:     total.String(),
		CompletedBlocks: s.Completed,
		LeasedBlocks:    s.Leased,
		Tickets:         s.Tickets,
		Workers:         s.Workers,
		FractionSwept:   frac,
		ClaimedByOthers: co.claims.Fraction(total),
		OddsPerBlock:    odds,
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
		ph[i] = payout.Holder{WorkerID: h.WorkerID, Tickets: h.Weight}
	}
	return payout.Compute(prizeSat, co.cfg.Split, finderID, ph)
}
