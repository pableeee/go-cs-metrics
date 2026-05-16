// Role-stats queries — pure derivation from existing tables (no schema change).
// These power the HLTV-style role decomposition in `player --roles`.
//
// All queries return per-demo rows so the caller (cmd/player) can apply
// --map / --since / --last filters in Go by keeping only the surviving demo
// hashes, then sum across that subset.
//
// Event-driven metrics (sniper rounds, utility kills, flashes thrown, time
// opponent flashed) read from player_death_events / grenade_events /
// flash_events. These tables are only populated for demos aggregated after
// passes 12/13 shipped; older demos return zero rows. Re-run
// `replay --dir <event>/ --force` to backfill an event corpus.

package storage

import (
	"strconv"

	"github.com/pable/go-cs-metrics/internal/model"
)

// GetAllPlayerRoundStats returns every per-round row for a given SteamID64
// across all demos, joined with the demos table so the caller can apply
// map / date / last-N filters in Go. Ordered ascending by match_date then
// (demo_hash, round_number) so the result is stable.
func (db *DB) GetAllPlayerRoundStats(steamID uint64) ([]model.PlayerRoundStats, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT p.demo_hash, p.round_number, p.team,
		       p.got_kill, p.got_assist, p.survived, p.was_traded, p.kast_earned,
		       p.is_opening_kill, p.is_opening_death, p.is_trade_kill, p.is_trade_death,
		       p.kills, p.assists, p.damage, p.unused_utility, p.buy_type,
		       p.is_post_plant, p.is_in_clutch, p.clutch_enemy_count, p.won_round
		FROM player_round_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE p.steam_id = ?
		ORDER BY d.match_date ASC, p.demo_hash ASC, p.round_number ASC`,
		steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerRoundStats
	for rows.Next() {
		var s model.PlayerRoundStats
		var teamStr string
		var gotKill, gotAssist, survived, wasTraded, kastEarned int
		var isOpeningKill, isOpeningDeath, isTradeKill, isTradeDeath int
		var isPostPlant, isInClutch, wonRound int
		if err := rows.Scan(
			&s.DemoHash, &s.RoundNumber, &teamStr,
			&gotKill, &gotAssist, &survived, &wasTraded, &kastEarned,
			&isOpeningKill, &isOpeningDeath, &isTradeKill, &isTradeDeath,
			&s.Kills, &s.Assists, &s.Damage, &s.UnusedUtility, &s.BuyType,
			&isPostPlant, &isInClutch, &s.ClutchEnemyCount, &wonRound,
		); err != nil {
			return nil, err
		}
		s.SteamID = steamID
		s.Team = parseTeam(teamStr)
		s.GotKill = gotKill != 0
		s.GotAssist = gotAssist != 0
		s.Survived = survived != 0
		s.WasTraded = wasTraded != 0
		s.KASTEarned = kastEarned != 0
		s.IsOpeningKill = isOpeningKill != 0
		s.IsOpeningDeath = isOpeningDeath != 0
		s.IsTradeKill = isTradeKill != 0
		s.IsTradeDeath = isTradeDeath != 0
		s.IsPostPlant = isPostPlant != 0
		s.IsInClutch = isInClutch != 0
		s.WonRound = wonRound != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetAllPlayerWeaponStats returns every per-weapon row for a given SteamID64
// across all demos. Used for sniper-share (AWP/SSG) and utility-kill totals.
func (db *DB) GetAllPlayerWeaponStats(steamID uint64) ([]model.PlayerWeaponStats, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT demo_hash, weapon, kills, headshot_kills, assists, deaths, damage, hits
		FROM player_weapon_stats
		WHERE steam_id = ?`,
		steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerWeaponStats
	for rows.Next() {
		var s model.PlayerWeaponStats
		if err := rows.Scan(
			&s.DemoHash, &s.Weapon,
			&s.Kills, &s.HeadshotKills, &s.Assists, &s.Deaths, &s.Damage, &s.Hits,
		); err != nil {
			return nil, err
		}
		s.SteamID = steamID
		out = append(out, s)
	}
	return out, rows.Err()
}

// SniperRoundKill is one row per (demo, round) where the player landed at
// least one AWP or SSG-08 kill. Multiple kills in the same round collapse
// into one row with KillCount = N.
type SniperRoundKill struct {
	DemoHash    string
	RoundNumber int
	KillCount   int
}

// GetPlayerSniperKillsByRound returns per-(demo, round) sniper-kill counts
// for the player. Source: player_death_events (filter weapon IN AWP/SSG 08).
// Only covers demos with PDE rows populated; see file header.
func (db *DB) GetPlayerSniperKillsByRound(steamID uint64) ([]SniperRoundKill, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT demo_hash, round_number, COUNT(*) AS kc
		FROM player_death_events
		WHERE killer_id = ? AND weapon IN ('AWP', 'SSG 08')
		GROUP BY demo_hash, round_number`,
		steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SniperRoundKill
	for rows.Next() {
		var r SniperRoundKill
		if err := rows.Scan(&r.DemoHash, &r.RoundNumber, &r.KillCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetPlayerUtilityKillsByDemo returns per-demo counts of kills credited to
// HE grenades, molotovs, or incendiaries. Source: player_death_events.
// Only covers demos with PDE rows populated; see file header.
func (db *DB) GetPlayerUtilityKillsByDemo(steamID uint64) (map[string]int, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT demo_hash, COUNT(*) AS kc
		FROM player_death_events
		WHERE killer_id = ? AND weapon IN ('HE Grenade', 'Molotov', 'Incendiary Grenade')
		GROUP BY demo_hash`,
		steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var demoHash string
		var kc int
		if err := rows.Scan(&demoHash, &kc); err != nil {
			return nil, err
		}
		out[demoHash] = kc
	}
	return out, rows.Err()
}

// GetPlayerFlashesThrownByDemo returns per-demo counts of flashbangs thrown
// by the player. Source: grenade_events (grenade_type='flash').
// Only covers demos with grenade_events rows populated; see file header.
func (db *DB) GetPlayerFlashesThrownByDemo(steamID uint64) (map[string]int, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT demo_hash, COUNT(*) AS c
		FROM grenade_events
		WHERE thrower_id = ? AND grenade_type = 'flash'
		GROUP BY demo_hash`,
		steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var demoHash string
		var c int
		if err := rows.Scan(&demoHash, &c); err != nil {
			return nil, err
		}
		out[demoHash] = c
	}
	return out, rows.Err()
}

// GetPlayerOpponentFlashSecondsByDemo returns per-demo total seconds of
// opponent blind time produced by the player's flashbangs (team flashes
// excluded). Source: flash_events (is_team_flash=0).
// Only covers demos with flash_events rows populated; see file header.
func (db *DB) GetPlayerOpponentFlashSecondsByDemo(steamID uint64) (map[string]float64, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT demo_hash, SUM(duration_s) AS s
		FROM flash_events
		WHERE thrower_id = ? AND is_team_flash = 0
		GROUP BY demo_hash`,
		steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]float64)
	for rows.Next() {
		var demoHash string
		var s float64
		if err := rows.Scan(&demoHash, &s); err != nil {
			return nil, err
		}
		out[demoHash] = s
	}
	return out, rows.Err()
}
