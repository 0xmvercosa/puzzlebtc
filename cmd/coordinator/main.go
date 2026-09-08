// Command coordinator serves one campaign: it hands out random blocks, verifies
// the proof of sweep that comes back, and keeps the ticket ledger.
//
//	coordinator -puzzle 71 -db pool.db -addr :8080
//
// The campaign secret must be supplied in PUZZLEPOOL_SECRET and must be stable
// across restarts. It does two jobs: it determines where canaries are planted in
// every block, and it fixes the tiling shift that makes blind lots worth
// anything. Changing it invalidates every outstanding lease and re-aligns the
// tiling, which would hand out ground that was already swept — so back it up
// before the first lease, not after.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/blind"
	"github.com/0xmvercosa/puzzlebtc/internal/coordinator"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
	"github.com/0xmvercosa/puzzlebtc/internal/store"
)

// These name the environment variables holding the campaign's secrets. Neither
// is a flag on purpose: flags land in shell history and in `ps` output.
//
// PUZZLEPOOL_SECRET seeds canary placement and the tiling shift.
// PUZZLEPOOL_ATTEST_KEY is the ed25519 seed that signs lease commitments;
// generate one with `attribute -genkey`.
const (
	secretEnv = "PUZZLEPOOL_SECRET"
	attestEnv = "PUZZLEPOOL_ATTEST_KEY"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		puzzleNum  = flag.Int("puzzle", 71, "Bitcoin Puzzle number to search (1-160)")
		targetHex  = flag.String("target-hash160", "", "target HASH160, 40 hex chars (required)")
		campaignID = flag.String("campaign", "", "campaign id (default: puzzle-<n>)")
		blockBits  = flag.Uint("block-bits", 33, "block size as a power of two; 2^33 keys is about an hour on a four-core CPU, which is what sets the standard lot")
		dbPath     = flag.String("db", "puzzlepool.db", "SQLite database path")
		addr       = flag.String("addr", ":8080", "listen address")
		leaseTTL   = flag.Duration("lease-ttl", 2*time.Hour, "how long a worker holds a block before it returns to the pool")
		auditRate  = flag.Float64("deep-audit-rate", 0.02, "fraction of submissions verified in full rather than sampled")
		noAttest   = flag.Bool("no-attest", false, "run without a lease-commitment key; gives up attribution")
		unblinded  = flag.Bool("unblinded", false, "hand out key ranges instead of curve points; only for debugging a worker against a known block")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	secret := os.Getenv(secretEnv)
	if len(secret) < 32 {
		return fmt.Errorf("%s must be set to at least 32 bytes; it seeds canary placement and the tiling shift, and must survive restarts", secretEnv)
	}
	if *targetHex == "" {
		return errors.New("-target-hash160 is required: the coordinator will not guess what it is searching for")
	}

	// Attestation is what makes a swept prize attributable, and the commitment
	// has to exist before anything is found. Refusing to start without it is
	// deliberate: a campaign that runs for a week and then adds the key has a
	// week of leases nobody can be held to. Checked here, before anything
	// touches the disk, so a misconfiguration fails on the first line.
	var attestKey ed25519.PrivateKey
	if seedHex := strings.TrimSpace(os.Getenv(attestEnv)); seedHex != "" {
		seed, err := hex.DecodeString(seedHex)
		if err != nil || len(seed) != ed25519.SeedSize {
			return fmt.Errorf("%s must be %d bytes of hex; generate one with `attribute -genkey`", attestEnv, ed25519.SeedSize)
		}
		attestKey = ed25519.NewKeyFromSeed(seed)
	} else if !*noAttest {
		return fmt.Errorf("%s is not set. Generate one with `attribute -genkey`, publish the public half with the campaign, and keep the private half. Pass -no-attest to run without it and give up naming whoever sweeps a prize outside the protocol", attestEnv)
	}

	min, max, err := keyspace.PuzzleRange(*puzzleNum)
	if err != nil {
		return err
	}
	id := *campaignID
	if id == "" {
		id = fmt.Sprintf("puzzle-%d", *puzzleNum)
	}
	// The shift is what turns the blind lot from theatre into work: without it a
	// worker recovers its lot's first key from the curve point with a
	// baby-step/giant-step search over the block count, which is milliseconds.
	// See internal/blind.
	shift, err := blind.DeriveShift([]byte(secret), id, *blockBits)
	if err != nil {
		return err
	}
	campaign, err := keyspace.NewShiftedCampaign(id, *puzzleNum, *targetHex, min, max, *blockBits, shift)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	cfg := coordinator.DefaultConfig()
	cfg.LeaseTTL = *leaseTTL
	cfg.DeepAuditRate = *auditRate
	cfg.CanarySecret = []byte(secret)
	cfg.Blind = !*unblinded
	cfg.AttestKey = attestKey

	params := proof.DefaultParams(*blockBits)
	co, err := coordinator.New(ctx, db, campaign, params, cfg)
	if err != nil {
		return err
	}

	log.Info("campaign ready",
		"id", campaign.ID,
		"puzzle", campaign.PuzzleNum,
		"target", campaign.TargetHash160,
		"blocks", campaign.NumBlocks().String(),
		"block_keys", campaign.BlockSize().String(),
		"witness_bits", params.WitnessBits,
		"expected_witnesses_per_block", int(params.ExpectedWitnesses(campaign.BlockSize())),
		"blind", cfg.Blind,
		"attesting", len(cfg.AttestKey) > 0,
	)
	if pub, err := co.AttestingKey(); err == nil {
		log.Info("publish this with the campaign; it is what anyone verifies an attribution against",
			"attest_public_key", hex.EncodeToString(pub))
	}
	if !cfg.Blind {
		log.Warn("BLINDING IS OFF: workers receive private key ranges and can keep any prize they find without running a discrete log first")
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           coordinator.NewServer(co, log).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Generous: a worker uploading 16k witnesses over a slow link is normal.
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("stopped")
	return nil
}
