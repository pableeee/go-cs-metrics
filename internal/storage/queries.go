package storage

import (
	"database/sql"
	"fmt"
	"strconv"

	"github.com/pable/go-cs-metrics/internal/model"
)

// DemoExists returns true if a demo with the given hash is already stored.
func (db *DB) DemoExists(hash string) (bool, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(1) FROM demos WHERE hash = ?", hash).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// InsertDemo inserts a demo record. Uses INSERT OR REPLACE for idempotency.
func (db *DB) InsertDemo(summary model.MatchSummary) error {
	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO demos(hash, map_name, match_date, match_type, tickrate, ct_score, t_score)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		summary.DemoHash, summary.MapName, summary.MatchDate, summary.MatchType,
		summary.Tickrate, summary.CTScore, summary.TScore,
	)
	return err
}

// InsertPlayerMatchStats bulk-inserts player match stats in a transaction.
func (db *DB) InsertPlayerMatchStats(stats []model.PlayerMatchStats) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO player_match_stats(
			demo_hash, steam_id, name, team,
			kills, assists, deaths, headshot_kills, flash_assists,
			total_damage, utility_damage, rounds_played,
			opening_kills, opening_deaths, trade_kills, trade_deaths,
			kast_rounds, unused_utility
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range stats {
		_, err = stmt.Exec(
			s.DemoHash, strconv.FormatUint(s.SteamID, 10), s.Name, s.Team.String(),
			s.Kills, s.Assists, s.Deaths, s.HeadshotKills, s.FlashAssists,
			s.TotalDamage, s.UtilityDamage, s.RoundsPlayed,
			s.OpeningKills, s.OpeningDeaths, s.TradeKills, s.TradeDeaths,
			s.KASTRounds, s.UnusedUtility,
		)
		if err != nil {
			return fmt.Errorf("insert player_match_stats for %d: %w", s.SteamID, err)
		}
	}
	return tx.Commit()
}

// InsertPlayerRoundStats bulk-inserts per-round stats in a transaction.
func (db *DB) InsertPlayerRoundStats(stats []model.PlayerRoundStats) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO player_round_stats(
			demo_hash, steam_id, round_number, team,
			got_kill, got_assist, survived, was_traded, kast_earned,
			is_opening_kill, is_opening_death, is_trade_kill, is_trade_death,
			kills, assists, damage, unused_utility
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range stats {
		_, err = stmt.Exec(
			s.DemoHash, strconv.FormatUint(s.SteamID, 10), s.RoundNumber, s.Team.String(),
			boolInt(s.GotKill), boolInt(s.GotAssist), boolInt(s.Survived),
			boolInt(s.WasTraded), boolInt(s.KASTEarned),
			boolInt(s.IsOpeningKill), boolInt(s.IsOpeningDeath),
			boolInt(s.IsTradeKill), boolInt(s.IsTradeDeath),
			s.Kills, s.Assists, s.Damage, s.UnusedUtility,
		)
		if err != nil {
			return fmt.Errorf("insert player_round_stats: %w", err)
		}
	}
	return tx.Commit()
}

// ListDemos returns all stored match summaries ordered by match_date desc.
func (db *DB) ListDemos() ([]model.MatchSummary, error) {
	rows, err := db.conn.Query(`
		SELECT hash, map_name, match_date, match_type, tickrate, ct_score, t_score
		FROM demos ORDER BY match_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.MatchSummary
	for rows.Next() {
		var s model.MatchSummary
		if err := rows.Scan(&s.DemoHash, &s.MapName, &s.MatchDate, &s.MatchType,
			&s.Tickrate, &s.CTScore, &s.TScore); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetDemoByPrefix finds the first demo whose hash starts with the given prefix.
func (db *DB) GetDemoByPrefix(prefix string) (*model.MatchSummary, error) {
	var s model.MatchSummary
	err := db.conn.QueryRow(`
		SELECT hash, map_name, match_date, match_type, tickrate, ct_score, t_score
		FROM demos WHERE hash LIKE ? LIMIT 1`, prefix+"%").
		Scan(&s.DemoHash, &s.MapName, &s.MatchDate, &s.MatchType,
			&s.Tickrate, &s.CTScore, &s.TScore)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetPlayerMatchStats returns all player stats for a demo hash.
func (db *DB) GetPlayerMatchStats(demoHash string) ([]model.PlayerMatchStats, error) {
	rows, err := db.conn.Query(`
		SELECT steam_id, name, team,
		       kills, assists, deaths, headshot_kills, flash_assists,
		       total_damage, utility_damage, rounds_played,
		       opening_kills, opening_deaths, trade_kills, trade_deaths,
		       kast_rounds, unused_utility
		FROM player_match_stats WHERE demo_hash = ?
		ORDER BY kills DESC`, demoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerMatchStats
	for rows.Next() {
		var s model.PlayerMatchStats
		var steamIDStr, teamStr string
		if err := rows.Scan(
			&steamIDStr, &s.Name, &teamStr,
			&s.Kills, &s.Assists, &s.Deaths, &s.HeadshotKills, &s.FlashAssists,
			&s.TotalDamage, &s.UtilityDamage, &s.RoundsPlayed,
			&s.OpeningKills, &s.OpeningDeaths, &s.TradeKills, &s.TradeDeaths,
			&s.KASTRounds, &s.UnusedUtility,
		); err != nil {
			return nil, err
		}
		s.DemoHash = demoHash
		s.SteamID, _ = strconv.ParseUint(steamIDStr, 10, 64)
		s.Team = parseTeam(teamStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertPlayerWeaponStats bulk-inserts per-weapon stats in a transaction.
func (db *DB) InsertPlayerWeaponStats(stats []model.PlayerWeaponStats) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO player_weapon_stats(
			demo_hash, steam_id, weapon,
			kills, headshot_kills, assists, deaths, damage, hits
		) VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range stats {
		_, err = stmt.Exec(
			s.DemoHash, strconv.FormatUint(s.SteamID, 10), s.Weapon,
			s.Kills, s.HeadshotKills, s.Assists, s.Deaths, s.Damage, s.Hits,
		)
		if err != nil {
			return fmt.Errorf("insert player_weapon_stats for %d/%s: %w", s.SteamID, s.Weapon, err)
		}
	}
	return tx.Commit()
}

// GetPlayerWeaponStats returns all weapon stats for a demo, ordered by kills DESC then damage DESC.
func (db *DB) GetPlayerWeaponStats(demoHash string) ([]model.PlayerWeaponStats, error) {
	rows, err := db.conn.Query(`
		SELECT steam_id, weapon, kills, headshot_kills, assists, deaths, damage, hits
		FROM player_weapon_stats WHERE demo_hash = ?
		ORDER BY kills DESC, damage DESC`, demoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerWeaponStats
	for rows.Next() {
		var s model.PlayerWeaponStats
		var steamIDStr string
		if err := rows.Scan(
			&steamIDStr, &s.Weapon,
			&s.Kills, &s.HeadshotKills, &s.Assists, &s.Deaths, &s.Damage, &s.Hits,
		); err != nil {
			return nil, err
		}
		s.DemoHash = demoHash
		s.SteamID, _ = strconv.ParseUint(steamIDStr, 10, 64)
		out = append(out, s)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTeam(s string) model.Team {
	switch s {
	case "T":
		return model.TeamT
	case "CT":
		return model.TeamCT
	default:
		return model.TeamUnknown
	}
}
