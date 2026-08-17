package cmd

import (
	"math"
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// almostEq returns true if a and b agree to 1e-3, which is plenty for the
// percentage / rate scales used in PlayerRoleStats.
func almostEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-3
}

// makeRound is a constructor that defaults every flag to a "boring round"
// (no kill, no assist, survived). Tests override specific fields.
func makeRound(demoHash string, n int, team model.Team) model.PlayerRoundStats {
	return model.PlayerRoundStats{
		DemoHash:    demoHash,
		RoundNumber: n,
		Team:        team,
		Survived:    true,
	}
}

// TestBuildRoleStats_EmptyInput verifies the all-zero case: empty stats slice
// returns the zero value with no allocations and no panics.
func TestBuildRoleStats_EmptyInput(t *testing.T) {
	rs := buildRoleStats(nil, nil, nil, model.PlayerClutchMatchStats{}, nil, nil, nil, nil, nil, "both")
	if rs.SteamID != 0 || rs.Matches != 0 || rs.RoundsPlayed != 0 {
		t.Errorf("expected zero stats, got %+v", rs)
	}
}

// TestBuildRoleStats_DivByZeroGuards constructs a player with one match where
// they never killed, never died, never attempted an opener, etc., and verifies
// no metric is NaN or Inf.
func TestBuildRoleStats_DivByZeroGuards(t *testing.T) {
	stats := []model.PlayerMatchStats{{
		DemoHash:     "d1",
		SteamID:      1,
		Name:         "test",
		RoundsPlayed: 5,
		// Everything else 0.
	}}
	rs := buildRoleStats(stats, nil, nil, model.PlayerClutchMatchStats{}, nil, nil, nil, nil, nil, "both")
	checks := map[string]float64{
		"Rating2Combined":            rs.Rating2Combined,
		"KASTPct":                    rs.KASTPct,
		"KPR":                        rs.KPR,
		"DPR":                        rs.DPR,
		"ADR":                        rs.ADR,
		"MultiKillPct":               rs.MultiKillPct,
		"RoundsWithKillPct":          rs.RoundsWithKillPct,
		"KillsPerRoundWin":           rs.KillsPerRoundWin,
		"DamagePerRoundWin":          rs.DamagePerRoundWin,
		"PistolRoundRating":          rs.PistolRoundRating,
		"OpeningDeathTradedPct":      rs.OpeningDeathTradedPct,
		"SupportRoundsPct":           rs.SupportRoundsPct,
		"DamagePerKill":              rs.DamagePerKill,
		"OpeningSuccessPct":          rs.OpeningSuccessPct,
		"WinAfterOpenPct":            rs.WinAfterOpenPct,
		"ClutchPointsPerRound":       rs.ClutchPointsPerRound,
		"OneVOneWinPct":              rs.OneVOneWinPct,
		"SavesPerLossPct":            rs.SavesPerLossPct,
		"SniperKillsPerRound":        rs.SniperKillsPerRound,
		"SniperKillsPct":             rs.SniperKillsPct,
		"RoundsWithSniperKillPct":    rs.RoundsWithSniperKillPct,
		"SniperOpeningKillsPerRound": rs.SniperOpeningKillsPerRound,
		"UtilityDamagePerRound":      rs.UtilityDamagePerRound,
		"UtilityKillsPer100R":        rs.UtilityKillsPer100R,
		"FlashesThrownPerRound":      rs.FlashesThrownPerRound,
		"OppFlashSecPerRound":        rs.OppFlashSecPerRound,
	}
	for name, v := range checks {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("%s is NaN or Inf: %v", name, v)
		}
	}
}

// TestBuildRoleStats_HappyPath builds a small, carefully-controlled scenario
// and asserts every Slice-1 metric against a hand-computed expected value.
//
// Scenario: one player ("alice"), one demo "d1", 10 rounds total.
//   - Player plays 5 rounds on CT (rounds 1-5) and 5 rounds on T (rounds 6-10).
//   - Rounds 1, 3, 6 are pistol-ish: but only round 1 and round 13 actually
//     count as pistols under our rule. Round 13 isn't played here; only round 1.
//   - Won rounds: 1, 2, 3, 6, 7 (5 wins, 5 losses).
//   - Kills: r1=2 (multi), r2=1, r3=0, r4=1, r5=0, r6=3 (multi), r7=1, r8=0, r9=0, r10=0.
//     Total = 8 kills.
//   - Survived: r1, r2, r3, r6 (4). Deaths = 6.
//   - Damage: 80+50+0+30+0+150+40+0+0+0 = 350.
//   - Opening kills: r1, r6 (2). Opening deaths: r5 (1, traded). r10 (1, not traded).
//   - Trade deaths: r5 (with is_opening_death=1).
//   - Assists: r4=1. Got assist: r4.
//   - WonRound flag set on rounds 1, 2, 3, 6, 7. Player kills in won rounds: 2+1+0+3+1 = 7.
//   - Pistol rounds: only r1. r1 kills=2, dmg=80, survived=true (so 0 deaths), kast=true.
func TestBuildRoleStats_HappyPath(t *testing.T) {
	const demoHash = "d1"
	stats := []model.PlayerMatchStats{{
		DemoHash:        demoHash,
		SteamID:         42,
		Name:            "alice",
		Team:            model.TeamCT,
		Kills:           8,
		Assists:         1,
		Deaths:          6,
		HeadshotKills:   4,
		TotalDamage:     350,
		UtilityDamage:   60,
		RoundsPlayed:    10,
		KASTRounds:      6,
		OpeningKills:    2,
		OpeningDeaths:   2,
		TradeKills:      2,
		TradeDeaths:     1,
		RoundsWon:       5,
		SavedByTeammate:       2,
		SavedTeammate:         3,
		AssistedKills:         4, // 4 of 8 kills had teammate damage already on the victim
		HltvFlashAssists:      2, // 2 enemies died blinded by alice's flashes with ≥25 dmg from killer
		AliveSecondsTotal:     500.0, // 50 s avg over 10 rounds
		LastAliveServerRounds: 3,     // sole survivor at some point in 3 of 10 rounds
	}}

	round := func(n int, team model.Team, kills, damage int, survived, gotKill, gotAssist, kast, won, openK, openD, tradeD bool) model.PlayerRoundStats {
		r := makeRound(demoHash, n, team)
		r.SteamID = 42
		r.Kills = kills
		r.Damage = damage
		r.Survived = survived
		r.GotKill = gotKill
		r.GotAssist = gotAssist
		r.KASTEarned = kast
		r.WonRound = won
		r.IsOpeningKill = openK
		r.IsOpeningDeath = openD
		r.IsTradeDeath = tradeD
		return r
	}

	roundStats := []model.PlayerRoundStats{
		// CT rounds 1-5.
		round(1, model.TeamCT, 2, 80, true, true, false, true, true, true, false, false),    // pistol, multi-kill, survived, won, opener
		round(2, model.TeamCT, 1, 50, true, true, false, true, true, false, false, false),   // kill, survived, won
		round(3, model.TeamCT, 0, 0, true, false, false, true, true, false, false, false),   // KAST via survive, won
		round(4, model.TeamCT, 1, 30, false, true, true, true, false, false, false, false),  // kill+assist, died, lost
		round(5, model.TeamCT, 0, 0, false, false, false, true, false, false, true, true),   // opening death, traded
		// T rounds 6-10.
		round(6, model.TeamT, 3, 150, true, true, false, true, true, true, false, false),    // multi-kill, won, opener
		round(7, model.TeamT, 1, 40, false, true, false, true, true, false, false, false),   // kill, lost personally but team won
		round(8, model.TeamT, 0, 0, false, false, false, false, false, false, false, false), // dead silence
		round(9, model.TeamT, 0, 0, false, false, false, false, false, false, false, false),
		round(10, model.TeamT, 0, 0, false, false, false, false, false, false, true, false), // opening death, NOT traded
	}
	// r4 has GotAssist=true; mirror that to the integer Assists field so per-side
	// totals match the headline.
	roundStats[3].Assists = 1
	// r5's death was traded — set WasTraded too (the KAST trade flag), since
	// IsTradeDeath alone doesn't satisfy the support-round predicate.
	roundStats[4].WasTraded = true

	weaponStats := []model.PlayerWeaponStats{
		{DemoHash: demoHash, SteamID: 42, Weapon: "AWP", Kills: 3, Damage: 200},
		{DemoHash: demoHash, SteamID: 42, Weapon: "AK-47", Kills: 5, Damage: 150},
	}
	clutch := model.PlayerClutchMatchStats{
		DemoHash: demoHash,
		SteamID:  42,
		Attempts: [6]int{0, 2, 1, 0, 0, 0}, // 2× 1v1, 1× 1v2
		Wins:     [6]int{0, 1, 0, 0, 0, 0}, // won 1× 1v1
	}
	sniperRounds := []storage.SniperRoundKill{
		{DemoHash: demoHash, RoundNumber: 1, KillCount: 2}, // round 1 multi-kill with AWP (matches r1 opener)
		{DemoHash: demoHash, RoundNumber: 7, KillCount: 1},
	}
	utilityKillsByDemo := map[string]int{demoHash: 1}
	flashesByDemo := map[string]int{demoHash: 6}
	oppFlashSecByDemo := map[string]float64{demoHash: 12.5}

	rs := buildRoleStats(stats, roundStats, weaponStats, clutch,
		sniperRounds, utilityKillsByDemo, flashesByDemo, oppFlashSecByDemo, nil, "both")

	// ---- Headline assertions ----
	if rs.SteamID != 42 {
		t.Errorf("SteamID = %d, want 42", rs.SteamID)
	}
	if rs.Matches != 1 {
		t.Errorf("Matches = %d, want 1", rs.Matches)
	}
	if rs.RoundsPlayed != 10 {
		t.Errorf("RoundsPlayed = %d, want 10", rs.RoundsPlayed)
	}
	if rs.RoundsWon != 5 {
		t.Errorf("RoundsWon = %d, want 5", rs.RoundsWon)
	}
	// KPR = 8/10 = 0.8, DPR = 6/10 = 0.6, ADR = 35.0, KAST% = 60.
	if !almostEq(rs.KPR, 0.8) {
		t.Errorf("KPR = %v, want 0.8", rs.KPR)
	}
	if !almostEq(rs.DPR, 0.6) {
		t.Errorf("DPR = %v, want 0.6", rs.DPR)
	}
	if !almostEq(rs.ADR, 35.0) {
		t.Errorf("ADR = %v, want 35.0", rs.ADR)
	}
	if !almostEq(rs.KASTPct, 60.0) {
		t.Errorf("KASTPct = %v, want 60.0", rs.KASTPct)
	}

	// ---- Side-split Rating 2.0 ----
	// CT: K=4, A=1, D=2, KAST=5, dmg=160, rp=5. T: K=4, A=0, D=4, KAST=2, dmg=190, rp=5.
	ctR := rating2(4, 1, 2, 5, 160, 5)
	tR := rating2(4, 0, 4, 2, 190, 5)
	if !almostEq(rs.Rating2CT, ctR) {
		t.Errorf("Rating2CT = %v, want %v", rs.Rating2CT, ctR)
	}
	if !almostEq(rs.Rating2T, tR) {
		t.Errorf("Rating2T = %v, want %v", rs.Rating2T, tR)
	}

	// ---- §2.1 Firepower ----
	// Rounds with kill: r1, r2, r4, r6, r7 = 5 / 10 = 50%.
	if !almostEq(rs.RoundsWithKillPct, 50.0) {
		t.Errorf("RoundsWithKillPct = %v, want 50.0", rs.RoundsWithKillPct)
	}
	// Multi-kill rounds: r1 (2), r6 (3) = 2 / 10 = 20%.
	if !almostEq(rs.MultiKillPct, 20.0) {
		t.Errorf("MultiKillPct = %v, want 20.0", rs.MultiKillPct)
	}
	// Kills in won rounds: r1(2)+r2(1)+r3(0)+r6(3)+r7(1) = 7. 7/5 = 1.4.
	if !almostEq(rs.KillsPerRoundWin, 1.4) {
		t.Errorf("KillsPerRoundWin = %v, want 1.4", rs.KillsPerRoundWin)
	}
	// Damage in won rounds: 80+50+0+150+40 = 320. 320/5 = 64.
	if !almostEq(rs.DamagePerRoundWin, 64.0) {
		t.Errorf("DamagePerRoundWin = %v, want 64.0", rs.DamagePerRoundWin)
	}
	// Pistol round: only round 1 played (no round 13). K=2, A=0, D=0, KAST=1, dmg=80, rp=1.
	pistolExpected := rating2(2, 0, 0, 1, 80, 1)
	if !almostEq(rs.PistolRoundRating, pistolExpected) {
		t.Errorf("PistolRoundRating = %v, want %v", rs.PistolRoundRating, pistolExpected)
	}

	// ---- §2.2 Entrying ----
	// Opening deaths traded: 1 of 2 (r5 traded, r10 not). 50%.
	if !almostEq(rs.OpeningDeathTradedPct, 50.0) {
		t.Errorf("OpeningDeathTradedPct = %v, want 50.0", rs.OpeningDeathTradedPct)
	}
	// Support rounds: rounds with assist/survive/traded-death but no kill.
	// r3 (survived, no kill), r5 (traded, no kill), r8/r9/r10 NOT survived and no other flag.
	// Actually r3 (survived=true, no kill): yes. r5 (traded=true, no kill): yes. r8 (survived=false, no flags): no.
	// r9, r10: same as r8. So support = 2. 2/10 = 20%.
	if !almostEq(rs.SupportRoundsPct, 20.0) {
		t.Errorf("SupportRoundsPct = %v, want 20.0", rs.SupportRoundsPct)
	}

	// Traded deaths: only r5 has IsTradeDeath. 1/10 rounds = 0.1; 1/6 deaths ≈ 16.67%.
	if !almostEq(rs.TradedDeathsPerRound, 0.1) {
		t.Errorf("TradedDeathsPerRound = %v, want 0.1", rs.TradedDeathsPerRound)
	}
	if !almostEq(rs.TradedDeathsPct, 100.0/6.0) {
		t.Errorf("TradedDeathsPct = %v, want %v", rs.TradedDeathsPct, 100.0/6.0)
	}
	// Assists: r4 only = 1 / 10 rounds = 0.1.
	if !almostEq(rs.AssistsPerRound, 0.1) {
		t.Errorf("AssistsPerRound = %v, want 0.1", rs.AssistsPerRound)
	}

	// ---- §2.2 Entrying — Pass 14 ----
	// SavedByTeammate=2 / 10 rounds = 0.2.
	if !almostEq(rs.SavedByTeammatePerRound, 0.2) {
		t.Errorf("SavedByTeammatePerRound = %v, want 0.2", rs.SavedByTeammatePerRound)
	}

	// ---- §2.3 Trading ----
	// TradeKills=2 (match-level) / 10 rounds = 0.2; / 8 kills = 25%.
	if !almostEq(rs.TradeKillsPerRound, 0.2) {
		t.Errorf("TradeKillsPerRound = %v, want 0.2", rs.TradeKillsPerRound)
	}
	if !almostEq(rs.TradeKillsPct, 25.0) {
		t.Errorf("TradeKillsPct = %v, want 25.0", rs.TradeKillsPct)
	}
	// Damage / kill = 350 / 8 = 43.75.
	if !almostEq(rs.DamagePerKill, 43.75) {
		t.Errorf("DamagePerKill = %v, want 43.75", rs.DamagePerKill)
	}
	// SavedTeammate=3 / 10 = 0.3.
	if !almostEq(rs.SavedTeammatePerRound, 0.3) {
		t.Errorf("SavedTeammatePerRound = %v, want 0.3", rs.SavedTeammatePerRound)
	}
	// AssistedKills=4 / 8 kills = 50%.
	if !almostEq(rs.AssistedKillsPct, 50.0) {
		t.Errorf("AssistedKillsPct = %v, want 50.0", rs.AssistedKillsPct)
	}

	// ---- §2.4 Opening ----
	// OpeningKills=2, OpeningDeaths=2.
	if !almostEq(rs.OpeningKPR, 0.2) {
		t.Errorf("OpeningKPR = %v, want 0.2", rs.OpeningKPR)
	}
	if !almostEq(rs.OpeningDPR, 0.2) {
		t.Errorf("OpeningDPR = %v, want 0.2", rs.OpeningDPR)
	}
	// Attempts = opener_k + opener_d = 4. 4/10 = 40%.
	if !almostEq(rs.OpeningAttemptsPct, 40.0) {
		t.Errorf("OpeningAttemptsPct = %v, want 40.0", rs.OpeningAttemptsPct)
	}
	// Success = 2/4 = 50%.
	if !almostEq(rs.OpeningSuccessPct, 50.0) {
		t.Errorf("OpeningSuccessPct = %v, want 50.0", rs.OpeningSuccessPct)
	}
	// Win after opener: r1 opener+won, r6 opener+won = 2/2 = 100%.
	if !almostEq(rs.WinAfterOpenPct, 100.0) {
		t.Errorf("WinAfterOpenPct = %v, want 100.0", rs.WinAfterOpenPct)
	}

	// ---- §2.5 Clutching ----
	// Clutch points = Wins[1]*1 + Wins[2]*2 = 1*1 + 0*2 = 1. /10 rounds = 0.1.
	if !almostEq(rs.ClutchPointsPerRound, 0.1) {
		t.Errorf("ClutchPointsPerRound = %v, want 0.1", rs.ClutchPointsPerRound)
	}
	if rs.OneVOneAttempts != 2 || rs.OneVOneWins != 1 {
		t.Errorf("1v1 attempts/wins = %d/%d, want 2/1", rs.OneVOneAttempts, rs.OneVOneWins)
	}
	if !almostEq(rs.OneVOneWinPct, 50.0) {
		t.Errorf("OneVOneWinPct = %v, want 50.0", rs.OneVOneWinPct)
	}
	// Saves per loss: lost rounds = r4, r5, r8, r9, r10 = 5. Survived among those = 0
	// (r4 false, r5 false, r8/9/10 false). So 0/5 = 0%.
	if !almostEq(rs.SavesPerLossPct, 0.0) {
		t.Errorf("SavesPerLossPct = %v, want 0.0", rs.SavesPerLossPct)
	}
	// 500 s alive / 10 rounds = 50 s per round.
	if !almostEq(rs.TimeAlivePerRoundSec, 50.0) {
		t.Errorf("TimeAlivePerRoundSec = %v, want 50.0", rs.TimeAlivePerRoundSec)
	}
	// 3 last-alive-server rounds / 10 = 30%.
	if !almostEq(rs.LastAliveServerPct, 30.0) {
		t.Errorf("LastAliveServerPct = %v, want 30.0", rs.LastAliveServerPct)
	}

	// ---- §2.6 Sniping ----
	// AWP kills = 3. /10 = 0.3.
	if !almostEq(rs.SniperKillsPerRound, 0.3) {
		t.Errorf("SniperKillsPerRound = %v, want 0.3", rs.SniperKillsPerRound)
	}
	// 3 sniper kills / 8 total = 37.5%.
	if !almostEq(rs.SniperKillsPct, 37.5) {
		t.Errorf("SniperKillsPct = %v, want 37.5", rs.SniperKillsPct)
	}
	// 2 sniper-kill rounds / 10 = 20%.
	if !almostEq(rs.RoundsWithSniperKillPct, 20.0) {
		t.Errorf("RoundsWithSniperKillPct = %v, want 20.0", rs.RoundsWithSniperKillPct)
	}
	// Multi-kill sniper rounds: only r1 (2 kills) = 1 / 10 = 10%.
	if !almostEq(rs.SniperMultiKillRoundPct, 10.0) {
		t.Errorf("SniperMultiKillRoundPct = %v, want 10.0", rs.SniperMultiKillRoundPct)
	}
	// Sniper opening kills: r1 is opening AND has sniper kill. r6 is opening but no sniper row.
	// So 1 / 10 = 0.1.
	if !almostEq(rs.SniperOpeningKillsPerRound, 0.1) {
		t.Errorf("SniperOpeningKillsPerRound = %v, want 0.1", rs.SniperOpeningKillsPerRound)
	}
	if !rs.HasSniperData {
		t.Errorf("HasSniperData should be true when sniperRounds is non-empty")
	}

	// ---- §2.7 Utility ----
	// UtilityDamage = 60, /10 = 6.0.
	if !almostEq(rs.UtilityDamagePerRound, 6.0) {
		t.Errorf("UtilityDamagePerRound = %v, want 6.0", rs.UtilityDamagePerRound)
	}
	// 1 utility kill / 10 rounds × 100 = 10.
	if !almostEq(rs.UtilityKillsPer100R, 10.0) {
		t.Errorf("UtilityKillsPer100R = %v, want 10.0", rs.UtilityKillsPer100R)
	}
	// 6 flashes / 10 = 0.6.
	if !almostEq(rs.FlashesThrownPerRound, 0.6) {
		t.Errorf("FlashesThrownPerRound = %v, want 0.6", rs.FlashesThrownPerRound)
	}
	// 12.5s / 10 = 1.25.
	if !almostEq(rs.OppFlashSecPerRound, 1.25) {
		t.Errorf("OppFlashSecPerRound = %v, want 1.25", rs.OppFlashSecPerRound)
	}
	if !rs.HasUtilityData || !rs.HasFlashThrowData || !rs.HasFlashTimeData {
		t.Errorf("coverage flags should all be true when event-table inputs are non-empty")
	}
	// HltvFlashAssists=2 / 10 rounds = 0.2.
	if !almostEq(rs.HltvFlashAssistsPerRound, 0.2) {
		t.Errorf("HltvFlashAssistsPerRound = %v, want 0.2", rs.HltvFlashAssistsPerRound)
	}
}

// TestBuildRoleStats_CoverageFlags verifies the HasXxxData flags correctly
// report false when the corresponding event-table input is empty.
func TestBuildRoleStats_CoverageFlags(t *testing.T) {
	stats := []model.PlayerMatchStats{{
		DemoHash: "d1", SteamID: 1, Name: "x", RoundsPlayed: 10,
	}}
	rs := buildRoleStats(stats, nil, nil, model.PlayerClutchMatchStats{},
		nil, nil, nil, nil, nil, "both")
	if rs.HasSniperData || rs.HasUtilityData || rs.HasFlashThrowData || rs.HasFlashTimeData {
		t.Errorf("coverage flags should all be false on empty inputs, got %+v", rs)
	}
}

// TestBuildRoleStats_SideView verifies that side="ct" replaces the headline
// and per-round denominators with side-only numbers (regression: these used
// to stay at both-sides match-level values), while metrics from
// non-side-tagged match columns stay combined.
func TestBuildRoleStats_SideView(t *testing.T) {
	const demoHash = "d1"
	stats := []model.PlayerMatchStats{{
		DemoHash: demoHash, SteamID: 42, Name: "alice",
		Kills: 8, Assists: 1, Deaths: 6, TotalDamage: 350,
		RoundsPlayed: 10, RoundsWon: 5, KASTRounds: 6,
		SavedByTeammate: 2, TradeKills: 2,
	}}
	round := func(n int, kills, damage int, survived, kast, won, tradeD bool) model.PlayerRoundStats {
		r := makeRound(demoHash, n, model.TeamCT)
		r.SteamID = 42
		r.Kills = kills
		r.Damage = damage
		r.Survived = survived
		r.KASTEarned = kast
		r.WonRound = won
		r.WasTraded = tradeD
		return r
	}
	// CT-only rounds, as filterRoundStatsBySide would produce for side=ct:
	// K=4 A=1 D=2 dmg=160 kast=5 rp=5, wins=3, 1 multi-kill, 1 traded death.
	ctRounds := []model.PlayerRoundStats{
		round(1, 2, 80, true, true, true, false),
		round(2, 1, 50, true, true, true, false),
		round(3, 0, 0, true, true, true, false),
		round(4, 1, 30, false, true, false, false),
		round(5, 0, 0, false, true, false, true),
	}
	ctRounds[3].Assists = 1

	rs := buildRoleStats(stats, ctRounds, nil, model.PlayerClutchMatchStats{},
		nil, nil, nil, nil, nil, "ct")

	if rs.RoundsPlayed != 5 {
		t.Errorf("RoundsPlayed = %d, want 5 (CT rounds only)", rs.RoundsPlayed)
	}
	if rs.RoundsWon != 3 {
		t.Errorf("RoundsWon = %d, want 3", rs.RoundsWon)
	}
	wantRating := rating2(4, 1, 2, 5, 160, 5)
	if !almostEq(rs.Rating2Combined, wantRating) {
		t.Errorf("Rating2Combined = %v, want CT-only %v", rs.Rating2Combined, wantRating)
	}
	if !almostEq(rs.KPR, 0.8) || !almostEq(rs.DPR, 0.4) || !almostEq(rs.ADR, 32.0) || !almostEq(rs.KASTPct, 100.0) {
		t.Errorf("headline rates = KPR %v DPR %v ADR %v KAST %v, want 0.8 / 0.4 / 32 / 100",
			rs.KPR, rs.DPR, rs.ADR, rs.KASTPct)
	}
	if !almostEq(rs.MultiKillPct, 20.0) {
		t.Errorf("MultiKillPct = %v, want 20.0 (1 of 5 CT rounds)", rs.MultiKillPct)
	}
	if !almostEq(rs.KillsPerRoundWin, 1.0) {
		t.Errorf("KillsPerRoundWin = %v, want 1.0 (3 kills / 3 CT wins)", rs.KillsPerRoundWin)
	}
	if !almostEq(rs.TradedDeathsPerRound, 0.2) || !almostEq(rs.TradedDeathsPct, 50.0) {
		t.Errorf("TradedDeaths = %v/rd %v%%, want 0.2 / 50", rs.TradedDeathsPerRound, rs.TradedDeathsPct)
	}
	// Match-level columns are not side-tagged: still divided by all 10 rounds.
	if !almostEq(rs.SavedByTeammatePerRound, 0.2) {
		t.Errorf("SavedByTeammatePerRound = %v, want combined 0.2", rs.SavedByTeammatePerRound)
	}
	if !almostEq(rs.TradeKillsPerRound, 0.2) {
		t.Errorf("TradeKillsPerRound = %v, want combined 0.2", rs.TradeKillsPerRound)
	}
}

// ---- Slice 5 helpers ----

func TestPercentileOf(t *testing.T) {
	cases := []struct {
		name   string
		sorted []float64
		v      float64
		want   float64
	}{
		{"empty", nil, 1.0, -1},
		{"single below", []float64{1.5}, 1.0, 0},   // before only entry
		{"single match", []float64{1.5}, 1.5, 50},  // tie midpoint
		{"single above", []float64{1.5}, 2.0, 100}, // after only entry
		{"five elements top", []float64{1.0, 1.1, 1.2, 1.3, 1.4}, 1.4, 90},   // last → midpoint of [4,5]
		{"five elements bot", []float64{1.0, 1.1, 1.2, 1.3, 1.4}, 1.0, 10},   // first
		{"value above all", []float64{1.0, 1.5, 2.0}, 3.0, 100},
		{"value below all", []float64{1.0, 1.5, 2.0}, 0.5, 0},
		{"value between with ties", []float64{1.0, 1.5, 1.5, 1.5, 2.0}, 1.5, 50}, // midpoint of [1,4]
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := percentileOf(tc.sorted, tc.v)
			if !almostEq(got, tc.want) {
				t.Errorf("percentileOf(%v, %v) = %v, want %v", tc.sorted, tc.v, got, tc.want)
			}
		})
	}
}

func TestFilterRoundStatsBySide(t *testing.T) {
	rs := []model.PlayerRoundStats{
		{RoundNumber: 1, Team: model.TeamCT},
		{RoundNumber: 2, Team: model.TeamT},
		{RoundNumber: 3, Team: model.TeamCT},
		{RoundNumber: 4, Team: model.TeamT},
	}
	// "both" passes through unchanged.
	if got := filterRoundStatsBySide(rs, "both"); len(got) != 4 {
		t.Errorf("side=both kept %d, want 4", len(got))
	}
	// "ct" keeps only TeamCT rows.
	ct := filterRoundStatsBySide(append([]model.PlayerRoundStats{}, rs...), "ct")
	if len(ct) != 2 {
		t.Errorf("side=ct kept %d, want 2", len(ct))
	}
	for _, r := range ct {
		if r.Team != model.TeamCT {
			t.Errorf("non-CT row leaked through side=ct filter: %+v", r)
		}
	}
	// "t" keeps only TeamT rows.
	tSide := filterRoundStatsBySide(append([]model.PlayerRoundStats{}, rs...), "t")
	if len(tSide) != 2 {
		t.Errorf("side=t kept %d, want 2", len(tSide))
	}
	for _, r := range tSide {
		if r.Team != model.TeamT {
			t.Errorf("non-T row leaked through side=t filter: %+v", r)
		}
	}
}
