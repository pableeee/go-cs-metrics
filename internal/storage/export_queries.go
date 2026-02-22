package storage

import (
	"fmt"
	"strings"
	"time"
)

// DemoRef holds a demo hash, map name, and match date, used by the simbo3 exporter.
type DemoRef struct {
	Hash      string
	MapName   string
	MatchDate string // "YYYY-MM-DD"
}

// WinOutcome captures round outcome data for a single demo.
type WinOutcome struct {
	Hash         string
	RoundsWon    int
	RoundsPlayed int
}

// SideStats holds aggregate CT and T round win counts across multiple demos.
type SideStats struct {
	CTWins  int
	CTTotal int
	TWins   int
	TTotal  int
}

// PlayerTotals holds summed stats for one player across multiple demos.
type PlayerTotals struct {
	SteamID      string
	Name         string
	Kills        int
	Deaths       int
	Assists      int
	KastRounds   int
	RoundsPlayed int
	TotalDamage  int
}

// QualifyingDemos returns demos within the time window where at least quorum
// of the given SteamIDs appear in player_match_stats, ordered by date descending.
func (db *DB) QualifyingDemos(steamIDs []string, since time.Time, quorum int) ([]DemoRef, error) {
	if len(steamIDs) == 0 {
		return nil, nil
	}
	ph := placeholders(len(steamIDs))
	args := make([]interface{}, 0, len(steamIDs)+1)
	for _, id := range steamIDs {
		args = append(args, id)
	}
	args = append(args, since.Format("2006-01-02"))

	query := fmt.Sprintf(`
		SELECT d.hash, d.map_name, d.match_date
		FROM demos d
		JOIN player_match_stats p ON p.demo_hash = d.hash
		WHERE p.steam_id IN (%s)
		  AND d.match_date >= ?
		GROUP BY d.hash
		HAVING COUNT(DISTINCT p.steam_id) >= %d
		ORDER BY d.match_date DESC`,
		ph, quorum)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DemoRef
	for rows.Next() {
		var r DemoRef
		if err := rows.Scan(&r.Hash, &r.MapName, &r.MatchDate); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MapWinOutcomes returns one WinOutcome per demo hash, using the roster player
// with the most rounds_played as the anchor to determine the match result.
func (db *DB) MapWinOutcomes(steamIDs []string, demoHashes []string) ([]WinOutcome, error) {
	if len(steamIDs) == 0 || len(demoHashes) == 0 {
		return nil, nil
	}
	idPH := placeholders(len(steamIDs))
	hashPH := placeholders(len(demoHashes))

	args := make([]interface{}, 0, len(steamIDs)+len(demoHashes))
	for _, id := range steamIDs {
		args = append(args, id)
	}
	for _, h := range demoHashes {
		args = append(args, h)
	}

	// Order by rounds_played DESC so the first row per demo is the anchor player
	// (the one who played the most rounds and thus has the most reliable outcome data).
	query := fmt.Sprintf(`
		SELECT demo_hash, rounds_won, rounds_played
		FROM player_match_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)
		ORDER BY rounds_played DESC`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var out []WinOutcome
	for rows.Next() {
		var w WinOutcome
		if err := rows.Scan(&w.Hash, &w.RoundsWon, &w.RoundsPlayed); err != nil {
			return nil, err
		}
		if !seen[w.Hash] {
			seen[w.Hash] = true
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

// RoundSideStats returns aggregate CT/T round wins and totals for roster players
// across the given demo hashes.
func (db *DB) RoundSideStats(steamIDs []string, demoHashes []string) (SideStats, error) {
	var s SideStats
	if len(steamIDs) == 0 || len(demoHashes) == 0 {
		return s, nil
	}
	idPH := placeholders(len(steamIDs))
	hashPH := placeholders(len(demoHashes))

	args := make([]interface{}, 0, len(steamIDs)+len(demoHashes))
	for _, id := range steamIDs {
		args = append(args, id)
	}
	for _, h := range demoHashes {
		args = append(args, h)
	}

	// COALESCE guards against NULL when no rows match.
	query := fmt.Sprintf(`
		SELECT
		  COALESCE(SUM(CASE WHEN team='CT' AND won_round=1 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN team='CT'                 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN team='T'  AND won_round=1 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN team='T'                  THEN 1 ELSE 0 END), 0)
		FROM player_round_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)`,
		idPH, hashPH)

	err := db.conn.QueryRow(query, args...).Scan(
		&s.CTWins, &s.CTTotal, &s.TWins, &s.TTotal)
	return s, err
}

// RosterMatchTotals returns per-player summed stats across the given demo hashes,
// ordered by total rounds_played descending (most active players first).
func (db *DB) RosterMatchTotals(steamIDs []string, demoHashes []string) ([]PlayerTotals, error) {
	if len(steamIDs) == 0 || len(demoHashes) == 0 {
		return nil, nil
	}
	idPH := placeholders(len(steamIDs))
	hashPH := placeholders(len(demoHashes))

	args := make([]interface{}, 0, len(steamIDs)+len(demoHashes))
	for _, id := range steamIDs {
		args = append(args, id)
	}
	for _, h := range demoHashes {
		args = append(args, h)
	}

	query := fmt.Sprintf(`
		SELECT steam_id, name,
		       SUM(kills), SUM(deaths), SUM(assists),
		       SUM(kast_rounds), SUM(rounds_played), SUM(total_damage)
		FROM player_match_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)
		GROUP BY steam_id
		ORDER BY SUM(rounds_played) DESC`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerTotals
	for rows.Next() {
		var p PlayerTotals
		if err := rows.Scan(
			&p.SteamID, &p.Name,
			&p.Kills, &p.Deaths, &p.Assists,
			&p.KastRounds, &p.RoundsPlayed, &p.TotalDamage,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// placeholders returns a comma-separated string of n "?" for SQL IN clauses,
// e.g. placeholders(3) → "?,?,?".
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
