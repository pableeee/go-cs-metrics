package swing

import (
	"math"
	"sort"
)

// ComputeByDemo partitions rounds and kills by DemoHash in one O(total) pass
// and runs Compute per demo with the shared corpus-wide tables. The tables are
// deliberately NOT rebuilt per demo: what a 3v2 is worth does not change with
// the demo, and per-demo cells would be pure noise.
//
// Summed over demos, each player's totals reconstruct the full-corpus Compute
// exactly — attribution never crosses a round, let alone a demo.
func ComputeByDemo(rounds []Round, kills []Kill, rt *RoundTable, dt *DuelTable) map[string]map[uint64]*PlayerSwing {
	roundsBy := map[string][]Round{}
	for _, r := range rounds {
		roundsBy[r.DemoHash] = append(roundsBy[r.DemoHash], r)
	}
	killsBy := map[string][]Kill{}
	for _, k := range kills {
		killsBy[k.DemoHash] = append(killsBy[k.DemoHash], k)
	}
	out := make(map[string]map[uint64]*PlayerSwing, len(roundsBy))
	for hash, rs := range roundsBy {
		out[hash] = Compute(rs, killsBy[hash], rt, dt)
	}
	return out
}

// PlayerFloorCeiling is a player's distribution of per-demo swing rates: the
// floor is how bad an off game is, the ceiling how good a peak is. A player
// whose FLOOR is above zero beats expectation even on bad days.
type PlayerFloorCeiling struct {
	SteamID     uint64
	Demos       int // demos meeting the per-demo round floor
	RoundsTotal int

	// p25 / p50 / p75 of per-demo RoundSwingPerRound.
	RndFloor, RndMedian, RndCeiling float64
	// Same for per-demo DuelSwingPerDuel.
	DuelFloor, DuelMedian, DuelCeiling float64
}

// FloorCeilings collapses ComputeByDemo output to per-player quantiles.
// Demos where the player has fewer than minRoundsPerDemo rounds are skipped —
// a three-round overtime appearance produces a garbage rate — and players
// left with fewer than minDemos qualifying demos are omitted entirely.
func FloorCeilings(byDemo map[string]map[uint64]*PlayerSwing, minDemos, minRoundsPerDemo int) map[uint64]PlayerFloorCeiling {
	rndRates := map[uint64][]float64{}
	duelRates := map[uint64][]float64{}
	roundsTotal := map[uint64]int{}
	for _, players := range byDemo {
		for id, p := range players {
			// A full demo with zero duels is not a player having a quiet game
			// — it is a coach, listed in the round rosters but never fighting.
			// Their exact 0.0 every demo would otherwise out-floor most of the
			// real field.
			if p.Rounds < minRoundsPerDemo || p.Duels == 0 {
				continue
			}
			rndRates[id] = append(rndRates[id], p.RoundSwingPerRound)
			duelRates[id] = append(duelRates[id], p.DuelSwingPerDuel)
			roundsTotal[id] += p.Rounds
		}
	}

	out := map[uint64]PlayerFloorCeiling{}
	for id, rr := range rndRates {
		if len(rr) < minDemos {
			continue
		}
		dr := duelRates[id]
		sort.Float64s(rr)
		sort.Float64s(dr)
		out[id] = PlayerFloorCeiling{
			SteamID:     id,
			Demos:       len(rr),
			RoundsTotal: roundsTotal[id],
			RndFloor:    quantile(rr, 0.25),
			RndMedian:   quantile(rr, 0.50),
			RndCeiling:  quantile(rr, 0.75),
			DuelFloor:   quantile(dr, 0.25),
			DuelMedian:  quantile(dr, 0.50),
			DuelCeiling: quantile(dr, 0.75),
		}
	}
	return out
}

// quantile returns the linearly interpolated q-quantile of an ascending-sorted
// slice.
func quantile(sorted []float64, q float64) float64 {
	switch len(sorted) {
	case 0:
		return 0
	case 1:
		return sorted[0]
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
