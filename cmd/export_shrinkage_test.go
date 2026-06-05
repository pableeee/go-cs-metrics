package cmd

import (
	"math"
	"testing"

	"github.com/pable/go-cs-metrics/internal/storage"
)

// makeDemo builds a single-demo stat line for one player. Stats are scaled by
// rounds so KPR/DPR/ADR are stable regardless of how many demos we stack.
func makeDemo(steam, name, hash string, rounds int, kpr, dpr, adr float64) storage.PlayerDemoTotals {
	return storage.PlayerDemoTotals{
		SteamID:      steam,
		Name:         name,
		DemoHash:     hash,
		Kills:        int(kpr * float64(rounds)),
		Deaths:       int(dpr * float64(rounds)),
		Assists:      int(0.1 * float64(rounds)),
		KastRounds:   int(0.72 * float64(rounds)),
		RoundsPlayed: rounds,
		TotalDamage:  int(adr * float64(rounds)),
	}
}

func uniformWeights(byDemo []storage.PlayerDemoTotals) map[string]float64 {
	w := make(map[string]float64)
	for _, d := range byDemo {
		w[d.DemoHash] = 1.0
	}
	return w
}

// A high-performing player with very few rounds should, after shrinkage, end up
// much closer to the prior than the same player gets with no shrinkage.
func TestShrinkagePullsThinSampleTowardPrior(t *testing.T) {
	// One star player, a single short demo (22 rounds) of inflated production.
	byDemo := []storage.PlayerDemoTotals{
		makeDemo("p1", "star", "h1", 22, 1.10, 0.50, 120),
	}
	w := uniformWeights(byDemo)

	rawRatings, _ := buildWeightedRatings(byDemo, w, -1, 0) // shrinkage disabled
	raw := rawRatings[0]

	const prior = 1.00
	shrRatings, _ := buildWeightedRatings(byDemo, w, prior, 200) // strong shrink
	shr := shrRatings[0]

	if !(raw > shr) {
		t.Fatalf("expected shrunk rating (%.3f) below raw rating (%.3f)", shr, raw)
	}
	if math.Abs(shr-prior) >= math.Abs(raw-prior) {
		t.Fatalf("shrunk rating %.3f not closer to prior %.2f than raw %.3f", shr, prior, raw)
	}
	// 22 rounds with k=200 -> factor 22/222 ≈ 0.099; rating should sit within ~15%% of prior.
	if math.Abs(shr-prior) > 0.15*math.Abs(raw-prior)+1e-9 {
		t.Fatalf("expected heavy pull toward prior; got shr=%.3f raw=%.3f prior=%.2f", shr, raw, prior)
	}
}

// A player with a large sample should be barely affected by shrinkage.
func TestShrinkageBarelyMovesLargeSample(t *testing.T) {
	byDemo := make([]storage.PlayerDemoTotals, 0, 40)
	for i := 0; i < 40; i++ { // 40 demos × 22 rounds = 880 rounds
		byDemo = append(byDemo, makeDemo("p1", "vet", string(rune('a'+i)), 22, 0.80, 0.65, 80))
	}
	w := uniformWeights(byDemo)

	raw, _ := buildWeightedRatings(byDemo, w, -1, 0)
	shr, _ := buildWeightedRatings(byDemo, w, 1.00, 200)

	if math.Abs(raw[0]-shr[0]) > 0.05 {
		t.Fatalf("large sample should barely shrink: raw=%.3f shr=%.3f", raw[0], shr[0])
	}
}

// Shrinkage disabled (shrinkRounds<=0 or priorMean<0) must reproduce raw ratings.
func TestShrinkageDisabledIsNoOp(t *testing.T) {
	byDemo := []storage.PlayerDemoTotals{
		makeDemo("p1", "a", "h1", 22, 1.10, 0.50, 120),
	}
	w := uniformWeights(byDemo)

	base, _ := buildWeightedRatings(byDemo, w, -1, 0)
	// priorMean set but shrinkRounds 0 -> no-op
	z1, _ := buildWeightedRatings(byDemo, w, 1.00, 0)
	// shrinkRounds set but priorMean negative -> no-op
	z2, _ := buildWeightedRatings(byDemo, w, -1, 200)

	if base[0] != z1[0] || base[0] != z2[0] {
		t.Fatalf("disabled shrinkage changed rating: base=%.3f z1=%.3f z2=%.3f", base[0], z1[0], z2[0])
	}
}
