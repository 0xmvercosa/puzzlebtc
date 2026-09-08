package coordinator

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeChain answers the only question a canary asks, from a fixed set.
type fakeChain struct {
	spent map[string]string // address -> txid
	err   error
}

func (f *fakeChain) Spent(_ context.Context, addr string) (bool, string, error) {
	if f.err != nil {
		return false, "", f.err
	}
	txid, ok := f.spent[addr]
	return ok, txid, nil
}

// plantAndDeal funds a canary and leases its block to workerID.
func plantAndDeal(t *testing.T, co *Coordinator, workerID string, blockIndex uint64) *CanaryPlan {
	t.Helper()
	ctx := context.Background()

	plan, err := co.PlanCanary(ctx, blockIndex)
	if err != nil {
		t.Fatal(err)
	}
	if err := co.ArmCanary(ctx, plan, 50_000, "funding-tx-"+plan.Address); err != nil {
		t.Fatal(err)
	}
	c, err := co.db.TakeArmedCanary(ctx, co.Campaign().ID, workerID, co.now(), time.Hour)
	if err != nil || c == nil {
		t.Fatalf("could not deal the canary: %v %v", c, err)
	}
	return plan
}

// The headline: a client that swept the block but never performed the rescue is
// banned, and stops receiving work.
func TestModifiedClientIsBannedBeforeItCanSteal(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	plan := plantAndDeal(t, co, "mallory", 7)

	// The chain shows nothing: the coins are still sitting there.
	audit := NewCanaryAuditor(co, &fakeChain{spent: map[string]string{}}, nil)

	co.now = func() time.Time { return time.Now().Add(2 * time.Hour) } // past the deadline
	settled, failed, err := audit.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settled != 1 || failed != 1 {
		t.Fatalf("settled=%d failed=%d, want 1/1", settled, failed)
	}

	banned, reason, err := co.db.IsBanned(ctx, "mallory")
	if err != nil {
		t.Fatal(err)
	}
	if !banned {
		t.Fatal("a client that skipped the rescue was not banned")
	}
	t.Logf("banido: %s (canario em %s)", reason, plan.Address)

	// And it can no longer get work.
	if _, err := co.LeaseBlock(ctx, "mallory"); !errors.Is(err, ErrBanned) {
		t.Fatalf("banned worker still got a block: %v", err)
	}
}

// An honest client swept the coins, so the chain shows the spend and nothing
// happens to it.
func TestHonestClientPassesTheCanary(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	plan := plantAndDeal(t, co, "alice", 11)

	audit := NewCanaryAuditor(co, &fakeChain{
		spent: map[string]string{plan.Address: "abc123"},
	}, nil)

	co.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	settled, failed, err := audit.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settled != 1 || failed != 0 {
		t.Fatalf("settled=%d failed=%d, want 1/0", settled, failed)
	}
	if banned, _, _ := co.db.IsBanned(ctx, "alice"); banned {
		t.Fatal("an honest client was banned")
	}
	if _, err := co.LeaseBlock(ctx, "alice"); err != nil {
		t.Fatalf("honest worker cannot lease: %v", err)
	}
}

// An observer that cannot answer must never produce a ban. Otherwise every
// outage of the operator's node would expel honest participants.
func TestObserverOutageDoesNotBanAnyone(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)
	plantAndDeal(t, co, "bob", 13)

	audit := NewCanaryAuditor(co, &fakeChain{err: errors.New("node unreachable")}, nil)
	co.now = func() time.Time { return time.Now().Add(2 * time.Hour) }

	settled, failed, err := audit.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settled != 0 || failed != 0 {
		t.Fatalf("settled=%d failed=%d, want 0/0 while the chain is unreadable", settled, failed)
	}
	if banned, _, _ := co.db.IsBanned(ctx, "bob"); banned {
		t.Fatal("a worker was banned on evidence nobody could read")
	}
}

// A canary still inside its deadline is not judged: a machine that is simply
// still working must not be mistaken for a modified one.
func TestCanaryInsideDeadlineIsNotJudged(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)
	plantAndDeal(t, co, "carol", 17)

	audit := NewCanaryAuditor(co, &fakeChain{spent: map[string]string{}}, nil)
	settled, failed, err := audit.RunOnce(ctx) // no clock jump
	if err != nil {
		t.Fatal(err)
	}
	if settled != 0 || failed != 0 {
		t.Fatalf("settled=%d failed=%d, want 0/0 before the deadline", settled, failed)
	}
}

// Without a chain observer nothing is settled and nobody is banned, which is
// the right behaviour for a pilot with no chain access.
func TestAuditWithoutObserverBansNobody(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)
	plantAndDeal(t, co, "dave", 19)

	audit := NewCanaryAuditor(co, nil, nil)
	co.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, _, err := audit.RunOnce(ctx); !errors.Is(err, ErrNoObserver) {
		t.Fatalf("got %v, want ErrNoObserver", err)
	}
	if banned, _, _ := co.db.IsBanned(ctx, "dave"); banned {
		t.Fatal("a worker was banned with no observer configured")
	}
}

// The planted canary must be a real key inside the block, and reproducible.
func TestCanaryPlanIsInsideTheBlockAndStable(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)

	plan, err := co.PlanCanary(ctx, 23)
	if err != nil {
		t.Fatal(err)
	}
	blk, err := co.Campaign().BlockAt(23)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := blk.KeyAt(plan.KeyOffset)
	if !ok {
		t.Fatal("canary offset falls outside its own block")
	}
	if !blk.Contains(key) {
		t.Fatal("canary key is not inside the block it was planted in")
	}
	if plan.Address == "" || plan.PrivateKey == "" {
		t.Fatal("plan is missing the address or key the operator needs to fund it")
	}

	again, err := co.PlanCanary(ctx, 23)
	if err != nil {
		t.Fatal(err)
	}
	if again.Address != plan.Address || again.KeyOffset != plan.KeyOffset {
		t.Fatal("canary plan is not reproducible; an operator who lost it could not regenerate it")
	}
}
