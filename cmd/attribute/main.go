// Command attribute answers one question: who was holding the ground a key sat
// on?
//
//	attribute -db pool.db -campaign puzzle-71 -key 40000000000000abcd
//	attribute -db pool.db -campaign puzzle-71 -root
//	attribute -genkey
//
// It is the operator half of internal/attest. With -root it publishes a signed
// commitment to every lease issued so far, which is the part that has to happen
// on a schedule and BEFORE anything goes wrong — a record that only appears
// after a prize is swept proves nothing, because the operator could have written
// any name into it in between. Publish it somewhere with a timestamp you do not
// control.
//
// With -key it opens the one leaf that names a holder, together with the Merkle
// path and the signature that tie it to a published root. Anyone holding the
// campaign's public key can check that output; they do not have to trust this
// program or the operator running it.
//
// It reads. It signs commitments over its own records. It moves no money and it
// accuses nobody: a lease that expired unswept and a lease whose holder reported
// honestly look exactly the same here. What it removes is anonymity.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/0xmvercosa/puzzlebtc/internal/blind"
	"github.com/0xmvercosa/puzzlebtc/internal/coordinator"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

const (
	secretEnv = "PUZZLEPOOL_SECRET"
	attestEnv = "PUZZLEPOOL_ATTEST_KEY"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		dbPath     = flag.String("db", "puzzlebtc.db", "coordinator database")
		campaignID = flag.String("campaign", "", "campaign id")
		keyHex     = flag.String("key", "", "private key that surfaced on chain, hex")
		wantRoot   = flag.Bool("root", false, "publish a signed commitment to every lease so far")
		genKey     = flag.Bool("genkey", false, "generate an attestation key and exit")
	)
	flag.Parse()

	if *genKey {
		return generateKey()
	}
	if *campaignID == "" {
		flag.Usage()
		return errors.New("-campaign is required")
	}
	if (*keyHex == "") == !*wantRoot {
		return errors.New("pass exactly one of -key or -root")
	}

	co, err := open(*dbPath, *campaignID)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if *wantRoot {
		root, err := co.PublishRoot(ctx)
		if err != nil {
			return err
		}
		return emit(root)
	}

	key, ok := new(big.Int).SetString(strings.TrimPrefix(*keyHex, "0x"), 16)
	if !ok {
		return fmt.Errorf("-key %q is not hex", *keyHex)
	}
	att, err := co.Attribute(ctx, key)
	if err != nil {
		return err
	}
	return emit(att)
}

func generateKey() error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	pub := priv.Public().(ed25519.PublicKey)
	fmt.Printf("# private — keep it out of shell history and out of the repository\n")
	fmt.Printf("export %s=%s\n\n", attestEnv, hex.EncodeToString(priv.Seed()))
	fmt.Printf("# public — publish this with the campaign; it is what anyone verifies against\n")
	fmt.Printf("%s\n", hex.EncodeToString(pub))
	return nil
}

// open rebuilds the campaign from the database and the operator's secrets. The
// tiling shift is derived, not stored, so the same secret that ran the
// coordinator has to be present here or every block would resolve to the wrong
// ground.
func open(dbPath, campaignID string) (*coordinator.Coordinator, error) {
	secret := os.Getenv(secretEnv)
	if len(secret) < 32 {
		return nil, fmt.Errorf("%s must be set to the same value the coordinator ran with", secretEnv)
	}
	seedHex := os.Getenv(attestEnv)
	if seedHex == "" {
		return nil, fmt.Errorf("%s is not set; run with -genkey to create one", attestEnv)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(seedHex))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s must be %d bytes of hex", attestEnv, ed25519.SeedSize)
	}

	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	row, err := db.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	min, ok := new(big.Int).SetString(row.MinKeyHex, 16)
	if !ok {
		return nil, fmt.Errorf("campaign %q has an unreadable min key", campaignID)
	}
	max, ok := new(big.Int).SetString(row.MaxKeyHex, 16)
	if !ok {
		return nil, fmt.Errorf("campaign %q has an unreadable max key", campaignID)
	}
	shift, err := blind.DeriveShift([]byte(secret), campaignID, row.BlockBits)
	if err != nil {
		return nil, err
	}
	c, err := keyspace.NewShiftedCampaign(campaignID, row.PuzzleNum, row.TargetHash160, min, max, row.BlockBits, shift)
	if err != nil {
		return nil, err
	}

	var params proof.Params
	if err := json.Unmarshal([]byte(row.ParamsJSON), &params); err != nil {
		return nil, fmt.Errorf("campaign %q has unreadable proof params: %w", campaignID, err)
	}

	cfg := coordinator.DefaultConfig()
	cfg.CanarySecret = []byte(secret)
	cfg.Blind = true
	cfg.AttestKey = ed25519.NewKeyFromSeed(seed)

	return coordinator.New(ctx, db, c, params, cfg)
}

func emit(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
