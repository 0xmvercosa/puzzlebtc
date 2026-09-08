package coordinator

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/attest"
	"github.com/0xmvercosa/puzzlebtc/internal/blind"
	"github.com/0xmvercosa/puzzlebtc/internal/btc"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// attestHarness is the blind harness with a signing key, which is how a real
// campaign runs.
func attestHarness(t *testing.T) (*Coordinator, ed25519.PublicKey, *big.Int) {
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
	shift, err := blind.DeriveShift([]byte(blindSecret), "t24a", testBlockBits)
	if err != nil {
		t.Fatal(err)
	}
	solution := new(big.Int).Add(min, big.NewInt(4321))
	target := btc.PubKeyHash160(solution)

	c, err := keyspace.NewShiftedCampaign("t24a", 24, hex.EncodeToString(target[:]), min, max, testBlockBits, shift)
	if err != nil {
		t.Fatal(err)
	}
	p := proof.DefaultParams(testBlockBits)
	p.WitnessBits, p.Buckets, p.Canaries, p.SampleSize = testWitnessBits, 16, 3, 32

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.CanarySecret = []byte("coordinator-test-secret-32-bytes-min!!")
	cfg.DeepAuditRate = 0
	cfg.Blind = true
	cfg.AttestKey = priv

	co, err := New(ctx, db, c, p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return co, pub, solution
}

// The scenario the mechanism exists for, start to finish.
//
// A participant leases ground. The coordinator publishes a root committing to
// that fact — before anything is found. Later a key surfaces on chain, swept by
// somebody who never reported it. Anyone holding only the campaign's public key
// can now check who was standing on that ground, against a commitment that
// predates the sweep.
func TestAKeyOnChainNamesWhoWasHoldingItsGround(t *testing.T) {
	ctx := context.Background()
	co, pub, solution := attestHarness(t)

	index, ok := co.Campaign().BlockIndexOf(solution)
	if !ok {
		t.Fatal("the planted key is outside the campaign")
	}

	// Noise: other participants holding other ground.
	for _, id := range []string{"bob", "carol", "dave"} {
		if _, err := co.LeaseBlock(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := co.recordLease(ctx, "mallory", index, TierFresh, time.Now()); err != nil {
		t.Fatal(err)
	}

	// The commitment, made before the key exists anywhere but in this test.
	root, err := co.PublishRoot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if root.Leaves != 4 {
		t.Fatalf("the root commits to %d leases, want 4", root.Leaves)
	}

	att, err := co.Attribute(ctx, solution)
	if err != nil {
		t.Fatal(err)
	}
	if !att.IsTarget {
		t.Fatal("the campaign target was not recognized as the target")
	}
	if len(att.Holders) != 1 {
		t.Fatalf("%d holders for that block, want 1", len(att.Holders))
	}
	if got := att.Holders[0].Receipt.WorkerID; got != "mallory" {
		t.Fatalf("the ground was attributed to %q, want mallory", got)
	}

	// The check a third party runs, holding nothing but the published key.
	if err := attest.Verify(pub, &att.Holders[0]); err != nil {
		t.Fatalf("the attribution did not verify against the campaign key: %v", err)
	}
}

// A key that is not the campaign target must not name anybody as a thief. The
// mechanism reports what it knows and refuses to overstate it.
func TestAttributeDoesNotCallAnUnrelatedKeyTheTarget(t *testing.T) {
	ctx := context.Background()
	co, _, solution := attestHarness(t)

	if _, err := co.LeaseBlock(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	other := new(big.Int).Add(solution, big.NewInt(1))
	index, ok := co.Campaign().BlockIndexOf(other)
	if !ok {
		t.Fatal("the neighbouring key is outside the campaign")
	}
	if _, err := co.recordLease(ctx, "erin", index, TierFresh, time.Now()); err != nil {
		t.Fatal(err)
	}

	att, err := co.Attribute(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if att.IsTarget {
		t.Fatal("a key that is not the target was reported as the target")
	}
}

// Ground nobody was ever handed cannot be attributed, and saying so is the
// point: the record only speaks about leases that were actually issued.
func TestAttributeRefusesGroundNobodyHeld(t *testing.T) {
	ctx := context.Background()
	co, _, solution := attestHarness(t)

	if _, err := co.LeaseBlock(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := co.Attribute(ctx, solution); err == nil {
		t.Fatal("a block that was never leased was attributed to somebody")
	}
	outside := new(big.Int).Lsh(big.NewInt(1), 200)
	if _, err := co.Attribute(ctx, outside); err == nil {
		t.Fatal("a key outside the campaign was attributed")
	}
}

// A lease that expires and is reissued leaves two holders on one block, and both
// have to survive in the record: overwriting the first would erase exactly the
// participant an expired-then-reissued block makes most interesting.
func TestReissuedBlockKeepsBothHolders(t *testing.T) {
	ctx := context.Background()
	co, pub, solution := attestHarness(t)

	index, ok := co.Campaign().BlockIndexOf(solution)
	if !ok {
		t.Fatal("the planted key is outside the campaign")
	}
	t0 := time.Unix(1789000000, 0)
	if _, err := co.recordLease(ctx, "first", index, TierFresh, t0); err != nil {
		t.Fatal(err)
	}
	// Expire it and hand the same ground to somebody else.
	if _, err := co.db.ReclaimExpired(ctx, co.Campaign().ID, t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := co.recordLease(ctx, "second", index, TierFresh, t0.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}

	att, err := co.Attribute(ctx, solution)
	if err != nil {
		t.Fatal(err)
	}
	if len(att.Holders) != 2 {
		t.Fatalf("%d holders recorded, want 2", len(att.Holders))
	}
	seen := map[string]int64{}
	for i := range att.Holders {
		if err := attest.Verify(pub, &att.Holders[i]); err != nil {
			t.Fatalf("holder %d did not verify: %v", i, err)
		}
		seen[att.Holders[i].Receipt.WorkerID] = att.Holders[i].Receipt.IssuedAt
	}
	if len(seen) != 2 || seen["first"] == 0 || seen["second"] == 0 {
		t.Fatalf("holders = %v, want both first and second with their own times", seen)
	}
	if seen["first"] >= seen["second"] {
		t.Fatalf("the two leases carry times %v that do not order them", seen)
	}
}

// Without a key the coordinator must say so rather than pretend.
func TestAttestationOffIsAnError(t *testing.T) {
	ctx := context.Background()
	co := testHarness(t)
	if _, err := co.PublishRoot(ctx); err == nil {
		t.Fatal("a root was published with no signing key")
	}
	if _, err := co.Attribute(ctx, big.NewInt(1)); err == nil {
		t.Fatal("an attribution was produced with no signing key")
	}
	if _, err := co.AttestingKey(); err == nil {
		t.Fatal("a public key was returned with no signing key")
	}
}
