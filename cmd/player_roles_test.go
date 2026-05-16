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
	rs := buildRoleStats(nil, nil, nil, model.PlayerClutchMatchStats{}, nil, nil, nil, nil)
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
	rs := buildRoleStats(stats, nil, nil, model.PlayerClutchMatchStats{}, nil, nil, nil, nil)
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
		TradeKills:      0,
		TradeDeaths:     1,
		RoundsWon:       5,
		SavedByTeammate:  2,
		SavedTeammate:    3,
		AssistedKills:    4, // 4 of 8 kills had teammate damage already on the victim
		HltvFlashAssists: 2, // 2 enemies died blinded by alice's flashes with ≥25 dmg from killer
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
		sniperRounds, utilityKillsByDemo, flashesByDemo, oppFlashSecByDemo)

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

	// ---- §2.2 Entrying — Pass 14 ----
	// SavedByTeammate=2 / 10 rounds = 0.2.
	if !almostEq(rs.SavedByTeammatePerRound, 0.2) {
		t.Errorf("SavedByTeammatePerRound = %v, want 0.2", rs.SavedByTeammatePerRound)
	}

	// ---- §2.3 Trading ----
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
		nil, nil, nil, nil)
	if rs.HasSniperData || rs.HasUtilityData || rs.HasFlashThrowData || rs.HasFlashTimeData {
		t.Errorf("coverage flags should all be false on empty inputs, got %+v", rs)
	}
}
