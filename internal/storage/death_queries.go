// Death-event queries — aggregations over player_death_events for the
// `deaths` command ("how do I die?").
//
// player_death_events denormalizes match_date and map_name, so these queries
// filter without joining the demos table. The table is populated by Pass 12
// (death events); demos aggregated before it shipped return zero rows — run
// `replay --dir <event>/ --force` to backfill.

package storage

import (
	"fmt"
	"strconv"
	"strings"
)

// DeathBreakdown is one grouped row of death statistics. Key is the group
// label (round phase, weapon, or map name depending on the query).
type DeathBreakdown struct {
	Key         string
	Deaths      int
	Headshots   int // deaths where the killing shot was a headshot
	Flashed     int // deaths where the victim was blind
	Traded      int // deaths a teammate traded back
	OpeningDeal int // deaths that were the round's opening death
	AvgDistM    float64
}

// DeathTotals is the un-grouped summary over the same filter.
type DeathTotals struct {
	Deaths      int
	Headshots   int
	Flashed     int
	Traded      int
	OpeningDeal int
	AvgDistM    float64
	Demos       int
	Maps        int
	FirstDate   string
	LastDate    string
}

// DeathFilter narrows the death-event scan. Zero values mean "no filter".
type DeathFilter struct {
	MapName string // matched with or without the "de_" prefix
	Since   string // inclusive lower bound on match_date (YYYY-MM-DD)
	Before  string // exclusive upper bound on match_date
	Phase   string // exact round_phase value
}

// where builds the shared WHERE clause and arg list. victim_id is always
// constrained; the rest depend on the filter.
func (f DeathFilter) where(steamID uint64) (string, []any) {
	clauses := []string{"victim_id = ?"}
	args := []any{strconv.FormatUint(steamID, 10)}

	if f.MapName != "" {
		m := strings.TrimPrefix(strings.ToLower(f.MapName), "de_")
		clauses = append(clauses, "LOWER(REPLACE(map_name, 'de_', '')) = ?")
		args = append(args, m)
	}
	if f.Since != "" {
		clauses = append(clauses, "match_date >= ?")
		args = append(args, f.Since)
	}
	if f.Before != "" {
		clauses = append(clauses, "match_date < ?")
		args = append(args, f.Before)
	}
	if f.Phase != "" {
		clauses = append(clauses, "round_phase = ?")
		args = append(args, f.Phase)
	}
	return strings.Join(clauses, " AND "), args
}

// DeathTotalsFor returns the un-grouped summary of a player's deaths.
func (db *DB) DeathTotalsFor(steamID uint64, f DeathFilter) (DeathTotals, error) {
	where, args := f.where(steamID)
	var t DeathTotals
	err := db.conn.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(is_headshot), 0),
		       COALESCE(SUM(was_flashed), 0),
		       COALESCE(SUM(was_traded), 0),
		       COALESCE(SUM(is_opening_death), 0),
		       COALESCE(AVG(distance_m), 0),
		       COUNT(DISTINCT demo_hash),
		       COUNT(DISTINCT map_name),
		       COALESCE(MIN(match_date), ''),
		       COALESCE(MAX(match_date), '')
		FROM player_death_events
		WHERE `+where, args...).Scan(
		&t.Deaths, &t.Headshots, &t.Flashed, &t.Traded, &t.OpeningDeal,
		&t.AvgDistM, &t.Demos, &t.Maps, &t.FirstDate, &t.LastDate)
	if err != nil {
		return DeathTotals{}, err
	}
	return t, nil
}

// deathBreakdownBy groups the player's deaths by an arbitrary column.
// groupCol must be a literal column name chosen by the caller — never user
// input — since it is interpolated into the SQL.
func (db *DB) deathBreakdownBy(steamID uint64, groupCol string, f DeathFilter, limit int) ([]DeathBreakdown, error) {
	where, args := f.where(steamID)
	q := fmt.Sprintf(`
		SELECT %s AS k,
		       COUNT(*) AS n,
		       COALESCE(SUM(is_headshot), 0),
		       COALESCE(SUM(was_flashed), 0),
		       COALESCE(SUM(was_traded), 0),
		       COALESCE(SUM(is_opening_death), 0),
		       COALESCE(AVG(distance_m), 0)
		FROM player_death_events
		WHERE %s
		GROUP BY k
		ORDER BY n DESC`, groupCol, where)
	if limit > 0 {
		q += " LIMIT " + strconv.Itoa(limit)
	}
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeathBreakdown
	for rows.Next() {
		var b DeathBreakdown
		if err := rows.Scan(&b.Key, &b.Deaths, &b.Headshots, &b.Flashed,
			&b.Traded, &b.OpeningDeal, &b.AvgDistM); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeathsByPhase groups deaths by round_phase (pistol/early/mid/late/post_plant).
func (db *DB) DeathsByPhase(steamID uint64, f DeathFilter) ([]DeathBreakdown, error) {
	return db.deathBreakdownBy(steamID, "round_phase", f, 0)
}

// DeathsByWeapon groups deaths by the weapon that killed the player.
// limit caps the number of rows (0 = all).
func (db *DB) DeathsByWeapon(steamID uint64, f DeathFilter, limit int) ([]DeathBreakdown, error) {
	return db.deathBreakdownBy(steamID, "weapon", f, limit)
}

// DeathsByMap groups deaths by map.
func (db *DB) DeathsByMap(steamID uint64, f DeathFilter) ([]DeathBreakdown, error) {
	return db.deathBreakdownBy(steamID, "map_name", f, 0)
}

// DeathsByDistanceBin groups deaths into fixed engagement-distance buckets.
// Bins mirror the duel-segment distance bins so the two tables can be read
// side by side.
func (db *DB) DeathsByDistanceBin(steamID uint64, f DeathFilter) ([]DeathBreakdown, error) {
	const bin = `CASE
		WHEN distance_m < 5  THEN '0-5m'
		WHEN distance_m < 10 THEN '5-10m'
		WHEN distance_m < 15 THEN '10-15m'
		WHEN distance_m < 20 THEN '15-20m'
		WHEN distance_m < 30 THEN '20-30m'
		ELSE '30m+'
	END`
	return db.deathBreakdownBy(steamID, bin, f, 0)
}

// RoundsPlayedForDeathFilter returns how many rounds the player actually
// played under the same map/date filter, so death counts can be expressed
// per round. Reads player_round_stats (full coverage) rather than the event
// table, joined to demos for the map/date predicates.
func (db *DB) RoundsPlayedForDeathFilter(steamID uint64, f DeathFilter) (int, error) {
	clauses := []string{"p.steam_id = ?"}
	args := []any{strconv.FormatUint(steamID, 10)}
	if f.MapName != "" {
		m := strings.TrimPrefix(strings.ToLower(f.MapName), "de_")
		clauses = append(clauses, "LOWER(REPLACE(d.map_name, 'de_', '')) = ?")
		args = append(args, m)
	}
	if f.Since != "" {
		clauses = append(clauses, "d.match_date >= ?")
		args = append(args, f.Since)
	}
	if f.Before != "" {
		clauses = append(clauses, "d.match_date < ?")
		args = append(args, f.Before)
	}
	var n int
	err := db.conn.QueryRow(`
		SELECT COUNT(*)
		FROM player_round_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE `+strings.Join(clauses, " AND "), args...).Scan(&n)
	return n, err
}
