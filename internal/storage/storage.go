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
	// SQLite is single-writer; one connection per process is correct.
	// busy_timeout makes concurrent processes wait instead of immediately returning SQLITE_BUSY.
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA busy_timeout = 30000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := conn.Exec(schemaSQL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Structural migrations run before additive ones: a table rebuild recreates
	// the table from a fixed column list, so any ADD COLUMN applied first would
	// be silently dropped by it.
	if err := migrateDuelSegmentsRound(conn); err != nil {
		conn.Close()
		return nil, err
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
		// Slice 2 (Pass 14): save / saved-by / assisted-kill annotation.
		`ALTER TABLE player_match_stats ADD COLUMN saved_by_teammate INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN saved_teammate INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN assisted_kills INTEGER NOT NULL DEFAULT 0`,
		// Slice 3 (Pass 15): HLTV-style flash assists (25 dmg threshold in blind window).
		`ALTER TABLE player_match_stats ADD COLUMN hltv_flash_assists INTEGER NOT NULL DEFAULT 0`,
		// Slice 4 (Pass 16): liveness — time alive and sole-survivor moments.
		`ALTER TABLE player_match_stats ADD COLUMN alive_seconds_total REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN last_alive_server_rounds INTEGER NOT NULL DEFAULT 0`,
		// Pass 17: scan volatility — out-of-combat crosshair discipline.
		`ALTER TABLE player_match_stats ADD COLUMN scan_ooc_seconds REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN scan_dwell_pct REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN scan_reversals_per_min REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_match_stats ADD COLUMN scan_avg_yaw_deg_per_sec REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN scan_ooc_seconds REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN scan_dwell_pct REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN scan_reversals INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN scan_avg_yaw_deg_per_sec REAL NOT NULL DEFAULT 0`,
		// Pass 18: shot accounting — accuracy and the aimed/blind split.
		`ALTER TABLE player_weapon_stats ADD COLUMN shots_fired INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_weapon_stats ADD COLUMN shots_visible INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_weapon_stats ADD COLUMN hits_visible INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_weapon_stats ADD COLUMN head_hits INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_weapon_stats ADD COLUMN head_hits_visible INTEGER NOT NULL DEFAULT 0`,
		// Pass 12: duel context on death events — first-sight ticks + plant state.
		`ALTER TABLE player_death_events ADD COLUMN killer_sight_tick INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE player_death_events ADD COLUMN victim_sight_tick INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE player_death_events ADD COLUMN bomb_planted INTEGER NOT NULL DEFAULT 0`,
		// Pass 6: sight → first-shot delay, to separate reaction from habit.
		`ALTER TABLE player_duel_segments ADD COLUMN median_shot_delay_ms REAL NOT NULL DEFAULT 0`,
		// Pass 19: round context — resource share, pack distance, contact timing.
		`ALTER TABLE player_round_stats ADD COLUMN gun_samples INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN gun_samples_rifle INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN gun_samples_sniper INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE player_round_stats ADD COLUMN pack_dist_avg_m REAL NOT NULL DEFAULT -1`,
		`ALTER TABLE player_round_stats ADD COLUMN first_contact_sec REAL NOT NULL DEFAULT -1`,
		`ALTER TABLE player_round_stats ADD COLUMN death_sec REAL NOT NULL DEFAULT -1`,
	}
	for _, stmt := range altMigrations {
		if _, err := conn.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			conn.Close()
			return nil, fmt.Errorf("migration: %w", err)
		}
	}
	return &DB{conn: conn}, nil
}

// migrateDuelSegmentsRound adds round_number to player_duel_segments.
//
// This one cannot be an ALTER: the round is part of the UNIQUE key, and SQLite
// cannot alter a constraint in place. So the table is rebuilt and existing rows
// are carried over with round_number = -1, meaning "aggregated before the round
// dimension existed" — distinguishable from a real round. Re-running
// `replay --force` replaces those rows with round-resolved ones.
//
// No-op once the column exists, so it is safe on every Open.
func migrateDuelSegmentsRound(conn *sql.DB) error {
	var n int
	err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('player_duel_segments') WHERE name = 'round_number'`,
	).Scan(&n)
	if err != nil {
		return fmt.Errorf("check duel-segment round column: %w", err)
	}
	if n > 0 {
		return nil
	}

	stmts := []string{
		`ALTER TABLE player_duel_segments RENAME TO player_duel_segments_old`,
		`CREATE TABLE player_duel_segments (
			demo_hash          TEXT NOT NULL REFERENCES demos(hash),
			steam_id           TEXT NOT NULL,
			round_number       INTEGER NOT NULL DEFAULT -1,
			weapon_bucket      TEXT NOT NULL,
			distance_bin       TEXT NOT NULL,
			duel_count         INTEGER NOT NULL DEFAULT 0,
			first_hit_count    INTEGER NOT NULL DEFAULT 0,
			first_hit_hs_count INTEGER NOT NULL DEFAULT 0,
			median_corr_deg    REAL    NOT NULL DEFAULT 0,
			median_sight_deg   REAL    NOT NULL DEFAULT 0,
			median_expo_win_ms REAL    NOT NULL DEFAULT 0,
			UNIQUE(demo_hash, steam_id, round_number, weapon_bucket, distance_bin)
		)`,
		`INSERT INTO player_duel_segments(
			demo_hash, steam_id, round_number, weapon_bucket, distance_bin,
			duel_count, first_hit_count, first_hit_hs_count,
			median_corr_deg, median_sight_deg, median_expo_win_ms)
		 SELECT demo_hash, steam_id, -1, weapon_bucket, distance_bin,
			duel_count, first_hit_count, first_hit_hs_count,
			median_corr_deg, median_sight_deg, median_expo_win_ms
		 FROM player_duel_segments_old`,
		`DROP TABLE player_duel_segments_old`,
		`CREATE INDEX IF NOT EXISTS idx_pds_steam_id  ON player_duel_segments(steam_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pds_demo_hash ON player_duel_segments(demo_hash)`,
	}
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin duel-segment migration: %w", err)
	}
	defer tx.Rollback()
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("duel-segment round migration: %w", err)
		}
	}
	return tx.Commit()
}

// Close closes the underlying connection.
func (db *DB) Close() error {
	return db.conn.Close()
}
