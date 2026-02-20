package aggregator

import (
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

var tickRate float64 = 64.0

// makeRound creates a minimal RawRound with the given number and a freeze-end tick.
func makeRound(number, freezeEnd int, playerIDs []uint64, aliveIDs map[uint64]bool) model.RawRound {
	endState := make(map[uint64]model.PlayerRoundEndState)
	for _, id := range playerIDs {
		alive := aliveIDs[id]
		endState[id] = model.PlayerRoundEndState{
			SteamID64: id,
			IsAlive:   alive,
			Team:      model.TeamT,
		}
	}
	return model.RawRound{
		Number:        number,
		StartTick:     0,
		FreezeEndTick: freezeEnd,
		EndTick:       freezeEnd + 10000,
		WinnerTeam:    model.TeamT,
		PlayerEndState: endState,
	}
}

// makeRaw builds a minimal RawMatch with two players: killer (T) and victim (CT).
func makeRaw(kills []model.RawKill, rounds []model.RawRound) *model.RawMatch {
	names := make(map[uint64]string)
	teams := make(map[uint64]model.Team)
	for _, k := range kills {
		names[k.KillerSteamID] = "killer"
		names[k.VictimSteamID] = "victim"
		teams[k.KillerSteamID] = k.KillerTeam
		teams[k.VictimSteamID] = k.VictimTeam
	}
	return &model.RawMatch{
		DemoHash:       "testhash",
		TicksPerSecond: tickRate,
		Rounds:         rounds,
		Kills:          kills,
		PlayerNames:    names,
		PlayerTeams:    teams,
	}
}

// IDs for test players.
const (
	playerA uint64 = 1001
	playerB uint64 = 1002
	playerC uint64 = 1003
	playerD uint64 = 1004
)

// ---- Trade window tests ----

// buildTradeKills creates two kills simulating a trade scenario:
//   kill 1: B kills A (at tick 1000)
//   kill 2: C kills B (at tick 1000 + deltaTicks)
// Returns (kills, round). Kill team assignments:
//   A and C are on TeamCT, B is on TeamT.
func buildTradeScenario(deltaTicks int) ([]model.RawKill, model.RawRound) {
	k1 := model.RawKill{
		Tick: 1000, RoundNumber: 1,
		KillerSteamID: playerB, VictimSteamID: playerA,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
	}
	k2 := model.RawKill{
		Tick: 1000 + deltaTicks, RoundNumber: 1,
		KillerSteamID: playerC, VictimSteamID: playerB,
		KillerTeam: model.TeamCT, VictimTeam: model.TeamT,
	}
	round := makeRound(1, 500, []uint64{playerA, playerB, playerC}, map[uint64]bool{playerC: true})
	return []model.RawKill{k1, k2}, round
}

// TestTradeKill_ExactlyAtWindow: delta = 5s exactly → should be a trade.
func TestTradeKill_ExactlyAtWindow(t *testing.T) {
	deltaTicks := int(5.0 * tickRate) // exactly 320 ticks at 64hz
	kills, round := buildTradeScenario(deltaTicks)
	raw := makeRaw(kills, []model.RawRound{round})

	matchStats, roundStats, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// playerC (killed B) should have a trade kill.
	tradeKill := false
	for _, rs := range roundStats {
		if rs.SteamID == playerC && rs.RoundNumber == 1 {
			tradeKill = rs.IsTradeKill
		}
	}
	if !tradeKill {
		t.Error("expected playerC to have IsTradeKill=true at exactly 5.0s window")
	}

	// playerA's killer (playerB) should have isTradeDeath flagged — playerB was traded.
	tradeDeath := false
	for _, rs := range roundStats {
		if rs.SteamID == playerB && rs.RoundNumber == 1 {
			tradeDeath = rs.IsTradeDeath
		}
	}
	if !tradeDeath {
		t.Error("expected playerB to have IsTradeDeath=true (was traded by C)")
	}

	_ = matchStats
}

// TestTradeKill_JustOverWindow: delta = 5.1s → should NOT be a trade.
func TestTradeKill_JustOverWindow(t *testing.T) {
	deltaTicks := int(5.1*tickRate) + 1 // just over 5.0s window at 64hz
	kills, round := buildTradeScenario(deltaTicks)
	raw := makeRaw(kills, []model.RawRound{round})

	_, roundStats, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rs := range roundStats {
		if rs.SteamID == playerC && rs.RoundNumber == 1 && rs.IsTradeKill {
			t.Error("expected NO trade kill at 5.1s (just over window)")
		}
		if rs.SteamID == playerB && rs.RoundNumber == 1 && rs.IsTradeDeath {
			t.Error("expected NO trade death at 5.1s (just over window)")
		}
	}
}

// TestTradeKill_DoesNotCrossRounds: identical scenario in different rounds → no cross-round trade.
func TestTradeKill_DoesNotCrossRounds(t *testing.T) {
	// B kills A in round 1 (late), C kills B in round 2 (early) — should not be a trade.
	k1 := model.RawKill{
		Tick: 5000, RoundNumber: 1,
		KillerSteamID: playerB, VictimSteamID: playerA,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
	}
	k2 := model.RawKill{
		Tick: 5010, RoundNumber: 2,
		KillerSteamID: playerC, VictimSteamID: playerB,
		KillerTeam: model.TeamCT, VictimTeam: model.TeamT,
	}
	r1 := makeRound(1, 500, []uint64{playerA, playerB}, map[uint64]bool{})
	r2 := makeRound(2, 5005, []uint64{playerB, playerC}, map[uint64]bool{playerC: true})

	raw := makeRaw([]model.RawKill{k1, k2}, []model.RawRound{r1, r2})
	_, roundStats, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rs := range roundStats {
		if rs.SteamID == playerC && rs.RoundNumber == 2 && rs.IsTradeKill {
			t.Error("cross-round trade detected — should be impossible")
		}
	}
}

// ---- KAST tests ----

// TestKAST_SurvivedCounts: player who survived but got no kills/assists earns KAST.
func TestKAST_Survived(t *testing.T) {
	// playerA kills playerB; playerC does nothing but survives.
	k1 := model.RawKill{
		Tick: 1000, RoundNumber: 1,
		KillerSteamID: playerA, VictimSteamID: playerB,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
	}
	round := makeRound(1, 500,
		[]uint64{playerA, playerB, playerC},
		map[uint64]bool{playerA: true, playerC: true},
	)
	raw := makeRaw([]model.RawKill{k1}, []model.RawRound{round})

	matchStats, roundStats, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rs := range roundStats {
		if rs.SteamID == playerC && rs.RoundNumber == 1 {
			if !rs.KASTEarned {
				t.Error("playerC survived: expected KASTEarned=true")
			}
		}
	}

	for _, ms := range matchStats {
		if ms.SteamID == playerC {
			if ms.KASTRounds != 1 {
				t.Errorf("playerC: expected KASTRounds=1, got %d", ms.KASTRounds)
			}
		}
	}
}

// TestKAST_TradedCounts: player who was killed but traded also earns KAST.
func TestKAST_Traded(t *testing.T) {
	// B kills A (within 5s), C kills B immediately (trade).
	deltaTicks := int(2.0 * tickRate)
	kills, round := buildTradeScenario(deltaTicks)
	raw := makeRaw(kills, []model.RawRound{round})

	matchStats, roundStats, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// playerA was killed and traded by playerC; should earn KAST.
	for _, rs := range roundStats {
		if rs.SteamID == playerA && rs.RoundNumber == 1 {
			if !rs.WasTraded {
				t.Error("playerA: expected WasTraded=true")
			}
			if !rs.KASTEarned {
				t.Error("playerA was traded: expected KASTEarned=true")
			}
		}
	}

	for _, ms := range matchStats {
		if ms.SteamID == playerA {
			if ms.KASTRounds != 1 {
				t.Errorf("playerA: expected KASTRounds=1 (traded), got %d", ms.KASTRounds)
			}
		}
	}
	_ = roundStats
}

// TestOpeningKill: first kill after freezeEndTick is the opening kill.
func TestOpeningKill(t *testing.T) {
	// k0 happens before freeze end — should not count.
	// k1 happens after freeze end — should be the opening kill.
	k0 := model.RawKill{
		Tick: 400, RoundNumber: 1, // before freezeEnd=500
		KillerSteamID: playerA, VictimSteamID: playerB,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
	}
	k1 := model.RawKill{
		Tick: 600, RoundNumber: 1, // after freezeEnd=500
		KillerSteamID: playerC, VictimSteamID: playerD,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
	}
	round := makeRound(1, 500, []uint64{playerA, playerB, playerC, playerD}, map[uint64]bool{playerA: true, playerC: true})

	raw := &model.RawMatch{
		DemoHash:       "testhash",
		TicksPerSecond: tickRate,
		Rounds:         []model.RawRound{round},
		Kills:          []model.RawKill{k0, k1},
		PlayerNames: map[uint64]string{
			playerA: "A", playerB: "B", playerC: "C", playerD: "D",
		},
		PlayerTeams: map[uint64]model.Team{
			playerA: model.TeamT, playerB: model.TeamCT,
			playerC: model.TeamT, playerD: model.TeamCT,
		},
	}

	_, roundStats, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rs := range roundStats {
		switch rs.SteamID {
		case playerA:
			if rs.IsOpeningKill {
				t.Error("playerA: kill before freeze end should NOT be opening kill")
			}
		case playerC:
			if !rs.IsOpeningKill {
				t.Error("playerC: first kill after freeze end SHOULD be opening kill")
			}
		case playerD:
			if !rs.IsOpeningDeath {
				t.Error("playerD: victim of opening kill SHOULD have IsOpeningDeath=true")
			}
		}
	}
}

// ---- Crosshair placement tests ----

// TestCrosshairAggregation: first-sight events are aggregated into median and pct-under-5.
func TestCrosshairAggregation(t *testing.T) {
	round := makeRound(1, 500, []uint64{playerA, playerB}, map[uint64]bool{playerA: true})
	raw := makeRaw(nil, []model.RawRound{round})
	// Two first-sight events for playerA: 3° and 7° → median = 5.0, 50% under 5°.
	raw.FirstSights = []model.RawFirstSight{
		{Tick: 600, RoundNumber: 1, ObserverID: playerA, EnemyID: playerB, AngleDeg: 3.0},
		{Tick: 700, RoundNumber: 1, ObserverID: playerA, EnemyID: playerB, AngleDeg: 7.0},
	}
	raw.PlayerNames[playerA] = "A"
	raw.PlayerNames[playerB] = "B"

	matchStats, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *model.PlayerMatchStats
	for i := range matchStats {
		if matchStats[i].SteamID == playerA {
			found = &matchStats[i]
		}
	}
	if found == nil {
		t.Fatal("playerA not found in matchStats")
	}
	if found.CrosshairEncounters != 2 {
		t.Errorf("CrosshairEncounters: want 2, got %d", found.CrosshairEncounters)
	}
	if found.CrosshairMedianDeg != 5.0 {
		t.Errorf("CrosshairMedianDeg: want 5.0, got %f", found.CrosshairMedianDeg)
	}
	if found.CrosshairPctUnder5 != 50.0 {
		t.Errorf("CrosshairPctUnder5: want 50.0, got %f", found.CrosshairPctUnder5)
	}
}

// TestCrosshairAggregation_NoData: player with no first-sight events has zero crosshair fields.
func TestCrosshairAggregation_NoData(t *testing.T) {
	round := makeRound(1, 500, []uint64{playerA, playerB}, map[uint64]bool{playerA: true})
	raw := makeRaw(nil, []model.RawRound{round})
	raw.PlayerNames[playerA] = "A"
	raw.PlayerNames[playerB] = "B"
	// No FirstSights.

	matchStats, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ms := range matchStats {
		if ms.CrosshairEncounters != 0 || ms.CrosshairMedianDeg != 0 || ms.CrosshairPctUnder5 != 0 {
			t.Errorf("player %d: expected all crosshair fields zero, got enc=%d med=%f pct=%f",
				ms.SteamID, ms.CrosshairEncounters, ms.CrosshairMedianDeg, ms.CrosshairPctUnder5)
		}
	}
}

// ---- Duel engine tests ----

// TestDuelEngine_BasicWin: one kill with matching first-sight and one head-hit damage.
// Asserts DuelWins==1, MedianHitsToKill==1, FirstHitHSRate==100.
func TestDuelEngine_BasicWin(t *testing.T) {
	// Setup: playerA kills playerB with a head-shot damage event and matching first-sight.
	sightTick := 1000
	killTick := 1100
	k1 := model.RawKill{
		Tick: killTick, RoundNumber: 1,
		KillerSteamID: playerA, VictimSteamID: playerB,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
		IsHeadshot: true,
	}
	round := makeRound(1, 500, []uint64{playerA, playerB}, map[uint64]bool{playerA: true})
	raw := makeRaw([]model.RawKill{k1}, []model.RawRound{round})

	// Add head-hit damage in the sight→kill window.
	raw.Damages = []model.RawDamage{
		{
			Tick: 1050, RoundNumber: 1,
			AttackerSteamID: playerA, VictimSteamID: playerB,
			AttackerTeam: model.TeamT,
			HealthDamage: 100, Weapon: "ak47", HitGroup: "head",
		},
	}

	// Add first-sight event: playerA spots playerB at sightTick.
	raw.FirstSights = []model.RawFirstSight{
		{Tick: sightTick, RoundNumber: 1, ObserverID: playerA, EnemyID: playerB, AngleDeg: 2.0},
	}

	matchStats, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found *model.PlayerMatchStats
	for i := range matchStats {
		if matchStats[i].SteamID == playerA {
			found = &matchStats[i]
		}
	}
	if found == nil {
		t.Fatal("playerA not found in matchStats")
	}
	if found.DuelWins != 1 {
		t.Errorf("DuelWins: want 1, got %d", found.DuelWins)
	}
	if found.MedianHitsToKill != 1.0 {
		t.Errorf("MedianHitsToKill: want 1.0, got %f", found.MedianHitsToKill)
	}
	if found.FirstHitHSRate != 100.0 {
		t.Errorf("FirstHitHSRate: want 100.0, got %f", found.FirstHitHSRate)
	}
}

// ---- FHHS segment tests ----

// TestWeaponBucket: weapon names map to expected buckets.
func TestWeaponBucket(t *testing.T) {
	cases := []struct {
		weapon string
		want   string
	}{
		{"AK-47", "AK"},
		{"M4A1-S", "M4"},
		{"M4A4", "M4"},
		{"Galil AR", "Galil"},
		{"FAMAS", "FAMAS"},
		{"AUG", "ScopedRifle"},
		{"SG 553", "ScopedRifle"},
		{"AWP", "AWP"},
		{"SSG 08", "Scout"},
		{"Desert Eagle", "Deagle"},
		{"Glock-18", "Pistol"},
		{"USP-S", "Pistol"},
		{"P250", "Pistol"},
		{"knife", "Other"},
	}
	for _, c := range cases {
		got := weaponBucket(c.weapon)
		if got != c.want {
			t.Errorf("weaponBucket(%q): want %q, got %q", c.weapon, c.want, got)
		}
	}
}

// TestDistanceBin: distance values map to correct bins, including edge cases.
func TestDistanceBin(t *testing.T) {
	cases := []struct {
		m    float64
		want string
	}{
		{-1.0, "unknown"},
		{0.0, "0-5m"},
		{4.99, "0-5m"},
		{5.0, "5-10m"},
		{9.99, "5-10m"},
		{10.0, "10-15m"},
		{14.99, "10-15m"},
		{15.0, "15-20m"},
		{19.99, "15-20m"},
		{20.0, "20-30m"},
		{29.99, "20-30m"},
		{30.0, "30m+"},
		{100.0, "30m+"},
	}
	for _, c := range cases {
		got := distanceBin(c.m)
		if got != c.want {
			t.Errorf("distanceBin(%.2f): want %q, got %q", c.m, c.want, got)
		}
	}
}

// TestFHHSSegment: a duel with head-hit damage and a weapon fire with position
// produces a PlayerDuelSegment with correct counts and distance bin.
func TestFHHSSegment(t *testing.T) {
	sightTick := 1000
	fireTick := 1050
	hurtTick := 1060
	killTick := 1100

	k1 := model.RawKill{
		Tick: killTick, RoundNumber: 1,
		KillerSteamID: playerA, VictimSteamID: playerB,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
		Weapon: "AK-47", IsHeadshot: true,
	}
	round := makeRound(1, 500, []uint64{playerA, playerB}, map[uint64]bool{playerA: true})
	raw := makeRaw([]model.RawKill{k1}, []model.RawRound{round})

	// Attacker at origin; victim 1000 units away in X → ~19.05m → "15-20m".
	raw.Damages = []model.RawDamage{
		{
			Tick: hurtTick, RoundNumber: 1,
			AttackerSteamID: playerA, VictimSteamID: playerB,
			AttackerTeam: model.TeamT,
			HealthDamage: 100, Weapon: "AK-47", HitGroup: "head",
			VictimPos: model.Vec3{X: 1000, Y: 0, Z: 0},
		},
	}
	raw.WeaponFires = []model.RawWeaponFire{
		{
			Tick: fireTick, RoundNumber: 1,
			ShooterID: playerA, Weapon: "AK-47",
			AttackerPos: model.Vec3{X: 0, Y: 0, Z: 0},
		},
	}
	raw.FirstSights = []model.RawFirstSight{
		{Tick: sightTick, RoundNumber: 1, ObserverID: playerA, EnemyID: playerB, AngleDeg: 2.0},
	}

	_, _, _, segs, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the segment for (playerA, "AK", "15-20m").
	var found *model.PlayerDuelSegment
	for i := range segs {
		if segs[i].SteamID == playerA && segs[i].WeaponBucket == "AK" && segs[i].DistanceBin == "15-20m" {
			found = &segs[i]
		}
	}
	if found == nil {
		t.Fatalf("segment (playerA, AK, 15-20m) not found; all segments: %+v", segs)
	}
	if found.DuelCount != 1 {
		t.Errorf("DuelCount: want 1, got %d", found.DuelCount)
	}
	if found.FirstHitCount != 1 {
		t.Errorf("FirstHitCount: want 1, got %d", found.FirstHitCount)
	}
	if found.FirstHitHSCount != 1 {
		t.Errorf("FirstHitHSCount: want 1, got %d", found.FirstHitHSCount)
	}
}

// TestADR_Basic: damage is correctly rolled into ADR.
func TestADR_Basic(t *testing.T) {
	k1 := model.RawKill{
		Tick: 1000, RoundNumber: 1,
		KillerSteamID: playerA, VictimSteamID: playerB,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
		IsHeadshot: true,
	}
	round := makeRound(1, 500, []uint64{playerA, playerB}, map[uint64]bool{playerA: true})
	raw := makeRaw([]model.RawKill{k1}, []model.RawRound{round})
	raw.Damages = []model.RawDamage{
		{Tick: 900, RoundNumber: 1, AttackerSteamID: playerA, VictimSteamID: playerB,
			AttackerTeam: model.TeamT, HealthDamage: 75},
	}

	matchStats, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ms := range matchStats {
		if ms.SteamID == playerA {
			if ms.TotalDamage != 75 {
				t.Errorf("expected TotalDamage=75, got %d", ms.TotalDamage)
			}
			if ms.HeadshotKills != 1 {
				t.Errorf("expected HeadshotKills=1, got %d", ms.HeadshotKills)
			}
			if ms.ADR() != 75.0 {
				t.Errorf("expected ADR=75.0, got %.2f", ms.ADR())
			}
		}
	}
}
