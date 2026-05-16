package cmd

import (
	"fmt"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// loadRoleStatsForPlayer is the cmd-side helper that loads every input
// buildRoleStats needs from the DB, filters by the caller's set of surviving
// demo hashes, and returns the assembled PlayerRoleStats.
//
// `keep` must contain exactly the demo hashes the caller has already filtered
// `stats` down to (it's the same set used elsewhere in cmd/player to filter
// duel segments and clutch counts). `name` is the display name to attach.
func loadRoleStatsForPlayer(
	db *storage.DB,
	id uint64,
	name string,
	stats []model.PlayerMatchStats,
	clutch model.PlayerClutchMatchStats,
	keep map[string]struct{},
) (model.PlayerRoleStats, error) {
	roundStats, err := db.GetAllPlayerRoundStats(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get round stats: %w", err)
	}
	roundStats = filterRoundStatsByDemo(roundStats, keep)

	weaponStats, err := db.GetAllPlayerWeaponStats(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get weapon stats: %w", err)
	}
	weaponStats = filterWeaponStatsByDemo(weaponStats, keep)

	sniperRounds, err := db.GetPlayerSniperKillsByRound(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get sniper rounds: %w", err)
	}
	sniperRounds = filterSniperRoundsByDemo(sniperRounds, keep)

	utilityKills, err := db.GetPlayerUtilityKillsByDemo(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get utility kills: %w", err)
	}
	utilityKills = filterIntMapByDemo(utilityKills, keep)

	flashesThrown, err := db.GetPlayerFlashesThrownByDemo(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get flashes thrown: %w", err)
	}
	flashesThrown = filterIntMapByDemo(flashesThrown, keep)

	oppFlashSec, err := db.GetPlayerOpponentFlashSecondsByDemo(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get opp flash seconds: %w", err)
	}
	oppFlashSec = filterFloatMapByDemo(oppFlashSec, keep)

	rs := buildRoleStats(stats, roundStats, weaponStats, clutch,
		sniperRounds, utilityKills, flashesThrown, oppFlashSec)
	// Overwrite display name in case the caller computed a normalised version.
	rs.Name = name
	return rs, nil
}

func filterRoundStatsByDemo(rs []model.PlayerRoundStats, keep map[string]struct{}) []model.PlayerRoundStats {
	if len(keep) == 0 {
		return rs
	}
	out := rs[:0]
	for _, r := range rs {
		if _, ok := keep[r.DemoHash]; ok {
			out = append(out, r)
		}
	}
	return out
}

func filterWeaponStatsByDemo(ws []model.PlayerWeaponStats, keep map[string]struct{}) []model.PlayerWeaponStats {
	if len(keep) == 0 {
		return ws
	}
	out := ws[:0]
	for _, w := range ws {
		if _, ok := keep[w.DemoHash]; ok {
			out = append(out, w)
		}
	}
	return out
}

func filterSniperRoundsByDemo(sr []storage.SniperRoundKill, keep map[string]struct{}) []storage.SniperRoundKill {
	if len(keep) == 0 {
		return sr
	}
	out := sr[:0]
	for _, s := range sr {
		if _, ok := keep[s.DemoHash]; ok {
			out = append(out, s)
		}
	}
	return out
}

func filterIntMapByDemo(m map[string]int, keep map[string]struct{}) map[string]int {
	if len(keep) == 0 || len(m) == 0 {
		return m
	}
	out := make(map[string]int, len(m))
	for h, v := range m {
		if _, ok := keep[h]; ok {
			out[h] = v
		}
	}
	return out
}

func filterFloatMapByDemo(m map[string]float64, keep map[string]struct{}) map[string]float64 {
	if len(keep) == 0 || len(m) == 0 {
		return m
	}
	out := make(map[string]float64, len(m))
	for h, v := range m {
		if _, ok := keep[h]; ok {
			out[h] = v
		}
	}
	return out
}

// pistolRound returns true if a round number falls on a pistol round. CS2 (MR12)
// has pistols at rounds 1 and 13. Our corpus is CS2-only as of 2026-05; CSGO
// (MR15: rounds 1 and 16) would need a per-demo format probe to handle.
func pistolRound(roundNumber int) bool {
	return roundNumber == 1 || roundNumber == 13
}

// sniperWeapons matches the weapon-name strings used in player_weapon_stats
// and player_death_events.
func isSniperWeapon(weapon string) bool {
	return weapon == "AWP" || weapon == "SSG 08"
}

// buildRoleStats produces the HLTV-style role decomposition for one player
// from pre-filtered inputs. Callers (cmd/player) must filter every slice /
// map by the same set of surviving demo hashes before calling.
//
// All slices may be empty; the function returns a zero PlayerRoleStats with
// the SteamID populated when stats is non-empty.
func buildRoleStats(
	stats []model.PlayerMatchStats,
	roundStats []model.PlayerRoundStats,
	weaponStats []model.PlayerWeaponStats,
	clutch model.PlayerClutchMatchStats,
	sniperRounds []storage.SniperRoundKill,
	utilityKillsByDemo map[string]int,
	flashesByDemo map[string]int,
	oppFlashSecByDemo map[string]float64,
) model.PlayerRoleStats {
	var rs model.PlayerRoleStats
	if len(stats) == 0 {
		return rs
	}
	rs.SteamID = stats[0].SteamID
	rs.Name = stats[0].Name
	rs.Matches = len(stats)

	// ---- §1 headline (combined) ----
	var (
		kills, assists, deaths                          int
		totalDamage, utilityDamage, kastRounds          int
		roundsPlayed, roundsWon                         int
		openingKills, openingDeaths                     int
		savedByTeammate, savedTeammate, assistedKills   int
	)
	for _, s := range stats {
		kills += s.Kills
		assists += s.Assists
		deaths += s.Deaths
		totalDamage += s.TotalDamage
		utilityDamage += s.UtilityDamage
		kastRounds += s.KASTRounds
		roundsPlayed += s.RoundsPlayed
		roundsWon += s.RoundsWon
		openingKills += s.OpeningKills
		openingDeaths += s.OpeningDeaths
		savedByTeammate += s.SavedByTeammate
		savedTeammate += s.SavedTeammate
		assistedKills += s.AssistedKills
	}
	rs.RoundsPlayed = roundsPlayed
	rs.RoundsWon = roundsWon
	rs.Rating2Combined = rating2(kills, assists, deaths, kastRounds, totalDamage, roundsPlayed)
	if roundsPlayed > 0 {
		rs.KASTPct = 100.0 * float64(kastRounds) / float64(roundsPlayed)
		rs.KPR = float64(kills) / float64(roundsPlayed)
		rs.DPR = float64(deaths) / float64(roundsPlayed)
		rs.ADR = float64(totalDamage) / float64(roundsPlayed)
		rs.UtilityDamagePerRound = float64(utilityDamage) / float64(roundsPlayed)
	}

	// ---- Per-round aggregates: side split + Firepower + Entrying + Opening + Pistol ----
	type sideAccum struct {
		kills, assists, deaths, kastRounds, damage, rp int
	}
	var ct, t sideAccum

	type pistolAccum struct {
		kills, assists, deaths, kastRounds, damage, rp int
	}
	var pistol pistolAccum

	var (
		roundsWithKill                  int
		roundsMultiKill                 int
		killsInWonRounds, dmgInWonRounds int
		openingDeathTraded              int
		supportRounds                   int
		openingAttempts                 int
		winsAfterOpener                 int
	)

	for _, r := range roundStats {
		// Per-side accumulator (CT or T). Skip if neither.
		var sa *sideAccum
		switch r.Team {
		case model.TeamCT:
			sa = &ct
		case model.TeamT:
			sa = &t
		}
		if sa != nil {
			sa.kills += r.Kills
			sa.assists += r.Assists
			if !r.Survived {
				sa.deaths++
			}
			if r.KASTEarned {
				sa.kastRounds++
			}
			sa.damage += r.Damage
			sa.rp++
		}

		// Pistol round subset (per round, agnostic of side; both teams play
		// pistol rounds).
		if pistolRound(r.RoundNumber) {
			pistol.kills += r.Kills
			pistol.assists += r.Assists
			if !r.Survived {
				pistol.deaths++
			}
			if r.KASTEarned {
				pistol.kastRounds++
			}
			pistol.damage += r.Damage
			pistol.rp++
		}

		// §2.1 — firepower per-round counters.
		if r.GotKill {
			roundsWithKill++
		}
		if r.Kills >= 2 {
			roundsMultiKill++
		}
		if r.WonRound {
			killsInWonRounds += r.Kills
			dmgInWonRounds += r.Damage
		}

		// §2.2 — entrying.
		if r.IsOpeningDeath && r.IsTradeDeath {
			openingDeathTraded++
		}
		if !r.GotKill && (r.GotAssist || r.Survived || r.WasTraded) {
			supportRounds++
		}

		// §2.4 — opening.
		if r.IsOpeningKill || r.IsOpeningDeath {
			openingAttempts++
		}
		if r.IsOpeningKill && r.WonRound {
			winsAfterOpener++
		}
	}

	rs.Rating2CT = rating2(ct.kills, ct.assists, ct.deaths, ct.kastRounds, ct.damage, ct.rp)
	rs.Rating2T = rating2(t.kills, t.assists, t.deaths, t.kastRounds, t.damage, t.rp)
	rs.PistolRoundRating = rating2(pistol.kills, pistol.assists, pistol.deaths, pistol.kastRounds, pistol.damage, pistol.rp)

	if roundsPlayed > 0 {
		rs.MultiKillPct = 100.0 * float64(roundsMultiKill) / float64(roundsPlayed)
		rs.RoundsWithKillPct = 100.0 * float64(roundsWithKill) / float64(roundsPlayed)
		rs.OpeningKPR = float64(openingKills) / float64(roundsPlayed)
		rs.OpeningDPR = float64(openingDeaths) / float64(roundsPlayed)
		rs.OpeningAttemptsPct = 100.0 * float64(openingAttempts) / float64(roundsPlayed)
		rs.SupportRoundsPct = 100.0 * float64(supportRounds) / float64(roundsPlayed)
	}
	if roundsWon > 0 {
		rs.KillsPerRoundWin = float64(killsInWonRounds) / float64(roundsWon)
		rs.DamagePerRoundWin = float64(dmgInWonRounds) / float64(roundsWon)
	}
	if openingDeaths > 0 {
		rs.OpeningDeathTradedPct = 100.0 * float64(openingDeathTraded) / float64(openingDeaths)
	}
	if openingAttempts > 0 {
		rs.OpeningSuccessPct = 100.0 * float64(openingKills) / float64(openingAttempts)
	}
	if openingKills > 0 {
		rs.WinAfterOpenPct = 100.0 * float64(winsAfterOpener) / float64(openingKills)
	}

	// ---- §2.3 Trading ----
	if kills > 0 {
		rs.DamagePerKill = float64(totalDamage) / float64(kills)
		rs.AssistedKillsPct = 100.0 * float64(assistedKills) / float64(kills)
	}
	if roundsPlayed > 0 {
		rs.SavedByTeammatePerRound = float64(savedByTeammate) / float64(roundsPlayed)
		rs.SavedTeammatePerRound = float64(savedTeammate) / float64(roundsPlayed)
	}

	// ---- §2.5 Clutching ----
	var clutchPoints float64
	for n := 1; n <= 5; n++ {
		// Weight 1, 2, 4, 8, 16 for 1v1..1v5.
		weight := 1
		for i := 1; i < n; i++ {
			weight *= 2
		}
		clutchPoints += float64(clutch.Wins[n] * weight)
	}
	if roundsPlayed > 0 {
		rs.ClutchPointsPerRound = clutchPoints / float64(roundsPlayed)
	}
	rs.OneVOneAttempts = clutch.Attempts[1]
	rs.OneVOneWins = clutch.Wins[1]
	if rs.OneVOneAttempts > 0 {
		rs.OneVOneWinPct = 100.0 * float64(rs.OneVOneWins) / float64(rs.OneVOneAttempts)
	}

	// Saves per round loss: rows where survived=true AND won_round=false,
	// divided by total round losses. Use the round-stats data we already have.
	var saves, losses int
	for _, r := range roundStats {
		if !r.WonRound {
			losses++
			if r.Survived {
				saves++
			}
		}
	}
	if losses > 0 {
		rs.SavesPerLossPct = 100.0 * float64(saves) / float64(losses)
	}

	// ---- §2.6 Sniping ----
	// Sniper kills come from per-weapon match aggregates (full coverage).
	var sniperKills int
	for _, w := range weaponStats {
		if isSniperWeapon(w.Weapon) {
			sniperKills += w.Kills
		}
	}
	if roundsPlayed > 0 {
		rs.SniperKillsPerRound = float64(sniperKills) / float64(roundsPlayed)
	}
	if kills > 0 {
		rs.SniperKillsPct = 100.0 * float64(sniperKills) / float64(kills)
	}

	// Rounds-with-sniper-kill and multi-kill come from player_death_events
	// (event-table). Build a quick (demoHash, roundNumber) → kills lookup
	// so we can intersect with the player's opening-kill rounds.
	type drKey struct {
		demoHash string
		round    int
	}
	sniperByRound := make(map[drKey]int, len(sniperRounds))
	if len(sniperRounds) > 0 {
		rs.HasSniperData = true
	}
	var roundsWithSniperKill, sniperMultiKillRounds int
	for _, sr := range sniperRounds {
		sniperByRound[drKey{sr.DemoHash, sr.RoundNumber}] = sr.KillCount
		roundsWithSniperKill++
		if sr.KillCount >= 2 {
			sniperMultiKillRounds++
		}
	}
	if roundsPlayed > 0 {
		rs.RoundsWithSniperKillPct = 100.0 * float64(roundsWithSniperKill) / float64(roundsPlayed)
		rs.SniperMultiKillRoundPct = 100.0 * float64(sniperMultiKillRounds) / float64(roundsPlayed)
	}

	// Sniper opening kills: rounds where the player is the opening killer AND
	// at least one of their kills in that round was a sniper kill.
	var sniperOpens int
	for _, r := range roundStats {
		if !r.IsOpeningKill {
			continue
		}
		if _, ok := sniperByRound[drKey{r.DemoHash, r.RoundNumber}]; ok {
			sniperOpens++
		}
	}
	if roundsPlayed > 0 {
		rs.SniperOpeningKillsPerRound = float64(sniperOpens) / float64(roundsPlayed)
	}

	// ---- §2.7 Utility ----
	var utilityKills int
	if len(utilityKillsByDemo) > 0 {
		rs.HasUtilityData = true
	}
	for _, v := range utilityKillsByDemo {
		utilityKills += v
	}
	if roundsPlayed > 0 {
		rs.UtilityKillsPer100R = 100.0 * float64(utilityKills) / float64(roundsPlayed)
	}

	var flashesThrown int
	if len(flashesByDemo) > 0 {
		rs.HasFlashThrowData = true
	}
	for _, v := range flashesByDemo {
		flashesThrown += v
	}
	if roundsPlayed > 0 {
		rs.FlashesThrownPerRound = float64(flashesThrown) / float64(roundsPlayed)
	}

	var oppFlashSec float64
	if len(oppFlashSecByDemo) > 0 {
		rs.HasFlashTimeData = true
	}
	for _, v := range oppFlashSecByDemo {
		oppFlashSec += v
	}
	if roundsPlayed > 0 {
		rs.OppFlashSecPerRound = oppFlashSec / float64(roundsPlayed)
	}

	return rs
}

// rating2 is the Rating 2.0 community-formula proxy. Returns 0 when roundsPlayed
// is 0. Coefficients mirror PlayerAggregate.Rating2.
func rating2(kills, assists, deaths, kastRounds, totalDamage, roundsPlayed int) float64 {
	if roundsPlayed == 0 {
		return 0
	}
	kpr := float64(kills) / float64(roundsPlayed)
	apr := float64(assists) / float64(roundsPlayed)
	dpr := float64(deaths) / float64(roundsPlayed)
	kast := 100.0 * float64(kastRounds) / float64(roundsPlayed)
	adr := float64(totalDamage) / float64(roundsPlayed)
	impact := 2.13*kpr + 0.42*apr - 0.41
	return 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impact + 0.0032*adr + 0.1587
}
