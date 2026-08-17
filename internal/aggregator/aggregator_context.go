package aggregator

import (
	"math"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/swing"
)

// Pass 19: round context — the denominator axes for the swing metrics.
//
// Per (player, round) it measures what the round GAVE the player, so their
// swing can be read against it:
//
//   - Resource share: how many alive in-round 16 Hz samples had a rifle or a
//     sniper in hand ("good gun %"). A star gets fed the good guns; a support
//     holds SMGs and pistols. Counts, not percentages, so they aggregate.
//   - Pack distance: average distance to the NEAREST alive teammate. A lurker
//     lives far from the pack; an entry pack player lives inside it.
//   - Contact timing: seconds after freeze end of the player's first combat
//     involvement (kill, death, damage dealt or received), and of their death.
//
// Sample caveats, inherited from the emitter: samples exist only while alive
// and only at 16 Hz baseline, so the sample counts approximate alive time,
// not round time. Weapon classification reuses swing.WeaponClass — the same
// taxonomy the duel table is keyed on — NOT the FHHS weaponBucket, which has
// no SMG class and splits rifles.
type roundContextResult struct {
	gunSamples, gunRifle, gunSniper int

	packDistSumM float64
	packDistN    int

	firstContactSec float64 // -1 = no combat involvement this round
	deathSec        float64 // -1 = survived
}

// computeRoundContext walks samples, kills and damages once and returns the
// per-player per-round context.
func computeRoundContext(raw *model.RawMatch) map[uint64]map[int]*roundContextResult {
	freezeEnd := make(map[int]int, len(raw.Rounds))
	for _, r := range raw.Rounds {
		freezeEnd[r.Number] = r.FreezeEndTick
	}
	tps := raw.TicksPerSecond
	if tps <= 0 {
		tps = 64
	}

	out := map[uint64]map[int]*roundContextResult{}
	get := func(id uint64, rn int) *roundContextResult {
		m := out[id]
		if m == nil {
			m = map[int]*roundContextResult{}
			out[id] = m
		}
		r := m[rn]
		if r == nil {
			r = &roundContextResult{firstContactSec: -1, deathSec: -1}
			m[rn] = r
		}
		return r
	}

	// ---- Resource share + tick grouping for pack distance. ----
	// Samples for all alive players are emitted in the same frame, so exact
	// tick equality is the correct grouping (verified in parserv2's emitter).
	type tickKey struct{ rn, tick int }
	groups := map[tickKey][]int{} // indices into raw.ViewSamples
	for i := range raw.ViewSamples {
		s := &raw.ViewSamples[i]
		fe, ok := freezeEnd[s.RoundNumber]
		if !ok || s.HP <= 0 || s.Tick < fe {
			continue
		}
		res := get(s.SteamID, s.RoundNumber)
		res.gunSamples++
		switch swing.WeaponClass(s.Weapon) {
		case "rifle":
			res.gunRifle++
		case "sniper":
			res.gunSniper++
		}
		if s.Team != model.TeamUnknown {
			k := tickKey{s.RoundNumber, s.Tick}
			groups[k] = append(groups[k], i)
		}
	}

	// ---- Pack distance: nearest alive teammate per sampled tick. ----
	// A sole survivor (no alive teammate at the tick) contributes nothing:
	// "infinitely far from the pack" would poison the average.
	for _, idxs := range groups {
		for _, i := range idxs {
			si := &raw.ViewSamples[i]
			best := math.MaxFloat64
			for _, j := range idxs {
				if i == j {
					continue
				}
				sj := &raw.ViewSamples[j]
				if sj.Team != si.Team || sj.SteamID == si.SteamID {
					continue
				}
				dx := si.Pos.X - sj.Pos.X
				dy := si.Pos.Y - sj.Pos.Y
				dz := si.Pos.Z - sj.Pos.Z
				if d := dx*dx + dy*dy + dz*dz; d < best {
					best = d
				}
			}
			if best < math.MaxFloat64 {
				res := get(si.SteamID, si.RoundNumber)
				res.packDistSumM += math.Sqrt(best) * unitsToMeters
				res.packDistN++
			}
		}
	}

	// ---- Contact and death timing, from events. ----
	// Events in freeze time clamp to 0 rather than going negative.
	contact := func(id uint64, rn, tick int) {
		if id == 0 {
			return
		}
		fe, ok := freezeEnd[rn]
		if !ok {
			return
		}
		sec := float64(tick-fe) / tps
		if sec < 0 {
			sec = 0
		}
		res := get(id, rn)
		if res.firstContactSec < 0 || sec < res.firstContactSec {
			res.firstContactSec = sec
		}
	}
	sameTeam := func(a, b model.Team) bool {
		return a != model.TeamUnknown && a == b
	}
	for _, k := range raw.Kills {
		if k.VictimSteamID != 0 {
			if fe, ok := freezeEnd[k.RoundNumber]; ok {
				sec := float64(k.Tick-fe) / tps
				if sec < 0 {
					sec = 0
				}
				res := get(k.VictimSteamID, k.RoundNumber)
				if res.deathSec < 0 || sec < res.deathSec {
					res.deathSec = sec
				}
			}
		}
		// Suicides and teamkills are not enemy contact.
		if k.KillerSteamID == k.VictimSteamID || sameTeam(k.KillerTeam, k.VictimTeam) {
			continue
		}
		contact(k.KillerSteamID, k.RoundNumber, k.Tick)
		contact(k.VictimSteamID, k.RoundNumber, k.Tick)
	}
	for _, d := range raw.Damages {
		if d.AttackerSteamID == 0 || d.AttackerSteamID == d.VictimSteamID ||
			sameTeam(d.AttackerTeam, d.VictimTeam) {
			continue
		}
		contact(d.AttackerSteamID, d.RoundNumber, d.Tick)
		contact(d.VictimSteamID, d.RoundNumber, d.Tick)
	}

	return out
}
