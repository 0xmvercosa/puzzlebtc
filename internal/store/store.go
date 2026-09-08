// Package store persists campaigns, block leases and tickets in SQLite.
//
// The block table is sparse on purpose. A campaign for puzzle #71 with 2^40-key
// blocks has 2^30 blocks, and larger puzzles are far worse; materializing a row
// per block is impossible. So a row exists only once a block has been leased,
// and "available" means "no row". Allocation picks a uniformly random index and
// lets the primary key reject the collision, which for any real campaign is
// astronomically rare — the retry loop exists for correctness, not throughput.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, no build tags
)

// Block lifecycle states.
const (
	StateLeased    = "leased"
	StateCompleted = "completed"
)

// ErrNoBlockAvailable is returned when allocation could not find a free index
// within its retry budget. For a campaign with real headroom this means the
// campaign is effectively exhausted.
var ErrNoBlockAvailable = errors.New("store: no block available")

// ErrLeaseInvalid is returned when a submission's token does not match the live
// lease — a stale worker finishing a block that already expired and was reissued.
var ErrLeaseInvalid = errors.New("store: lease token does not match a live lease")

// DB wraps the SQLite handle.
type DB struct{ sql *sql.DB }

// Open connects to path (use ":memory:" for tests) and applies the schema.
func Open(ctx context.Context, path string) (*DB, error) {
	// busy_timeout keeps concurrent lease writes from failing outright under the
	// single-writer lock; WAL lets readers proceed during a write.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// SQLite takes one writer at a time; more connections just queue and can
	// deadlock across transactions.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sql: sqlDB}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the handle.
func (db *DB) Close() error { return db.sql.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS campaigns (
  id             TEXT PRIMARY KEY,
  puzzle_num     INTEGER NOT NULL,
  target_hash160 TEXT NOT NULL,
  min_key_hex    TEXT NOT NULL,
  max_key_hex    TEXT NOT NULL,
  block_bits     INTEGER NOT NULL,
  params_json    TEXT NOT NULL,
  state          TEXT NOT NULL DEFAULT 'open',
  created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS blocks (
  campaign_id  TEXT    NOT NULL REFERENCES campaigns(id),
  block_index  INTEGER NOT NULL,
  state        TEXT    NOT NULL,
  worker_id    TEXT    NOT NULL,
  lease_token  TEXT    NOT NULL,
  leased_at    INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  completed_at INTEGER,
  witnesses    INTEGER,
  PRIMARY KEY (campaign_id, block_index)
);

-- Drives expired-lease reclamation without scanning completed blocks.
CREATE INDEX IF NOT EXISTS blocks_by_expiry ON blocks(campaign_id, state, expires_at);
CREATE INDEX IF NOT EXISTS blocks_by_worker ON blocks(campaign_id, worker_id, state);

CREATE TABLE IF NOT EXISTS tickets (
  campaign_id TEXT    NOT NULL REFERENCES campaigns(id),
  block_index INTEGER NOT NULL,
  worker_id   TEXT    NOT NULL,
  -- Blocks are all the same size, so this is the same number on almost every
  -- row. It is stored anyway because the LAST block of a campaign is truncated
  -- at the range end and covers less ground than the others. Paying per key
  -- rather than per row keeps that final block worth exactly what it covered.
  keys_swept  INTEGER NOT NULL,
  awarded_at  INTEGER NOT NULL,
  -- One ticket per block for all time. Even if a block were somehow reissued
  -- and completed twice, it can never mint a second ticket.
  PRIMARY KEY (campaign_id, block_index)
);

CREATE INDEX IF NOT EXISTS tickets_by_worker ON tickets(campaign_id, worker_id);

-- Where each participant gets paid. The address is the only thing the pool needs
-- from them, and it is public information: no key, no seed, nothing that could
-- move their funds. It is recorded on first sight and can be changed by the
-- participant, with history kept so a payout can always be traced to the address
-- that was on file when the campaign closed.
CREATE TABLE IF NOT EXISTS participants (
  worker_id   TEXT PRIMARY KEY,
  btc_address TEXT NOT NULL,
  first_seen  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS participant_address_history (
  worker_id   TEXT    NOT NULL,
  btc_address TEXT    NOT NULL,
  changed_at  INTEGER NOT NULL
);

-- Third-party scan claims: ranges somebody says they already searched. These are
-- NEVER treated as swept — no proof this project accepts backs them. They only
-- push those blocks to the back of the allocation queue.
CREATE TABLE IF NOT EXISTS external_claims (
  campaign_id TEXT    NOT NULL REFERENCES campaigns(id),
  lo_block    INTEGER NOT NULL,
  hi_block    INTEGER NOT NULL,
  source      TEXT    NOT NULL,
  note        TEXT    NOT NULL DEFAULT '',
  imported_at INTEGER NOT NULL,
  PRIMARY KEY (campaign_id, lo_block, hi_block, source)
);

CREATE TABLE IF NOT EXISTS solutions (
  campaign_id  TEXT PRIMARY KEY REFERENCES campaigns(id),
  block_index  INTEGER NOT NULL,
  worker_id    TEXT NOT NULL,
  private_key  TEXT NOT NULL,
  found_at     INTEGER NOT NULL
);
`

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.sql.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// CampaignRow is the stored form of a campaign.
type CampaignRow struct {
	ID            string
	PuzzleNum     int
	TargetHash160 string
	MinKeyHex     string
	MaxKeyHex     string
	BlockBits     uint
	ParamsJSON    string
	State         string
}

// PutCampaign inserts a campaign, or does nothing if the ID already exists.
func (db *DB) PutCampaign(ctx context.Context, c CampaignRow) error {
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO campaigns (id, puzzle_num, target_hash160, min_key_hex, max_key_hex, block_bits, params_json, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'open', ?)
		ON CONFLICT(id) DO NOTHING`,
		c.ID, c.PuzzleNum, c.TargetHash160, c.MinKeyHex, c.MaxKeyHex, c.BlockBits, c.ParamsJSON, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: put campaign %s: %w", c.ID, err)
	}
	return nil
}

// GetCampaign loads a campaign by ID.
func (db *DB) GetCampaign(ctx context.Context, id string) (CampaignRow, error) {
	var c CampaignRow
	err := db.sql.QueryRowContext(ctx, `
		SELECT id, puzzle_num, target_hash160, min_key_hex, max_key_hex, block_bits, params_json, state
		FROM campaigns WHERE id = ?`, id).
		Scan(&c.ID, &c.PuzzleNum, &c.TargetHash160, &c.MinKeyHex, &c.MaxKeyHex, &c.BlockBits, &c.ParamsJSON, &c.State)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("store: campaign %q not found", id)
	}
	if err != nil {
		return c, fmt.Errorf("store: get campaign %s: %w", id, err)
	}
	return c, nil
}

// ReclaimExpired frees leases whose deadline has passed, returning how many.
// Completed blocks are never touched, so work is never handed out twice.
func (db *DB) ReclaimExpired(ctx context.Context, campaignID string, now time.Time) (int64, error) {
	res, err := db.sql.ExecContext(ctx, `
		DELETE FROM blocks WHERE campaign_id = ? AND state = ? AND expires_at <= ?`,
		campaignID, StateLeased, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("store: reclaim expired: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// TryLease attempts to claim one specific index. It reports taken=true when the
// index already has a row — leased by someone else, or already completed.
func (db *DB) TryLease(ctx context.Context, campaignID string, index uint64, workerID, token string, now time.Time, ttl time.Duration) (taken bool, err error) {
	res, err := db.sql.ExecContext(ctx, `
		INSERT INTO blocks (campaign_id, block_index, state, worker_id, lease_token, leased_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(campaign_id, block_index) DO NOTHING`,
		campaignID, int64(index), StateLeased, workerID, token, now.Unix(), now.Add(ttl).Unix())
	if err != nil {
		return false, fmt.Errorf("store: lease block %d: %w", index, err)
	}
	n, _ := res.RowsAffected()
	return n == 0, nil
}

// CompleteBlock marks a verified block done and mints its ticket, atomically.
//
// The two writes must not be separable: a ticket without a completed block, or
// a completed block without its ticket, both corrupt the payout ledger.
func (db *DB) CompleteBlock(ctx context.Context, campaignID string, index uint64, workerID, token string, witnesses, keysSwept uint64, now time.Time) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful commit

	res, err := tx.ExecContext(ctx, `
		UPDATE blocks SET state = ?, completed_at = ?, witnesses = ?
		WHERE campaign_id = ? AND block_index = ? AND state = ? AND lease_token = ? AND worker_id = ?`,
		StateCompleted, now.Unix(), int64(witnesses), campaignID, int64(index), StateLeased, token, workerID)
	if err != nil {
		return fmt.Errorf("store: complete block %d: %w", index, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either the lease expired and was reclaimed, or it belongs to someone
		// else, or the block is already complete. All three mean: do not pay.
		return ErrLeaseInvalid
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tickets (campaign_id, block_index, worker_id, keys_swept, awarded_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(campaign_id, block_index) DO NOTHING`,
		campaignID, int64(index), workerID, int64(keysSwept), now.Unix()); err != nil {
		return fmt.Errorf("store: award ticket for block %d: %w", index, err)
	}
	return tx.Commit()
}

// RecordSolution stores a verified jackpot and closes the campaign.
func (db *DB) RecordSolution(ctx context.Context, campaignID string, index uint64, workerID, privKeyHex string, now time.Time) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO solutions (campaign_id, block_index, worker_id, private_key, found_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(campaign_id) DO NOTHING`,
		campaignID, int64(index), workerID, privKeyHex, now.Unix()); err != nil {
		return fmt.Errorf("store: record solution: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE campaigns SET state = 'solved' WHERE id = ?`, campaignID); err != nil {
		return fmt.Errorf("store: close campaign: %w", err)
	}
	return tx.Commit()
}

// AddExternalClaim records a third-party scan claim over a block range.
func (db *DB) AddExternalClaim(ctx context.Context, campaignID string, lo, hi uint64, source, note string) error {
	if lo > hi {
		return fmt.Errorf("store: claim range %d-%d is inverted", lo, hi)
	}
	if source == "" {
		return errors.New("store: a claim must name its source; an unattributed claim cannot be re-checked later")
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO external_claims (campaign_id, lo_block, hi_block, source, note, imported_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(campaign_id, lo_block, hi_block, source) DO NOTHING`,
		campaignID, int64(lo), int64(hi), source, note, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: add external claim: %w", err)
	}
	return nil
}

// ExternalClaim is one stored third-party claim.
type ExternalClaim struct {
	Lo, Hi uint64
	Source string
	Note   string
}

// ExternalClaims lists every claim recorded for a campaign.
func (db *DB) ExternalClaims(ctx context.Context, campaignID string) ([]ExternalClaim, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT lo_block, hi_block, source, note FROM external_claims
		WHERE campaign_id = ? ORDER BY lo_block, hi_block`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("store: external claims: %w", err)
	}
	defer rows.Close()

	var out []ExternalClaim
	for rows.Next() {
		var c ExternalClaim
		var lo, hi int64
		if err := rows.Scan(&lo, &hi, &c.Source, &c.Note); err != nil {
			return nil, fmt.Errorf("store: scan external claim: %w", err)
		}
		c.Lo, c.Hi = uint64(lo), uint64(hi)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetPayoutAddress records where a participant should be paid, keeping the old
// address in history. Validation of the address format belongs to the caller.
func (db *DB) SetPayoutAddress(ctx context.Context, workerID, address string) error {
	if workerID == "" || address == "" {
		return errors.New("store: worker id and payout address are both required")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()
	var prev string
	err = tx.QueryRowContext(ctx, `SELECT btc_address FROM participants WHERE worker_id = ?`, workerID).Scan(&prev)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO participants (worker_id, btc_address, first_seen, updated_at)
			VALUES (?, ?, ?, ?)`, workerID, address, now, now); err != nil {
			return fmt.Errorf("store: insert participant: %w", err)
		}
	case err != nil:
		return fmt.Errorf("store: read participant: %w", err)
	case prev != address:
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO participant_address_history (worker_id, btc_address, changed_at)
			VALUES (?, ?, ?)`, workerID, prev, now); err != nil {
			return fmt.Errorf("store: archive address: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE participants SET btc_address = ?, updated_at = ? WHERE worker_id = ?`,
			address, now, workerID); err != nil {
			return fmt.Errorf("store: update address: %w", err)
		}
	}
	return tx.Commit()
}

// PayoutAddresses maps every known participant to where they get paid.
func (db *DB) PayoutAddresses(ctx context.Context) (map[string]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT worker_id, btc_address FROM participants`)
	if err != nil {
		return nil, fmt.Errorf("store: payout addresses: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var id, addr string
		if err := rows.Scan(&id, &addr); err != nil {
			return nil, fmt.Errorf("store: scan payout address: %w", err)
		}
		out[id] = addr
	}
	return out, rows.Err()
}

// Solution is a recorded winning key.
type Solution struct {
	BlockIndex uint64
	WorkerID   string
	PrivateKey string
	FoundAt    int64
}

// GetSolution returns the campaign's recorded solution, if any.
func (db *DB) GetSolution(ctx context.Context, campaignID string) (*Solution, error) {
	var s Solution
	var idx int64
	err := db.sql.QueryRowContext(ctx, `
		SELECT block_index, worker_id, private_key, found_at FROM solutions WHERE campaign_id = ?`,
		campaignID).Scan(&idx, &s.WorkerID, &s.PrivateKey, &s.FoundAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get solution: %w", err)
	}
	s.BlockIndex = uint64(idx)
	return &s, nil
}

// Stats summarizes a campaign's progress.
type Stats struct {
	Completed int64 `json:"completed_blocks"`
	Leased    int64 `json:"leased_blocks"`
	Tickets   int64 `json:"tickets"`
	Workers   int64 `json:"workers"`
}

// Stats counts blocks and tickets for a campaign.
func (db *DB) Stats(ctx context.Context, campaignID string) (Stats, error) {
	var s Stats
	err := db.sql.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM blocks  WHERE campaign_id = ? AND state = ?),
		  (SELECT COUNT(*) FROM blocks  WHERE campaign_id = ? AND state = ?),
		  (SELECT COUNT(*) FROM tickets WHERE campaign_id = ?),
		  (SELECT COUNT(DISTINCT worker_id) FROM tickets WHERE campaign_id = ?)`,
		campaignID, StateCompleted, campaignID, StateLeased, campaignID, campaignID).
		Scan(&s.Completed, &s.Leased, &s.Tickets, &s.Workers)
	if err != nil {
		return s, fmt.Errorf("store: stats: %w", err)
	}
	return s, nil
}

// TicketHolder is one worker's verified contribution: how many blocks they
// closed, and the total keys those blocks covered. Weight is what the payout
// divides by; Blocks is for display.
type TicketHolder struct {
	WorkerID string
	Blocks   int64
	Weight   int64 // total keys swept
}

// TicketHolders lists every worker with at least one ticket, ordered by ID so
// the payout computation is reproducible.
func (db *DB) TicketHolders(ctx context.Context, campaignID string) ([]TicketHolder, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT worker_id, COUNT(*), COALESCE(SUM(keys_swept), 0) FROM tickets WHERE campaign_id = ?
		GROUP BY worker_id ORDER BY worker_id`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("store: ticket holders: %w", err)
	}
	defer rows.Close()

	var out []TicketHolder
	for rows.Next() {
		var h TicketHolder
		if err := rows.Scan(&h.WorkerID, &h.Blocks, &h.Weight); err != nil {
			return nil, fmt.Errorf("store: scan ticket holder: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
