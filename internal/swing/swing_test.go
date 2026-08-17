package swing

import (
	"math"
	"testing"
)

func mkRound(hash string, n int, ctWon bool) Round {
	return Round{
		DemoHash: hash, RoundNumber: n, CTWon: ctWon,
		PlayersCT: []uint64{1, 2, 3, 4, 5},
		PlayersT:  []uint64{6, 7, 8, 9, 10},
	}
}

func mkKill(hash string, rn, tick int, killer, victim uint64, killerCT bool) Kill {
	return Kill{
		DemoHash: hash, RoundNumber: rn, Tick: tick,
		KillerID: killer, VictimID: victim, KillerIsCT: killerCT,
		DistanceBin: "10-15m", WeaponBucket: "rifle",
		KillerSawFirst: true, VictimSawFirst: true,
	}
}

// The duel table must be built from both sides of every duel. Entering only
// the observed direction would make every cell 1.0 — the killer always won.
func TestBuildDuelTable_MirrorsBothSides(t *testing.T) {
	var kills []Kill
	for i := 0; i < 100; i++ {
		k := mkKill("h", 1, i, 1, 6, true)
		k.SightAdvantageMs = 500 // killer spotted first every time
		kills = append(kills, k)
	}
	dt := BuildDuelTable(kills)

	// Winner spotted 500ms first → "+400ms" bucket, should be ~1.0.
	p, n := dt.P("+400ms", "n/a", "10-15m", "rifle")
	if n < 100 || p < 0.99 {
		t.Errorf("P(win | +400ms) = %.3f (n=%d), want ~1.0", p, n)
	}
	// The mirrored side sat at -500ms and always lost → should be ~0.0.
	p2, n2 := dt.P("-400ms", "n/a", "10-15m", "rifle")
	if n2 < 100 || p2 > 0.01 {
		t.Errorf("P(win | -400ms) = %.3f (n=%d), want ~0.0", p2, n2)
	}
}

// Round swing must be zero-sum: every probability point one player gains is
// taken from an opponent.
func TestCompute_RoundSwingIsZeroSum(t *testing.T) {
	var rounds []Round
	var kills []Kill
	for r := 1; r <= 40; r++ {
		ctWon := r%3 != 0
		rounds = append(rounds, mkRound("h", r, ctWon))
		// A few kills alternating sides so states are populated.
		kills = append(kills,
			mkKill("h", r, 100, 1, 6, true),
			mkKill("h", r, 200, 7, 2, false),
			mkKill("h", r, 300, 3, 8, true),
		)
	}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	res := Compute(rounds, kills, rt, dt)
	if err := Validate(res); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// P(win) must rise with man advantage, or the metric is not measuring leverage.
//
// The corpus is built so the three states genuinely differ: every round visits
// 5v5, only some reach 5v4, and only CT-win rounds go deeper. A fixture where
// all rounds pass through the same states cannot test monotonicity at all.
func TestRoundTable_LateKillsAreWorthMore(t *testing.T) {
	var rounds []Round
	var kills []Kill
	add := func(n int, ctWon bool, tDeaths, ctDeaths int) {
		for i := 0; i < n; i++ {
			r := len(rounds) + 1
			rounds = append(rounds, mkRound("h", r, ctWon))
			tick := 100
			for j := 0; j < tDeaths; j++ {
				kills = append(kills, mkKill("h", r, tick, 1, uint64(6+j), true))
				tick += 10
			}
			for j := 0; j < ctDeaths; j++ {
				kills = append(kills, mkKill("h", r, tick, 6, uint64(1+j), false))
				tick += 10
			}
		}
	}
	// CT wins by grinding the T side down: visits 5v5, 5v4, 5v3, 5v2.
	add(200, true, 4, 0)
	// T wins without losing anyone: visits 5v5, then 4v5, 3v5, 2v5.
	add(200, false, 0, 4)
	// T wins after trading one: visits 5v5, 5v4, then 4v4, 3v4, 2v4.
	add(200, false, 1, 3)

	rt := BuildRoundTable(rounds, kills)
	p5, n5 := rt.P(5, 5, false)
	p4, n4 := rt.P(5, 4, false)
	p3, n3 := rt.P(5, 3, false)
	if n5 < MinCellSamples || n4 < MinCellSamples || n3 < MinCellSamples {
		t.Fatalf("insufficient samples: 5v5 n=%d, 5v4 n=%d, 5v3 n=%d", n5, n4, n3)
	}
	if !(p3 > p4 && p4 > p5) {
		t.Errorf("P should rise with man advantage: 5v5=%.3f 5v4=%.3f 5v3=%.3f", p5, p4, p3)
	}
}

// Wiping the enemy team is a certain win; being wiped is a certain loss.
// These are boundary conditions, not empirical cells.
func TestRoundTable_TerminalStates(t *testing.T) {
	rt := BuildRoundTable(nil, nil)
	if p, _ := rt.P(3, 0, false); p != 1 {
		t.Errorf("P(CT wins | 3v0) = %v, want 1", p)
	}
	if p, _ := rt.P(0, 3, false); p != 0 {
		t.Errorf("P(CT wins | 0v3) = %v, want 0", p)
	}
}

// Sparse cells must fall back rather than return a wild frequency from n=1.
func TestRoundTable_BacksOffBelowSampleFloor(t *testing.T) {
	rounds := []Round{mkRound("h", 1, true)}
	kills := []Kill{mkKill("h", 1, 100, 1, 6, true)}
	rt := BuildRoundTable(rounds, kills)
	// 5v4 was visited exactly once — well under the floor.
	p, n := rt.P(5, 4, false)
	if n >= MinCellSamples {
		t.Fatalf("expected a below-floor cell, got n=%d", n)
	}
	if p != rt.base {
		t.Errorf("P = %v, want the corpus base rate %v", p, rt.base)
	}
}

// Validate must actually catch a leak, or it is decorative.
func TestValidate_DetectsLeak(t *testing.T) {
	res := map[uint64]*PlayerSwing{
		1: {SteamID: 1, RoundSwingTotal: 0.5},
		2: {SteamID: 2, RoundSwingTotal: -0.4}, // 0.1 unaccounted
	}
	if err := Validate(res); err == nil {
		t.Error("Validate accepted a non-zero-sum result")
	}
}

func TestAdvantageBucket(t *testing.T) {
	cases := []struct {
		ms                float64
		killerSaw, vicSaw bool
		want              string
	}{
		{0, true, false, "unseen"},
		{0, false, true, "blind"},
		{0, false, false, "neither"},
		{1500, true, true, "+1000ms"},
		{500, true, true, "+400ms"},
		{200, true, true, "+150ms"},
		{0, true, true, "even"},
		{-200, true, true, "-150ms"},
		{-2000, true, true, "-1000ms"},
	}
	for _, c := range cases {
		if got := AdvantageBucket(c.ms, c.killerSaw, c.vicSaw); got != c.want {
			t.Errorf("AdvantageBucket(%v, %v, %v) = %q, want %q", c.ms, c.killerSaw, c.vicSaw, got, c.want)
		}
	}
}

func TestDistanceBinAndWeaponClass(t *testing.T) {
	if got := DistanceBin(12); got != "10-15m" {
		t.Errorf("DistanceBin(12) = %q", got)
	}
	if got := DistanceBin(-1); got != "unknown" {
		t.Errorf("DistanceBin(-1) = %q", got)
	}
	// The csraw2 weapon table calls the silenced M4 "M4A1" — the same trap
	// that put 44k duels in the wrong FHHS bucket.
	if got := WeaponClass("M4A1"); got != "rifle" {
		t.Errorf("WeaponClass(\"M4A1\") = %q, want rifle", got)
	}
	if got := WeaponClass("AWP"); got != "sniper" {
		t.Errorf("WeaponClass(\"AWP\") = %q", got)
	}
}

// Beating the odds must score higher than confirming them.
//
// The corpus gives the sighting advantage real predictive power (80/20), which
// the previous version of this test did not: with a 50/50 corpus every duel is
// worth the same and the assertion is vacuous.
func TestCompute_DuelSwingRewardsUnlikelyWins(t *testing.T) {
	var rounds []Round
	var kills []Kill
	add := func(n int, killer, victim uint64, advMs float64) {
		for i := 0; i < n; i++ {
			r := len(rounds) + 1
			rounds = append(rounds, mkRound("h", r, true))
			k := mkKill("h", r, 100, killer, victim, true)
			k.SightAdvantageMs = advMs
			kills = append(kills, k)
		}
	}
	add(400, 1, 6, 800)  // player 1 wins from a big advantage — the common case
	add(100, 3, 8, -800) // player 3 wins from a big disadvantage — the rare one

	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)

	// Sanity: the table should have learned that seeing first wins ~80%.
	if p, _ := dt.P("+400ms", "n/a", "10-15m", "rifle"); p < 0.75 || p > 0.85 {
		t.Fatalf("P(win | +400ms) = %.3f, want ~0.80 — fixture does not encode an advantage", p)
	}

	res := Compute(rounds, kills, rt, dt)
	if err := Validate(res); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res[3].DuelSwingPerDuel <= res[1].DuelSwingPerDuel {
		t.Errorf("winning from a disadvantage should score higher: disadvantaged=%.4f favoured=%.4f",
			res[3].DuelSwingPerDuel, res[1].DuelSwingPerDuel)
	}
	if math.Abs(res[1].DuelSwingPerDuel) > math.Abs(res[3].DuelSwingPerDuel) {
		t.Errorf("expected wins should score near zero, got %.4f", res[1].DuelSwingPerDuel)
	}
}

// A sighting lead decays. Two duels with the same advantage but different
// freshness must not be given the same expectation — that conflation is what
// made the advantage axis look non-monotonic on the real corpus.
func TestDuelTable_FreshnessSeparatesStaleLeads(t *testing.T) {
	var kills []Kill
	// killerAdvMs is always from the killer's point of view, so a negative
	// value means the player who won had been at a sighting disadvantage.
	add := func(n int, killerAdvMs, mutualMs float64) {
		for i := 0; i < n; i++ {
			k := mkKill("h", 1, len(kills), 1, 6, true)
			k.SightAdvantageMs = killerAdvMs
			k.MutualAwarenessMs = mutualMs
			k.MutualKnown = true
			kills = append(kills, k)
		}
	}
	// Fresh: the sighting lead converts 90% of the time.
	add(90, 800, 100)
	add(10, -800, 100)
	// Stale: the same lead is worth nothing.
	add(50, 800, 5000)
	add(50, -800, 5000)

	dt := BuildDuelTable(kills)
	fresh, nf := dt.P("+400ms", "<300ms", "10-15m", "rifle")
	stale, ns := dt.P("+400ms", ">3s", "10-15m", "rifle")
	if nf < MinCellSamples || ns < MinCellSamples {
		t.Fatalf("insufficient samples: fresh n=%d stale n=%d", nf, ns)
	}
	if fresh <= stale {
		t.Errorf("fresh lead %.3f should beat stale lead %.3f", fresh, stale)
	}
	if stale < 0.4 || stale > 0.6 {
		t.Errorf("stale lead = %.3f, want ~0.5 (the lead should have decayed)", stale)
	}
	if fresh < 0.8 {
		t.Errorf("fresh lead = %.3f, want ~0.9", fresh)
	}
}

// Freshness is symmetric, so mirroring must not change it — otherwise the two
// sides of one duel would be scored against different cells.
func TestFreshnessBucket_IsSymmetricAndHandlesUnknown(t *testing.T) {
	if got := FreshnessBucket(0, false); got != "n/a" {
		t.Errorf("FreshnessBucket(_, false) = %q, want n/a", got)
	}
	for _, c := range []struct {
		ms   float64
		want string
	}{{100, "<300ms"}, {500, "300ms-1s"}, {2000, "1-3s"}, {9000, ">3s"}} {
		if got := FreshnessBucket(c.ms, true); got != c.want {
			t.Errorf("FreshnessBucket(%v) = %q, want %q", c.ms, got, c.want)
		}
	}
}

// mkSwapRound puts 1-5 on CT and 6-10 on T for the first half and swaps them
// for the second, which is what makes the side split non-trivial: without a
// halftime swap every player has one empty side.
func mkSwapRound(hash string, n int, ctWon bool) Round {
	first := []uint64{1, 2, 3, 4, 5}
	second := []uint64{6, 7, 8, 9, 10}
	if n > 12 {
		first, second = second, first
	}
	return Round{
		DemoHash: hash, RoundNumber: n, CTWon: ctWon,
		PlayersCT: first, PlayersT: second,
	}
}

// The whole point of the split: a player who beats expectation on one side and
// misses it by the same margin on the other has a combined number of zero. If
// the sides were not attributed separately that player would be indistinguishable
// from someone who was exactly average on both.
func TestCompute_SideSplitSeparatesOpposingSides(t *testing.T) {
	var rounds []Round
	var kills []Kill
	for r := 1; r <= 24; r++ {
		rounds = append(rounds, mkSwapRound("h", r, r%2 == 0))
		if r <= 12 {
			// Player 1 is CT and wins every duel.
			kills = append(kills, mkKill("h", r, 100, 1, 6, true))
		} else {
			// Sides swapped: player 6 is now CT, player 1 is T and loses.
			kills = append(kills, mkKill("h", r, 100, 6, 1, true))
		}
	}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	res := Compute(rounds, kills, rt, dt)
	if err := Validate(res); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	p := res[1]
	if p.Rounds != 24 || p.CT.Rounds != 12 || p.T.Rounds != 12 {
		t.Fatalf("rounds = %d (CT %d, T %d), want 24 (12, 12)", p.Rounds, p.CT.Rounds, p.T.Rounds)
	}
	if p.CT.Duels != 12 || p.T.Duels != 12 || p.CT.DuelsWon != 12 || p.T.DuelsWon != 0 {
		t.Fatalf("duels = CT %d/%d won, T %d/%d won; want CT 12/12, T 0/12",
			p.CT.DuelsWon, p.CT.Duels, p.T.DuelsWon, p.T.Duels)
	}

	// Every duel is mutually-sighted and even, so mirroring pins P at exactly
	// 0.500 and each outcome is worth ±0.5.
	if math.Abs(p.DuelSwingPerDuel) > 1e-9 {
		t.Errorf("combined DUEL/DUEL = %+.4f, want 0 — the sides should cancel", p.DuelSwingPerDuel)
	}
	if math.Abs(p.CT.DuelSwingPerDuel-0.5) > 1e-9 {
		t.Errorf("CT DUEL/DUEL = %+.4f, want +0.5", p.CT.DuelSwingPerDuel)
	}
	if math.Abs(p.T.DuelSwingPerDuel+0.5) > 1e-9 {
		t.Errorf("T DUEL/DUEL = %+.4f, want -0.5", p.T.DuelSwingPerDuel)
	}
}

// A side is not zero-sum on its own — only the two together are. The report
// tells readers to compare CT against CT for this reason, so pin the property
// that makes that advice necessary.
func TestCompute_SidesAreNotIndividuallyZeroSum(t *testing.T) {
	var rounds []Round
	var kills []Kill
	for r := 1; r <= 24; r++ {
		rounds = append(rounds, mkSwapRound("h", r, true))
		// Every duel is mutually-sighted and even, so its cell is pinned at
		// exactly 0.500 by mirroring no matter who actually wins. CT then wins
		// three out of every four, which the 50/50 expectation cannot absorb.
		ctWinsDuel := r%4 != 0
		first, second := uint64(1), uint64(6) // CT, T for the first half
		if r > 12 {
			first, second = 6, 1
		}
		killer, victim, killerIsCT := first, second, true
		if !ctWinsDuel {
			killer, victim, killerIsCT = second, first, false
		}
		kills = append(kills, mkKill("h", r, 100, killer, victim, killerIsCT))
	}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	res := Compute(rounds, kills, rt, dt)
	if err := Validate(res); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	var ctSum, tSum, total float64
	for _, p := range res {
		ctSum += p.CT.DuelSwingTotal
		tSum += p.T.DuelSwingTotal
		total += p.DuelSwingTotal
	}
	if math.Abs(total) > 1e-6 {
		t.Fatalf("combined duel swing = %.9f, want 0", total)
	}
	if math.Abs(ctSum+tSum) > 1e-6 {
		t.Fatalf("CT + T = %.9f, want 0", ctSum+tSum)
	}
	// 18 CT duel wins at +0.5 and 6 losses at -0.5 against a flat 0.500
	// expectation: +6.0 for CT, the mirror image for T.
	if math.Abs(ctSum-6) > 1e-9 || math.Abs(tSum+6) > 1e-9 {
		t.Errorf("CT sum = %+.4f, T sum = %+.4f; want +6 / -6 — neither side is zero-sum alone", ctSum, tSum)
	}
}

// The reported error bar must be the real Bernoulli standard error, not a
// rule-of-thumb. Every duel in this fixture is mutually-sighted and even, so
// mirroring pins P at exactly 0.500 and Σp(1−p) is exactly n/4 — which makes
// the closed form 0.5/√n and lets the accumulator be checked against it.
func TestCompute_DuelSwingStandardError(t *testing.T) {
	var rounds []Round
	var kills []Kill
	for r := 1; r <= 24; r++ {
		rounds = append(rounds, mkSwapRound("h", r, r%2 == 0))
		if r <= 12 {
			kills = append(kills, mkKill("h", r, 100, 1, 6, true))
		} else {
			kills = append(kills, mkKill("h", r, 100, 6, 1, true))
		}
	}
	rt := BuildRoundTable(rounds, kills)
	dt := BuildDuelTable(kills)
	p := Compute(rounds, kills, rt, dt)[1]

	if got, want := p.DuelVarSum, 0.25*float64(p.Duels); math.Abs(got-want) > 1e-9 {
		t.Fatalf("DuelVarSum = %.6f, want %.6f (n/4 at p=0.5)", got, want)
	}
	for _, c := range []struct {
		label string
		se    float64
		n     int
	}{
		{"overall", p.DuelSwingSE, p.Duels},
		{"CT", p.CT.DuelSwingSE, p.CT.Duels},
		{"T", p.T.DuelSwingSE, p.T.Duels},
	} {
		want := 0.5 / math.Sqrt(float64(c.n))
		if math.Abs(c.se-want) > 1e-9 {
			t.Errorf("%s SE = %.6f (n=%d), want %.6f", c.label, c.se, c.n, want)
		}
	}
	// The error bar must shrink with sample, or it is not measuring anything:
	// each side has half the duels, so its bar is √2 wider.
	if p.CT.DuelSwingSE <= p.DuelSwingSE {
		t.Errorf("per-side SE %.6f should exceed the combined SE %.6f", p.CT.DuelSwingSE, p.DuelSwingSE)
	}
}

// A player with no duels must not produce a NaN error bar from a 0/0 divide.
func TestCompute_NoDuelsHasZeroStandardError(t *testing.T) {
	rounds := []Round{mkRound("h", 1, true)}
	rt := BuildRoundTable(rounds, nil)
	dt := BuildDuelTable(nil)
	p := Compute(rounds, nil, rt, dt)[1]
	if p.Duels != 0 || p.DuelSwingSE != 0 {
		t.Errorf("duels=%d SE=%v, want 0 and 0", p.Duels, p.DuelSwingSE)
	}
}
