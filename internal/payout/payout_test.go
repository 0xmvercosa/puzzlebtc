package payout

import (
	"math/big"
	"testing"
)

// The invariant that matters most: not one satoshi is created or lost, for any
// prize and any ticket distribution.
func TestDistributionIsExhaustive(t *testing.T) {
	prizes := []int64{0, 1, 7, 100, 99999, 660000000, 7_000_000_000_000}
	ticketSets := [][]Holder{
		{{"alice", 1}},
		{{"alice", 1}, {"bob", 1}},
		{{"alice", 1}, {"bob", 2}, {"carol", 3}},
		{{"alice", 7}, {"bob", 11}, {"carol", 13}, {"dave", 17}},
		{{"alice", 1}, {"bob", 999999}},
	}
	for _, p := range prizes {
		for _, hs := range ticketSets {
			prize := big.NewInt(p)
			d, err := Compute(prize, DefaultSplit(), "alice", hs)
			if err != nil {
				t.Fatalf("prize=%d: %v", p, err)
			}
			if got := d.Total(); got.Cmp(prize) != 0 {
				t.Errorf("prize=%d holders=%v: parts total %s, want %s", p, hs, got, prize)
			}
			for _, a := range d.Helpers {
				if a.Satoshis.Sign() < 0 {
					t.Errorf("negative award for %s", a.WorkerID)
				}
			}
		}
	}
}

func TestDefaultPercentages(t *testing.T) {
	prize := big.NewInt(1_000_000_000) // 10 BTC
	d, err := Compute(prize, DefaultSplit(), "finder", []Holder{
		{"finder", 1}, {"helper1", 1}, {"helper2", 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.FinderSat.Cmp(big.NewInt(500_000_000)) != 0 {
		t.Errorf("finder got %s, want 500000000", d.FinderSat)
	}
	if d.PlatformSat.Cmp(big.NewInt(300_000_000)) != 0 {
		t.Errorf("platform got %s, want 300000000", d.PlatformSat)
	}
	if d.HelperSat.Cmp(big.NewInt(200_000_000)) != 0 {
		t.Errorf("helper pool %s, want 200000000", d.HelperSat)
	}
	// 4 tickets total: finder 1/4, helper1 1/4, helper2 2/4.
	want := map[string]int64{"finder": 50_000_000, "helper1": 50_000_000, "helper2": 100_000_000}
	for _, a := range d.Helpers {
		if a.Satoshis.Cmp(big.NewInt(want[a.WorkerID])) != 0 {
			t.Errorf("%s got %s, want %d", a.WorkerID, a.Satoshis, want[a.WorkerID])
		}
	}
}

// A prize that does not divide evenly must still be fully distributed, with the
// leftover going to the largest fractional entitlements — deterministically.
func TestLeftoverIsDeterministic(t *testing.T) {
	prize := big.NewInt(1001)
	holders := []Holder{{"a", 1}, {"b", 1}, {"c", 1}}
	first, err := Compute(prize, DefaultSplit(), "a", holders)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := Compute(prize, DefaultSplit(), "a", holders)
		if err != nil {
			t.Fatal(err)
		}
		for j := range first.Helpers {
			if first.Helpers[j].WorkerID != again.Helpers[j].WorkerID ||
				first.Helpers[j].Satoshis.Cmp(again.Helpers[j].Satoshis) != 0 {
				t.Fatal("payout is not deterministic across runs")
			}
		}
	}
	if got := first.Total(); got.Cmp(prize) != 0 {
		t.Errorf("total %s, want %s", got, prize)
	}
}

// With no other participants the helper pool must not be stranded.
func TestSoloFinderTakesHelperPool(t *testing.T) {
	prize := big.NewInt(1_000_000)
	s := DefaultSplit()
	s.FinderKeepsTickets = false
	d, err := Compute(prize, s, "solo", []Holder{{"solo", 5}})
	if err != nil {
		t.Fatal(err)
	}
	if d.FinderSat.Cmp(big.NewInt(700_000)) != 0 {
		t.Errorf("solo finder got %s, want 700000 (50%% + the unclaimed 20%%)", d.FinderSat)
	}
	if got := d.Total(); got.Cmp(prize) != 0 {
		t.Errorf("total %s, want %s", got, prize)
	}
}

func TestBadSplitRejected(t *testing.T) {
	if _, err := Compute(big.NewInt(1), Split{FinderPct: 50, PlatformPct: 30, HelperPct: 30}, "x", nil); err == nil {
		t.Fatal("a split totalling 110% was accepted")
	}
	if _, err := Compute(big.NewInt(-1), DefaultSplit(), "x", nil); err == nil {
		t.Fatal("a negative prize was accepted")
	}
}
