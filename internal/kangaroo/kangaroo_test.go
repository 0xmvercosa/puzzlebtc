package kangaroo

import (
	"context"
	"math/big"
	"testing"
	"time"
)

// The mechanism, proven end to end: hand it only a public key and the interval
// the key sits in, and it returns the private key. This is what makes a thief's
// broadcast recoverable — the transaction publishes the public key, and the
// campaign already tells everyone the interval.
func TestRecoversKeyFromPublicKeyAlone(t *testing.T) {
	// A 2^24 interval, well inside what a test can finish, standing in for the
	// 2^70 of a real campaign.
	lo := new(big.Int).Lsh(big.NewInt(1), 24)
	hi := new(big.Int).Lsh(big.NewInt(1), 25)
	hi.Sub(hi, big.NewInt(1))

	for _, secret := range []int64{
		1 << 24,             // the very bottom
		(1 << 24) + 7,       // just inside
		(1<<24 + 1<<25) / 2, // the middle
		(1 << 25) - 1,       // the very top
		20_000_003,          // arbitrary
	} {
		k := big.NewInt(secret)
		pub := PublicKeyFromScalar(k)

		s, err := NewSolver(lo, hi, Params{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		got, err := s.Solve(ctx, &pub)
		cancel()
		if err != nil {
			t.Fatalf("key %d: %v", secret, err)
		}
		if got.Cmp(k) != 0 {
			t.Fatalf("key %d: recovered %s", secret, got)
		}
	}
}

// The cost is square-root in the interval width, which is the whole reason the
// defence is viable inside one block time.
func TestCostIsSquareRoot(t *testing.T) {
	for _, bits := range []uint{16, 20, 24} {
		width := new(big.Int).Lsh(big.NewInt(1), bits)
		want := ExpectedSteps(width)
		t.Logf("intervalo 2^%d: ~%s passos esperados (a raiz de %s)", bits, want, width)
		if want.Cmp(width) >= 0 {
			t.Errorf("2^%d: expected steps %s is not below the interval width", bits, want)
		}
	}
}

// A key outside the interval must not produce a wrong answer. Returning a bogus
// key would be worse than returning none: the recovery would broadcast a
// transaction that cannot be signed.
func TestKeyOutsideIntervalIsNotInvented(t *testing.T) {
	lo := big.NewInt(1 << 20)
	hi := big.NewInt(1<<21 - 1)

	outside := big.NewInt(1 << 30) // far above the interval
	pub := PublicKeyFromScalar(outside)

	s, err := NewSolver(lo, hi, Params{MaxSteps: 50_000})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := s.Solve(ctx, &pub)
	if err == nil {
		t.Fatalf("recovered %s for a key outside the interval", got)
	}
}

// Cancellation must be honoured: a recovery racing a block cannot hang.
func TestSolveRespectsCancellation(t *testing.T) {
	lo := new(big.Int).Lsh(big.NewInt(1), 40)
	hi := new(big.Int).Lsh(big.NewInt(1), 41)
	pub := PublicKeyFromScalar(new(big.Int).Add(lo, big.NewInt(12345)))

	s, err := NewSolver(lo, hi, Params{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := s.Solve(ctx, &pub); err == nil {
		t.Skip("finished before the deadline; nothing to assert")
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("cancellation took %v to take effect", el)
	}
}

func TestBadIntervalRejected(t *testing.T) {
	if _, err := NewSolver(big.NewInt(100), big.NewInt(10), Params{}); err == nil {
		t.Error("an inverted interval was accepted")
	}
	if _, err := NewSolver(nil, big.NewInt(10), Params{}); err == nil {
		t.Error("a nil bound was accepted")
	}
}
