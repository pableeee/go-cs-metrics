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

// DemoSideStats holds CT/T round win counts for a single demo.
type DemoSideStats struct {
	Hash    string
	CTWins  int
	CTTotal int
	TWins   int
	TTotal  int
}

// PlayerDemoTotals holds per-demo stats for one player (not aggregated).
type PlayerDemoTotals struct {
	SteamID      string
	Name         string
	DemoHash     string
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

// QualifyingDemosWindow is like QualifyingDemos but uses a half-open window
// [from, before) so callers can exclude demos on or after a cutoff date.
// Used by backtest-dataset to avoid temporal lookahead: pass before=event_date.
func (db *DB) QualifyingDemosWindow(steamIDs []string, from, before time.Time, quorum int) ([]DemoRef, error) {
	if len(steamIDs) == 0 {
		return nil, nil
	}
	ph := placeholders(len(steamIDs))
	args := make([]interface{}, 0, len(steamIDs)+2)
	for _, id := range steamIDs {
		args = append(args, id)
	}
	args = append(args, from.Format("2006-01-02"))
	args = append(args, before.Format("2006-01-02"))

	query := fmt.Sprintf(`
		SELECT d.hash, d.map_name, d.match_date
		FROM demos d
		JOIN player_match_stats p ON p.demo_hash = d.hash
		WHERE p.steam_id IN (%s)
		  AND d.match_date >= ?
		  AND d.match_date < ?
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

	// Order by rounds_played DESC, steam_id ASC so the anchor player is deterministic
	// when two roster players have equal rounds_played in a demo.
	query := fmt.Sprintf(`
		SELECT demo_hash, rounds_won, rounds_played
		FROM player_match_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)
		ORDER BY rounds_played DESC, steam_id ASC`,
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

// PlayerDemoCount holds how many demos a single roster player appears in.
type PlayerDemoCount struct {
	SteamID string
	Name    string
	Count   int
}

// PlayerDemoCounts returns, for each steam ID in the roster, the number of demos
// they appear in within the since window — without any quorum filter. Used to
// produce diagnostic output when QualifyingDemos returns empty.
func (db *DB) PlayerDemoCounts(steamIDs []string, since time.Time) ([]PlayerDemoCount, error) {
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
		SELECT p.steam_id, MAX(p.name), COUNT(DISTINCT p.demo_hash)
		FROM player_match_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE p.steam_id IN (%s)
		  AND d.match_date >= ?
		GROUP BY p.steam_id
		ORDER BY COUNT(DISTINCT p.demo_hash) DESC`,
		ph)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerDemoCount
	for rows.Next() {
		var c PlayerDemoCount
		if err := rows.Scan(&c.SteamID, &c.Name, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MapEntryStats holds opening kill/death counts and rounds for one map.
type MapEntryStats struct {
	OpeningKills  int
	OpeningDeaths int
	RoundsPlayed  int
}

// TradeStats holds trade kill/death counts across all maps.
type TradeStats struct {
	TradeKills   int
	TradeDeaths  int
	RoundsPlayed int
}

// BuyTypeWinRate holds win/total counts for eco and force buy types.
type BuyTypeWinRate struct {
	EcoWins    int
	EcoTotal   int
	ForceWins  int
	ForceTotal int
}

// PostPlantStats holds T-side post-plant win counts for one map.
type PostPlantStats struct {
	TWins  int
	TTotal int
}

// MapEntryStats returns per-map opening kill/death counts and rounds_played
// for the given roster players across the given demo hashes.
func (db *DB) MapEntryStats(steamIDs []string, demoHashes []string) (map[string]MapEntryStats, error) {
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
		SELECT d.map_name,
		       COALESCE(SUM(p.opening_kills), 0),
		       COALESCE(SUM(p.opening_deaths), 0),
		       COALESCE(SUM(p.rounds_played), 0)
		FROM player_match_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE p.steam_id IN (%s)
		  AND p.demo_hash IN (%s)
		GROUP BY d.map_name`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]MapEntryStats)
	for rows.Next() {
		var mapName string
		var s MapEntryStats
		if err := rows.Scan(&mapName, &s.OpeningKills, &s.OpeningDeaths, &s.RoundsPlayed); err != nil {
			return nil, err
		}
		out[mapName] = s
	}
	return out, rows.Err()
}

// TeamTradeStats returns aggregate trade kill/death counts and rounds_played
// for the given roster players across all given demo hashes.
func (db *DB) TeamTradeStats(steamIDs []string, demoHashes []string) (TradeStats, error) {
	var s TradeStats
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

	query := fmt.Sprintf(`
		SELECT
		  COALESCE(SUM(trade_kills), 0),
		  COALESCE(SUM(trade_deaths), 0),
		  COALESCE(SUM(rounds_played), 0)
		FROM player_match_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)`,
		idPH, hashPH)

	err := db.conn.QueryRow(query, args...).Scan(&s.TradeKills, &s.TradeDeaths, &s.RoundsPlayed)
	return s, err
}

// BuyTypeWinRates returns eco and force buy-type win/total counts for the
// given roster players across the given demo hashes.
func (db *DB) BuyTypeWinRates(steamIDs []string, demoHashes []string) (BuyTypeWinRate, error) {
	var r BuyTypeWinRate
	if len(steamIDs) == 0 || len(demoHashes) == 0 {
		return r, nil
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
		SELECT buy_type,
		       COALESCE(SUM(won_round), 0),
		       COUNT(*)
		FROM player_round_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)
		  AND buy_type IN ('eco', 'force')
		GROUP BY buy_type`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return r, err
	}
	defer rows.Close()

	for rows.Next() {
		var buyType string
		var wins, total int
		if err := rows.Scan(&buyType, &wins, &total); err != nil {
			return r, err
		}
		switch buyType {
		case "eco":
			r.EcoWins, r.EcoTotal = wins, total
		case "force":
			r.ForceWins, r.ForceTotal = wins, total
		}
	}
	return r, rows.Err()
}

// MapPostPlantTWinRates returns per-map T-side post-plant win/total counts
// for the given roster players across the given demo hashes.
func (db *DB) MapPostPlantTWinRates(steamIDs []string, demoHashes []string) (map[string]PostPlantStats, error) {
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
		SELECT d.map_name,
		       COALESCE(SUM(CASE WHEN prs.team='T' AND prs.is_post_plant=1 AND prs.won_round=1 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN prs.team='T' AND prs.is_post_plant=1 THEN 1 ELSE 0 END), 0)
		FROM player_round_stats prs
		JOIN demos d ON d.hash = prs.demo_hash
		WHERE prs.steam_id IN (%s)
		  AND prs.demo_hash IN (%s)
		GROUP BY d.map_name`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]PostPlantStats)
	for rows.Next() {
		var mapName string
		var s PostPlantStats
		if err := rows.Scan(&mapName, &s.TWins, &s.TTotal); err != nil {
			return nil, err
		}
		out[mapName] = s
	}
	return out, rows.Err()
}

// RoundSideStatsByDemo returns per-demo CT/T round win counts for the given
// roster players and demo hashes, grouped by demo_hash.
func (db *DB) RoundSideStatsByDemo(steamIDs []string, demoHashes []string) ([]DemoSideStats, error) {
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
		SELECT demo_hash,
		  COALESCE(SUM(CASE WHEN team='CT' AND won_round=1 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN team='CT'                 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN team='T'  AND won_round=1 THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN team='T'                  THEN 1 ELSE 0 END), 0)
		FROM player_round_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)
		GROUP BY demo_hash`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DemoSideStats
	for rows.Next() {
		var s DemoSideStats
		if err := rows.Scan(&s.Hash, &s.CTWins, &s.CTTotal, &s.TWins, &s.TTotal); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RosterMatchTotalsByDemo returns per-player per-demo stats (not aggregated)
// for the given roster players across the given demo hashes.
func (db *DB) RosterMatchTotalsByDemo(steamIDs []string, demoHashes []string) ([]PlayerDemoTotals, error) {
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
		SELECT steam_id, name, demo_hash,
		       kills, deaths, assists, kast_rounds, rounds_played, total_damage
		FROM player_match_stats
		WHERE steam_id IN (%s)
		  AND demo_hash IN (%s)
		ORDER BY steam_id, demo_hash`,
		idPH, hashPH)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayerDemoTotals
	for rows.Next() {
		var p PlayerDemoTotals
		if err := rows.Scan(
			&p.SteamID, &p.Name, &p.DemoHash,
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
