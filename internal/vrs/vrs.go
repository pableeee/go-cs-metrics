// Package vrs provides storage and lookup for Valve Regional Standings snapshots.
package vrs

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS vrs_snapshots (
    snapshot_date TEXT NOT NULL,
    vrs_rank      INTEGER NOT NULL,
    team_name     TEXT NOT NULL,
    vrs_points    REAL NOT NULL,
    roster        TEXT NOT NULL,
    PRIMARY KEY (snapshot_date, vrs_rank)
);
CREATE INDEX IF NOT EXISTS idx_vrs_date ON vrs_snapshots(snapshot_date);
`

// VRSEntry represents a single team entry in a VRS global standings snapshot.
type VRSEntry struct {
	SnapshotDate string
	Rank         int
	TeamName     string
	Points       float64
	Roster       []string // in-game tags as listed in the standings
}

// VRSSnapshot represents a complete VRS global standings snapshot for one date.
type VRSSnapshot struct {
	Date    string     // "YYYY-MM-DD"
	Entries []VRSEntry // ordered by rank ascending
}

// VRSStore wraps a SQLite database for VRS snapshot storage and lookup.
type VRSStore struct {
	conn *sql.DB
}

// Open opens (or creates) the VRS SQLite database at the given path.
// The directory is created if it does not exist.
func Open(path string) (*VRSStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create vrs db dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open vrs db: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply vrs schema: %w", err)
	}
	return &VRSStore{conn: conn}, nil
}

// Close closes the underlying database connection.
func (s *VRSStore) Close() error {
	return s.conn.Close()
}

// InsertSnapshot upserts all entries from the snapshot into the database.
// Already-existing rows for the same (snapshot_date, team_name) are replaced.
func (s *VRSStore) InsertSnapshot(snap VRSSnapshot) error {
	tx, err := s.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO vrs_snapshots (snapshot_date, vrs_rank, team_name, vrs_points, roster)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range snap.Entries {
		rosterJSON, err := json.Marshal(e.Roster)
		if err != nil {
			return fmt.Errorf("marshal roster for %s: %w", e.TeamName, err)
		}
		if _, err := stmt.Exec(snap.Date, e.Rank, e.TeamName, e.Points, string(rosterJSON)); err != nil {
			return fmt.Errorf("insert %s/%s: %w", snap.Date, e.TeamName, err)
		}
	}
	return tx.Commit()
}

// LatestSnapshotBefore returns all VRSEntry rows for the most recent snapshot
// on or before the given date (format "YYYY-MM-DD"). Returns nil, nil if no
// snapshot exists at or before that date.
func (s *VRSStore) LatestSnapshotBefore(date string) ([]VRSEntry, error) {
	row := s.conn.QueryRow(
		`SELECT MAX(snapshot_date) FROM vrs_snapshots WHERE snapshot_date <= ?`, date)
	var snapshotDate sql.NullString
	if err := row.Scan(&snapshotDate); err != nil {
		return nil, err
	}
	if !snapshotDate.Valid {
		return nil, nil
	}

	rows, err := s.conn.Query(`
		SELECT vrs_rank, team_name, vrs_points, roster
		FROM vrs_snapshots
		WHERE snapshot_date = ?
		ORDER BY vrs_rank`,
		snapshotDate.String)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VRSEntry
	for rows.Next() {
		var e VRSEntry
		var rosterJSON string
		if err := rows.Scan(&e.Rank, &e.TeamName, &e.Points, &rosterJSON); err != nil {
			return nil, err
		}
		e.SnapshotDate = snapshotDate.String
		if err := json.Unmarshal([]byte(rosterJSON), &e.Roster); err != nil {
			return nil, fmt.Errorf("parse roster JSON for %s: %w", e.TeamName, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ExistingDates returns all snapshot dates already stored in the database,
// in ascending order. Used for incremental sync.
func (s *VRSStore) ExistingDates() ([]string, error) {
	rows, err := s.conn.Query(
		`SELECT DISTINCT snapshot_date FROM vrs_snapshots ORDER BY snapshot_date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
