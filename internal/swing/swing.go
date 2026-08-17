// Package swing computes two win-probability-added metrics from the stored
// kill log, and the comparison between them.
//
//   - Round swing: how much a player moved their team's chance of winning the
//     round. Attributed at every kill, from an empirical P(win | man-advantage
//     state) table.
//   - Duel swing: how much a player over- or under-performed the expected
//     outcome of the duels they took, from an empirical P(killer wins | first-
//     sight advantage, distance, weapon class) table.
//
// Both probability tables are counted from the corpus rather than fitted. With
// tens of thousands of rounds the cells are dense enough that a frequency is
// both more accurate and more auditable than a regression: you can read a cell
// and check it against intuition. The cost is that rare states are noisy, so
// every cell carries its sample count and callers fall back to a coarser prior
// below a minimum.
//
// Both metrics are zero-sum by construction — one player's gain is the
// opponent's loss — which gives a free correctness check (see Validate).
package swing

import (
	"fmt"
	"math"
	"sort"
)

// MinCellSamples is the sample floor for using an empirical cell directly.
// Below it, the lookup backs off to a coarser key (see RoundTable.P).
const MinCellSamples = 30

// Kill is one resolved duel: a win for the killer, a loss for the victim.
// It is the single input record for both metrics.
type Kill struct {
	DemoHash    string
	RoundNumber int
	Tick        int
	KillerID    uint64
	VictimID    uint64
	KillerIsCT  bool
	BombPlanted bool
	// SightAdvantageMs is how long the killer saw the victim before the victim
	// saw the killer. Positive = killer spotted first. Zero when neither or
	// both sight ticks are unknown.
	SightAdvantageMs float64
	// KillerSawFirst / VictimSawFirst record whether each party ever spotted
	// the other, which is different from "who spotted sooner".
	KillerSawFirst bool
	VictimSawFirst bool
	DistanceBin    string
	WeaponBucket   string
}

// Round is the outcome and roster of one round, used to seed the state walk.
type Round struct {
	DemoHash    string
	RoundNumber int
	CTWon       bool
	// PlayersCT / PlayersT are the SteamIDs on each side. Used to size the
	// starting man-count and to attribute the round's baseline.
	PlayersCT []uint64
	PlayersT  []uint64
}

// ---- Round win-probability table ----

// roundState is the man-advantage state the empirical table is keyed on.
type roundState struct {
	aliveCT, aliveT int
	planted         bool
}

type cell struct {
	ctWins, n int
}

// RoundTable is an empirical P(CT wins | state) lookup.
type RoundTable struct {
	cells map[roundState]*cell
	// backoff ignores the plant flag, for sparse states.
	backoff map[[2]int]*cell
	// base is the corpus-wide CT win rate, the last resort.
	base float64
	n    int
}

// P returns P(CT wins round | state) and the sample count backing it.
// Sparse cells back off to the plant-agnostic count, then to the corpus base
// rate, so a rare state degrades to a coarser answer instead of a wild one.
func (t *RoundTable) P(aliveCT, aliveT int, planted bool) (float64, int) {
	if aliveCT <= 0 {
		return 0, t.n
	}
	if aliveT <= 0 {
		return 1, t.n
	}
	if c := t.cells[roundState{aliveCT, aliveT, planted}]; c != nil && c.n >= MinCellSamples {
		return float64(c.ctWins) / float64(c.n), c.n
	}
	if c := t.backoff[[2]int{aliveCT, aliveT}]; c != nil && c.n >= MinCellSamples {
		return float64(c.ctWins) / float64(c.n), c.n
	}
	return t.base, t.n
}

// BuildRoundTable counts P(CT wins | alive counts, plant) over every state a
// round actually passed through. A round contributes one observation per state
// it visited, so states are weighted by how often play reaches them.
func BuildRoundTable(rounds []Round, kills []Kill) *RoundTable {
	t := &RoundTable{
		cells:   map[roundState]*cell{},
		backoff: map[[2]int]*cell{},
	}
	byRound := groupKills(kills)
	ctWinTotal := 0

	for _, r := range rounds {
		if r.CTWon {
			ctWinTotal++
		}
		t.n++
		aliveCT, aliveT := len(r.PlayersCT), len(r.PlayersT)
		if aliveCT == 0 || aliveT == 0 {
			continue
		}
		planted := false
		record := func() {
			st := roundState{aliveCT, aliveT, planted}
			c := t.cells[st]
			if c == nil {
				c = &cell{}
				t.cells[st] = c
			}
			c.n++
			b := t.backoff[[2]int{aliveCT, aliveT}]
			if b == nil {
				b = &cell{}
				t.backoff[[2]int{aliveCT, aliveT}] = b
			}
			b.n++
			if r.CTWon {
				c.ctWins++
				b.ctWins++
			}
		}
		record() // opening state
		for _, k := range byRound[roundKey{r.DemoHash, r.RoundNumber}] {
			if k.BombPlanted {
				planted = true
			}
			// The victim is on the opposite side to the killer.
			if k.KillerIsCT {
				aliveT--
			} else {
				aliveCT--
			}
			if aliveCT <= 0 || aliveT <= 0 {
				break
			}
			record()
		}
	}
	if t.n > 0 {
		t.base = float64(ctWinTotal) / float64(t.n)
	}
	return t
}

// ---- Duel win-probability table ----

type duelState struct {
	advantageBucket string
	distanceBin     string
	weaponBucket    string
}

// DuelTable is an empirical P(the player who spotted first wins) lookup,
// keyed on how big the sighting advantage was, the range, and the weapon class.
type DuelTable struct {
	cells   map[duelState]*cell
	backoff map[string]*cell // advantage bucket alone
	base    float64
	n       int
	wins    int
}

// AdvantageBucket labels a first-sight advantage in ms. Buckets are coarse on
// purpose: the signal is "who was ready", not the exact millisecond.
func AdvantageBucket(ms float64, killerSaw, victimSaw bool) string {
	switch {
	case killerSaw && !victimSaw:
		return "unseen" // victim never spotted the killer at all
	case !killerSaw && victimSaw:
		return "blind" // killer never spotted the victim (wallbang, spray-through)
	case !killerSaw && !victimSaw:
		return "neither"
	case ms >= 1000:
		return "+1000ms"
	case ms >= 400:
		return "+400ms"
	case ms >= 150:
		return "+150ms"
	case ms > -150:
		return "even"
	case ms > -400:
		return "-150ms"
	case ms > -1000:
		return "-400ms"
	default:
		return "-1000ms"
	}
}

// P returns P(killer wins this duel | state) and the backing sample count.
func (t *DuelTable) P(adv, dist, weapon string) (float64, int) {
	if c := t.cells[duelState{adv, dist, weapon}]; c != nil && c.n >= MinCellSamples {
		return float64(c.ctWins) / float64(c.n), c.n
	}
	if c := t.backoff[adv]; c != nil && c.n >= MinCellSamples {
		return float64(c.ctWins) / float64(c.n), c.n
	}
	return t.base, t.n
}

// BuildDuelTable counts, for each duel state, how often the killer won.
//
// Every kill is entered twice: once as observed (killer won) and once mirrored
// from the victim's perspective (victim lost, with the advantage negated). Only
// entering the observed direction would make every cell 100% by construction —
// the killer always won. Mirroring is what makes the table a real probability.
func BuildDuelTable(kills []Kill) *DuelTable {
	t := &DuelTable{
		cells:   map[duelState]*cell{},
		backoff: map[string]*cell{},
	}
	add := func(st duelState, won bool) {
		c := t.cells[st]
		if c == nil {
			c = &cell{}
			t.cells[st] = c
		}
		c.n++
		b := t.backoff[st.advantageBucket]
		if b == nil {
			b = &cell{}
			t.backoff[st.advantageBucket] = b
		}
		b.n++
		if won {
			c.ctWins++
			b.ctWins++
		}
		t.n++
		if won {
			t.wins++
		}
	}
	for _, k := range kills {
		adv := AdvantageBucket(k.SightAdvantageMs, k.KillerSawFirst, k.VictimSawFirst)
		add(duelState{adv, k.DistanceBin, k.WeaponBucket}, true)
		mirror := AdvantageBucket(-k.SightAdvantageMs, k.VictimSawFirst, k.KillerSawFirst)
		add(duelState{mirror, k.DistanceBin, k.WeaponBucket}, false)
	}
	if t.n > 0 {
		t.base = float64(t.wins) / float64(t.n)
	}
	return t
}

// ---- Per-player results ----

// PlayerSwing holds both metrics for one player.
type PlayerSwing struct {
	SteamID uint64
	// Rounds the player appeared in, and duels they took (won + lost).
	Rounds int
	Duels  int
	// RoundSwingTotal is summed win-probability added across all kills and
	// deaths; RoundSwingPerRound divides by Rounds.
	RoundSwingTotal    float64
	RoundSwingPerRound float64
	// DuelSwingTotal is summed (outcome − expected) across duels;
	// DuelSwingPerDuel divides by Duels. Positive = beat expectation.
	DuelSwingTotal   float64
	DuelSwingPerDuel float64
	DuelsWon         int
	ExpectedWins     float64
}

type roundKey struct {
	hash  string
	round int
}

func groupKills(kills []Kill) map[roundKey][]Kill {
	out := map[roundKey][]Kill{}
	for _, k := range kills {
		rk := roundKey{k.DemoHash, k.RoundNumber}
		out[rk] = append(out[rk], k)
	}
	for rk := range out {
		ks := out[rk]
		sort.Slice(ks, func(i, j int) bool { return ks[i].Tick < ks[j].Tick })
		out[rk] = ks
	}
	return out
}

// Compute walks every round, attributing round swing at each kill and duel
// swing at each duel, and returns one result per player.
func Compute(rounds []Round, kills []Kill, rt *RoundTable, dt *DuelTable) map[uint64]*PlayerSwing {
	out := map[uint64]*PlayerSwing{}
	get := func(id uint64) *PlayerSwing {
		p := out[id]
		if p == nil {
			p = &PlayerSwing{SteamID: id}
			out[id] = p
		}
		return p
	}

	byRound := groupKills(kills)
	for _, r := range rounds {
		for _, id := range r.PlayersCT {
			get(id).Rounds++
		}
		for _, id := range r.PlayersT {
			get(id).Rounds++
		}
		aliveCT, aliveT := len(r.PlayersCT), len(r.PlayersT)
		if aliveCT == 0 || aliveT == 0 {
			continue
		}
		planted := false
		pCT, _ := rt.P(aliveCT, aliveT, planted)

		for _, k := range byRound[roundKey{r.DemoHash, r.RoundNumber}] {
			if k.BombPlanted {
				planted = true
			}
			if k.KillerIsCT {
				aliveT--
			} else {
				aliveCT--
			}
			nextCT, _ := rt.P(aliveCT, aliveT, planted)

			// Round swing: the change in the killer's team's win probability.
			delta := nextCT - pCT
			if !k.KillerIsCT {
				delta = -delta
			}
			get(k.KillerID).RoundSwingTotal += delta
			get(k.VictimID).RoundSwingTotal -= delta
			pCT = nextCT

			// Duel swing: outcome minus expectation, zero-sum across the pair.
			adv := AdvantageBucket(k.SightAdvantageMs, k.KillerSawFirst, k.VictimSawFirst)
			p, _ := dt.P(adv, k.DistanceBin, k.WeaponBucket)
			kw := get(k.KillerID)
			vw := get(k.VictimID)
			kw.Duels++
			vw.Duels++
			kw.DuelsWon++
			kw.ExpectedWins += p
			vw.ExpectedWins += 1 - p
			kw.DuelSwingTotal += 1 - p
			vw.DuelSwingTotal -= 1 - p
		}
	}

	for _, p := range out {
		if p.Rounds > 0 {
			p.RoundSwingPerRound = p.RoundSwingTotal / float64(p.Rounds)
		}
		if p.Duels > 0 {
			p.DuelSwingPerDuel = p.DuelSwingTotal / float64(p.Duels)
		}
	}
	return out
}

// Validate checks the zero-sum property both metrics must satisfy: summed over
// every player, each total should be 0 to within floating-point slack. A
// non-zero sum means attribution is leaking somewhere and the numbers should
// not be trusted.
func Validate(res map[uint64]*PlayerSwing) error {
	var roundSum, duelSum float64
	for _, p := range res {
		roundSum += p.RoundSwingTotal
		duelSum += p.DuelSwingTotal
	}
	const tol = 1e-6
	if math.Abs(roundSum) > tol || math.Abs(duelSum) > tol {
		return fmt.Errorf("swing is not zero-sum: round=%.9f duel=%.9f (attribution is leaking)", roundSum, duelSum)
	}
	return nil
}

// ---- Feature bucketing ----
//
// These mirror the aggregator's own binning so a swing cell means the same
// thing as the corresponding FHHS cell. They live here rather than being
// imported so that the swing tables stay computable from the DB alone.

// DistanceBin buckets a duel distance in metres.
func DistanceBin(m float64) string {
	switch {
	case m < 0:
		return "unknown"
	case m < 5:
		return "0-5m"
	case m < 10:
		return "5-10m"
	case m < 15:
		return "10-15m"
	case m < 20:
		return "15-20m"
	case m < 30:
		return "20-30m"
	default:
		return "30m+"
	}
}

// WeaponClass groups weapons into the classes the duel table is keyed on.
// Coarser than the aggregator's FHHS buckets: what matters for "who should
// have won" is time-to-kill class, not the exact gun.
func WeaponClass(weapon string) string {
	switch weapon {
	case "AWP", "SSG 08", "G3SG1", "SCAR-20":
		return "sniper"
	case "AK-47", "M4A1", "M4A1-S", "M4A4", "Galil AR", "FAMAS", "AUG", "SG 553":
		return "rifle"
	case "Desert Eagle", "R8 Revolver":
		return "heavy_pistol"
	case "USP-S", "Glock-18", "P250", "P2000", "Five-SeveN", "Tec-9", "CZ75 Auto", "Dual Berettas":
		return "pistol"
	case "MP9", "MAC-10", "MP7", "MP5-SD", "UMP-45", "P90", "PP-Bizon":
		return "smg"
	case "Nova", "XM1014", "MAG-7", "Sawed-Off":
		return "shotgun"
	default:
		return "other"
	}
}

// ---- Table introspection ----
//
// Counted tables are only better than fitted ones if you can actually read
// them, so both expose their cells.

// RoundEntry is one row of the round win-probability table.
type RoundEntry struct {
	AliveCT, AliveT int
	Planted         bool
	P               float64
	N               int
}

// Dump returns the round table's cells above the sample floor, ordered by
// alive counts then plant state.
func (t *RoundTable) Dump() []RoundEntry {
	var out []RoundEntry
	for st, c := range t.cells {
		if c.n < MinCellSamples {
			continue
		}
		out = append(out, RoundEntry{
			AliveCT: st.aliveCT, AliveT: st.aliveT, Planted: st.planted,
			P: float64(c.ctWins) / float64(c.n), N: c.n,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AliveCT != out[j].AliveCT {
			return out[i].AliveCT > out[j].AliveCT
		}
		if out[i].AliveT != out[j].AliveT {
			return out[i].AliveT > out[j].AliveT
		}
		return !out[i].Planted && out[j].Planted
	})
	return out
}

// AdvantageEntry is one row of the duel table collapsed to the sight-advantage
// axis — the axis that carries almost all of the signal.
type AdvantageEntry struct {
	Bucket string
	P      float64
	N      int
}

// DumpByAdvantage returns P(win) per first-sight advantage bucket.
func (t *DuelTable) DumpByAdvantage() []AdvantageEntry {
	var out []AdvantageEntry
	for b, c := range t.backoff {
		if c.n < MinCellSamples {
			continue
		}
		out = append(out, AdvantageEntry{Bucket: b, P: float64(c.ctWins) / float64(c.n), N: c.n})
	}
	return out
}
