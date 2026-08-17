package cmd

import (
	"fmt"
	"sort"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// CohortMinPlayers is the minimum cohort size to compute percentile labels.
// Below this, PlayerRoleStats.Rating2CohortPercentile stays at -1 and the
// renderer hides the rank column. Picked to ensure each percentile bucket
// has enough mass to be statistically meaningful.
const CohortMinPlayers = 30

// CohortPlayerMinRounds is the per-player rounds-played threshold for being
// included in the cohort. Filters out cameo appearances and ringers that
// distort the distribution.
const CohortPlayerMinRounds = 200

// buildCohortRatings returns a sorted slice of Rating 2.0 values, one per
// player in the cohort. Use percentileOf to find a focal player's percentile.
func buildCohortRatings(cohort []storage.CohortAggregate) []float64 {
	ratings := make([]float64, 0, len(cohort))
	for _, c := range cohort {
		if c.RoundsPlayed <= 0 {
			continue
		}
		ratings = append(ratings, rating2(
			c.Kills, c.Assists, c.Deaths,
			c.KASTRounds, c.TotalDamage, c.RoundsPlayed,
		))
	}
	sort.Float64s(ratings)
	return ratings
}

// percentileOf returns the percentile (0–100) of value v within a sorted
// slice. Linear-interpolated for ties. Returns -1 when the cohort is empty.
func percentileOf(sorted []float64, v float64) float64 {
	n := len(sorted)
	if n == 0 {
		return -1
	}
	// sort.Search returns the first index where sorted[i] >= v.
	idx := sort.SearchFloat64s(sorted, v)
	// Count how many entries are strictly less; idx is the leftmost match.
	lower := idx
	upper := idx
	for upper < n && sorted[upper] == v {
		upper++
	}
	// Midpoint of [lower, upper) gives a stable percentile even with ties.
	rank := (float64(lower) + float64(upper)) / 2.0
	return 100.0 * rank / float64(n)
}

// loadRoleStatsForPlayer is the cmd-side helper that loads every input
// buildRoleStats needs from the DB, filters by the caller's set of surviving
// demo hashes, and returns the assembled PlayerRoleStats.
//
// `keep` must contain exactly the demo hashes the caller has already filtered
// `stats` down to (it's the same set used elsewhere in cmd/player to filter
// duel segments and clutch counts). `name` is the display name to attach.
// `side` is "both", "ct", or "t" — when not "both", per-round-derived metrics
// are computed only from rounds on that side. Match-level fields (sniper kills,
// HLTV flash assists, etc.) stay combined since they are not side-tagged in
// the schema; this is documented in the renderer.
func loadRoleStatsForPlayer(
	db *storage.DB,
	id uint64,
	name string,
	stats []model.PlayerMatchStats,
	clutch model.PlayerClutchMatchStats,
	keep map[string]struct{},
	side string,
) (model.PlayerRoleStats, error) {
	roundStats, err := db.GetAllPlayerRoundStats(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get round stats: %w", err)
	}
	roundStats = filterRoundStatsByDemo(roundStats, keep)
	roundStats = filterRoundStatsBySide(roundStats, side)

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

	teamFlashSec, err := db.GetPlayerTeamFlashSecondsByDemo(id)
	if err != nil {
		return model.PlayerRoleStats{}, fmt.Errorf("get team flash seconds: %w", err)
	}
	teamFlashSec = filterFloatMapByDemo(teamFlashSec, keep)

	rs := buildRoleStats(stats, roundStats, weaponStats, clutch,
		sniperRounds, utilityKills, flashesThrown, oppFlashSec, teamFlashSec, side)
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

// filterRoundStatsBySide drops rounds whose team doesn't match the requested
// side ("ct" or "t"). "both" (or any other value) passes through unchanged.
func filterRoundStatsBySide(rs []model.PlayerRoundStats, side string) []model.PlayerRoundStats {
	var want model.Team
	switch side {
	case "ct":
		want = model.TeamCT
	case "t":
		want = model.TeamT
	default:
		return rs
	}
	out := rs[:0]
	for _, r := range rs {
		if r.Team == want {
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
// `side` must match the filter already applied to roundStats ("both", "ct",
// or "t"). When not "both", every round-stats-derived metric (headline rating,
// KAST/KPR/DPR/ADR, sample sizes, per-round rates) is computed over the
// side-filtered rounds only. Match-level metrics whose columns are not
// side-tagged (saved/saved-by, assisted kills, trade kills, sniper kills,
// utility, HLTV flash assists, time alive) stay combined across both sides —
// documented in the renderer.
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
	teamFlashSecByDemo map[string]float64,
	side string,
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
		tradeKills                                      int
		savedByTeammate, savedTeammate, assistedKills   int
		hltvFlashAssists                                int
		aliveSecondsTotal                               float64
		lastAliveServerRounds                           int
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
		tradeKills += s.TradeKills
		savedByTeammate += s.SavedByTeammate
		savedTeammate += s.SavedTeammate
		assistedKills += s.AssistedKills
		hltvFlashAssists += s.HltvFlashAssists
		aliveSecondsTotal += s.AliveSecondsTotal
		lastAliveServerRounds += s.LastAliveServerRounds
	}
	// matchRounds is the both-sides denominator used by metrics whose
	// numerators come from non-side-tagged match-level columns. roundsPlayed
	// below may be replaced by the side-round count when side != "both".
	matchRounds := roundsPlayed
	rs.RoundsPlayed = roundsPlayed
	rs.RoundsWon = roundsWon
	rs.Rating2Combined = rating2(kills, assists, deaths, kastRounds, totalDamage, roundsPlayed)
	if roundsPlayed > 0 {
		rs.KASTPct = 100.0 * float64(kastRounds) / float64(roundsPlayed)
		rs.KPR = float64(kills) / float64(roundsPlayed)
		rs.DPR = float64(deaths) / float64(roundsPlayed)
		rs.ADR = float64(totalDamage) / float64(roundsPlayed)
	}
	if matchRounds > 0 {
		rs.UtilityDamagePerRound = float64(utilityDamage) / float64(matchRounds)
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
		// Side-aware counters recomputed from the (possibly side-filtered)
		// round stream. A player records at most one death / opening kill /
		// opening death per round, so these flag sums are exact counts.
		prsRounds, prsWins, prsDeaths int
		prsAssists                    int
		prsOpeningKills               int
		prsOpeningDeaths              int
		tradedDeaths                  int
	)

	for _, r := range roundStats {
		prsRounds++
		if r.WonRound {
			prsWins++
		}
		if !r.Survived {
			prsDeaths++
		}
		prsAssists += r.Assists
		if r.IsOpeningKill {
			prsOpeningKills++
		}
		if r.IsOpeningDeath {
			prsOpeningDeaths++
		}
		// HLTV "traded death" = my death was avenged by a teammate within the
		// trade window — that is WasTraded. (IsTradeDeath is the opponent's
		// perspective: this player died as a trade after making a kill.)
		if r.WasTraded {
			tradedDeaths++
		}
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

		// §2.2 — entrying. WasTraded, not IsTradeDeath: an opening death can
		// never satisfy IsTradeDeath (there is no earlier kill to trade), so
		// the old flag pinned this metric at ~0% DB-wide.
		if r.IsOpeningDeath && r.WasTraded {
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

	// Side-restricted view: replace the headline (which was computed from
	// both-sides match-level columns) with side-only numbers, and shrink the
	// sample sizes to side rounds. roundStats is already side-filtered, so
	// the CT or T accumulator holds exactly the requested side.
	if side == "ct" || side == "t" {
		sa := &ct
		if side == "t" {
			sa = &t
		}
		rs.RoundsPlayed = sa.rp
		rs.RoundsWon = prsWins
		rs.Rating2Combined = rating2(sa.kills, sa.assists, sa.deaths, sa.kastRounds, sa.damage, sa.rp)
		rs.KASTPct, rs.KPR, rs.DPR, rs.ADR = 0, 0, 0, 0
		if sa.rp > 0 {
			rs.KASTPct = 100.0 * float64(sa.kastRounds) / float64(sa.rp)
			rs.KPR = float64(sa.kills) / float64(sa.rp)
			rs.DPR = float64(sa.deaths) / float64(sa.rp)
			rs.ADR = float64(sa.damage) / float64(sa.rp)
		}
	}

	if prsRounds > 0 {
		rs.MultiKillPct = 100.0 * float64(roundsMultiKill) / float64(prsRounds)
		rs.RoundsWithKillPct = 100.0 * float64(roundsWithKill) / float64(prsRounds)
		rs.OpeningKPR = float64(prsOpeningKills) / float64(prsRounds)
		rs.OpeningDPR = float64(prsOpeningDeaths) / float64(prsRounds)
		rs.OpeningAttemptsPct = 100.0 * float64(openingAttempts) / float64(prsRounds)
		rs.SupportRoundsPct = 100.0 * float64(supportRounds) / float64(prsRounds)
		rs.TradedDeathsPerRound = float64(tradedDeaths) / float64(prsRounds)
		rs.AssistsPerRound = float64(prsAssists) / float64(prsRounds)
	}
	if prsDeaths > 0 {
		rs.TradedDeathsPct = 100.0 * float64(tradedDeaths) / float64(prsDeaths)
	}
	if prsWins > 0 {
		rs.KillsPerRoundWin = float64(killsInWonRounds) / float64(prsWins)
		rs.DamagePerRoundWin = float64(dmgInWonRounds) / float64(prsWins)
	}
	if prsOpeningDeaths > 0 {
		rs.OpeningDeathTradedPct = 100.0 * float64(openingDeathTraded) / float64(prsOpeningDeaths)
	}
	if openingAttempts > 0 {
		rs.OpeningSuccessPct = 100.0 * float64(prsOpeningKills) / float64(openingAttempts)
	}
	if prsOpeningKills > 0 {
		rs.WinAfterOpenPct = 100.0 * float64(winsAfterOpener) / float64(prsOpeningKills)
	}

	// ---- §2.3 Trading ----
	// Trade kills, saves, and assisted kills come from match-level columns
	// that are not side-tagged, so they stay combined in side views (over
	// matchRounds / match-level kill totals).
	if kills > 0 {
		rs.DamagePerKill = float64(totalDamage) / float64(kills)
		rs.AssistedKillsPct = 100.0 * float64(assistedKills) / float64(kills)
		rs.TradeKillsPct = 100.0 * float64(tradeKills) / float64(kills)
	}
	if matchRounds > 0 {
		rs.TradeKillsPerRound = float64(tradeKills) / float64(matchRounds)
		rs.SavedByTeammatePerRound = float64(savedByTeammate) / float64(matchRounds)
		rs.SavedTeammatePerRound = float64(savedTeammate) / float64(matchRounds)
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
	if roundsPlayed > 0 {
		rs.TimeAlivePerRoundSec = aliveSecondsTotal / float64(roundsPlayed)
		rs.LastAliveServerPct = 100.0 * float64(lastAliveServerRounds) / float64(roundsPlayed)
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
	var teamFlashSec float64
	for _, v := range teamFlashSecByDemo {
		teamFlashSec += v
	}
	if matchRounds > 0 {
		rs.OppFlashSecPerRound = oppFlashSec / float64(matchRounds)
		rs.TeamFlashSecPerRound = teamFlashSec / float64(matchRounds)
		rs.HltvFlashAssistsPerRound = float64(hltvFlashAssists) / float64(matchRounds)
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
