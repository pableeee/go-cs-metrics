package cmd

import (
	"math"
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

// The whole point of count-weighting is that the merged medians do not depend
// on how the underlying duels happen to be split across rows. Splitting one
// segment into per-round segments must not move the result.
func TestMergeSegments_InvariantToRoundPartitioning(t *testing.T) {
	const id = uint64(1001)

	// One coarse row: 4 duels, median correction 4°.
	coarse := []model.PlayerDuelSegment{{
		DemoHash: "h", SteamID: id, RoundNumber: -1,
		WeaponBucket: "AK", DistanceBin: "10-15m",
		DuelCount: 4, FirstHitCount: 4, FirstHitHSCount: 1,
		MedianCorrDeg: 4, MedianSightDeg: 6, MedianExpoWinMs: 1000,
	}}

	// The same duels split per round: three rounds carrying 1, 1 and 2 duels,
	// whose count-weighted correction is also 4° ((2+2+6*2)/4).
	split := []model.PlayerDuelSegment{
		{DemoHash: "h", SteamID: id, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "10-15m",
			DuelCount: 1, FirstHitCount: 1, FirstHitHSCount: 0, MedianCorrDeg: 2, MedianSightDeg: 6, MedianExpoWinMs: 1000},
		{DemoHash: "h", SteamID: id, RoundNumber: 2, WeaponBucket: "AK", DistanceBin: "10-15m",
			DuelCount: 1, FirstHitCount: 1, FirstHitHSCount: 0, MedianCorrDeg: 2, MedianSightDeg: 6, MedianExpoWinMs: 1000},
		{DemoHash: "h", SteamID: id, RoundNumber: 3, WeaponBucket: "AK", DistanceBin: "10-15m",
			DuelCount: 2, FirstHitCount: 2, FirstHitHSCount: 1, MedianCorrDeg: 6, MedianSightDeg: 6, MedianExpoWinMs: 1000},
	}

	c := mergeSegments(id, coarse)
	s := mergeSegments(id, split)
	if len(c) != 1 || len(s) != 1 {
		t.Fatalf("expected one merged row each, got %d and %d", len(c), len(s))
	}
	if c[0].DuelCount != s[0].DuelCount || c[0].FirstHitCount != s[0].FirstHitCount ||
		c[0].FirstHitHSCount != s[0].FirstHitHSCount {
		t.Errorf("counts diverged: coarse %+v vs split %+v", c[0], s[0])
	}
	if math.Abs(c[0].MedianCorrDeg-s[0].MedianCorrDeg) > 1e-9 {
		t.Errorf("MedianCorrDeg: coarse %v vs split %v — weighting is not partition-invariant",
			c[0].MedianCorrDeg, s[0].MedianCorrDeg)
	}
}

// An unweighted mean would let a 1-duel round pull as hard as a 5-duel round.
func TestMergeSegments_WeightsMediansByDuelCount(t *testing.T) {
	const id = uint64(1001)
	segs := []model.PlayerDuelSegment{
		{SteamID: id, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "5-10m",
			DuelCount: 1, MedianCorrDeg: 10},
		{SteamID: id, RoundNumber: 2, WeaponBucket: "AK", DistanceBin: "5-10m",
			DuelCount: 9, MedianCorrDeg: 0.5},
	}
	got := mergeSegments(id, segs)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	// Weighted: (10*1 + 0.5*9) / 10 = 1.45. Unweighted would be 5.25.
	want := 1.45
	if math.Abs(got[0].MedianCorrDeg-want) > 1e-9 {
		t.Errorf("MedianCorrDeg = %v, want %v (unweighted would give 5.25)", got[0].MedianCorrDeg, want)
	}
}

// Zero-valued medians carry no information and must not drag the mean down.
func TestMergeSegments_SkipsZeroMedians(t *testing.T) {
	const id = uint64(1001)
	segs := []model.PlayerDuelSegment{
		{SteamID: id, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "5-10m",
			DuelCount: 1, MedianCorrDeg: 4},
		{SteamID: id, RoundNumber: 2, WeaponBucket: "AK", DistanceBin: "5-10m",
			DuelCount: 1, MedianCorrDeg: 0}, // never computed
	}
	got := mergeSegments(id, segs)
	if got[0].MedianCorrDeg != 4 {
		t.Errorf("MedianCorrDeg = %v, want 4 (zero median should be skipped)", got[0].MedianCorrDeg)
	}
	if got[0].DuelCount != 2 {
		t.Errorf("DuelCount = %d, want 2 (counts still sum)", got[0].DuelCount)
	}
}
