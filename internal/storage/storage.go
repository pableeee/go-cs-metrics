// Package storage provides SQLite-backed persistence for parsed demo data and player metrics.
package storage

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// DB wraps a sql.DB for the metrics store.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at the given path and applies the schema.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := conn.Exec(schemaSQL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Migrations: add columns introduced after initial schema creation.
	// ALTER TABLE returns "duplicate column name" for already-existing columns; that is safe to ignore.
	altMigrations := []string{
		`ALTER TABLE player_match_stats ADD COLUMN crosshair_encounters INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN crosshair_median_deg REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN crosshair_pct_under5 REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE demos ADD COLUMN tier TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE demos ADD COLUMN is_baseline INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN role TEXT NOT NULL DEFAULT 'Rifler'`,
		`ALTER TABLE player_match_stats ADD COLUMN median_ttk_ms REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN median_ttd_ms REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN buy_type TEXT NOT NULL DEFAULT 'eco'`,
		`ALTER TABLE player_match_stats ADD COLUMN one_tap_kills INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN is_post_plant INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN is_in_clutch INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN clutch_enemy_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN counter_strafe_pct REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN won_round INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN rounds_won INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN median_trade_kill_delay_ms REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN median_trade_death_delay_ms REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE demos ADD COLUMN event_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE demos ADD COLUMN quick_hash TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_demos_quick_hash ON demos(quick_hash) WHERE quick_hash IS NOT NULL`,
	}
	for _, stmt := range altMigrations {
		if _, err := conn.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			conn.Close()
			return nil, fmt.Errorf("migration: %w", err)
		}
	}
	return &DB{conn: conn}, nil
}

// Close closes the underlying connection.
func (db *DB) Close() error {
	return db.conn.Close()
}
