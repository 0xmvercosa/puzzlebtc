package btc

import (
	"encoding/hex"
	"math/big"
	"testing"
)

// The compressed public key of private key 1 is the secp256k1 generator, whose
// HASH160 is a fixed, well-known constant. If this test fails, every proof the
// coordinator verifies is meaningless — so it is the anchor for the whole repo.
func TestGeneratorHash160(t *testing.T) {
	got := PubKeyHash160(big.NewInt(1))
	const want = "751e76e8199196d454941c45d1b3a323f1433bd6"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("hash160 of generator = %x, want %s", got, want)
	}
}

// Known solved puzzles, cross-checked against the public puzzle addresses.
// These pin the derivation to real data rather than to itself.
func TestKnownPuzzleKeys(t *testing.T) {
	cases := []struct {
		puzzle  int
		privHex string
		want    string
	}{
		{1, "1", "751e76e8199196d454941c45d1b3a323f1433bd6"},
		{5, "15", "8f9dff39a81ee4abcbad2ad8bafff090415a2be8"},
		{38, "22382facd0", "b190e2d40cfdeee2cee072954a2be89e7ba39364"},
	}
	for _, c := range cases {
		priv, ok := new(big.Int).SetString(c.privHex, 16)
		if !ok {
			t.Fatalf("puzzle %d: bad hex %q", c.puzzle, c.privHex)
		}
		h := PubKeyHash160(priv)
		if got := hex.EncodeToString(h[:]); got != c.want {
			t.Errorf("puzzle %d: got %s want %s", c.puzzle, got, c.want)
		}
	}
}

func TestLeadingZeroBits(t *testing.T) {
	var h Hash160
	if got := h.LeadingZeroBits(); got != 160 {
		t.Errorf("all-zero: got %d want 160", got)
	}
	h[0] = 0x80
	if got := h.LeadingZeroBits(); got != 0 {
		t.Errorf("0x80...: got %d want 0", got)
	}
	h[0] = 0x01
	if got := h.LeadingZeroBits(); got != 7 {
		t.Errorf("0x01...: got %d want 7", got)
	}
	h[0], h[1] = 0x00, 0x0f
	if got := h.LeadingZeroBits(); got != 12 {
		t.Errorf("0x000f...: got %d want 12", got)
	}
}
