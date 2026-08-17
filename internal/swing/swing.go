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
	// MutualAwarenessMs is how long both players had been aware of each other
	// when the duel resolved: kill tick minus the later of the two sightings.
	// It is symmetric, so mirroring a duel leaves it unchanged. Meaningful only
	// when MutualKnown is set.
	MutualAwarenessMs float64
	MutualKnown       bool
	DistanceBin       string
	WeaponBucket      string
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
	freshnessBucket string
	distanceBin     string
	weaponBucket    string
}

// DuelTable is an empirical P(the player who spotted first wins) lookup, keyed
// on how big the sighting advantage was, how fresh it still was when the duel
// resolved, the range, and the weapon class.
type DuelTable struct {
	cells map[duelState]*cell
	// mid backs off to (advantage, freshness); adv backs off to advantage alone.
	mid  map[[2]string]*cell
	adv  map[string]*cell
	base float64
	n    int
	wins int
}

// FreshnessBucket labels how long both players had been mutually aware when
// the duel resolved.
//
// This axis exists because sighting advantage alone is non-monotonic: measured
// on the corpus, a +400ms lead is worth 0.680 when the duel resolves within
// 300ms of mutual awareness but only 0.503 once both sides have known about
// each other for more than three seconds. A stale lead is not a lead — the
// player saw and did not act, and the opponent chose the moment.
func FreshnessBucket(ms float64, known bool) string {
	switch {
	case !known:
		return "n/a"
	case ms < 300:
		return "<300ms"
	case ms < 1000:
		return "300ms-1s"
	case ms < 3000:
		return "1-3s"
	default:
		return ">3s"
	}
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

// P returns P(killer wins this duel | state) and the backing sample count,
// backing off from the full key through (advantage, freshness) to advantage
// alone and finally the corpus base rate.
func (t *DuelTable) P(adv, fresh, dist, weapon string) (float64, int) {
	if c := t.cells[duelState{adv, fresh, dist, weapon}]; c != nil && c.n >= MinCellSamples {
		return float64(c.ctWins) / float64(c.n), c.n
	}
	if c := t.mid[[2]string{adv, fresh}]; c != nil && c.n >= MinCellSamples {
		return float64(c.ctWins) / float64(c.n), c.n
	}
	if c := t.adv[adv]; c != nil && c.n >= MinCellSamples {
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
		cells: map[duelState]*cell{},
		mid:   map[[2]string]*cell{},
		adv:   map[string]*cell{},
	}
	bump := func(c *cell, won bool) {
		c.n++
		if won {
			c.ctWins++
		}
	}
	add := func(st duelState, won bool) {
		c := t.cells[st]
		if c == nil {
			c = &cell{}
			t.cells[st] = c
		}
		bump(c, won)
		mk := [2]string{st.advantageBucket, st.freshnessBucket}
		m := t.mid[mk]
		if m == nil {
			m = &cell{}
			t.mid[mk] = m
		}
		bump(m, won)
		a := t.adv[st.advantageBucket]
		if a == nil {
			a = &cell{}
			t.adv[st.advantageBucket] = a
		}
		bump(a, won)
		t.n++
		if won {
			t.wins++
		}
	}
	for _, k := range kills {
		// Freshness is symmetric, so it is the same on both sides of the mirror.
		fresh := FreshnessBucket(k.MutualAwarenessMs, k.MutualKnown)
		adv := AdvantageBucket(k.SightAdvantageMs, k.KillerSawFirst, k.VictimSawFirst)
		add(duelState{adv, fresh, k.DistanceBin, k.WeaponBucket}, true)
		mirror := AdvantageBucket(-k.SightAdvantageMs, k.VictimSawFirst, k.KillerSawFirst)
		add(duelState{mirror, fresh, k.DistanceBin, k.WeaponBucket}, false)
	}
	if t.n > 0 {
		t.base = float64(t.wins) / float64(t.n)
	}
	return t
}

// ---- Per-player results ----

// SideSwing is one side's slice of both metrics. Sides swap at halftime, so a
// player almost always has both populated, and CT + T reconstructs the total.
//
// Unlike the totals, a side is NOT zero-sum on its own: summed over every
// player, CT duel swing is the whole CT side's net over expectation, which is
// only zero if the probability tables happen to be side-neutral. Read a side
// against the same side's population, never against the other one.
type SideSwing struct {
	Rounds int
	Duels  int

	RoundSwingTotal    float64
	RoundSwingPerRound float64

	DuelSwingTotal   float64
	DuelSwingPerDuel float64
	DuelSwingSE      float64
	DuelsWon         int
	ExpectedWins     float64
	DuelVarSum       float64
}

// PlayerSwing holds both metrics for one player, overall and per side.
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
	DuelSwingSE      float64
	DuelsWon         int
	ExpectedWins     float64
	// DuelVarSum is Σ p(1−p) over the player's duels: each duel is one
	// Bernoulli draw whose variance the table already gives us. DuelSwingSE is
	// √DuelVarSum / Duels, the sampling standard error of DuelSwingPerDuel.
	//
	// It is a FLOOR, not the real uncertainty. It assumes duels are independent
	// draws, and they are not — they cluster within rounds and matches, so the
	// true interval is wider. It also covers sampling only: nothing here
	// accounts for the features the table omits (HP, armour, flashed state) or
	// for the systematic CT/T offset those omissions produce.
	DuelVarSum float64

	// CT and T are the same numbers restricted to the side the player was on
	// at the time. Their totals sum back to the fields above.
	CT SideSwing
	T  SideSwing

	// Weapons slices the same numbers by the weapon class that RESOLVED each
	// duel — the killer's weapon, since the victim's own is not stored. Slices
	// sum back to the totals. Rounds stays 0 here: a round is not played "with
	// a weapon class", so per-round rates are undefined within a slice.
	Weapons map[string]*SideSwing
}

// Side returns the per-side bucket to attribute to.
func (p *PlayerSwing) Side(isCT bool) *SideSwing {
	if isCT {
		return &p.CT
	}
	return &p.T
}

// WeaponSlice returns the bucket for one weapon class, allocating on first use.
func (p *PlayerSwing) WeaponSlice(bucket string) *SideSwing {
	if p.Weapons == nil {
		p.Weapons = map[string]*SideSwing{}
	}
	s := p.Weapons[bucket]
	if s == nil {
		s = &SideSwing{}
		p.Weapons[bucket] = s
	}
	return s
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
			p := get(id)
			p.Rounds++
			p.CT.Rounds++
		}
		for _, id := range r.PlayersT {
			p := get(id)
			p.Rounds++
			p.T.Rounds++
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
			kw := get(k.KillerID)
			vw := get(k.VictimID)
			// The victim is on the opposite side by construction: a duel is
			// always across the divide.
			kSide, vSide := kw.Side(k.KillerIsCT), vw.Side(!k.KillerIsCT)
			// Both parties share the duel's weapon bucket (the killer's
			// weapon), which is what keeps each class slice zero-sum.
			kWpn, vWpn := kw.WeaponSlice(k.WeaponBucket), vw.WeaponSlice(k.WeaponBucket)
			kw.RoundSwingTotal += delta
			kSide.RoundSwingTotal += delta
			kWpn.RoundSwingTotal += delta
			vw.RoundSwingTotal -= delta
			vSide.RoundSwingTotal -= delta
			vWpn.RoundSwingTotal -= delta
			pCT = nextCT

			// Duel swing: outcome minus expectation, zero-sum across the pair.
			adv := AdvantageBucket(k.SightAdvantageMs, k.KillerSawFirst, k.VictimSawFirst)
			fresh := FreshnessBucket(k.MutualAwarenessMs, k.MutualKnown)
			p, _ := dt.P(adv, fresh, k.DistanceBin, k.WeaponBucket)
			kw.Duels++
			vw.Duels++
			// Both parties face the same Bernoulli variance: the winner's draw
			// had p, the loser's 1−p, and p(1−p) is symmetric in the two.
			v := p * (1 - p)
			kw.DuelsWon++
			kw.ExpectedWins += p
			vw.ExpectedWins += 1 - p
			kw.DuelSwingTotal += 1 - p
			vw.DuelSwingTotal -= 1 - p
			kw.DuelVarSum += v
			vw.DuelVarSum += v

			kSide.Duels++
			vSide.Duels++
			kSide.DuelsWon++
			kSide.ExpectedWins += p
			vSide.ExpectedWins += 1 - p
			kSide.DuelSwingTotal += 1 - p
			vSide.DuelSwingTotal -= 1 - p
			kSide.DuelVarSum += v
			vSide.DuelVarSum += v

			kWpn.Duels++
			vWpn.Duels++
			kWpn.DuelsWon++
			kWpn.ExpectedWins += p
			vWpn.ExpectedWins += 1 - p
			kWpn.DuelSwingTotal += 1 - p
			vWpn.DuelSwingTotal -= 1 - p
			kWpn.DuelVarSum += v
			vWpn.DuelVarSum += v
		}
	}

	for _, p := range out {
		if p.Rounds > 0 {
			p.RoundSwingPerRound = p.RoundSwingTotal / float64(p.Rounds)
		}
		if p.Duels > 0 {
			p.DuelSwingPerDuel = p.DuelSwingTotal / float64(p.Duels)
			p.DuelSwingSE = math.Sqrt(p.DuelVarSum) / float64(p.Duels)
		}
		slices := []*SideSwing{&p.CT, &p.T}
		for _, s := range p.Weapons {
			slices = append(slices, s)
		}
		for _, s := range slices {
			if s.Rounds > 0 {
				s.RoundSwingPerRound = s.RoundSwingTotal / float64(s.Rounds)
			}
			if s.Duels > 0 {
				s.DuelSwingPerDuel = s.DuelSwingTotal / float64(s.Duels)
				s.DuelSwingSE = math.Sqrt(s.DuelVarSum) / float64(s.Duels)
			}
		}
	}
	return out
}

// Validate checks the zero-sum property both metrics must satisfy: summed over
// every player, each total should be 0 to within floating-point slack. A
// non-zero sum means attribution is leaking somewhere and the numbers should
// not be trusted.
//
// It also checks that each player's CT and T slices reconstruct their totals.
// The sides are not zero-sum themselves — only their sum is — so this is the
// only structural check available on the split.
func Validate(res map[uint64]*PlayerSwing) error {
	const tol = 1e-6
	var roundSum, duelSum float64
	for _, p := range res {
		roundSum += p.RoundSwingTotal
		duelSum += p.DuelSwingTotal

		if p.CT.Rounds+p.T.Rounds != p.Rounds || p.CT.Duels+p.T.Duels != p.Duels {
			return fmt.Errorf("player %d: side counts do not sum to the total (rounds %d+%d vs %d, duels %d+%d vs %d)",
				p.SteamID, p.CT.Rounds, p.T.Rounds, p.Rounds, p.CT.Duels, p.T.Duels, p.Duels)
		}
		if math.Abs(p.CT.RoundSwingTotal+p.T.RoundSwingTotal-p.RoundSwingTotal) > tol ||
			math.Abs(p.CT.DuelSwingTotal+p.T.DuelSwingTotal-p.DuelSwingTotal) > tol {
			return fmt.Errorf("player %d: side swing does not sum to the total", p.SteamID)
		}

		// The weapon slices must also reconstruct the totals: every kill has
		// exactly one bucket, so nothing may fall between the classes.
		var wRound, wDuel float64
		var wDuels int
		for _, s := range p.Weapons {
			wRound += s.RoundSwingTotal
			wDuel += s.DuelSwingTotal
			wDuels += s.Duels
		}
		if wDuels != p.Duels {
			return fmt.Errorf("player %d: weapon duel counts sum to %d, want %d", p.SteamID, wDuels, p.Duels)
		}
		if math.Abs(wRound-p.RoundSwingTotal) > tol || math.Abs(wDuel-p.DuelSwingTotal) > tol {
			return fmt.Errorf("player %d: weapon swing does not sum to the total", p.SteamID)
		}
	}

	// Each weapon class is a closed zero-sum slice on its own — both parties
	// of a duel share the bucket — so a per-class leak is detectable too.
	classRound := map[string]float64{}
	classDuel := map[string]float64{}
	for _, p := range res {
		for cls, s := range p.Weapons {
			classRound[cls] += s.RoundSwingTotal
			classDuel[cls] += s.DuelSwingTotal
		}
	}
	for cls := range classRound {
		if math.Abs(classRound[cls]) > tol || math.Abs(classDuel[cls]) > tol {
			return fmt.Errorf("weapon class %q is not zero-sum: round=%.9f duel=%.9f", cls, classRound[cls], classDuel[cls])
		}
	}
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
	Bucket    string
	Freshness string // empty when collapsed to the advantage axis alone
	P         float64
	N         int
}

// DumpByAdvantage returns P(win) per first-sight advantage bucket.
func (t *DuelTable) DumpByAdvantage() []AdvantageEntry {
	var out []AdvantageEntry
	for b, c := range t.adv {
		if c.n < MinCellSamples {
			continue
		}
		out = append(out, AdvantageEntry{Bucket: b, P: float64(c.ctWins) / float64(c.n), N: c.n})
	}
	return out
}

// DumpByAdvantageFreshness returns P(win) per (advantage, freshness) pair —
// the level at which the non-monotonicity in advantage alone resolves.
func (t *DuelTable) DumpByAdvantageFreshness() []AdvantageEntry {
	var out []AdvantageEntry
	for k, c := range t.mid {
		if c.n < MinCellSamples {
			continue
		}
		out = append(out, AdvantageEntry{
			Bucket: k[0], Freshness: k[1],
			P: float64(c.ctWins) / float64(c.n), N: c.n,
		})
	}
	return out
}
