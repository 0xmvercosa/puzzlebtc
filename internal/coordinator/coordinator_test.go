package coordinator

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

const (
	testBlockBits   = 12
	testWitnessBits = 4
)

func testHarness(t *testing.T) *Coordinator { return testHarnessAt(t, ":memory:") }

func testHarnessAt(t *testing.T, path string) *Coordinator {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	min, max, err := keyspace.PuzzleRange(24)
	if err != nil {
		t.Fatal(err)
	}
	// Plant the solution at offset 100 of block 0 so the jackpot path is real.
	target := btc.PubKeyHash160(new(big.Int).Add(min, big.NewInt(100)))

	c, err := keyspace.NewCampaign("t24", 24, hex.EncodeToString(target[:]), min, max, testBlockBits)
	if err != nil {
		t.Fatal(err)
	}
	p := proof.DefaultParams(testBlockBits)
	p.WitnessBits = testWitnessBits
	p.Buckets = 16
	p.Canaries = 3
	p.SampleSize = 32

	cfg := DefaultConfig()
	cfg.CanarySecret = []byte("coordinator-test-secret-32-bytes-min!!")
	cfg.DeepAuditRate = 0 // deterministic tests; deep audits are exercised in internal/proof

	co, err := New(ctx, db, c, p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return co
}

// sweep runs the reference worker against a lease.
func sweep(t *testing.T, co *Coordinator, l *Lease) proof.Submission {
	t.Helper()
	sw, err := proof.NewSweeper(l.Params, co.Campaign().TargetHash160, l.Watchlist)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := co.Campaign().BlockAt(leaseIndex(l))
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sw.Sweep(context.Background(), blk)
	if err != nil {
		t.Fatal(err)
	}
	sub.CampaignID = l.CampaignID
	sub.LeaseToken = l.Token
	return sub
}

// The whole loop: lease, sweep, submit, get a ticket, see progress move.
func TestLeaseSweepSubmitAwardsTicket(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	lease, err := co.LeaseBlock(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(lease.Watchlist) != 4 { // target + 3 canaries
		t.Errorf("watchlist has %d entries, want 4", len(lease.Watchlist))
	}

	receipt, err := co.Submit(ctx, "alice", sweep(t, co, lease))
	if err != nil {
		t.Fatalf("honest submission rejected: %v", err)
	}
	t.Logf("block %s accepted: %d witnesses, %d verified", receipt.TicketID, receipt.Witnesses, receipt.Verified)

	prog, err := co.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prog.CompletedBlocks != 1 || prog.Tickets != 1 || prog.Workers != 1 {
		t.Errorf("progress = %+v, want 1 completed / 1 ticket / 1 worker", prog)
	}
}

// The requirement: a block that has been swept must never be handed out again.
func TestBlocksAreNeverReissued(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	seen := map[uint64]bool{}
	for i := 0; i < 50; i++ {
		lease, err := co.LeaseBlock(ctx, "worker")
		if err != nil {
			t.Fatal(err)
		}
		if seen[leaseIndex(lease)] {
			t.Fatalf("block %d was leased twice while still outstanding", leaseIndex(lease))
		}
		seen[leaseIndex(lease)] = true
	}
}

// Allocation must be random, not sequential: a predictable next block lets a
// worker pre-compute it, and leaks the pool's position to outsiders.
func TestAllocationIsRandom(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	var indices []uint64
	for i := 0; i < 24; i++ {
		l, err := co.LeaseBlock(ctx, "w")
		if err != nil {
			t.Fatal(err)
		}
		indices = append(indices, leaseIndex(l))
	}
	sequential := true
	for i := 1; i < len(indices); i++ {
		if indices[i] != indices[i-1]+1 {
			sequential = false
			break
		}
	}
	if sequential {
		t.Fatal("blocks were handed out in sequence; allocation is not random")
	}
}

// A worker that fakes the sweep gets no ticket, and the block is not consumed.
func TestCheaterGetsNoTicket(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	lease, err := co.LeaseBlock(ctx, "mallory")
	if err != nil {
		t.Fatal(err)
	}
	blk, err := co.Campaign().BlockAt(leaseIndex(lease))
	if err != nil {
		t.Fatal(err)
	}

	// Well-spread, plausible-looking, entirely invented.
	fake := proof.Submission{CampaignID: lease.CampaignID, BlockIndex: leaseIndex(lease), LeaseToken: lease.Token}
	for off := uint64(0); off < blk.Len.Uint64(); off += 12 {
		fake.Witnesses = append(fake.Witnesses, off)
	}

	if _, err := co.Submit(ctx, "mallory", fake); err == nil {
		t.Fatal("a fabricated submission was accepted")
	} else {
		t.Logf("rejected as expected: %v", err)
	}

	prog, err := co.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prog.Tickets != 0 {
		t.Errorf("cheater earned %d tickets", prog.Tickets)
	}
	if prog.CompletedBlocks != 0 {
		t.Errorf("a rejected block was marked complete")
	}
}

// A stale worker whose lease expired and was reissued must not be paid for it.
func TestExpiredLeaseCannotBeRedeemed(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	lease, err := co.LeaseBlock(ctx, "slowpoke")
	if err != nil {
		t.Fatal(err)
	}
	sub := sweep(t, co, lease)

	// Jump past the lease deadline; the next allocation reclaims it.
	co.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	if _, err := co.LeaseBlock(ctx, "someone-else"); err != nil {
		t.Fatal(err)
	}

	_, err = co.Submit(ctx, "slowpoke", sub)
	if !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("expired lease redeemed: got %v, want ErrLeaseInvalid", err)
	}
}

// A worker cannot claim a block leased to someone else.
func TestForeignLeaseRejected(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	lease, err := co.LeaseBlock(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	sub := sweep(t, co, lease)

	if _, err := co.Submit(ctx, "bob", sub); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("bob redeemed alice's lease: got %v", err)
	}
}

// Submitting the same verified block twice must not mint a second ticket.
func TestDoubleSubmitAwardsOneTicket(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	lease, err := co.LeaseBlock(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	sub := sweep(t, co, lease)

	if _, err := co.Submit(ctx, "alice", sub); err != nil {
		t.Fatal(err)
	}
	if _, err := co.Submit(ctx, "alice", sub); !errors.Is(err, store.ErrLeaseInvalid) {
		t.Fatalf("second submit of the same block: got %v, want ErrLeaseInvalid", err)
	}

	prog, err := co.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prog.Tickets != 1 {
		t.Errorf("tickets = %d, want 1", prog.Tickets)
	}
}

// The jackpot: block 0 holds the planted key, so sweeping it must record a
// solution and produce a payout that sums to exactly the prize.
func TestSolutionRecordedAndPaidOut(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	// Give a couple of other workers tickets so the helper pool has claimants.
	for _, w := range []string{"helper1", "helper2"} {
		l, err := co.LeaseBlock(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := co.Submit(ctx, w, sweep(t, co, l)); err != nil {
			t.Fatal(err)
		}
	}

	// Lease block 0 directly — the one holding the planted target.
	blk, err := co.Campaign().BlockAt(0)
	if err != nil {
		t.Fatal(err)
	}
	wl, err := co.verifier.Watchlist(blk)
	if err != nil {
		t.Fatal(err)
	}
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	taken, err := co.db.TryLease(ctx, co.Campaign().ID, 0, "finder", token, time.Now(), time.Hour)
	if err != nil || taken {
		t.Fatalf("could not lease block 0: taken=%v err=%v", taken, err)
	}
	lease := &Lease{CampaignID: co.Campaign().ID, BlockIndex: new(uint64), Token: token, Params: co.verifier.Params(), Watchlist: wl}

	receipt, err := co.Submit(ctx, "finder", sweep(t, co, lease))
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Solved {
		t.Fatal("the block containing the target was not reported as solved")
	}

	prize := big.NewInt(6_600_000_000) // 66 BTC in satoshis
	d, err := co.Distribution(ctx, prize, "finder")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Total(); got.Cmp(prize) != 0 {
		t.Fatalf("payout totals %s, want exactly %s", got, prize)
	}
	if d.PlatformSat.Cmp(big.NewInt(1_980_000_000)) != 0 {
		t.Errorf("platform got %s, want 30%% = 1980000000", d.PlatformSat)
	}
	t.Logf("finder=%s platform=%s helper_pool=%s across %d holders",
		d.FinderSat, d.PlatformSat, d.HelperSat, len(d.Helpers))
}

// A batch must hand out distinct blocks, all of them leased to the asking
// worker, in one round trip.
func TestLeaseBatchReturnsDistinctBlocks(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	leases, err := co.LeaseBatch(ctx, "rig", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 40 {
		t.Fatalf("got %d leases, want 40", len(leases))
	}
	seen := map[uint64]bool{}
	tokens := map[string]bool{}
	for _, l := range leases {
		if seen[leaseIndex(l)] {
			t.Errorf("block %d appears twice in one batch", leaseIndex(l))
		}
		seen[leaseIndex(l)] = true
		if tokens[l.Token] {
			t.Errorf("lease token reused across blocks")
		}
		tokens[l.Token] = true
	}

	// A second batch must not overlap the first.
	more, err := co.LeaseBatch(ctx, "rig", 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range more {
		if seen[leaseIndex(l)] {
			t.Errorf("block %d handed out in two batches", leaseIndex(l))
		}
	}
}

func TestLeaseBatchRejectsBadSizes(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	if _, err := co.LeaseBatch(ctx, "w", 0); err == nil {
		t.Error("a zero-size batch was accepted")
	}
	if _, err := co.LeaseBatch(ctx, "w", MaxLeaseBatch+1); err == nil {
		t.Error("an oversized batch was accepted")
	}
}

// Every block in a batch must be independently redeemable, with its own proof.
func TestBatchedBlocksAreIndividuallyRedeemable(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	leases, err := co.LeaseBatch(ctx, "rig", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range leases {
		if _, err := co.Submit(ctx, "rig", sweep(t, co, l)); err != nil {
			t.Fatalf("block %d: %v", leaseIndex(l), err)
		}
	}
	prog, err := co.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prog.CompletedBlocks != 3 || prog.Tickets != 3 {
		t.Errorf("progress = %d blocks / %d tickets, want 3/3", prog.CompletedBlocks, prog.Tickets)
	}
}

// leaseIndex reads the block index a lease carries. It is deliberately absent on
// a blinded campaign — see Lease — so a nil here means a test built for the
// unblinded path is running against a blinded coordinator.
func leaseIndex(l *Lease) uint64 {
	if l.BlockIndex == nil {
		panic("lease carries no block index: this test needs an unblinded campaign")
	}
	return *l.BlockIndex
}
