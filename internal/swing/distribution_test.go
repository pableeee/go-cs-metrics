package swing

import (
	"math"
	"testing"
)

// Two weapon classes, asymmetric outcomes: the slices must reconstruct each
// player's totals and each class must be zero-sum across players — a kill's
// bucket is shared by both parties, so nothing can leak between classes.
func TestCompute_WeaponSlicesReconstructTotals(t *testing.T) {
	var rounds []Round
	var kills []Kill
	for r := 1; r <= 24; r++ {
		rounds = append(rounds, mkRound("h", r, r%2 == 0))
		k1 := mkKill("h", r, 100, 1, 6, true)
		k1.WeaponBucket = "rifle"
		k2 := mkKill("h", r, 200, 7, 2, false)
		k2.WeaponBucket = "pistol"
		kills = append(kills, k1, k2)
	}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	res := Compute(rounds, kills, rt, dt)
	if err := Validate(res); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	for id, p := range res {
		var round, duel float64
		var duels int
		for _, s := range p.Weapons {
			round += s.RoundSwingTotal
			duel += s.DuelSwingTotal
			duels += s.Duels
		}
		if duels != p.Duels {
			t.Errorf("player %d: weapon duels %d != %d", id, duels, p.Duels)
		}
		if math.Abs(round-p.RoundSwingTotal) > 1e-9 || math.Abs(duel-p.DuelSwingTotal) > 1e-9 {
			t.Errorf("player %d: weapon sums do not reconstruct totals", id)
		}
	}

	// Player 1 dueled only with rifles, player 7 only with pistols.
	if res[1].Weapons["pistol"] != nil || res[7].Weapons["rifle"] != nil {
		t.Error("players have slices for classes they never dueled in")
	}
	if got := res[1].Weapons["rifle"].Duels; got != 24 {
		t.Errorf("player 1 rifle duels = %d, want 24", got)
	}
}

// A corrupted weapon slice must trip the new Validate checks.
func TestValidate_DetectsWeaponLeak(t *testing.T) {
	rounds := []Round{mkRound("h", 1, true)}
	kills := []Kill{mkKill("h", 1, 100, 1, 6, true)}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	res := Compute(rounds, kills, rt, dt)
	res[1].Weapons["rifle"].DuelSwingTotal += 0.25
	if err := Validate(res); err == nil {
		t.Error("Validate accepted a corrupted weapon slice")
	}
}

// Per-demo attribution must be exactly decomposable: summed over demos, every
// player's numbers reconstruct the single full-corpus Compute.
func TestComputeByDemo_ReconstructsFullCorpus(t *testing.T) {
	var rounds []Round
	var kills []Kill
	for _, hash := range []string{"demoA", "demoB"} {
		for r := 1; r <= 20; r++ {
			rounds = append(rounds, mkRound(hash, r, r%2 == 0))
			kills = append(kills,
				mkKill(hash, r, 100, 1, 6, true),
				mkKill(hash, r, 200, 7, 2, false),
			)
		}
	}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	full := Compute(rounds, kills, rt, dt)
	byDemo := ComputeByDemo(rounds, kills, rt, dt)

	if len(byDemo) != 2 {
		t.Fatalf("got %d demos, want 2", len(byDemo))
	}
	sums := map[uint64]*PlayerSwing{}
	for _, players := range byDemo {
		if err := Validate(players); err != nil {
			t.Fatalf("per-demo Validate: %v", err)
		}
		for id, p := range players {
			s := sums[id]
			if s == nil {
				s = &PlayerSwing{SteamID: id}
				sums[id] = s
			}
			s.Rounds += p.Rounds
			s.Duels += p.Duels
			s.RoundSwingTotal += p.RoundSwingTotal
			s.DuelSwingTotal += p.DuelSwingTotal
		}
	}
	for id, want := range full {
		got := sums[id]
		if got == nil {
			t.Fatalf("player %d missing from per-demo sums", id)
		}
		if got.Rounds != want.Rounds || got.Duels != want.Duels {
			t.Errorf("player %d: rounds/duels %d/%d, want %d/%d", id, got.Rounds, got.Duels, want.Rounds, want.Duels)
		}
		if math.Abs(got.RoundSwingTotal-want.RoundSwingTotal) > 1e-9 ||
			math.Abs(got.DuelSwingTotal-want.DuelSwingTotal) > 1e-9 {
			t.Errorf("player %d: per-demo totals do not reconstruct the corpus", id)
		}
	}
}

// FloorCeilings must order its quantiles, honour both floors, and drop
// fragment appearances from the distribution.
func TestFloorCeilings(t *testing.T) {
	byDemo := map[string]map[uint64]*PlayerSwing{}
	// Player 1: 5 full demos with varying rates. Player 2: only 2 demos.
	// Player 3: many demos but all below the per-demo round floor.
	for i, rate := range []float64{-0.02, 0.01, 0.03, 0.00, 0.05} {
		hash := string(rune('a' + i))
		byDemo[hash] = map[uint64]*PlayerSwing{
			1: {SteamID: 1, Rounds: 24, Duels: 30, RoundSwingPerRound: rate, DuelSwingPerDuel: rate * 2},
			3: {SteamID: 3, Rounds: 5, Duels: 6, RoundSwingPerRound: 0.9},
		}
		if i < 2 {
			byDemo[hash][2] = &PlayerSwing{SteamID: 2, Rounds: 24, Duels: 30, RoundSwingPerRound: rate}
		}
	}
	fc := FloorCeilings(byDemo, 3, 10)

	if _, ok := fc[2]; ok {
		t.Error("player 2 has 2 demos < minDemos 3, should be omitted")
	}
	if _, ok := fc[3]; ok {
		t.Error("player 3 has only sub-floor demos, should be omitted")
	}
	p1, ok := fc[1]
	if !ok {
		t.Fatal("player 1 missing")
	}
	if p1.Demos != 5 || p1.RoundsTotal != 120 {
		t.Errorf("player 1: demos=%d rounds=%d, want 5/120", p1.Demos, p1.RoundsTotal)
	}
	if !(p1.RndFloor <= p1.RndMedian && p1.RndMedian <= p1.RndCeiling) {
		t.Errorf("quantiles out of order: %v %v %v", p1.RndFloor, p1.RndMedian, p1.RndCeiling)
	}
	if math.Abs(p1.RndMedian-0.01) > 1e-9 {
		t.Errorf("median = %v, want 0.01", p1.RndMedian)
	}
	// Sorted rates: -0.02, 0, 0.01, 0.03, 0.05 → p25 = 0, p75 = 0.03.
	if math.Abs(p1.RndFloor-0.0) > 1e-9 || math.Abs(p1.RndCeiling-0.03) > 1e-9 {
		t.Errorf("floor/ceiling = %v/%v, want 0/0.03", p1.RndFloor, p1.RndCeiling)
	}
}

func TestQuantile(t *testing.T) {
	s := []float64{1, 2, 3, 4}
	for _, c := range []struct{ q, want float64 }{
		{0, 1}, {1, 4}, {0.5, 2.5}, {0.25, 1.75},
	} {
		if got := quantile(s, c.q); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("quantile(%v) = %v, want %v", c.q, got, c.want)
		}
	}
	if got := quantile([]float64{7}, 0.9); got != 7 {
		t.Errorf("single element = %v, want 7", got)
	}
	if got := quantile(nil, 0.5); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
}
