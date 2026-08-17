package storage

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// PlayerContextRow is one player-side aggregate of the Pass 19 round-context
// columns: what the rounds GAVE the player, as opposed to what they did with
// it (swing).
type PlayerContextRow struct {
	SteamID uint64
	Side    string // "CT" | "T"
	// Rounds counts only measured rounds (gun_samples > 0), so pre-backfill
	// rows never dilute the shares.
	Rounds     int
	GunSamples int64
	GunRifle   int64
	GunSniper  int64
	// Means over rounds where the value is >= 0; -1 when no round qualifies.
	PackDistAvgM    float64
	FirstContactSec float64
	DeathSec        float64
}

const contextSelect = `
	SELECT %s prs.team,
	       COUNT(CASE WHEN prs.gun_samples > 0 THEN 1 END),
	       COALESCE(SUM(prs.gun_samples), 0),
	       COALESCE(SUM(prs.gun_samples_rifle), 0),
	       COALESCE(SUM(prs.gun_samples_sniper), 0),
	       AVG(CASE WHEN prs.pack_dist_avg_m   >= 0 THEN prs.pack_dist_avg_m   END),
	       AVG(CASE WHEN prs.first_contact_sec >= 0 THEN prs.first_contact_sec END),
	       AVG(CASE WHEN prs.death_sec         >= 0 THEN prs.death_sec         END)
	FROM player_round_stats prs
	JOIN demos d ON d.hash = prs.demo_hash
	WHERE prs.team IN ('CT','T')%s
	GROUP BY %s prs.team`

// LoadPlayerContext aggregates the round-context columns per player per side
// for the given players, under the same filter the swing corpus uses.
func (db *DB) LoadPlayerContext(steamIDs []uint64, f SwingFilter) (map[uint64]map[string]*PlayerContextRow, error) {
	if len(steamIDs) == 0 {
		return map[uint64]map[string]*PlayerContextRow{}, nil
	}
	clause, args := f.where("d")
	ph := make([]string, len(steamIDs))
	for i, id := range steamIDs {
		ph[i] = "?"
		args = append(args, strconv.FormatUint(id, 10))
	}
	q := fmt.Sprintf(contextSelect, "prs.steam_id,",
		clause+" AND prs.steam_id IN ("+strings.Join(ph, ",")+")", "prs.steam_id,")

	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[uint64]map[string]*PlayerContextRow{}
	for rows.Next() {
		var idStr string
		r := &PlayerContextRow{}
		var pack, contact, death sql.NullFloat64
		if err := rows.Scan(&idStr, &r.Side, &r.Rounds,
			&r.GunSamples, &r.GunRifle, &r.GunSniper,
			&pack, &contact, &death); err != nil {
			return nil, err
		}
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			continue
		}
		r.SteamID = id
		r.PackDistAvgM = nullOr(pack, -1)
		r.FirstContactSec = nullOr(contact, -1)
		r.DeathSec = nullOr(death, -1)
		if out[id] == nil {
			out[id] = map[string]*PlayerContextRow{}
		}
		out[id][r.Side] = r
	}
	return out, rows.Err()
}

// LoadPopulationContext returns the per-side aggregate over every player in
// the filtered corpus — the reference the individual rows are read against.
func (db *DB) LoadPopulationContext(f SwingFilter) (map[string]*PlayerContextRow, error) {
	clause, args := f.where("d")
	q := fmt.Sprintf(contextSelect, "", clause, "")

	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]*PlayerContextRow{}
	for rows.Next() {
		r := &PlayerContextRow{}
		var pack, contact, death sql.NullFloat64
		if err := rows.Scan(&r.Side, &r.Rounds,
			&r.GunSamples, &r.GunRifle, &r.GunSniper,
			&pack, &contact, &death); err != nil {
			return nil, err
		}
		r.PackDistAvgM = nullOr(pack, -1)
		r.FirstContactSec = nullOr(contact, -1)
		r.DeathSec = nullOr(death, -1)
		out[r.Side] = r
	}
	return out, rows.Err()
}

func nullOr(v sql.NullFloat64, def float64) float64 {
	if v.Valid {
		return v.Float64
	}
	return def
}
