// Command worker is the reference participant: it leases a block, sweeps it on
// the CPU, and submits the proof.
//
//	worker -server http://localhost:8080 -id alice
//
// It is deliberately the slow, obvious implementation — a few tens of thousands
// of keys per second per core. Its purpose is to define the protocol and to give
// a GPU worker something to diff against: point both at the same small block and
// the two submissions must be byte-identical.
//
// A GPU worker only needs to reproduce three behaviours:
//
//  1. Collect every offset whose HASH160 has at least witness_bits leading zero
//     bits, and submit them strictly ascending (sort if the kernel emits out of
//     order — the coordinator rejects unordered witnesses as duplicates).
//  2. Report every offset matching any watchlist entry.
//  3. Report the offset matching the campaign target as found_offset.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xmvercosa/puzzlebtc/internal/coordinator"
	"github.com/0xmvercosa/puzzlebtc/internal/keyspace"
	"github.com/0xmvercosa/puzzlebtc/internal/proof"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		server = flag.String("server", "http://localhost:8080", "coordinator base URL")
		id     = flag.String("id", "", "worker id (required)")
		target = flag.String("target-hash160", "", "the campaign's target HASH160, 40 hex chars (required)")
		blocks = flag.Int("blocks", 1, "how many blocks to sweep before exiting; 0 means run forever")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *id == "" {
		return errors.New("-id is required: tickets are credited to it")
	}
	if len(*target) != 40 {
		return errors.New("-target-hash160 is required: it is what tells a jackpot hit apart from a canary")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 2 * time.Minute}

	for n := 0; *blocks == 0 || n < *blocks; n++ {
		if ctx.Err() != nil {
			return nil
		}
		if err := sweepOne(ctx, client, *server, *id, *target, log); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
	return nil
}

func sweepOne(ctx context.Context, client *http.Client, server, workerID, targetHash160 string, log *slog.Logger) error {
	var lease coordinator.Lease
	if err := postJSON(ctx, client, server+"/v1/lease", map[string]string{"worker_id": workerID}, &lease); err != nil {
		return fmt.Errorf("lease: %w", err)
	}

	lo, ok := new(big.Int).SetString(lease.LoKeyHex, 16)
	if !ok {
		return fmt.Errorf("lease returned an unparseable start key %q", lease.LoKeyHex)
	}
	hi, ok := new(big.Int).SetString(lease.HiKeyHex, 16)
	if !ok {
		return fmt.Errorf("lease returned an unparseable end key %q", lease.HiKeyHex)
	}
	length := new(big.Int).Sub(hi, lo)
	length.Add(length, big.NewInt(1))
	blk := keyspace.Block{Index: lease.BlockIndex, Lo: lo, Hi: hi, Len: length}

	log.Info("leased block",
		"index", blk.Index, "keys", blk.Len.String(),
		"witness_bits", lease.Params.WitnessBits,
		"expires_in", time.Until(time.Unix(lease.ExpiresAt, 0)).Round(time.Second))

	// The coordinator shuffles the watchlist, so the worker is told separately
	// which entry is the real target. For a public puzzle this is not a secret,
	// and pretending otherwise would only hide it from honest workers — see the
	// trust model in the README. The coordinator re-derives it during
	// verification anyway, so a worker configured with the wrong target loses a
	// find to nobody.
	sweeper, err := proof.NewSweeper(lease.Params, targetHash160, lease.Watchlist)
	if err != nil {
		return err
	}

	start := time.Now()
	sub, err := sweeper.Sweep(ctx, blk)
	if err != nil {
		return fmt.Errorf("sweep: %w", err)
	}
	elapsed := time.Since(start)

	rate := new(big.Float).Quo(new(big.Float).SetInt(blk.Len), big.NewFloat(elapsed.Seconds()))
	log.Info("swept",
		"witnesses", len(sub.Witnesses), "canaries", len(sub.Canaries),
		"elapsed", elapsed.Round(time.Millisecond), "keys_per_sec", rate.Text('f', 0))

	sub.CampaignID = lease.CampaignID
	sub.LeaseToken = lease.Token

	body := struct {
		WorkerID string `json:"worker_id"`
		proof.Submission
	}{WorkerID: workerID, Submission: sub}

	var receipt coordinator.Receipt
	if err := postJSON(ctx, client, server+"/v1/submit", body, &receipt); err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	log.Info("accepted", "block", receipt.BlockIndex, "ticket", receipt.TicketID,
		"verified", receipt.Verified, "deep_audit", receipt.DeepAudit)

	if receipt.Solved {
		log.Warn("*** THIS BLOCK CONTAINED THE TARGET KEY ***", "block", receipt.BlockIndex)
	}
	return nil
}

func postJSON(ctx context.Context, client *http.Client, url string, in, out any) error {
	buf, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s: %s", url, resp.Status, bytes.TrimSpace(body))
	}
	return json.Unmarshal(body, out)
}
