package storage

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
// MapName is normalized to title-case (e.g. "de_mirage" → "Mirage") before storage
// so all reads return a consistent name regardless of what the demo header contains.
func (db *DB) InsertDemo(summary model.MatchSummary) error {
	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO demos(hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline, event_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		summary.DemoHash, normalizeMapName(summary.MapName), summary.MatchDate, summary.MatchType,
		summary.Tickrate, summary.CTScore, summary.TScore,
		summary.Tier, boolInt(summary.IsBaseline), summary.EventID,
	)
	return err
}

// normalizeMapName converts a CS2 map identifier to the title-case name used
// throughout the pipeline (e.g. "de_mirage" → "Mirage", "de_dust2" → "Dust2").
// The function is idempotent: already-normalized names are returned unchanged.
func normalizeMapName(name string) string {
	name = strings.TrimPrefix(name, "de_")
	if len(name) == 0 {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
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
			effective_flashes,
			role, median_ttk_ms, median_ttd_ms, one_tap_kills, counter_strafe_pct,
			rounds_won, median_trade_kill_delay_ms, median_trade_death_delay_ms
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
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
			s.Role, s.MedianTTKMs, s.MedianTTDMs, s.OneTapKills, s.CounterStrafePercent,
			s.RoundsWon, s.MedianTradeKillDelayMs, s.MedianTradeDeathDelayMs,
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
			kills, assists, damage, unused_utility, buy_type,
			is_post_plant, is_in_clutch, clutch_enemy_count, won_round
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
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
			s.Kills, s.Assists, s.Damage, s.UnusedUtility, s.BuyType,
			boolInt(s.IsPostPlant), boolInt(s.IsInClutch), s.ClutchEnemyCount,
			boolInt(s.WonRound),
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
		SELECT hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline, event_id
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
			&s.Tickrate, &s.CTScore, &s.TScore, &s.Tier, &isBaselineInt, &s.EventID); err != nil {
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
		SELECT hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline, event_id
		FROM demos WHERE hash LIKE ? LIMIT 1`, prefix+"%").
		Scan(&s.DemoHash, &s.MapName, &s.MatchDate, &s.MatchType,
			&s.Tickrate, &s.CTScore, &s.TScore, &s.Tier, &isBaselineInt, &s.EventID)
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
		       effective_flashes,
		       role, median_ttk_ms, median_ttd_ms, one_tap_kills, counter_strafe_pct
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
			&s.Role, &s.MedianTTKMs, &s.MedianTTDMs, &s.OneTapKills, &s.CounterStrafePercent,
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

// GetPlayerSideStats returns per-side (CT/T) basic stats for all players in a demo,
// derived by aggregating player_round_stats. Deaths = rounds played - rounds survived.
func (db *DB) GetPlayerSideStats(demoHash string) ([]model.PlayerSideStats, error) {
	rows, err := db.conn.Query(`
		SELECT p.steam_id, m.name, p.team,
		       SUM(p.kills), SUM(p.assists),
		       COUNT(*) - SUM(p.survived),
		       SUM(p.damage),
		       COUNT(*),
		       SUM(p.kast_earned),
		       SUM(p.is_opening_kill), SUM(p.is_opening_death),
		       SUM(p.is_trade_kill),   SUM(p.is_trade_death)
		FROM player_round_stats p
		JOIN player_match_stats m ON m.demo_hash = p.demo_hash AND m.steam_id = p.steam_id
		WHERE p.demo_hash = ?
		GROUP BY p.steam_id, p.team
		ORDER BY m.kills DESC, p.steam_id ASC, p.team ASC`, demoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerSideStats
	for rows.Next() {
		var s model.PlayerSideStats
		var steamIDStr, teamStr string
		if err := rows.Scan(
			&steamIDStr, &s.Name, &teamStr,
			&s.Kills, &s.Assists, &s.Deaths,
			&s.TotalDamage, &s.RoundsPlayed, &s.KASTRounds,
			&s.OpeningKills, &s.OpeningDeaths,
			&s.TradeKills, &s.TradeDeaths,
		); err != nil {
			return nil, err
		}
		s.SteamID, _ = strconv.ParseUint(steamIDStr, 10, 64)
		s.Team = parseTeam(teamStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetPlayerRoundStats returns per-round stats for a single player in a single demo,
// ordered by round number ascending.
func (db *DB) GetPlayerRoundStats(demoHash string, steamID uint64) ([]model.PlayerRoundStats, error) {
	steamIDStr := strconv.FormatUint(steamID, 10)
	rows, err := db.conn.Query(`
		SELECT round_number, team,
		       got_kill, got_assist, survived, was_traded, kast_earned,
		       is_opening_kill, is_opening_death, is_trade_kill, is_trade_death,
		       kills, assists, damage, unused_utility, buy_type,
		       is_post_plant, is_in_clutch, clutch_enemy_count, won_round
		FROM player_round_stats
		WHERE demo_hash = ? AND steam_id = ?
		ORDER BY round_number ASC`,
		demoHash, steamIDStr)
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
			&s.RoundNumber, &teamStr,
			&gotKill, &gotAssist, &survived, &wasTraded, &kastEarned,
			&isOpeningKill, &isOpeningDeath, &isTradeKill, &isTradeDeath,
			&s.Kills, &s.Assists, &s.Damage, &s.UnusedUtility, &s.BuyType,
			&isPostPlant, &isInClutch, &s.ClutchEnemyCount, &wonRound,
		); err != nil {
			return nil, err
		}
		s.DemoHash = demoHash
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
		SELECT p.demo_hash, d.map_name, d.match_date, p.name, p.team,
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
		       p.effective_flashes,
		       p.role, p.median_ttk_ms, p.median_ttd_ms, p.one_tap_kills, p.counter_strafe_pct,
		       p.rounds_won, p.median_trade_kill_delay_ms, p.median_trade_death_delay_ms
		FROM player_match_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE p.steam_id = ?
		ORDER BY d.match_date ASC, p.demo_hash ASC`, steamIDStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.PlayerMatchStats
	for rows.Next() {
		var s model.PlayerMatchStats
		var teamStr string
		if err := rows.Scan(
			&s.DemoHash, &s.MapName, &s.MatchDate, &s.Name, &teamStr,
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
			&s.Role, &s.MedianTTKMs, &s.MedianTTDMs, &s.OneTapKills, &s.CounterStrafePercent,
			&s.RoundsWon, &s.MedianTradeKillDelayMs, &s.MedianTradeDeathDelayMs,
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

// GetClutchStatsByDemo returns per-player clutch attempt/win counts for a single
// demo, keyed by SteamID. No schema changes needed — reads existing player_round_stats.
func (db *DB) GetClutchStatsByDemo(demoHash string) (map[uint64]*model.PlayerClutchMatchStats, error) {
	rows, err := db.conn.Query(`
		SELECT steam_id, clutch_enemy_count, survived, COUNT(*) AS cnt
		FROM player_round_stats
		WHERE demo_hash = ? AND is_in_clutch = 1
		GROUP BY steam_id, clutch_enemy_count, survived`,
		demoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[uint64]*model.PlayerClutchMatchStats)
	for rows.Next() {
		var steamIDStr string
		var enemyCount, survived, cnt int
		if err := rows.Scan(&steamIDStr, &enemyCount, &survived, &cnt); err != nil {
			return nil, err
		}
		id, err := strconv.ParseUint(steamIDStr, 10, 64)
		if err != nil {
			continue
		}
		if result[id] == nil {
			result[id] = &model.PlayerClutchMatchStats{DemoHash: demoHash, SteamID: id}
		}
		if enemyCount >= 1 && enemyCount <= 5 {
			result[id].Attempts[enemyCount] += cnt
			if survived == 1 {
				result[id].Wins[enemyCount] += cnt
			}
		}
	}
	return result, rows.Err()
}

// GetPlayerClutchStatsByMatch returns per-match clutch attempt/win counts for a
// given SteamID64, keyed by demo hash. No schema changes needed — reads existing
// player_round_stats rows where is_in_clutch = 1.
func (db *DB) GetPlayerClutchStatsByMatch(steamID uint64) (map[string]*model.PlayerClutchMatchStats, error) {
	rows, err := db.conn.Query(`
		SELECT demo_hash, clutch_enemy_count, survived, COUNT(*) AS cnt
		FROM player_round_stats
		WHERE steam_id = ? AND is_in_clutch = 1
		GROUP BY demo_hash, clutch_enemy_count, survived`,
		strconv.FormatUint(steamID, 10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*model.PlayerClutchMatchStats)
	for rows.Next() {
		var demoHash string
		var enemyCount, survived, cnt int
		if err := rows.Scan(&demoHash, &enemyCount, &survived, &cnt); err != nil {
			return nil, err
		}
		if result[demoHash] == nil {
			result[demoHash] = &model.PlayerClutchMatchStats{DemoHash: demoHash, SteamID: steamID}
		}
		if enemyCount >= 1 && enemyCount <= 5 {
			result[demoHash].Attempts[enemyCount] += cnt
			if survived == 1 {
				result[demoHash].Wins[enemyCount] += cnt
			}
		}
	}
	return result, rows.Err()
}

// DBOverview holds top-level statistics about the entire database.
type DBOverview struct {
	TotalMatches  int
	UniqueMaps    int
	UniquePlayers int
	TotalRounds   int
	EarliestMatch string
	LatestMatch   string
}

// MapStat holds per-map match and win counts across all stored demos.
type MapStat struct {
	MapName string
	Matches int
	CTWins  int
	TWins   int
}

// PlayerFrequency holds a player's match count and cross-match aggregate stats.
type PlayerFrequency struct {
	Name    string
	SteamID string
	Matches int
	AvgKD   float64
	AvgADR  float64
	AvgKAST float64
}

// MatchTypeCount holds a match type label and how many demos use it.
type MatchTypeCount struct {
	MatchType string
	Matches   int
}

// GetDBOverview returns high-level statistics about the entire database.
func (db *DB) GetDBOverview() (DBOverview, error) {
	var ov DBOverview
	err := db.conn.QueryRow(`
		SELECT COUNT(*), COUNT(DISTINCT map_name),
		       COALESCE(MIN(match_date), ''), COALESCE(MAX(match_date), ''),
		       COALESCE(SUM(ct_score + t_score), 0)
		FROM demos`).Scan(
		&ov.TotalMatches, &ov.UniqueMaps,
		&ov.EarliestMatch, &ov.LatestMatch, &ov.TotalRounds)
	if err != nil {
		return ov, err
	}
	err = db.conn.QueryRow(
		`SELECT COUNT(DISTINCT steam_id) FROM player_match_stats`).Scan(&ov.UniquePlayers)
	return ov, err
}

// GetMapStats returns match counts and round-win breakdowns per map, ordered by match count desc.
func (db *DB) GetMapStats() ([]MapStat, error) {
	rows, err := db.conn.Query(`
		SELECT map_name, COUNT(*) AS matches, SUM(ct_score) AS ct_wins, SUM(t_score) AS t_wins
		FROM demos
		GROUP BY map_name
		ORDER BY matches DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MapStat
	for rows.Next() {
		var s MapStat
		if err := rows.Scan(&s.MapName, &s.Matches, &s.CTWins, &s.TWins); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetTopPlayersByMatches returns the top N players ordered by number of demos they appear in,
// with averaged K/D, ADR, and KAST% across those matches.
func (db *DB) GetTopPlayersByMatches(limit int) ([]PlayerFrequency, error) {
	rows, err := db.conn.Query(`
		SELECT name, steam_id, COUNT(*) AS matches,
		       ROUND(COALESCE(AVG(CAST(kills AS REAL) / NULLIF(deaths, 0)), 0), 2),
		       ROUND(COALESCE(AVG(CAST(total_damage AS REAL) / NULLIF(rounds_played, 0)), 0), 1),
		       ROUND(100.0 * COALESCE(AVG(CAST(kast_rounds AS REAL) / NULLIF(rounds_played, 0)), 0), 1)
		FROM player_match_stats
		GROUP BY steam_id
		ORDER BY matches DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlayerFrequency
	for rows.Next() {
		var p PlayerFrequency
		if err := rows.Scan(&p.Name, &p.SteamID, &p.Matches, &p.AvgKD, &p.AvgADR, &p.AvgKAST); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlayerRatingRow holds a player's aggregated stats and computed rating proxy,
// used for top-N ranking in the player command.
type PlayerRatingRow struct {
	SteamID string
	Name    string
	Rating  float64
	Matches int
}

// ratingProxy computes the community approximation of HLTV Rating 2.0.
//
//	Impact = 2.13*KPR + 0.42*APR − 0.41
//	Rating ≈ 0.0073*KAST% + 0.3591*KPR − 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
func ratingProxy(kills, assists, deaths, rounds, kastRounds, damage int) float64 {
	if rounds == 0 {
		return 0
	}
	kpr := float64(kills) / float64(rounds)
	apr := float64(assists) / float64(rounds)
	dpr := float64(deaths) / float64(rounds)
	kast := 100.0 * float64(kastRounds) / float64(rounds)
	adr := float64(damage) / float64(rounds)
	impact := 2.13*kpr + 0.42*apr - 0.41
	return 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impact + 0.0032*adr + 0.1587
}

// GetTopPlayersByRating returns up to limit players ranked by the Rating 2.0 proxy,
// computed from aggregated match stats across the filtered demo set. mapFilter must
// be de_-stripped and lowercased (e.g. "mirage"); since is a YYYY-MM-DD cutoff.
// Players with fewer than minMatches qualifying demos are excluded.
func (db *DB) GetTopPlayersByRating(limit, minMatches int, mapFilter, since string) ([]PlayerRatingRow, error) {
	conds := ""
	args := []any{}
	if mapFilter != "" {
		conds += " AND LOWER(REPLACE(d.map_name, 'de_', '')) = ?"
		args = append(args, mapFilter)
	}
	if since != "" {
		conds += " AND d.match_date >= ?"
		args = append(args, since)
	}

	rows, err := db.conn.Query(`
		SELECT p.steam_id, p.name,
		       SUM(p.kills), SUM(p.assists), SUM(p.deaths),
		       SUM(p.rounds_played), SUM(p.kast_rounds), SUM(p.total_damage),
		       COUNT(DISTINCT p.demo_hash)
		FROM player_match_stats p
		JOIN demos d ON d.hash = p.demo_hash
		WHERE 1=1`+conds+`
		GROUP BY p.steam_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		steamID string
		name    string
		kills   int
		assists int
		deaths  int
		rounds  int
		kast    int
		damage  int
		matches int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.steamID, &c.name,
			&c.kills, &c.assists, &c.deaths,
			&c.rounds, &c.kast, &c.damage, &c.matches); err != nil {
			return nil, err
		}
		if c.matches >= minMatches {
			candidates = append(candidates, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type rated struct {
		candidate
		rating float64
	}
	ranked := make([]rated, len(candidates))
	for i, c := range candidates {
		ranked[i] = rated{c, ratingProxy(c.kills, c.assists, c.deaths, c.rounds, c.kast, c.damage)}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].rating > ranked[j].rating })

	out := make([]PlayerRatingRow, 0, limit)
	for _, r := range ranked {
		if len(out) >= limit {
			break
		}
		out = append(out, PlayerRatingRow{SteamID: r.steamID, Name: r.name, Rating: r.rating, Matches: r.matches})
	}
	return out, nil
}

// GetMatchTypeCounts returns the number of demos per match type, ordered by count desc.
func (db *DB) GetMatchTypeCounts() ([]MatchTypeCount, error) {
	rows, err := db.conn.Query(`
		SELECT match_type, COUNT(*) AS matches
		FROM demos
		GROUP BY match_type
		ORDER BY matches DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchTypeCount
	for rows.Next() {
		var mt MatchTypeCount
		if err := rows.Scan(&mt.MatchType, &mt.Matches); err != nil {
			return nil, err
		}
		out = append(out, mt)
	}
	return out, rows.Err()
}

// QueryRaw executes an arbitrary SQL query and returns the column names and
// all row values as strings. NULL values are rendered as "NULL".
func (db *DB) QueryRaw(query string) (cols []string, rows [][]string, err error) {
	r, err := db.conn.Query(query)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()

	cols, err = r.Columns()
	if err != nil {
		return nil, nil, err
	}

	for r.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := r.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				row[i] = "NULL"
			} else {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		rows = append(rows, row)
	}
	return cols, rows, r.Err()
}

// boolInt converts a bool to an int (0 or 1) for SQLite storage.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parseTeam converts a team string ("T", "CT") back to a model.Team value.
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
