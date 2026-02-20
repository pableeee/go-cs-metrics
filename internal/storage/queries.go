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
		INSERT OR REPLACE INTO demos(hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		summary.DemoHash, summary.MapName, summary.MatchDate, summary.MatchType,
		summary.Tickrate, summary.CTScore, summary.TScore,
		summary.Tier, boolInt(summary.IsBaseline),
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
			kast_rounds, unused_utility,
			crosshair_encounters, crosshair_median_deg, crosshair_pct_under5,
			crosshair_median_pitch_deg, crosshair_median_yaw_deg,
			duel_wins, duel_losses,
			median_exposure_win_ms, median_exposure_loss_ms,
			median_hits_to_kill, first_hit_hs_rate,
			median_correction_deg, pct_correction_under2_deg,
			awp_deaths, awp_deaths_dry, awp_deaths_repeek, awp_deaths_isolated,
			effective_flashes
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
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
			s.CrosshairEncounters, s.CrosshairMedianDeg, s.CrosshairPctUnder5,
			s.CrosshairMedianPitchDeg, s.CrosshairMedianYawDeg,
			s.DuelWins, s.DuelLosses,
			s.MedianExposureWinMs, s.MedianExposureLossMs,
			s.MedianHitsToKill, s.FirstHitHSRate,
			s.MedianCorrectionDeg, s.PctCorrectionUnder2Deg,
			s.AWPDeaths, s.AWPDeathsDry, s.AWPDeathsRePeek, s.AWPDeathsIsolated,
			s.EffectiveFlashes,
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
		SELECT hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline
		FROM demos ORDER BY match_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.MatchSummary
	for rows.Next() {
		var s model.MatchSummary
		var isBaselineInt int
		if err := rows.Scan(&s.DemoHash, &s.MapName, &s.MatchDate, &s.MatchType,
			&s.Tickrate, &s.CTScore, &s.TScore, &s.Tier, &isBaselineInt); err != nil {
			return nil, err
		}
		s.IsBaseline = isBaselineInt != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetDemoByPrefix finds the first demo whose hash starts with the given prefix.
func (db *DB) GetDemoByPrefix(prefix string) (*model.MatchSummary, error) {
	var s model.MatchSummary
	var isBaselineInt int
	err := db.conn.QueryRow(`
		SELECT hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline
		FROM demos WHERE hash LIKE ? LIMIT 1`, prefix+"%").
		Scan(&s.DemoHash, &s.MapName, &s.MatchDate, &s.MatchType,
			&s.Tickrate, &s.CTScore, &s.TScore, &s.Tier, &isBaselineInt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.IsBaseline = isBaselineInt != 0
	return &s, nil
}

// GetPlayerMatchStats returns all player stats for a demo hash.
func (db *DB) GetPlayerMatchStats(demoHash string) ([]model.PlayerMatchStats, error) {
	rows, err := db.conn.Query(`
		SELECT steam_id, name, team,
		       kills, assists, deaths, headshot_kills, flash_assists,
		       total_damage, utility_damage, rounds_played,
		       opening_kills, opening_deaths, trade_kills, trade_deaths,
		       kast_rounds, unused_utility,
		       crosshair_encounters, crosshair_median_deg, crosshair_pct_under5,
		       crosshair_median_pitch_deg, crosshair_median_yaw_deg,
		       duel_wins, duel_losses,
		       median_exposure_win_ms, median_exposure_loss_ms,
		       median_hits_to_kill, first_hit_hs_rate,
		       median_correction_deg, pct_correction_under2_deg,
		       awp_deaths, awp_deaths_dry, awp_deaths_repeek, awp_deaths_isolated,
		       effective_flashes
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
			&s.CrosshairEncounters, &s.CrosshairMedianDeg, &s.CrosshairPctUnder5,
			&s.CrosshairMedianPitchDeg, &s.CrosshairMedianYawDeg,
			&s.DuelWins, &s.DuelLosses,
			&s.MedianExposureWinMs, &s.MedianExposureLossMs,
			&s.MedianHitsToKill, &s.FirstHitHSRate,
			&s.MedianCorrectionDeg, &s.PctCorrectionUnder2Deg,
			&s.AWPDeaths, &s.AWPDeathsDry, &s.AWPDeathsRePeek, &s.AWPDeathsIsolated,
			&s.EffectiveFlashes,
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

// GetAllPlayerMatchStats returns all stored match-stats rows for a given SteamID64 across all demos,
// joined with the demos table to include map_name.
func (db *DB) GetAllPlayerMatchStats(steamID uint64) ([]model.PlayerMatchStats, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT p.demo_hash, d.map_name, p.name, p.team,
		       p.kills, p.assists, p.deaths, p.headshot_kills, p.flash_assists,
		       p.total_damage, p.utility_damage, p.rounds_played,
		       p.opening_kills, p.opening_deaths, p.trade_kills, p.trade_deaths,
		       p.kast_rounds, p.unused_utility,
		       p.crosshair_encounters, p.crosshair_median_deg, p.crosshair_pct_under5,
		       p.crosshair_median_pitch_deg, p.crosshair_median_yaw_deg,
		       p.duel_wins, p.duel_losses,
		       p.median_exposure_win_ms, p.median_exposure_loss_ms,
		       p.median_hits_to_kill, p.first_hit_hs_rate,
		       p.median_correction_deg, p.pct_correction_under2_deg,
		       p.awp_deaths, p.awp_deaths_dry, p.awp_deaths_repeek, p.awp_deaths_isolated,
		       p.effective_flashes
		FROM player_match_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE p.steam_id = ?`, steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerMatchStats
	for rows.Next() {
		var s model.PlayerMatchStats
		var teamStr string
		if err := rows.Scan(
			&s.DemoHash, &s.MapName, &s.Name, &teamStr,
			&s.Kills, &s.Assists, &s.Deaths, &s.HeadshotKills, &s.FlashAssists,
			&s.TotalDamage, &s.UtilityDamage, &s.RoundsPlayed,
			&s.OpeningKills, &s.OpeningDeaths, &s.TradeKills, &s.TradeDeaths,
			&s.KASTRounds, &s.UnusedUtility,
			&s.CrosshairEncounters, &s.CrosshairMedianDeg, &s.CrosshairPctUnder5,
			&s.CrosshairMedianPitchDeg, &s.CrosshairMedianYawDeg,
			&s.DuelWins, &s.DuelLosses,
			&s.MedianExposureWinMs, &s.MedianExposureLossMs,
			&s.MedianHitsToKill, &s.FirstHitHSRate,
			&s.MedianCorrectionDeg, &s.PctCorrectionUnder2Deg,
			&s.AWPDeaths, &s.AWPDeathsDry, &s.AWPDeathsRePeek, &s.AWPDeathsIsolated,
			&s.EffectiveFlashes,
		); err != nil {
			return nil, err
		}
		s.SteamID = steamID
		s.Team = parseTeam(teamStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetAllPlayerDuelSegments returns all stored duel segment rows for a given SteamID64 across all demos.
func (db *DB) GetAllPlayerDuelSegments(steamID uint64) ([]model.PlayerDuelSegment, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT demo_hash, weapon_bucket, distance_bin,
		       duel_count, first_hit_count, first_hit_hs_count,
		       median_corr_deg, median_sight_deg, median_expo_win_ms
		FROM player_duel_segments WHERE steam_id = ?`, steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerDuelSegment
	for rows.Next() {
		var s model.PlayerDuelSegment
		if err := rows.Scan(
			&s.DemoHash, &s.WeaponBucket, &s.DistanceBin,
			&s.DuelCount, &s.FirstHitCount, &s.FirstHitHSCount,
			&s.MedianCorrDeg, &s.MedianSightDeg, &s.MedianExpoWinMs,
		); err != nil {
			return nil, err
		}
		s.SteamID = steamID
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertPlayerDuelSegments bulk-inserts FHHS segments in a transaction.
func (db *DB) InsertPlayerDuelSegments(segs []model.PlayerDuelSegment) error {
	if len(segs) == 0 {
		return nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO player_duel_segments(
			demo_hash, steam_id, weapon_bucket, distance_bin,
			duel_count, first_hit_count, first_hit_hs_count,
			median_corr_deg, median_sight_deg, median_expo_win_ms
		) VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range segs {
		_, err = stmt.Exec(
			s.DemoHash, strconv.FormatUint(s.SteamID, 10), s.WeaponBucket, s.DistanceBin,
			s.DuelCount, s.FirstHitCount, s.FirstHitHSCount,
			s.MedianCorrDeg, s.MedianSightDeg, s.MedianExpoWinMs,
		)
		if err != nil {
			return fmt.Errorf("insert player_duel_segments for %d/%s/%s: %w", s.SteamID, s.WeaponBucket, s.DistanceBin, err)
		}
	}
	return tx.Commit()
}

// GetPlayerDuelSegments returns all FHHS segments for a demo hash.
func (db *DB) GetPlayerDuelSegments(demoHash string) ([]model.PlayerDuelSegment, error) {
	rows, err := db.conn.Query(`
		SELECT steam_id, weapon_bucket, distance_bin,
		       duel_count, first_hit_count, first_hit_hs_count,
		       median_corr_deg, median_sight_deg, median_expo_win_ms
		FROM player_duel_segments WHERE demo_hash = ?`, demoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerDuelSegment
	for rows.Next() {
		var s model.PlayerDuelSegment
		var steamIDStr string
		if err := rows.Scan(
			&steamIDStr, &s.WeaponBucket, &s.DistanceBin,
			&s.DuelCount, &s.FirstHitCount, &s.FirstHitHSCount,
			&s.MedianCorrDeg, &s.MedianSightDeg, &s.MedianExpoWinMs,
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
