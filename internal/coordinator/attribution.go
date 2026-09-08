package coordinator

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"

	"github.com/0xmvercosa/puzzlebtc/internal/attest"
	"github.com/0xmvercosa/puzzlebtc/internal/btc"
)

// ErrAttestationDisabled is returned when the campaign has no signing key.
var ErrAttestationDisabled = errors.New("coordinator: no attestation key configured")

// PublishRoot signs a commitment to every lease issued so far.
//
// This is the half that has to happen before anything goes wrong. A root that
// only appears after a prize is swept proves nothing: the operator could have
// written any name into the record in between. Published on a schedule, to
// somewhere with a timestamp the operator does not control — the repository, a
// post, an OP_RETURN — it pins the whole history of who held what.
//
// The root reveals nothing. Block indices stay closed until one is opened.
func (co *Coordinator) PublishRoot(ctx context.Context) (*attest.SignedRoot, error) {
	if len(co.cfg.AttestKey) == 0 {
		return nil, ErrAttestationDisabled
	}
	receipts, err := co.receipts(ctx)
	if err != nil {
		return nil, err
	}
	root, err := attest.Sign(co.cfg.AttestKey, co.campaign.ID, receipts, co.now().Unix())
	if err != nil {
		return nil, err
	}
	return &root, nil
}

// AttestingKey is the public half, for publication with the campaign.
func (co *Coordinator) AttestingKey() (ed25519.PublicKey, error) {
	if len(co.cfg.AttestKey) == 0 {
		return nil, ErrAttestationDisabled
	}
	return co.cfg.AttestKey.Public().(ed25519.PublicKey), nil
}

// Attribution answers, for a private key that surfaced on chain, who was holding
// the ground it sat on.
type Attribution struct {
	// Key is the key that was resolved, hex.
	Key string `json:"key_hex"`
	// Hash160 is what that key hashes to. If it does not match the campaign
	// target, this is not the prize and Attribution says so rather than naming
	// somebody over an unrelated key.
	Hash160    string `json:"hash160"`
	IsTarget   bool   `json:"is_target"`
	BlockIndex uint64 `json:"block_index"`
	// Holders is every lease ever issued for that block, oldest first. It is
	// usually one. More than one means the lease expired and was reissued, and
	// then the times are what decide — which is why they are in the receipt.
	Holders []attest.Opening `json:"holders"`
}

// Attribute resolves a key to the participants who held its block.
//
// The claim it produces is narrow on purpose: the coordinator committed, before
// the key could have been found, to these participants holding this ground. It
// is not an accusation and it does not become one on its own — a lease that
// expired unswept and a lease whose holder reported honestly look the same here.
// What it removes is anonymity.
func (co *Coordinator) Attribute(ctx context.Context, key *big.Int) (*Attribution, error) {
	if len(co.cfg.AttestKey) == 0 {
		return nil, ErrAttestationDisabled
	}
	if key == nil || key.Sign() <= 0 {
		return nil, errors.New("coordinator: key must be a positive scalar")
	}
	index, ok := co.campaign.BlockIndexOf(key)
	if !ok {
		return nil, fmt.Errorf("coordinator: key %s is outside campaign %s", key.Text(16), co.campaign.ID)
	}
	h := btc.PubKeyHash160(key)

	receipts, err := co.receipts(ctx)
	if err != nil {
		return nil, err
	}
	out := &Attribution{
		Key:        key.Text(16),
		Hash160:    fmt.Sprintf("%x", h),
		IsTarget:   fmt.Sprintf("%x", h) == co.campaign.TargetHash160,
		BlockIndex: index,
	}
	now := co.now().Unix()
	for _, r := range receipts {
		if r.BlockIndex != index {
			continue
		}
		o, err := attest.OpenReceipt(co.cfg.AttestKey, co.campaign.ID, receipts, now, r)
		if err != nil {
			return nil, err
		}
		out.Holders = append(out.Holders, *o)
	}
	if len(out.Holders) == 0 {
		return nil, fmt.Errorf("coordinator: block %d was never leased; nobody held that ground", index)
	}
	return out, nil
}

func (co *Coordinator) receipts(ctx context.Context) ([]attest.Receipt, error) {
	log, err := co.db.LeaseHistory(ctx, co.campaign.ID)
	if err != nil {
		return nil, err
	}
	out := make([]attest.Receipt, 0, len(log))
	for _, l := range log {
		out = append(out, attest.Receipt{
			CampaignID: co.campaign.ID,
			BlockIndex: l.BlockIndex,
			WorkerID:   l.WorkerID,
			IssuedAt:   l.IssuedAt,
			ExpiresAt:  l.ExpiresAt,
		})
	}
	return out, nil
}
