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
  awarded_at  INTEGER NOT NULL,
  -- One ticket per block for all time. Even if a block were somehow reissued
  -- and completed twice, it can never mint a second ticket.
  PRIMARY KEY (campaign_id, block_index)
);

CREATE INDEX IF NOT EXISTS tickets_by_worker ON tickets(campaign_id, worker_id);

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
func (db *DB) CompleteBlock(ctx context.Context, campaignID string, index uint64, workerID, token string, witnesses uint64, now time.Time) error {
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
		INSERT INTO tickets (campaign_id, block_index, worker_id, awarded_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(campaign_id, block_index) DO NOTHING`,
		campaignID, int64(index), workerID, now.Unix()); err != nil {
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

// TicketHolder is one worker's verified ticket count.
type TicketHolder struct {
	WorkerID string
	Tickets  int64
}

// TicketHolders lists every worker with at least one ticket, ordered by ID so
// the payout computation is reproducible.
func (db *DB) TicketHolders(ctx context.Context, campaignID string) ([]TicketHolder, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT worker_id, COUNT(*) FROM tickets WHERE campaign_id = ?
		GROUP BY worker_id ORDER BY worker_id`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("store: ticket holders: %w", err)
	}
	defer rows.Close()

	var out []TicketHolder
	for rows.Next() {
		var h TicketHolder
		if err := rows.Scan(&h.WorkerID, &h.Tickets); err != nil {
			return nil, fmt.Errorf("store: scan ticket holder: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
