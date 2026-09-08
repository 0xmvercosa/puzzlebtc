package attest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func keypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func receipts(n int) []Receipt {
	out := make([]Receipt, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Receipt{
			CampaignID: "puzzle-71",
			BlockIndex: uint64(1000 + i*7),
			WorkerID:   string(rune('a'+i%26)) + "-worker",
			IssuedAt:   1789000000 + int64(i),
			ExpiresAt:  1789007200 + int64(i),
		})
	}
	return out
}

// The whole point: open one lease against a published root and have a third
// party confirm it, holding nothing but the public key.
func TestOpenAndVerify(t *testing.T) {
	pub, priv := keypair(t)
	rs := receipts(37)

	o, err := Open(priv, "puzzle-71", rs, 1789000000, 1000+13*7)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(pub, o); err != nil {
		t.Fatalf("an honest opening did not verify: %v", err)
	}
	if o.Receipt.BlockIndex != 1000+13*7 {
		t.Fatalf("opened block %d, want %d", o.Receipt.BlockIndex, 1000+13*7)
	}
}

// Tree sizes hit the duplicated-last-node path differently, so every small size
// gets opened at every position.
func TestEveryLeafOpensAtEverySize(t *testing.T) {
	pub, priv := keypair(t)
	for n := 1; n <= 33; n++ {
		rs := sortReceipts(receipts(n))
		for i := range rs {
			o, err := Open(priv, "puzzle-71", rs, 1789000000, rs[i].BlockIndex)
			if err != nil {
				t.Fatalf("n=%d leaf=%d: %v", n, i, err)
			}
			if err := Verify(pub, o); err != nil {
				t.Fatalf("n=%d leaf=%d did not verify: %v", n, i, err)
			}
		}
	}
}

// An operator who could rewrite the record after seeing a theft would prove
// nothing. These are the rewrites, and each has to fail.
func TestOpeningCannotBeForged(t *testing.T) {
	pub, priv := keypair(t)
	rs := receipts(16)

	base, err := Open(priv, "puzzle-71", rs, 1789000000, 1000+5*7)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("another worker's name on the same block", func(t *testing.T) {
		o := *base
		o.Receipt.WorkerID = "somebody-else"
		if err := Verify(pub, &o); err == nil {
			t.Fatal("a receipt with a swapped worker id verified")
		}
	})

	t.Run("another block under the same name", func(t *testing.T) {
		o := *base
		o.Receipt.BlockIndex = 999999
		if err := Verify(pub, &o); err == nil {
			t.Fatal("a receipt with a swapped block index verified")
		}
	})

	t.Run("a receipt that was never issued", func(t *testing.T) {
		o := *base
		o.Receipt = Receipt{CampaignID: "puzzle-71", BlockIndex: 42, WorkerID: "mallory", IssuedAt: 1, ExpiresAt: 2}
		if err := Verify(pub, &o); err == nil {
			t.Fatal("a fabricated receipt verified")
		}
	})

	t.Run("a root signed by somebody else", func(t *testing.T) {
		otherPub, otherPriv := keypair(t)
		o, err := Open(otherPriv, "puzzle-71", rs, 1789000000, 1000+5*7)
		if err != nil {
			t.Fatal(err)
		}
		if err := Verify(pub, o); err == nil {
			t.Fatal("a root signed by the wrong key verified against the campaign key")
		}
		if err := Verify(otherPub, o); err != nil {
			t.Fatalf("the other key should verify its own root: %v", err)
		}
	})

	t.Run("a root moved to another campaign", func(t *testing.T) {
		o := *base
		o.Root.CampaignID = "puzzle-72"
		o.Receipt.CampaignID = "puzzle-72"
		if err := Verify(pub, &o); err == nil {
			t.Fatal("a root replayed onto another campaign verified")
		}
	})

	t.Run("a root backdated after the fact", func(t *testing.T) {
		o := *base
		o.Root.PublishedAt -= 86400
		if err := Verify(pub, &o); err == nil {
			t.Fatal("a root with a rewritten timestamp verified")
		}
	})

	t.Run("a tampered path", func(t *testing.T) {
		o := *base
		o.Path.Siblings = append([]string(nil), base.Path.Siblings...)
		o.Path.Siblings[0] = hex.EncodeToString(make([]byte, 32))
		if err := Verify(pub, &o); err == nil {
			t.Fatal("a rewritten Merkle path verified")
		}
	})
}

// Field boundaries have to be unambiguous, or an operator could open a leaf as a
// worker who never held it by choosing ids that concatenate the same way.
func TestLeafEncodingIsUnambiguous(t *testing.T) {
	a := Receipt{CampaignID: "puzzle-7", WorkerID: "1alice", BlockIndex: 1, IssuedAt: 2, ExpiresAt: 3}
	b := Receipt{CampaignID: "puzzle-71", WorkerID: "alice", BlockIndex: 1, IssuedAt: 2, ExpiresAt: 3}
	if a.Leaf() == b.Leaf() {
		t.Fatal("two different receipts share a leaf hash; the encoding is not length-prefixed")
	}
}

// A leaf must never collide with an internal node, or a path could be presented
// as a receipt.
func TestLeavesAndNodesAreDomainSeparated(t *testing.T) {
	l := Receipt{CampaignID: "c", WorkerID: "w", BlockIndex: 1}.Leaf()
	if hashPair(l, l) == l {
		t.Fatal("a node hash collided with its leaf")
	}
	var zero [32]byte
	if hashPair(zero, zero) == (Receipt{}).Leaf() {
		t.Fatal("an empty node and an empty leaf hash the same")
	}
}

// The same receipts must always give the same root, whatever order they came
// out of the database in — otherwise a published root cannot be reproduced.
func TestRootIsOrderIndependent(t *testing.T) {
	_, priv := keypair(t)
	rs := receipts(20)

	shuffled := make([]Receipt, len(rs))
	for i := range rs {
		shuffled[i] = rs[len(rs)-1-i]
	}

	a, err := Sign(priv, "puzzle-71", sortReceipts(rs), 1789000000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Sign(priv, "puzzle-71", sortReceipts(shuffled), 1789000000)
	if err != nil {
		t.Fatal(err)
	}
	if a.Root != b.Root {
		t.Fatalf("roots differ by input order: %s vs %s", a.Root, b.Root)
	}
}

func TestEmptyTreeIsRefused(t *testing.T) {
	_, priv := keypair(t)
	if _, err := Sign(priv, "puzzle-71", nil, 1); err == nil {
		t.Fatal("a root over no receipts was signed")
	}
}
