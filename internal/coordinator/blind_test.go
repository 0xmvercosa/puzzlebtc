package coordinator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/blind"
	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

const blindSecret = "coordinator-blind-test-secret-not-a-real-one"

// blindHarness is testHarness with the campaign shifted and the lease blinded,
// which is the configuration a real campaign runs in.
func blindHarness(t *testing.T) (*Coordinator, uint64) {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	min, max, err := keyspace.PuzzleRange(24)
	if err != nil {
		t.Fatal(err)
	}
	shift, err := blind.DeriveShift([]byte(blindSecret), "t24b", testBlockBits)
	if err != nil {
		t.Fatal(err)
	}
	// Plant the solution somewhere inside the range so the jackpot path is real.
	solution := new(big.Int).Add(min, big.NewInt(4321))
	target := btc.PubKeyHash160(solution)

	c, err := keyspace.NewShiftedCampaign("t24b", 24, hex.EncodeToString(target[:]), min, max, testBlockBits, shift)
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
	cfg.DeepAuditRate = 0
	cfg.Blind = true

	co, err := New(ctx, db, c, p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	idx, ok := c.BlockIndexOf(solution)
	if !ok {
		t.Fatal("planted solution fell outside the campaign")
	}
	return co, idx
}

// The full loop with nothing but points on the wire: lease, walk, submit, ticket.
func TestBlindLeaseSweepSubmit(t *testing.T) {
	ctx := context.Background()
	co, _ := blindHarness(t)

	lease, err := co.LeaseBlock(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if lease.Lot == nil {
		t.Fatal("a blinded coordinator handed out a lease with no lot")
	}

	sw, err := proof.NewBlindSweeper(lease.Params, lease.Watchlist)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sw.SweepLot(ctx, *lease.Lot)
	if err != nil {
		t.Fatal(err)
	}
	sub.CampaignID = lease.CampaignID
	sub.LeaseToken = lease.Token

	receipt, err := co.Submit(ctx, "alice", sub)
	if err != nil {
		t.Fatalf("honest blind submission rejected: %v", err)
	}
	if receipt.BlockIndex != nil {
		t.Error("the receipt leaked the block index back to a blinded worker")
	}

	prog, err := co.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prog.CompletedBlocks != 1 || prog.Tickets != 1 {
		t.Errorf("progress = %+v, want 1 completed / 1 ticket", prog)
	}
}

// The property the whole design rests on: nothing a worker receives locates its
// lot. Not the keys, not the block index, not the campaign geometry combined
// with either. This is asserted on the serialized lease, because that is what a
// participant actually sees.
func TestBlindLeaseCarriesNoLocation(t *testing.T) {
	ctx := context.Background()
	co, _ := blindHarness(t)

	lease, err := co.LeaseBlock(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	wire := strings.ToLower(string(raw))

	if strings.Contains(wire, "block_index") {
		t.Errorf("the lease names the block index: %s", wire)
	}
	if strings.Contains(wire, "lo_key") || strings.Contains(wire, "hi_key") {
		t.Errorf("the lease carries key bounds: %s", wire)
	}

	// And the values themselves, in either encoding, must not appear anywhere.
	c := co.Campaign()
	for idx := uint64(0); idx < 8; idx++ {
		blk, err := c.BlockAt(idx)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []*big.Int{blk.Lo, blk.Hi} {
			for _, form := range []string{v.Text(16), v.String()} {
				if strings.Contains(wire, strings.ToLower(form)) {
					t.Fatalf("a key bound (%s) appears in the lease: %s", form, wire)
				}
			}
		}
	}
}

// A blinded worker submits against a lease token, so the token has to be checked
// against the worker presenting it. Otherwise anyone who saw a token could claim
// the ticket for work somebody else did.
func TestBlindSubmitRejectsAnotherWorkersToken(t *testing.T) {
	ctx := context.Background()
	co, _ := blindHarness(t)

	lease, err := co.LeaseBlock(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	sw, err := proof.NewBlindSweeper(lease.Params, lease.Watchlist)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sw.SweepLot(ctx, *lease.Lot)
	if err != nil {
		t.Fatal(err)
	}
	sub.CampaignID = lease.CampaignID
	sub.LeaseToken = lease.Token

	if _, err := co.Submit(ctx, "mallory", sub); err == nil {
		t.Fatal("a submission under someone else's lease token was accepted")
	}

	sub.LeaseToken = "not-a-token"
	if _, err := co.Submit(ctx, "alice", sub); err == nil {
		t.Fatal("a submission with an unknown lease token was accepted")
	}
}

// The find must still land, and it must land on the coordinator: the worker
// reports an offset, the coordinator turns it into the key. This is the moment
// the design exists for, so it is exercised end to end on the block that really
// contains the planted solution.
func TestBlindFindIsRecoveredByTheCoordinatorOnly(t *testing.T) {
	ctx := context.Background()
	co, solvedIndex := blindHarness(t)

	lease, err := co.recordLease(ctx, "alice", solvedIndex, TierFresh, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sw, err := proof.NewBlindSweeper(lease.Params, lease.Watchlist)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := sw.SweepLot(ctx, *lease.Lot)
	if err != nil {
		t.Fatal(err)
	}
	sub.CampaignID = lease.CampaignID
	sub.LeaseToken = lease.Token

	// The blind sweeper does not single out the jackpot: it cannot tell the
	// target apart from a canary without being told, and it is not told.
	if sub.FoundOffset != nil {
		t.Fatal("a blinded worker claimed to recognize the jackpot")
	}

	receipt, err := co.Submit(ctx, "alice", sub)
	if err != nil {
		t.Fatalf("submission carrying the prize was rejected: %v", err)
	}
	if !receipt.Solved {
		t.Fatal("the coordinator did not recover the key from the reported offset")
	}

	sol, err := co.db.GetSolution(ctx, co.Campaign().ID)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := new(big.Int).SetString(sol.PrivateKey, 16)
	if !ok {
		t.Fatalf("stored solution %q is not hex", sol.PrivateKey)
	}
	h := btc.PubKeyHash160(key)
	if hex.EncodeToString(h[:]) != co.Campaign().TargetHash160 {
		t.Fatalf("stored key hashes to %x, not the campaign target", h)
	}
}

// The ticket id goes back to the worker, so it must not spell out the block
// index either. An index in a display string would be exactly as damaging as an
// index in the lease: it collapses the discrete log that protects the lot from
// the width of the campaign to the width of the tiling shift.
func TestBlindTicketIDHidesTheIndex(t *testing.T) {
	ctx := context.Background()
	co, _ := blindHarness(t)

	for i := 0; i < 8; i++ {
		lease, err := co.LeaseBlock(ctx, "alice")
		if err != nil {
			t.Fatal(err)
		}
		sw, err := proof.NewBlindSweeper(lease.Params, lease.Watchlist)
		if err != nil {
			t.Fatal(err)
		}
		sub, err := sw.SweepLot(ctx, *lease.Lot)
		if err != nil {
			t.Fatal(err)
		}
		sub.CampaignID, sub.LeaseToken = lease.CampaignID, lease.Token

		receipt, err := co.Submit(ctx, "alice", sub)
		if err != nil {
			t.Fatal(err)
		}
		handle := strings.TrimPrefix(receipt.TicketID, co.Campaign().ID+"/")
		if _, err := strconv.ParseUint(handle, 10, 64); err == nil {
			t.Fatalf("ticket id %q is the block index in decimal", receipt.TicketID)
		}
		if len(handle) != 16 {
			t.Fatalf("ticket handle %q is not the expected keyed digest", handle)
		}
	}
}

// The same block must always mint the same ticket id, or an operator cannot
// reconcile a ticket with the work it paid for.
func TestBlindTicketIDIsStable(t *testing.T) {
	co, _ := blindHarness(t)
	a, b := co.ticketID(4242), co.ticketID(4242)
	if a != b {
		t.Fatalf("same block gave %q then %q", a, b)
	}
	if c := co.ticketID(4243); a == c {
		t.Fatalf("two blocks share a ticket id: %q", a)
	}
}
