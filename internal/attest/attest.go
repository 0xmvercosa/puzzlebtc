// Package attest builds the record that names whoever was holding the ground a
// stolen key sat on.
//
// # What it is for
//
// The blind lot in internal/blind makes theft expensive and deliberate. It does
// not make it impossible: a participant who runs a discrete log against the lot
// they were handed can extract the key and sweep the prize. This package makes
// that person nameable, by a record that existed before they could have done it.
//
// The claim it supports is narrow and checkable:
//
//	the coordinator committed, at time T, to "worker W holds block j"
//	block j covers keys [lo, hi]
//	the key swept at time T' > T falls in [lo, hi]
//	therefore W was the only participant holding that ground
//
// Every step is verifiable by a third party. The commitment is the part that has
// to come first, or it proves nothing: an operator who could write the record
// after seeing the theft could write any name into it.
//
// # How the commitment works
//
// Each lease becomes a Receipt, hashed into a leaf. Leaves accumulate into a
// Merkle tree, and the coordinator signs the root with a long-lived Ed25519 key
// whose public half is published with the campaign. Publishing the root — in the
// repository, in a post, in an OP_RETURN, anywhere with a timestamp somebody
// else controls — commits to every lease issued so far without revealing any of
// them.
//
// Revealing one lease later means opening one leaf: the Receipt plus its path to
// a signed root. Verify checks the path and the signature together.
//
// The receipts stay closed until there is a reason to open one, and that matters
// for more than privacy. A receipt names a block index, and a worker who learns
// its own block index can recover its lot's first key with a discrete log over
// the tiling shift alone — cheap enough to matter. So the tree is published as a
// root and opened one leaf at a time, never as a list.
package attest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Receipt is one statement: this worker held this block from this moment.
type Receipt struct {
	CampaignID string `json:"campaign_id"`
	BlockIndex uint64 `json:"block_index"`
	WorkerID   string `json:"worker_id"`
	// IssuedAt and ExpiresAt are Unix seconds. Both are part of the statement:
	// a lease that expired and was reissued produces a second receipt, and the
	// times are what say which holder a given moment belongs to.
	IssuedAt  int64 `json:"issued_at"`
	ExpiresAt int64 `json:"expires_at"`
}

// Leaf is the hash a receipt contributes to the tree.
//
// The encoding is length-prefixed on every variable-length field. Concatenating
// them raw would let a worker id ending in digits and a campaign id starting
// with them produce the same bytes as a different pair — which is exactly the
// ambiguity an operator would exploit to open a leaf as somebody else's.
func (r Receipt) Leaf() [32]byte {
	h := sha256.New()
	h.Write([]byte{0x00}) // domain separation from internal nodes
	writeString(h, "puzzlebtc/attest/receipt/v1")
	writeString(h, r.CampaignID)
	writeString(h, r.WorkerID)
	writeUint64(h, r.BlockIndex)
	writeUint64(h, uint64(r.IssuedAt))
	writeUint64(h, uint64(r.ExpiresAt))

	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func writeString(h interface{ Write([]byte) (int, error) }, s string) {
	writeUint64(h, uint64(len(s)))
	_, _ = h.Write([]byte(s))
}

func writeUint64(h interface{ Write([]byte) (int, error) }, v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, _ = h.Write(b[:])
}

// Tree is a Merkle tree over receipt leaves.
type Tree struct {
	levels [][][32]byte // levels[0] is the leaves
}

// ErrEmptyTree is returned when a root or a proof is asked of no leaves.
var ErrEmptyTree = errors.New("attest: no receipts to commit to")

// NewTree builds the tree. Leaves are taken in the order given; the caller is
// expected to keep a stable order (this package's own callers sort by block
// index, which is unique per receipt within a campaign).
//
// An odd level duplicates its last node to pair it. That is the common
// convention and it has a known wart — a tree of [a, b, b] and one of [a, b]
// share a root — which is closed here by binding the leaf count into the signed
// root rather than by changing the tree shape.
func NewTree(leaves [][32]byte) (*Tree, error) {
	if len(leaves) == 0 {
		return nil, ErrEmptyTree
	}
	level := make([][32]byte, len(leaves))
	copy(level, leaves)

	t := &Tree{levels: [][][32]byte{level}}
	for len(level) > 1 {
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			right := level[i]
			if i+1 < len(level) {
				right = level[i+1]
			}
			next = append(next, hashPair(level[i], right))
		}
		t.levels = append(t.levels, next)
		level = next
	}
	return t, nil
}

func hashPair(l, r [32]byte) [32]byte {
	h := sha256.New()
	h.Write([]byte{0x01}) // domain separation from leaves
	h.Write(l[:])
	h.Write(r[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Root is the commitment.
func (t *Tree) Root() [32]byte { return t.levels[len(t.levels)-1][0] }

// Len is how many leaves the tree holds.
func (t *Tree) Len() int { return len(t.levels[0]) }

// Path is a leaf's route to the root: the sibling at each level, and which side
// it sits on.
type Path struct {
	Index    int      `json:"index"`
	Siblings []string `json:"siblings"` // hex, bottom-up
}

// Proof returns the path for the leaf at index i.
func (t *Tree) Proof(i int) (Path, error) {
	if i < 0 || i >= t.Len() {
		return Path{}, fmt.Errorf("attest: leaf %d out of range (have %d)", i, t.Len())
	}
	p := Path{Index: i}
	idx := i
	for lvl := 0; lvl < len(t.levels)-1; lvl++ {
		sib := idx ^ 1
		if sib >= len(t.levels[lvl]) {
			sib = idx // the duplicated-last-node case
		}
		p.Siblings = append(p.Siblings, hex.EncodeToString(t.levels[lvl][sib][:]))
		idx /= 2
	}
	return p, nil
}

// SignedRoot is what gets published: a commitment somebody else can timestamp.
type SignedRoot struct {
	CampaignID  string `json:"campaign_id"`
	Root        string `json:"root"`   // hex
	Leaves      int    `json:"leaves"` // bound in so a duplicated tail cannot be re-read
	PublishedAt int64  `json:"published_at"`
	PublicKey   string `json:"public_key"` // hex, ed25519
	Signature   string `json:"signature"`  // hex
}

// signingBytes is what the signature covers. Everything that gives the root its
// meaning is inside: a signature over the root alone could be replayed onto
// another campaign or another moment.
func signingBytes(campaignID string, root [32]byte, leaves int, publishedAt int64) []byte {
	h := sha256.New()
	writeString(h, "puzzlebtc/attest/root/v1")
	writeString(h, campaignID)
	_, _ = h.Write(root[:])
	writeUint64(h, uint64(leaves))
	writeUint64(h, uint64(publishedAt))
	return h.Sum(nil)
}

// Sign publishes a root over the receipts, in the order given.
func Sign(priv ed25519.PrivateKey, campaignID string, receipts []Receipt, publishedAt int64) (SignedRoot, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return SignedRoot{}, errors.New("attest: signing key is not an ed25519 private key")
	}
	tree, err := treeOf(receipts)
	if err != nil {
		return SignedRoot{}, err
	}
	root := tree.Root()
	sig := ed25519.Sign(priv, signingBytes(campaignID, root, tree.Len(), publishedAt))

	return SignedRoot{
		CampaignID:  campaignID,
		Root:        hex.EncodeToString(root[:]),
		Leaves:      tree.Len(),
		PublishedAt: publishedAt,
		PublicKey:   hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
		Signature:   hex.EncodeToString(sig),
	}, nil
}

// Opening is one receipt revealed against a published root.
type Opening struct {
	Receipt Receipt    `json:"receipt"`
	Path    Path       `json:"path"`
	Root    SignedRoot `json:"root"`
}

// Open reveals the first receipt recorded for a block.
//
// A block leased more than once — expired and reissued — has a receipt per
// holder, and this returns the earliest. Use OpenReceipt to name a specific one.
func Open(priv ed25519.PrivateKey, campaignID string, receipts []Receipt, publishedAt int64, blockIndex uint64) (*Opening, error) {
	for _, r := range sortReceipts(receipts) {
		if r.BlockIndex == blockIndex {
			return OpenReceipt(priv, campaignID, receipts, publishedAt, r)
		}
	}
	return nil, fmt.Errorf("attest: no receipt for block %d", blockIndex)
}

// OpenReceipt reveals one exact receipt against a root over the whole set.
//
// The set must be every receipt the root commits to; passing a subset would
// produce a root nobody else can reproduce, which is the same as producing no
// proof at all.
func OpenReceipt(priv ed25519.PrivateKey, campaignID string, receipts []Receipt, publishedAt int64, want Receipt) (*Opening, error) {
	sorted := sortReceipts(receipts)
	i := -1
	for n, r := range sorted {
		if r == want {
			i = n
			break
		}
	}
	if i < 0 {
		return nil, fmt.Errorf("attest: block %d held by %q at %d is not in this set", want.BlockIndex, want.WorkerID, want.IssuedAt)
	}
	tree, err := treeOf(sorted)
	if err != nil {
		return nil, err
	}
	path, err := tree.Proof(i)
	if err != nil {
		return nil, err
	}
	root, err := Sign(priv, campaignID, sorted, publishedAt)
	if err != nil {
		return nil, err
	}
	return &Opening{Receipt: sorted[i], Path: path, Root: root}, nil
}

// Verify checks an opening against the public key the campaign published.
//
// It answers exactly one question: did the holder of that key commit, at the
// moment the root was published, to this worker holding this block? It says
// nothing about whether the published root was itself timestamped honestly —
// that is what publishing it somewhere the operator does not control is for.
func Verify(pub ed25519.PublicKey, o *Opening) error {
	if o == nil {
		return errors.New("attest: nil opening")
	}
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("attest: not an ed25519 public key")
	}
	declared, err := hex.DecodeString(o.Root.PublicKey)
	if err != nil || !strings.EqualFold(hex.EncodeToString(pub), hex.EncodeToString(declared)) {
		return errors.New("attest: the opening names a different public key than the one being trusted")
	}
	if o.Receipt.CampaignID != o.Root.CampaignID {
		return fmt.Errorf("attest: receipt is for campaign %q, root is for %q", o.Receipt.CampaignID, o.Root.CampaignID)
	}

	rootBytes, err := hex.DecodeString(o.Root.Root)
	if err != nil || len(rootBytes) != 32 {
		return errors.New("attest: root is not a 32-byte hex digest")
	}
	sig, err := hex.DecodeString(o.Root.Signature)
	if err != nil {
		return fmt.Errorf("attest: signature is not hex: %w", err)
	}
	var root [32]byte
	copy(root[:], rootBytes)
	if !ed25519.Verify(pub, signingBytes(o.Root.CampaignID, root, o.Root.Leaves, o.Root.PublishedAt), sig) {
		return errors.New("attest: signature does not check out")
	}

	if o.Path.Index < 0 || o.Path.Index >= o.Root.Leaves {
		return fmt.Errorf("attest: leaf %d is outside the %d the root commits to", o.Path.Index, o.Root.Leaves)
	}

	cur := o.Receipt.Leaf()
	idx := o.Path.Index
	for _, sh := range o.Path.Siblings {
		sb, err := hex.DecodeString(sh)
		if err != nil || len(sb) != 32 {
			return errors.New("attest: a sibling in the path is not a 32-byte hex digest")
		}
		var sib [32]byte
		copy(sib[:], sb)
		if idx%2 == 0 {
			cur = hashPair(cur, sib)
		} else {
			cur = hashPair(sib, cur)
		}
		idx /= 2
	}
	if cur != root {
		return errors.New("attest: the receipt is not in the tree this root commits to")
	}
	return nil
}

func treeOf(receipts []Receipt) (*Tree, error) {
	leaves := make([][32]byte, 0, len(receipts))
	for _, r := range receipts {
		leaves = append(leaves, r.Leaf())
	}
	return NewTree(leaves)
}

// sortReceipts fixes the leaf order so a root is reproducible from the same set
// of receipts however they came out of the database.
func sortReceipts(in []Receipt) []Receipt {
	out := make([]Receipt, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].BlockIndex != out[j].BlockIndex {
			return out[i].BlockIndex < out[j].BlockIndex
		}
		if out[i].IssuedAt != out[j].IssuedAt {
			return out[i].IssuedAt < out[j].IssuedAt
		}
		return out[i].WorkerID < out[j].WorkerID
	})
	return out
}
