// Swing queries — load the kill log and round rosters that internal/swing
// needs to build its empirical probability tables.
//
// Both metrics are corpus-level: the probability tables are counted over every
// round available, not per demo, so these loaders deliberately fetch the whole
// (filtered) corpus in one pass rather than per-player.
package storage

import (
	"strconv"

	"github.com/pable/go-cs-metrics/internal/swing"
)

// SwingFilter narrows which demos feed the tables and the per-player results.
type SwingFilter struct {
	Tier    string // "" = all tiers
	MapName string // "" = all maps; normalized title-case (e.g. "Nuke")
	Since   string // "" = no lower bound; YYYY-MM-DD inclusive
	Before  string // "" = no upper bound; YYYY-MM-DD exclusive
}

func (f SwingFilter) where(prefix string) (string, []any) {
	clause := ""
	var args []any
	if f.Tier != "" {
		clause += " AND " + prefix + ".tier = ?"
		args = append(args, f.Tier)
	}
	if f.MapName != "" {
		clause += " AND " + prefix + ".map_name = ?"
		args = append(args, f.MapName)
	}
	if f.Since != "" {
		clause += " AND " + prefix + ".match_date >= ?"
		args = append(args, f.Since)
	}
	if f.Before != "" {
		clause += " AND " + prefix + ".match_date < ?"
		args = append(args, f.Before)
	}
	return clause, args
}

// LoadSwingKills returns every kill in the filtered corpus as a swing.Kill,
// with the first-sight advantage already resolved to milliseconds.
//
// Kills where the killer is also the victim (world / fall damage) are skipped:
// they are not duels and would corrupt both the man-advantage walk and the
// duel table.
func (db *DB) LoadSwingKills(f SwingFilter) ([]swing.Kill, error) {
	clause, args := f.where("d")
	rows, err := db.conn.Query(`
		SELECT pde.demo_hash, pde.round_number, pde.tick,
		       pde.killer_id, pde.victim_id, pde.killer_team,
		       pde.bomb_planted, pde.killer_sight_tick, pde.victim_sight_tick,
		       pde.distance_m, pde.weapon, d.tickrate
		FROM player_death_events pde
		JOIN demos d ON d.hash = pde.demo_hash
		WHERE pde.killer_id != pde.victim_id`+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []swing.Kill
	for rows.Next() {
		var (
			k                     swing.Kill
			killerStr, victimStr  string
			killerTeam, weapon    string
			killerSight, vicSight int
			distanceM, tickrate   float64
			bombPlanted           int
		)
		if err := rows.Scan(
			&k.DemoHash, &k.RoundNumber, &k.Tick,
			&killerStr, &victimStr, &killerTeam,
			&bombPlanted, &killerSight, &vicSight,
			&distanceM, &weapon, &tickrate,
		); err != nil {
			return nil, err
		}
		k.KillerID, _ = strconv.ParseUint(killerStr, 10, 64)
		k.VictimID, _ = strconv.ParseUint(victimStr, 10, 64)
		if k.KillerID == 0 || k.VictimID == 0 || k.KillerID == k.VictimID {
			continue
		}
		k.KillerIsCT = killerTeam == "CT"
		k.BombPlanted = bombPlanted != 0
		k.KillerSawFirst = killerSight >= 0
		k.VictimSawFirst = vicSight >= 0
		if tickrate <= 0 {
			tickrate = 64
		}
		if k.KillerSawFirst && k.VictimSawFirst {
			k.SightAdvantageMs = float64(vicSight-killerSight) / tickrate * 1000
			// How long both had been aware of each other when it resolved.
			later := killerSight
			if vicSight > later {
				later = vicSight
			}
			k.MutualAwarenessMs = float64(k.Tick-later) / tickrate * 1000
			k.MutualKnown = true
		}
		k.DistanceBin = swing.DistanceBin(distanceM)
		k.WeaponBucket = swing.WeaponClass(weapon)
		out = append(out, k)
	}
	return out, rows.Err()
}

// LoadSwingRounds returns one swing.Round per (demo, round) with the roster on
// each side and the winner, derived from player_round_stats.
//
// The winner is taken from won_round on the rows themselves rather than a
// separate table: every player on the winning side carries won_round = 1, so a
// single row settles it. Rounds where neither side has any player (corrupt or
// unparsed) are dropped.
func (db *DB) LoadSwingRounds(f SwingFilter) ([]swing.Round, error) {
	clause, args := f.where("d")
	rows, err := db.conn.Query(`
		SELECT prs.demo_hash, prs.round_number, prs.steam_id, prs.team, prs.won_round
		FROM player_round_stats prs
		JOIN demos d ON d.hash = prs.demo_hash
		WHERE 1=1`+clause+`
		ORDER BY prs.demo_hash, prs.round_number`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct {
		hash  string
		round int
	}
	acc := map[key]*swing.Round{}
	var order []key
	for rows.Next() {
		var (
			hash, steamStr, team string
			round, won           int
		)
		if err := rows.Scan(&hash, &round, &steamStr, &team, &won); err != nil {
			return nil, err
		}
		id, err := strconv.ParseUint(steamStr, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		kk := key{hash, round}
		r := acc[kk]
		if r == nil {
			r = &swing.Round{DemoHash: hash, RoundNumber: round}
			acc[kk] = r
			order = append(order, kk)
		}
		switch team {
		case "CT":
			r.PlayersCT = append(r.PlayersCT, id)
			if won != 0 {
				r.CTWon = true
			}
		case "T":
			r.PlayersT = append(r.PlayersT, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]swing.Round, 0, len(order))
	for _, kk := range order {
		r := acc[kk]
		if len(r.PlayersCT) == 0 || len(r.PlayersT) == 0 {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

// PlayerMapsPlayed returns the maps a player has rounds on, most-played first.
func (db *DB) PlayerMapsPlayed(steamID uint64, f SwingFilter) ([]string, error) {
	clause, args := f.where("d")
	args = append([]any{strconv.FormatUint(steamID, 10)}, args...)
	rows, err := db.conn.Query(`
		SELECT d.map_name, COUNT(*) c
		FROM player_round_stats prs
		JOIN demos d ON d.hash = prs.demo_hash
		WHERE prs.steam_id = ?`+clause+`
		GROUP BY d.map_name ORDER BY c DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		var c int
		if err := rows.Scan(&m, &c); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PlayerNames returns the most recent display name for every player in the
// filtered corpus. One query, so a leaderboard does not fan out into hundreds.
func (db *DB) PlayerNames(f SwingFilter) (map[uint64]string, error) {
	clause, args := f.where("d")
	rows, err := db.conn.Query(`
		SELECT pms.steam_id, pms.name
		FROM player_match_stats pms
		JOIN demos d ON d.hash = pms.demo_hash
		WHERE pms.name != ''`+clause+`
		ORDER BY d.match_date ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint64]string{}
	for rows.Next() {
		var idStr, name string
		if err := rows.Scan(&idStr, &name); err != nil {
			return nil, err
		}
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		out[id] = name // later rows overwrite → most recent name wins
	}
	return out, rows.Err()
}

// PlayerTiers returns each player's dominant tier, so a leaderboard can be
// restricted to (or labelled by) the level a player actually competes at.
func (db *DB) PlayerTiers(f SwingFilter) (map[uint64]string, error) {
	clause, args := f.where("d")
	rows, err := db.conn.Query(`
		SELECT pms.steam_id, d.tier, COUNT(*) c
		FROM player_match_stats pms
		JOIN demos d ON d.hash = pms.demo_hash
		WHERE 1=1`+clause+`
		GROUP BY pms.steam_id, d.tier
		ORDER BY c ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint64]string{}
	for rows.Next() {
		var idStr, tier string
		var c int
		if err := rows.Scan(&idStr, &tier, &c); err != nil {
			return nil, err
		}
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		out[id] = tier // ordered ascending → the most frequent tier wins
	}
	return out, rows.Err()
}
