package aggregator

import (
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

// ---- Pass 17 tests: scan volatility ----

// sampleStep is the baseline sampling interval (16 Hz at 64 tickrate).
const sampleStep = 4

// makeViewSamples emits one sample per yaw value, 4 ticks apart starting at
// startTick, all alive with no enemies visible.
func makeViewSamples(id uint64, round, startTick int, yaws []float64) []model.RawViewSample {
	out := make([]model.RawViewSample, len(yaws))
	for i, y := range yaws {
		out[i] = model.RawViewSample{
			Tick:        startTick + i*sampleStep,
			RoundNumber: round,
			SteamID:     id,
			YawDeg:      y,
			HP:          100,
		}
	}
	return out
}

func scanRaw(samples []model.RawViewSample) *model.RawMatch {
	return &model.RawMatch{
		DemoHash:       "h",
		TicksPerSecond: tickRate,
		Rounds:         []model.RawRound{makeRound(1, 100, []uint64{playerA}, map[uint64]bool{playerA: true})},
		ViewSamples:    samples,
		PlayerNames:    map[uint64]string{playerA: "A"},
		PlayerTeams:    map[uint64]model.Team{playerA: model.TeamT},
	}
}

// TestPass17_Settled: constant yaw → 100% dwell, no reversals.
func TestPass17_Settled(t *testing.T) {
	yaws := make([]float64, 33) // 32 steps of 0.0625 s = 2 s
	raw := scanRaw(makeViewSamples(playerA, 1, 200, yaws))
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result for playerA round 1")
	}
	if !almostEqual(res.oocSeconds, 2.0) {
		t.Errorf("oocSeconds = %v, want 2.0", res.oocSeconds)
	}
	if !almostEqual(res.dwellSec, 2.0) {
		t.Errorf("dwellSec = %v, want 2.0 (fully settled)", res.dwellSec)
	}
	if res.reversals != 0 {
		t.Errorf("reversals = %d, want 0", res.reversals)
	}
}

// TestPass17_OneWaySweep: steady +5°/step (80 °/s) → zero dwell, zero
// reversals (fast but never changes direction — deliberate clearing).
func TestPass17_OneWaySweep(t *testing.T) {
	yaws := make([]float64, 17)
	for i := range yaws {
		yaws[i] = float64(i) * 5
	}
	raw := scanRaw(makeViewSamples(playerA, 1, 200, yaws))
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result")
	}
	if res.dwellSec != 0 {
		t.Errorf("dwellSec = %v, want 0", res.dwellSec)
	}
	if res.reversals != 0 {
		t.Errorf("reversals = %d, want 0 (one-way sweep)", res.reversals)
	}
}

// TestPass17_Swiping: yaw oscillates ±10° every step (160 °/s legs) → every
// direction change past the first leg counts as a reversal.
func TestPass17_Swiping(t *testing.T) {
	yaws := make([]float64, 17)
	for i := range yaws {
		if i%2 == 1 {
			yaws[i] = 10
		}
	}
	raw := scanRaw(makeViewSamples(playerA, 1, 200, yaws))
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result")
	}
	// 16 steps alternate direction: first sets lastDir, the other 15 flip.
	if res.reversals != 15 {
		t.Errorf("reversals = %d, want 15", res.reversals)
	}
	if res.dwellSec != 0 {
		t.Errorf("dwellSec = %v, want 0", res.dwellSec)
	}
}

// TestPass17_YawWrap: crossing the ±180° seam is a small delta, not ~360°.
func TestPass17_YawWrap(t *testing.T) {
	yaws := []float64{178, 179, 180 - 0.5, -179.5, -178.5} // steady ~+1°/step through the seam
	raw := scanRaw(makeViewSamples(playerA, 1, 200, yaws))
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result")
	}
	// 4 steps × ~1° ≈ 4° total travel; a wrap bug would give ~360°.
	if res.absYawDeg > 10 {
		t.Errorf("absYawDeg = %v, want ~4 (yaw wrap mishandled)", res.absYawDeg)
	}
}

// TestPass17_FlickClamp: a single 170° step (flick/teleport at 16 Hz) is
// dropped from both time and travel.
func TestPass17_FlickClamp(t *testing.T) {
	yaws := []float64{0, 0, 170, 170, 170}
	raw := scanRaw(makeViewSamples(playerA, 1, 200, yaws))
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result")
	}
	// 4 steps, 1 clamped → 3 × 0.0625 s counted, all settled.
	if !almostEqual(res.oocSeconds, 3*0.0625) {
		t.Errorf("oocSeconds = %v, want %v (flick step dropped)", res.oocSeconds, 3*0.0625)
	}
	if res.absYawDeg != 0 {
		t.Errorf("absYawDeg = %v, want 0 (flick travel excluded)", res.absYawDeg)
	}
}

// TestPass17_CombatExclusion: a kill by the player at tick 300 excludes all
// samples within ±2 s (±128 ticks).
func TestPass17_CombatExclusion(t *testing.T) {
	// Samples from tick 200 to 800 (150 steps). Combat window covers
	// [172, 428]; pairs fully outside it start at tick ≥ 432.
	yaws := make([]float64, 151)
	samples := makeViewSamples(playerA, 1, 200, yaws)
	raw := scanRaw(samples)
	raw.Kills = []model.RawKill{{
		Tick: 300, RoundNumber: 1,
		KillerSteamID: playerA, VictimSteamID: playerB,
		KillerTeam: model.TeamT, VictimTeam: model.TeamCT,
	}}
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result")
	}
	// Qualifying pairs: both endpoints > 428 → prev ≥ 432: ticks 432..800,
	// 92 steps × 0.0625 s = 5.75 s.
	want := 92 * 0.0625
	if !almostEqual(res.oocSeconds, want) {
		t.Errorf("oocSeconds = %v, want %v (combat window excluded)", res.oocSeconds, want)
	}
}

// TestPass17_EnemiesVisibleExcluded: samples with an enemy on screen don't count.
func TestPass17_EnemiesVisibleExcluded(t *testing.T) {
	samples := makeViewSamples(playerA, 1, 200, make([]float64, 11))
	for i := 4; i < 8; i++ {
		samples[i].EnemiesVisible = true
	}
	raw := scanRaw(samples)
	res := computeScanVolatility(raw)[playerA][1]
	if res == nil {
		t.Fatal("no scan result")
	}
	// 10 steps, 4 with a visible-enemy endpoint → 6 counted.
	if !almostEqual(res.oocSeconds, 6*0.0625) {
		t.Errorf("oocSeconds = %v, want %v", res.oocSeconds, 6*0.0625)
	}
}

// TestPass17_DeadAndPreFreezeSkipped: HP 0 samples and pre-freeze-end ticks
// contribute nothing.
func TestPass17_DeadAndPreFreezeSkipped(t *testing.T) {
	samples := makeViewSamples(playerA, 1, 40, make([]float64, 11)) // ticks 40..80 < freezeEnd 100
	dead := makeViewSamples(playerA, 1, 200, make([]float64, 11))
	for i := range dead {
		dead[i].HP = 0
	}
	raw := scanRaw(append(samples, dead...))
	if res := computeScanVolatility(raw)[playerA][1]; res != nil {
		t.Errorf("scan result = %+v, want none (all samples dead or pre-freeze)", res)
	}
}

// TestPass17_EndToEnd: Aggregate populates round and match scan fields.
func TestPass17_EndToEnd(t *testing.T) {
	yaws := make([]float64, 33)
	raw := scanRaw(makeViewSamples(playerA, 1, 200, yaws))
	matchStats, roundStats, _, _, _, _, err := Aggregate(raw)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	var ms *model.PlayerMatchStats
	for i := range matchStats {
		if matchStats[i].SteamID == playerA {
			ms = &matchStats[i]
		}
	}
	if ms == nil {
		t.Fatal("no match stats for playerA")
	}
	if !almostEqual(ms.ScanOOCSeconds, 2.0) || !almostEqual(ms.ScanDwellPct, 100.0) {
		t.Errorf("match scan = %.3fs / %.1f%%, want 2.0s / 100%%", ms.ScanOOCSeconds, ms.ScanDwellPct)
	}
	found := false
	for _, rs := range roundStats {
		if rs.SteamID == playerA && rs.RoundNumber == 1 {
			found = true
			if !almostEqual(rs.ScanOOCSeconds, 2.0) || !almostEqual(rs.ScanDwellPct, 100.0) {
				t.Errorf("round scan = %.3fs / %.1f%%, want 2.0s / 100%%", rs.ScanOOCSeconds, rs.ScanDwellPct)
			}
		}
	}
	if !found {
		t.Error("no round stats row for playerA round 1")
	}
}
