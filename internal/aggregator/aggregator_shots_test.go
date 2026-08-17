package aggregator

import (
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

// ---- Pass 18 tests: shot accounting ----

// shotSamples emits one view sample per entry, sampleStep ticks apart from
// startTick, alive, with the given enemy-visibility flags.
func shotSamples(id uint64, round, startTick int, visible []bool) []model.RawViewSample {
	out := make([]model.RawViewSample, len(visible))
	for i, v := range visible {
		out[i] = model.RawViewSample{
			Tick:           startTick + i*sampleStep,
			RoundNumber:    round,
			SteamID:        id,
			HP:             100,
			EnemiesVisible: v,
		}
	}
	return out
}

func fire(id uint64, tick int, weapon string) model.RawWeaponFire {
	return model.RawWeaponFire{Tick: tick, RoundNumber: 1, ShooterID: id, Weapon: weapon}
}

func hurt(attacker, victim uint64, tick int, weapon, hitGroup string) model.RawDamage {
	return model.RawDamage{
		Tick: tick, RoundNumber: 1,
		AttackerSteamID: attacker, VictimSteamID: victim,
		AttackerTeam: model.TeamT, VictimTeam: model.TeamCT,
		HealthDamage: 30, HealthDamageTaken: 30,
		Weapon: weapon, HitGroup: hitGroup,
	}
}

func shotRaw(fires []model.RawWeaponFire, dmg []model.RawDamage, samples []model.RawViewSample) *model.RawMatch {
	return &model.RawMatch{
		DemoHash:       "h",
		TicksPerSecond: tickRate,
		Rounds:         []model.RawRound{makeRound(1, 100, []uint64{playerA}, map[uint64]bool{playerA: true})},
		WeaponFires:    fires,
		Damages:        dmg,
		ViewSamples:    samples,
		PlayerNames:    map[uint64]string{playerA: "A"},
		PlayerTeams:    map[uint64]model.Team{playerA: model.TeamT},
	}
}

// Shots land in the visible or blind bucket according to the nearest sample.
func TestPass18_SplitsVisibleAndBlindShots(t *testing.T) {
	// Samples at ticks 200,204,208,212: visible, visible, blind, blind.
	samples := shotSamples(playerA, 1, 200, []bool{true, true, false, false})
	fires := []model.RawWeaponFire{
		fire(playerA, 200, "AK-47"), // exactly on a visible sample
		fire(playerA, 205, "AK-47"), // nearest is tick 204 → visible
		fire(playerA, 208, "AK-47"), // blind
		fire(playerA, 213, "AK-47"), // nearest is tick 212 → blind
	}
	got := computeShotAccounting(shotRaw(fires, nil, samples))[shotKey{playerA, "AK-47"}]
	if got == nil {
		t.Fatal("no shot accounting for playerA/AK-47")
	}
	if got.shots != 4 {
		t.Errorf("shots = %d, want 4", got.shots)
	}
	if got.shotsVisible != 2 {
		t.Errorf("shotsVisible = %d, want 2", got.shotsVisible)
	}
}

// A shot with no sample within tolerance counts toward shots but not toward
// the visible bucket — "not measured" must not be reported as "aimed".
func TestPass18_ShotOutsideSampleToleranceIsNotVisible(t *testing.T) {
	samples := shotSamples(playerA, 1, 200, []bool{true})
	// tolerance at 64 tps is ceil(64/16)+2 = 6 ticks; 220 is far outside.
	fires := []model.RawWeaponFire{fire(playerA, 220, "AK-47")}
	got := computeShotAccounting(shotRaw(fires, nil, samples))[shotKey{playerA, "AK-47"}]
	if got.shots != 1 {
		t.Errorf("shots = %d, want 1", got.shots)
	}
	if got.shotsVisible != 0 {
		t.Errorf("shotsVisible = %d, want 0 (no sample in range)", got.shotsVisible)
	}
}

// Head hits are counted separately from lethal headshots, and split by
// visibility the same way shots are.
func TestPass18_CountsHeadHitsAndVisibleHits(t *testing.T) {
	samples := shotSamples(playerA, 1, 200, []bool{true, false})
	dmg := []model.RawDamage{
		hurt(playerA, playerB, 200, "AK-47", "head"),  // visible head hit
		hurt(playerA, playerB, 200, "AK-47", "chest"), // visible body hit
		hurt(playerA, playerB, 204, "AK-47", "head"),  // blind head hit
	}
	got := computeShotAccounting(shotRaw(nil, dmg, samples))[shotKey{playerA, "AK-47"}]
	if got.headHits != 2 {
		t.Errorf("headHits = %d, want 2", got.headHits)
	}
	if got.hitsVisible != 2 {
		t.Errorf("hitsVisible = %d, want 2", got.hitsVisible)
	}
	if got.headHitsVis != 1 {
		t.Errorf("headHitsVis = %d, want 1", got.headHitsVis)
	}
}

// Team damage is excluded, matching the weapon-hit accumulator in Aggregate,
// so HitsVisible can never exceed Hits.
func TestPass18_ExcludesTeamDamage(t *testing.T) {
	samples := shotSamples(playerA, 1, 200, []bool{true})
	teamHit := hurt(playerA, playerB, 200, "AK-47", "chest")
	teamHit.VictimTeam = model.TeamT // same team as attacker
	got := computeShotAccounting(shotRaw(nil, []model.RawDamage{teamHit}, samples))[shotKey{playerA, "AK-47"}]
	if got != nil && got.hitsVisible != 0 {
		t.Errorf("hitsVisible = %d, want 0 (team damage excluded)", got.hitsVisible)
	}
}

// With no view samples at all, shots are still counted but the split stays
// zero, so callers can distinguish "all blind" from "not measured".
func TestPass18_NoViewSamplesStillCountsShots(t *testing.T) {
	fires := []model.RawWeaponFire{fire(playerA, 200, "AK-47"), fire(playerA, 204, "AK-47")}
	got := computeShotAccounting(shotRaw(fires, nil, nil))[shotKey{playerA, "AK-47"}]
	if got.shots != 2 {
		t.Errorf("shots = %d, want 2", got.shots)
	}
	if got.shotsVisible != 0 {
		t.Errorf("shotsVisible = %d, want 0", got.shotsVisible)
	}
}

// A weapon that was fired but never hit anything must still produce a row —
// otherwise the worst-accuracy weapons would silently vanish.
func TestPass18_FiredButNeverHitProducesWeaponRow(t *testing.T) {
	samples := shotSamples(playerA, 1, 200, []bool{true})
	raw := shotRaw([]model.RawWeaponFire{fire(playerA, 200, "Galil AR")}, nil, samples)
	_, _, weapons, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	var found *model.PlayerWeaponStats
	for i := range weapons {
		if weapons[i].SteamID == playerA && weapons[i].Weapon == "Galil AR" {
			found = &weapons[i]
		}
	}
	if found == nil {
		t.Fatal("no weapon row for a fired-but-never-hit weapon")
	}
	if found.ShotsFired != 1 || found.Hits != 0 {
		t.Errorf("ShotsFired=%d Hits=%d, want 1 and 0", found.ShotsFired, found.Hits)
	}
	if found.Accuracy() != 0 {
		t.Errorf("Accuracy() = %v, want 0", found.Accuracy())
	}
}

func TestShotSampleToleranceTicks(t *testing.T) {
	cases := []struct {
		tps  float64
		want int
	}{
		{64, 6},   // ceil(4)+2
		{128, 10}, // ceil(8)+2
		{0, 0},    // unknown tickrate → no lookup
	}
	for _, c := range cases {
		if got := shotSampleToleranceTicks(c.tps); got != c.want {
			t.Errorf("shotSampleToleranceTicks(%v) = %d, want %d", c.tps, got, c.want)
		}
	}
}

// ---- Weapon bucketing ----

// TestWeaponBucket_CoversPrimaryWeapons pins the bucket for every weapon name
// the csraw2 weapon table actually emits for the guns FHHS reports on.
//
// This exists because "M4A1-S" was matched while the weapon table emits
// "M4A1", so every silenced-M4 duel fell through to "Other" — a silent
// misclassification that no test caught. The names below are the ones
// observed in the corpus; add to them, do not paraphrase them.
func TestWeaponBucket_CoversPrimaryWeapons(t *testing.T) {
	cases := map[string]string{
		"AK-47":        "AK",
		"M4A1":         "M4", // csraw2 name for the silenced M4
		"M4A1-S":       "M4", // storefront name, tolerated
		"M4A4":         "M4",
		"Galil AR":     "Galil",
		"FAMAS":        "FAMAS",
		"AUG":          "ScopedRifle",
		"SG 553":       "ScopedRifle",
		"AWP":          "AWP",
		"SSG 08":       "Scout",
		"Desert Eagle": "Deagle",

		"USP-S":         "Pistol",
		"Glock-18":      "Pistol",
		"P250":          "Pistol",
		"P2000":         "Pistol",
		"Five-SeveN":    "Pistol",
		"Tec-9":         "Pistol",
		"CZ75 Auto":     "Pistol",
		"Dual Berettas": "Pistol",
		"R8 Revolver":   "Pistol",

		// Deliberately unbucketed — SMGs, shotguns, autosnipers, utility.
		"MP9":        "Other",
		"MAC-10":     "Other",
		"P90":        "Other",
		"XM1014":     "Other",
		"Negev":      "Other",
		"HE Grenade": "Other",
		"Knife":      "Other",
	}
	for weapon, want := range cases {
		if got := weaponBucket(weapon); got != want {
			t.Errorf("weaponBucket(%q) = %q, want %q", weapon, got, want)
		}
	}
}

// ---- Pass 6: sight → first-shot delay ----

// The delay must be measured to the FIRST shot in the duel window, not the
// kill — that is what separates "no time to react" from "had time, overshot".
func TestDuelSegment_ShotDelayUsesFirstShot(t *testing.T) {
	const sightTick, firstShot, killTick = 200, 232, 360 // 0.5s to first shot at 64tps

	raw := &model.RawMatch{
		DemoHash:       "h",
		TicksPerSecond: tickRate,
		Rounds:         []model.RawRound{makeRound(1, 100, []uint64{playerA, playerB}, map[uint64]bool{playerA: true})},
		FirstSights: []model.RawFirstSight{{
			Tick: sightTick, RoundNumber: 1, ObserverID: playerA, EnemyID: playerB,
			AngleDeg: 10, ObserverPitchDeg: 0, ObserverYawDeg: 0,
		}},
		WeaponFires: []model.RawWeaponFire{
			// A shot before first sight must be ignored.
			{Tick: sightTick - 50, RoundNumber: 1, ShooterID: playerA, Weapon: "AK-47", YawDeg: 90},
			{Tick: firstShot, RoundNumber: 1, ShooterID: playerA, Weapon: "AK-47", YawDeg: 8},
			// A later shot must not override the first one.
			{Tick: firstShot + 60, RoundNumber: 1, ShooterID: playerA, Weapon: "AK-47", YawDeg: 2},
		},
		Damages: []model.RawDamage{
			{Tick: firstShot, RoundNumber: 1, AttackerSteamID: playerA, VictimSteamID: playerB,
				AttackerTeam: model.TeamT, VictimTeam: model.TeamCT,
				HealthDamage: 30, HealthDamageTaken: 30, Weapon: "AK-47", HitGroup: "chest"},
		},
		Kills: []model.RawKill{{
			Tick: killTick, RoundNumber: 1, KillerSteamID: playerA, VictimSteamID: playerB,
			KillerTeam: model.TeamT, VictimTeam: model.TeamCT, Weapon: "AK-47",
		}},
		PlayerNames: map[uint64]string{playerA: "A", playerB: "B"},
		PlayerTeams: map[uint64]model.Team{playerA: model.TeamT, playerB: model.TeamCT},
	}

	_, _, _, segs, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	var seg *model.PlayerDuelSegment
	for i := range segs {
		if segs[i].SteamID == playerA && segs[i].WeaponBucket == "AK" {
			seg = &segs[i]
		}
	}
	if seg == nil {
		t.Fatal("no AK duel segment for playerA")
	}
	// (232-200)/64*1000 = 500ms
	if seg.MedianShotDelayMs < 499 || seg.MedianShotDelayMs > 501 {
		t.Errorf("MedianShotDelayMs = %v, want ~500 (first shot, not the kill)", seg.MedianShotDelayMs)
	}
	// The segment must be tied to the round it happened in.
	if seg.RoundNumber != 1 {
		t.Errorf("RoundNumber = %d, want 1", seg.RoundNumber)
	}
}

// ---- Pass 3: per-round team resolution ----

// Sides swap at halftime, so a player's team is a property of the round, not
// of the match. This pins the case that broke: a player who appears in a round
// only through a kill (no end state) must take their side from that kill, not
// from whichever side they played most across the match.
func TestPass3_TeamComesFromTheRoundNotTheMatch(t *testing.T) {
	// playerA plays T in rounds 1-2 (so T is their match-level dominant side)
	// and CT in round 3, where they have no end state.
	firstHalf := func(n int) model.RawRound {
		return model.RawRound{
			Number: n, StartTick: 0, FreezeEndTick: 100, EndTick: 5000,
			WinnerTeam: model.TeamT,
			PlayerEndState: map[uint64]model.PlayerRoundEndState{
				playerA: {SteamID64: playerA, IsAlive: true, Team: model.TeamT},
				playerB: {SteamID64: playerB, IsAlive: false, Team: model.TeamCT},
			},
		}
	}
	// Round 3: post-swap, and playerA is absent from the end state entirely.
	third := model.RawRound{
		Number: 3, StartTick: 0, FreezeEndTick: 100, EndTick: 5000,
		WinnerTeam: model.TeamCT,
		PlayerEndState: map[uint64]model.PlayerRoundEndState{
			playerB: {SteamID64: playerB, IsAlive: false, Team: model.TeamT},
		},
	}

	raw := &model.RawMatch{
		DemoHash: "h", TicksPerSecond: tickRate,
		Rounds: []model.RawRound{firstHalf(1), firstHalf(2), third},
		Kills: []model.RawKill{
			{Tick: 200, RoundNumber: 1, KillerSteamID: playerA, VictimSteamID: playerB,
				KillerTeam: model.TeamT, VictimTeam: model.TeamCT, Weapon: "AK-47"},
			{Tick: 200, RoundNumber: 2, KillerSteamID: playerA, VictimSteamID: playerB,
				KillerTeam: model.TeamT, VictimTeam: model.TeamCT, Weapon: "AK-47"},
			// Round 3, sides swapped: playerA is CT now.
			{Tick: 200, RoundNumber: 3, KillerSteamID: playerA, VictimSteamID: playerB,
				KillerTeam: model.TeamCT, VictimTeam: model.TeamT, Weapon: "M4A1"},
		},
		PlayerNames: map[uint64]string{playerA: "A", playerB: "B"},
		PlayerTeams: map[uint64]model.Team{playerA: model.TeamT, playerB: model.TeamCT},
	}

	_, roundStats, _, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	var got model.Team
	found := false
	for _, rs := range roundStats {
		if rs.SteamID == playerA && rs.RoundNumber == 3 {
			got, found = rs.Team, true
		}
	}
	if !found {
		t.Fatal("no round-3 row for playerA")
	}
	if got != model.TeamCT {
		t.Errorf("round 3 team = %v, want CT — the match-level dominant side (T) leaked in", got)
	}
}

// No round may end up with more than five players on a side. This is the
// symptom the team-resolution bug produced at corpus scale.
func TestPass3_NoRoundExceedsFivePerSide(t *testing.T) {
	var kills []model.RawKill
	endState := map[uint64]model.PlayerRoundEndState{}
	ids := []uint64{playerA, playerB, playerC, playerD}
	teams := []model.Team{model.TeamT, model.TeamT, model.TeamCT, model.TeamCT}
	for i, id := range ids {
		endState[id] = model.PlayerRoundEndState{SteamID64: id, IsAlive: true, Team: teams[i]}
	}
	// Second-half round where nobody has an end state — every side must still
	// come from the kills, not collapse onto one team.
	swapped := model.RawRound{
		Number: 13, StartTick: 0, FreezeEndTick: 100, EndTick: 5000,
		WinnerTeam:     model.TeamT,
		PlayerEndState: map[uint64]model.PlayerRoundEndState{},
	}
	kills = append(kills,
		model.RawKill{Tick: 200, RoundNumber: 13, KillerSteamID: playerA, VictimSteamID: playerC,
			KillerTeam: model.TeamCT, VictimTeam: model.TeamT, Weapon: "AK-47"},
		model.RawKill{Tick: 300, RoundNumber: 13, KillerSteamID: playerD, VictimSteamID: playerB,
			KillerTeam: model.TeamT, VictimTeam: model.TeamCT, Weapon: "AK-47"},
	)

	raw := &model.RawMatch{
		DemoHash: "h", TicksPerSecond: tickRate,
		Rounds: []model.RawRound{
			{Number: 1, FreezeEndTick: 100, EndTick: 5000, WinnerTeam: model.TeamT, PlayerEndState: endState},
			swapped,
		},
		Kills:       kills,
		PlayerNames: map[uint64]string{playerA: "A", playerB: "B", playerC: "C", playerD: "D"},
		PlayerTeams: map[uint64]model.Team{playerA: model.TeamT, playerB: model.TeamT,
			playerC: model.TeamCT, playerD: model.TeamCT},
	}

	_, roundStats, _, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	perSide := map[int]map[model.Team]int{}
	for _, rs := range roundStats {
		if perSide[rs.RoundNumber] == nil {
			perSide[rs.RoundNumber] = map[model.Team]int{}
		}
		perSide[rs.RoundNumber][rs.Team]++
	}
	for rn, sides := range perSide {
		for team, n := range sides {
			if n > 5 {
				t.Errorf("round %d has %d players on %v, want <= 5", rn, n, team)
			}
		}
		if sides[model.TeamT] == 0 || sides[model.TeamCT] == 0 {
			t.Errorf("round %d collapsed onto one side: %v", rn, sides)
		}
	}
}
