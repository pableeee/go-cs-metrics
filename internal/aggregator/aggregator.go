// Package aggregator implements the 10-pass pipeline that transforms a parsed
// RawMatch into per-player, per-round, per-weapon, and per-duel-segment
// statistics. The passes run in order: trade annotation, opening kills,
// per-round stats (with buy-type classification), match rollup, crosshair
// placement, duel engine + FHHS segments, AWP death classification, flash
// quality window, role classification, TTK/TTD and one-tap kills.
package aggregator

import (
	"fmt"
	"math"
	"sort"

	"github.com/pable/go-cs-metrics/internal/model"
)

// unitsToMeters is the conversion factor from Source 2 Hammer units to meters.
const unitsToMeters = 0.01905

// enemyDamageTaken returns the HLTV-style damage credit for a damage event:
// the health the victim actually lost (HealthDamageTaken, capped at remaining
// HP — raw HealthDamage is NOT capped, so an AWP shot on a 20 HP player logs
// ~90+), and 0 for team damage (attacker and victim on the same known team).
// Self-damage is already filtered at parse/bridge time. This is the value
// that must feed ADR / total_damage / the Rating 2.0 proxy.
func enemyDamageTaken(d model.RawDamage) int {
	if d.AttackerTeam != model.TeamUnknown && d.AttackerTeam == d.VictimTeam {
		return 0
	}
	return d.HealthDamageTaken
}

// weaponBucket maps a weapon name (as returned by demoinfocs .String()) to a
// broad category bucket used for FHHS segment grouping. For example, "M4A1-S"
// and "M4A4" both map to "M4". Weapons that do not match any known category
// are placed in the "Other" bucket.
func weaponBucket(weapon string) string {
	switch weapon {
	case "AK-47":
		return "AK"
	// The csraw2 weapon table names the silenced M4 "M4A1"; "M4A1-S" is kept
	// for any path that emits the storefront name. Matching only "M4A1-S"
	// silently dumped every silenced-M4 duel into "Other" — the most-used CT
	// rifle was missing from the FHHS table entirely.
	case "M4A1", "M4A1-S", "M4A4":
		return "M4"
	case "Galil AR":
		return "Galil"
	case "FAMAS":
		return "FAMAS"
	case "AUG", "SG 553":
		return "ScopedRifle"
	case "AWP":
		return "AWP"
	case "SSG 08":
		return "Scout"
	case "Desert Eagle":
		return "Deagle"
	case "USP-S", "Glock-18", "P250", "Five-SeveN", "Tec-9", "CZ75 Auto", "P2000", "Dual Berettas", "R8 Revolver":
		return "Pistol"
	default:
		return "Other"
	}
}

// distanceBin converts a distance in meters to a named bin string used for
// FHHS segment grouping. Bins are: "0-5m", "5-10m", "10-15m", "15-20m",
// "20-30m", "30m+". A negative value (unknown distance) returns "unknown".
func distanceBin(meters float64) string {
	if meters < 0 {
		return "unknown"
	}
	switch {
	case meters < 5:
		return "0-5m"
	case meters < 10:
		return "5-10m"
	case meters < 15:
		return "10-15m"
	case meters < 20:
		return "15-20m"
	case meters < 30:
		return "20-30m"
	default:
		return "30m+"
	}
}

// roundPhase returns a coarse label for where a kill falls in a round's timeline.
// Pistol rounds are detected as round 1 (first pistol) or round 13 (MR12
// second-half pistol — a heuristic that matches pro CS2 but misses MR15).
// post_plant overrides time-based labels once the bomb is down.
// Otherwise the post-freeze-to-end span is split into thirds: early/mid/late.
func roundPhase(killTick int, r model.RawRound, roundNumber int) string {
	if roundNumber == 1 || roundNumber == 13 {
		return "pistol"
	}
	if r.BombPlantTick > 0 && killTick >= r.BombPlantTick {
		return "post_plant"
	}
	freeze := r.FreezeEndTick
	end := r.EndTick
	if end <= freeze || killTick < freeze {
		return "early"
	}
	frac := float64(killTick-freeze) / float64(end-freeze)
	switch {
	case frac < 1.0/3.0:
		return "early"
	case frac < 2.0/3.0:
		return "mid"
	default:
		return "late"
	}
}

// wilsonCI computes the 95% Wilson score confidence interval for a proportion
// p = hits/n. This is preferred over the Wald interval because it remains
// stable for small sample sizes. Returns (lo, hi) as fractions in [0, 1].
// When n is 0, the full interval [0, 1] is returned.
func wilsonCI(hits, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	z := 1.96
	p := float64(hits) / float64(n)
	nf := float64(n)
	denom := 1 + z*z/nf
	center := (p + z*z/(2*nf)) / denom
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / denom
	return math.Max(0, center-half), math.Min(1, center+half)
}

// Aggregate runs the full multi-pass pipeline on a parsed RawMatch and returns
// six result slices: per-player match stats, per-round stats, per-weapon
// stats, per-duel-segment (FHHS) stats, per-death events, and per-flash
// events. The passes are:
//  1. Trade annotation (backward + forward scan within 5 s window)
//  2. Opening kills (first kill after FreezeEndTick)
//  3. Per-round per-player stats (with buy-type classification)
//  4. Match-level rollup into PlayerMatchStats
//  5. Crosshair placement aggregation (median angle, pitch/yaw split)
//  6. Duel engine + FHHS segments (exposure time, pre-shot correction)
//  7. AWP death classifier (dry/repeek/isolated)
//  8. Flash quality window (effective flashes within 1.5 s)
//  9. Role classification (AWPer/Entry/Support/Rifler)
//
// 10. TTK and TTD (median ms from first hit to kill/death)
// 11. Counter-strafe % (shots fired at horizontal velocity ≤ 34 u/s)
// 12. Death events (per-kill rows with position, weapon, tactical context)
// 13. Flash events (per-PlayerFlashed rows with blind angle)
// 14. Save & Assist annotation (HLTV-style 1 s save window + assisted-kill flag)
// 15. HLTV-style flash assists (25 dmg threshold during blind window)
// 16. Liveness (per-player time alive + sole-survivor moments)
func Aggregate(raw *model.RawMatch) ([]model.PlayerMatchStats, []model.PlayerRoundStats, []model.PlayerWeaponStats, []model.PlayerDuelSegment, []model.PlayerDeathEvent, []model.FlashEvent, error) {
	if raw == nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("nil RawMatch")
	}

	tradeWindowTicks := int(5.0 * raw.TicksPerSecond)

	// ---- Pass 1: annotate kills with trade flags. ----

	type annotatedKill struct {
		model.RawKill
		isTradeKill          bool // this kill trades a previous enemy kill
		isTradeDeath         bool // this kill will be traded (victim traded the killer)
		tradeKillDelayTicks  int  // ticks from the traded kill to this kill
		tradeDeathDelayTicks int  // ticks from this kill to when the killer was traded
	}

	// Group kills by round, sort each group by tick ascending.
	killsByRound := make(map[int][]annotatedKill)
	for _, k := range raw.Kills {
		killsByRound[k.RoundNumber] = append(killsByRound[k.RoundNumber], annotatedKill{RawKill: k})
	}
	for rn := range killsByRound {
		sort.Slice(killsByRound[rn], func(i, j int) bool {
			return killsByRound[rn][i].Tick < killsByRound[rn][j].Tick
		})
	}

	// For each kill, look backward for a kill that:
	//   kills[j].KillerSteamID == K.VictimSteamID   → the enemy who killed someone was killed by K.Victim
	//   kills[j].VictimTeam == K.KillerTeam          → the killed-one was a teammate of K.Killer
	//   → K is avenging / trading that prior kill
	for rn, kills := range killsByRound {
		for i := range kills {
			k := &killsByRound[rn][i]

			// TradeKill: look backward within window.
			for j := i - 1; j >= 0; j-- {
				prev := kills[j]
				if k.Tick-prev.Tick > tradeWindowTicks {
					break
				}
				// prev killed k.Victim's side; k.Killer now kills prev.Killer
				if prev.KillerSteamID == k.VictimSteamID && prev.VictimTeam == k.KillerTeam {
					k.isTradeKill = true
					k.tradeKillDelayTicks = k.Tick - prev.Tick
					break
				}
			}

			// TradeDeath: look forward within window.
			for j := i + 1; j < len(kills); j++ {
				next := kills[j]
				if next.Tick-k.Tick > tradeWindowTicks {
					break
				}
				// k killed someone; next kills k.Killer (teammate of k.Victim trades)
				if next.VictimSteamID == k.KillerSteamID && next.KillerTeam == k.VictimTeam {
					k.isTradeDeath = true
					k.tradeDeathDelayTicks = next.Tick - k.Tick
					break
				}
			}
		}
	}

	// Collect per-player trade delay samples from the annotated kills.
	tradeKillDelays := make(map[uint64][]float64)  // killerID → ms delays for their trade kills
	tradeDeathDelays := make(map[uint64][]float64) // victimID → ms delays until their death was traded
	for _, kills := range killsByRound {
		for _, k := range kills {
			if k.isTradeKill && k.tradeKillDelayTicks > 0 {
				ms := float64(k.tradeKillDelayTicks) / raw.TicksPerSecond * 1000
				tradeKillDelays[k.KillerSteamID] = append(tradeKillDelays[k.KillerSteamID], ms)
			}
			if k.isTradeDeath && k.tradeDeathDelayTicks > 0 {
				ms := float64(k.tradeDeathDelayTicks) / raw.TicksPerSecond * 1000
				tradeDeathDelays[k.VictimSteamID] = append(tradeDeathDelays[k.VictimSteamID], ms)
			}
		}
	}

	// ---- Pass 14: Save & Assist Annotation (HLTV-compatible). ----
	//
	// For each kill K=(killer A, victim V, tick T):
	//   • Save check: find the latest damage V dealt to any teammate of A
	//     within saveWindowTicks before T. If one exists, credit A with
	//     "saved teammate" and that teammate with "saved by teammate".
	//     At most one save credit per kill (the most recently-damaged teammate).
	//   • Assisted-kill check: if any teammate of A (other than A) dealt
	//     damage to V earlier in the same round, credit A with one assisted-kill.
	//     Counted once per kill regardless of how many teammates contributed.
	//
	// Damage events have no VictimTeam field, but CS2 friendly fire is off in
	// pro play — so damage events are effectively enemy-on-enemy, which lets
	// us skip the team check on the damage's victim/attacker for the save and
	// assisted-kill cases.
	saveWindowTicks := int(1.0 * raw.TicksPerSecond)
	damagesByRound := make(map[int][]model.RawDamage)
	for _, d := range raw.Damages {
		damagesByRound[d.RoundNumber] = append(damagesByRound[d.RoundNumber], d)
	}
	savedByTeammate := make(map[uint64]int)
	savedTeammate := make(map[uint64]int)
	assistedKills := make(map[uint64]int)
	for rn, kills := range killsByRound {
		damages := damagesByRound[rn]
		for _, k := range kills {
			A := k.KillerSteamID
			V := k.VictimSteamID
			if A == 0 || V == 0 || A == V {
				continue // world damage, suicide, self-kill — no credit.
			}
			// Save: find latest damage from V to a teammate of A within 1s.
			var bestSavedID uint64
			bestSavedTick := -1
			for _, d := range damages {
				if d.AttackerSteamID != V {
					continue // damage must be inflicted by the victim of K
				}
				if d.VictimSteamID == A || d.VictimSteamID == 0 {
					continue // we want teammates of A, not A itself or world
				}
				if d.Tick >= k.Tick {
					continue // damage must precede the kill
				}
				if k.Tick-d.Tick > saveWindowTicks {
					continue // outside the 1s window
				}
				if d.Tick > bestSavedTick {
					bestSavedTick = d.Tick
					bestSavedID = d.VictimSteamID
				}
			}
			if bestSavedID != 0 {
				savedTeammate[A]++
				savedByTeammate[bestSavedID]++
			}
			// Assisted-kill: any teammate of A (≠ A) damaged V earlier in the round.
			for _, d := range damages {
				if d.VictimSteamID != V {
					continue
				}
				if d.AttackerSteamID == A || d.AttackerSteamID == 0 {
					continue
				}
				if d.Tick >= k.Tick {
					continue
				}
				assistedKills[A]++
				break // count once per kill
			}
		}
	}

	// ---- Pass 15: HLTV-style flash assists (25-dmg threshold in blind window). ----
	//
	// For each kill K=(killer A, victim V, tick T):
	//   • Find the most recent RawFlash that blinded V and is still active at T
	//     (Tick ≤ T ≤ Tick + duration_ticks), thrown by a teammate of A
	//     (not A himself — self-flash assists don't count).
	//   • Sum A's damage to V during that flash's blind window.
	//   • If total ≥ 25 dmg, credit the flash thrower with one HltvFlashAssist.
	//
	// This is distinct from the in-game FlashAssists field on PlayerMatchStats,
	// which uses the demoinfocs ~40 dmg kill-feed rule and is sourced upstream.
	// We keep both side-by-side so users can compare.
	const hltvFlashAssistDmgThreshold = 25

	type fkey struct {
		round  int
		victim uint64
	}
	flashesByRoundVictim := make(map[fkey][]model.RawFlash)
	for _, f := range raw.Flashes {
		k := fkey{f.RoundNumber, f.VictimSteamID}
		flashesByRoundVictim[k] = append(flashesByRoundVictim[k], f)
	}

	type dkey struct {
		round            int
		attacker, victim uint64
	}
	damagesByAtkVic := make(map[dkey][]model.RawDamage)
	for _, d := range raw.Damages {
		k := dkey{d.RoundNumber, d.AttackerSteamID, d.VictimSteamID}
		damagesByAtkVic[k] = append(damagesByAtkVic[k], d)
	}

	hltvFlashAssists := make(map[uint64]int)
	for _, kills := range killsByRound {
		for _, k := range kills {
			A := k.KillerSteamID
			V := k.VictimSteamID
			if A == 0 || V == 0 || A == V {
				continue
			}
			// Find the flash on V with the latest start tick that's still
			// active at the kill tick and was thrown by A's teammate.
			flashes := flashesByRoundVictim[fkey{k.RoundNumber, V}]
			var (
				bestFlash     *model.RawFlash
				bestStartTick = -1
				bestEndTick   int
			)
			for i := range flashes {
				f := &flashes[i]
				if f.AttackerSteamID == 0 || f.AttackerSteamID == A {
					continue // null thrower, or self-flash → no assist
				}
				if f.AttackerTeam != k.KillerTeam {
					continue // team-flash from the wrong side
				}
				endTick := f.Tick + int(f.FlashDuration.Seconds()*raw.TicksPerSecond)
				if f.Tick > k.Tick || endTick < k.Tick {
					continue // flash window doesn't cover the kill
				}
				if f.Tick > bestStartTick {
					bestStartTick = f.Tick
					bestEndTick = endTick
					bestFlash = f
				}
			}
			if bestFlash == nil {
				continue
			}
			// Sum A's damage to V during the blind window.
			var dmgInWindow int
			for _, d := range damagesByAtkVic[dkey{k.RoundNumber, A, V}] {
				if d.Tick >= bestFlash.Tick && d.Tick <= bestEndTick {
					dmgInWindow += enemyDamageTaken(d)
				}
			}
			if dmgInWindow >= hltvFlashAssistDmgThreshold {
				hltvFlashAssists[bestFlash.AttackerSteamID]++
			}
		}
	}

	// ---- Pass 16: Liveness — time alive and sole-survivor moments. ----
	//
	// For each round R:
	//   • Time alive per player = (death_tick or R.EndTick) − R.FreezeEndTick,
	//     converted to seconds via raw.TicksPerSecond. Anchoring at
	//     FreezeEndTick (action start) — not StartTick — matches HLTV's
	//     "time alive" semantics where the typical cohort lands at 50–90 s
	//     of in-action time. Players who survived get credited for the full
	//     action duration. Players not in PlayerEndState are skipped (likely
	//     disconnected).
	//   • Last alive on server: sweep kills in tick order, removing victims
	//     from the alive set. The first kill that drops the alive count to 1
	//     credits that lone player with one "last alive on server" round.
	//     Capped at one credit per round.
	aliveSeconds := make(map[uint64]float64)
	lastAliveServer := make(map[uint64]int)
	for _, round := range raw.Rounds {
		if len(round.PlayerEndState) == 0 {
			continue
		}
		kills := killsByRound[round.Number]
		// Per-player first-death tick this round.
		deathTick := make(map[uint64]int, len(kills))
		for _, k := range kills {
			if _, seen := deathTick[k.VictimSteamID]; !seen {
				deathTick[k.VictimSteamID] = k.Tick
			}
		}
		// Time alive (action time, anchored at FreezeEndTick).
		for playerID := range round.PlayerEndState {
			endTick := round.EndTick
			if dt, died := deathTick[playerID]; died {
				endTick = dt
			}
			ticksAlive := max(endTick-round.FreezeEndTick, 0)
			aliveSeconds[playerID] += float64(ticksAlive) / raw.TicksPerSecond
		}
		// Last-alive-on-server: walk kills in tick order (Pass 1 already sorted them).
		alive := make(map[uint64]bool, len(round.PlayerEndState))
		for playerID := range round.PlayerEndState {
			alive[playerID] = true
		}
		credited := false
		for _, k := range kills {
			if !alive[k.VictimSteamID] {
				continue
			}
			delete(alive, k.VictimSteamID)
			if !credited && len(alive) == 1 {
				for survivorID := range alive {
					lastAliveServer[survivorID]++
				}
				credited = true
				break
			}
		}
	}

	// ---- Pass 2: first kill per round after FreezeEndTick = opening kill/death. ----

	type openingResult struct {
		killerID uint64
		victimID uint64
	}
	openingByRound := make(map[int]openingResult)
	for _, round := range raw.Rounds {
		kills := killsByRound[round.Number]
		for _, k := range kills {
			if k.Tick < round.FreezeEndTick {
				continue // pre-round kill (shouldn't happen but guard anyway)
			}
			// First valid kill is the opening kill/death.
			openingByRound[round.Number] = openingResult{
				killerID: k.KillerSteamID,
				victimID: k.VictimSteamID,
			}
			break
		}
	}

	// ---- Pass 3: per-round per-player stats. ----

	// Build indexed damage/flash maps.
	type damageKey struct {
		roundN               int
		attackerID, victimID uint64
	}
	type flashKey struct {
		roundN               int
		attackerID, victimID uint64
	}

	// player x round → total damage dealt, utility damage, flash assists.
	type roundDamage struct {
		health  int
		utility int
	}
	damageLedger := make(map[damageKey]roundDamage)
	for _, d := range raw.Damages {
		k := damageKey{d.RoundNumber, d.AttackerSteamID, d.VictimSteamID}
		prev := damageLedger[k]
		prev.health += enemyDamageTaken(d)
		if d.IsUtility {
			prev.utility += enemyDamageTaken(d)
		}
		damageLedger[k] = prev
	}

	// Flash assists: attacker flashed victim who was killed by a teammate of attacker.
	// Strategy: for each kill with AssistedFlash=true, the assister is the flasher.
	// Track total health damage per (attacker, round).
	type playerRoundKey struct {
		playerID uint64
		roundN   int
	}
	totalDmgByPlayerRound := make(map[playerRoundKey]int)
	utilDmgByPlayerRound := make(map[playerRoundKey]int)
	for _, d := range raw.Damages {
		// ADR path: capped, enemy-only damage (HLTV definition).
		pk := playerRoundKey{d.AttackerSteamID, d.RoundNumber}
		totalDmgByPlayerRound[pk] += enemyDamageTaken(d)
		if d.IsUtility {
			utilDmgByPlayerRound[pk] += enemyDamageTaken(d)
		}
	}

	// Weapon-level accumulators.
	type weaponKey struct {
		playerID uint64
		weapon   string
	}
	weaponKills := make(map[weaponKey]int)
	weaponHS := make(map[weaponKey]int)
	weaponDeaths := make(map[weaponKey]int)
	weaponAssist := make(map[weaponKey]int)
	weaponDamage := make(map[weaponKey]int)
	weaponHits := make(map[weaponKey]int)

	for _, d := range raw.Damages {
		if d.AttackerSteamID == 0 {
			continue
		}
		// Skip team damage: hits on teammates shouldn't count toward
		// per-weapon damage or hit counts (combat accuracy vs enemies).
		if d.AttackerTeam != model.TeamUnknown && d.AttackerTeam == d.VictimTeam {
			continue
		}
		wk := weaponKey{d.AttackerSteamID, d.Weapon}
		weaponDamage[wk] += enemyDamageTaken(d)
		weaponHits[wk]++
	}

	// Flash assists per (attacker, round).
	flashAssistsByPlayerRound := make(map[playerRoundKey]int)
	for _, k := range raw.Kills {
		if k.AssistedFlash && k.AssisterSteamID != 0 {
			pk := playerRoundKey{k.AssisterSteamID, k.RoundNumber}
			flashAssistsByPlayerRound[pk]++
		}
	}
	_ = flashKey{}
	_ = damageLedger

	// Collect all unique player IDs.
	playerSet := make(map[uint64]struct{})
	for id := range raw.PlayerNames {
		playerSet[id] = struct{}{}
	}
	for _, r := range raw.Rounds {
		for id := range r.PlayerEndState {
			playerSet[id] = struct{}{}
		}
	}

	// Determine dominant team per player (most common across rounds).
	playerDominantTeam := make(map[uint64]model.Team)
	teamCount := make(map[uint64]map[model.Team]int)
	for _, k := range raw.Kills {
		if teamCount[k.KillerSteamID] == nil {
			teamCount[k.KillerSteamID] = make(map[model.Team]int)
		}
		teamCount[k.KillerSteamID][k.KillerTeam]++
		if teamCount[k.VictimSteamID] == nil {
			teamCount[k.VictimSteamID] = make(map[model.Team]int)
		}
		teamCount[k.VictimSteamID][k.VictimTeam]++
	}
	for id := range playerSet {
		teams := teamCount[id]
		best, bestCount := model.TeamUnknown, 0
		for t, c := range teams {
			if c > bestCount {
				best, bestCount = t, c
			}
		}
		if best == model.TeamUnknown {
			if t, ok := raw.PlayerTeams[id]; ok {
				best = t
			}
		}
		playerDominantTeam[id] = best
	}

	// Build per-round per-player round stats.
	var allRoundStats []model.PlayerRoundStats

	// Map kill results indexed by round.
	type killRoundStats struct {
		killerID     uint64
		victimID     uint64
		assisterID   uint64
		isTradeKill  bool
		isTradeDeath bool
		isHeadshot   bool
		assistFlash  bool
	}

	roundKillResults := make(map[int][]killRoundStats)
	for rn, kills := range killsByRound {
		for _, k := range kills {
			roundKillResults[rn] = append(roundKillResults[rn], killRoundStats{
				killerID:     k.KillerSteamID,
				victimID:     k.VictimSteamID,
				assisterID:   k.AssisterSteamID,
				isTradeKill:  k.isTradeKill,
				isTradeDeath: k.isTradeDeath,
				isHeadshot:   k.IsHeadshot,
				assistFlash:  k.AssistedFlash,
			})
		}
	}

	// Match-level accumulators per player.
	type matchAccum struct {
		kills, assists, deaths      int
		headshotKills, flashAssists int
		totalDamage, utilityDamage  int
		openingKills, openingDeaths int
		tradeKills, tradeDeaths     int
		kastRounds, roundsPlayed    int
		unusedUtility               int
		roundsWon                   int
	}
	matchAccums := make(map[uint64]*matchAccum)
	for id := range playerSet {
		matchAccums[id] = &matchAccum{}
	}

	for _, round := range raw.Rounds {
		rn := round.Number
		kills := roundKillResults[rn]
		opening := openingByRound[rn]

		// Which players participated in this round (appeared in end state or had an event).
		roundPlayers := make(map[uint64]struct{})
		for id := range round.PlayerEndState {
			roundPlayers[id] = struct{}{}
		}
		for _, k := range kills {
			roundPlayers[k.killerID] = struct{}{}
			roundPlayers[k.victimID] = struct{}{}
		}

		// Build victim order for clutch detection (kills are already sorted by tick via Pass 1).
		victimOrder := make([]uint64, 0, len(kills))
		for _, k := range kills {
			victimOrder = append(victimOrder, k.victimID)
		}
		clutchMap := computeClutch(roundPlayers, victimOrder, func(id uint64) model.Team {
			if es, ok := round.PlayerEndState[id]; ok {
				return es.Team
			}
			return playerDominantTeam[id]
		})

		for playerID := range roundPlayers {
			if playerID == 0 {
				continue
			}

			endState, hasEndState := round.PlayerEndState[playerID]

			rs := model.PlayerRoundStats{
				DemoHash:    raw.DemoHash,
				SteamID:     playerID,
				RoundNumber: rn,
				Team:        playerDominantTeam[playerID],
			}
			if hasEndState {
				rs.Team = endState.Team
			}

			// Per-kill accounting.
			wasKilled := false
			for _, k := range kills {
				if k.killerID == playerID {
					rs.Kills++
					rs.GotKill = true
					if k.isTradeKill {
						rs.IsTradeKill = true
					}
					// isTradeDeath on a kill means this killer's subsequent death was a trade
					if k.isTradeDeath {
						rs.IsTradeDeath = true
					}
				}
				if k.victimID == playerID {
					wasKilled = true
					// victim of a kill that was traded gets WasTraded (earns KAST)
					if k.isTradeDeath {
						rs.WasTraded = true
					}
				}
				if k.assisterID == playerID {
					rs.Assists++
					rs.GotAssist = true
				}
			}

			// Surviving: a player survived the round iff they were not a victim of
			// any kill that round. HP sampling CANNOT be used here — the per-tick
			// sampler only records living players, so a dead player's last in-round
			// sample still shows HP>0. Reading rs.Survived from endState.IsAlive made
			// KAST ≈100% for everyone (kast_rounds == rounds_played). The kill record
			// is the authoritative death signal.
			rs.Survived = !wasKilled
			if hasEndState {
				rs.UnusedUtility = endState.GrenadeCount
			}

			// Opening kill/death.
			if opening.killerID == playerID {
				rs.IsOpeningKill = true
			}
			if opening.victimID == playerID {
				rs.IsOpeningDeath = true
			}

			// Buy type classification from equipment value at freeze-end.
			buyType := "eco"
			if equip, ok := round.PlayerEquipValues[playerID]; ok {
				switch {
				case equip >= 4500:
					buyType = "full"
				case equip >= 2000:
					buyType = "force"
				case equip >= 1000:
					buyType = "half"
				}
			}
			rs.BuyType = buyType

			// Damage.
			pk := playerRoundKey{playerID, rn}
			rs.Damage = totalDmgByPlayerRound[pk]

			// KAST: Kill, Assist, Survive, or Traded.
			rs.KASTEarned = rs.GotKill || rs.GotAssist || rs.Survived || rs.WasTraded

			// Round context: post-plant, clutch, and win/loss.
			rs.IsPostPlant = round.BombPlantTick > 0
			if ci, ok := clutchMap[playerID]; ok {
				rs.IsInClutch = ci.isClutch
				rs.ClutchEnemyCount = ci.enemyCount
			}
			rs.WonRound = round.WinnerTeam != model.TeamUnknown && round.WinnerTeam == rs.Team

			allRoundStats = append(allRoundStats, rs)

			// Accumulate match-level stats.
			acc := matchAccums[playerID]
			acc.roundsPlayed++
			if rs.WonRound {
				acc.roundsWon++
			}
			acc.kills += rs.Kills
			acc.assists += rs.Assists
			acc.totalDamage += rs.Damage
			acc.utilityDamage += utilDmgByPlayerRound[pk]
			acc.unusedUtility += rs.UnusedUtility
			if rs.GotKill {
				// headshot kills counted below
			}
			if rs.IsOpeningKill {
				acc.openingKills++
			}
			if rs.IsOpeningDeath {
				acc.openingDeaths++
			}
			if rs.IsTradeKill {
				acc.tradeKills++
			}
			if rs.IsTradeDeath {
				acc.tradeDeaths++
			}
			if rs.KASTEarned {
				acc.kastRounds++
			}
		}
	}

	// Count deaths (from kills list) and populate weapon kill maps.
	for _, k := range raw.Kills {
		if acc, ok := matchAccums[k.VictimSteamID]; ok {
			acc.deaths++
		}
		if k.IsHeadshot {
			if acc, ok := matchAccums[k.KillerSteamID]; ok {
				acc.headshotKills++
			}
		}
		if k.AssistedFlash && k.AssisterSteamID != 0 {
			if acc, ok := matchAccums[k.AssisterSteamID]; ok {
				acc.flashAssists++
			}
		}
		// Weapon kills/HS/deaths/assists.
		if k.KillerSteamID != 0 && k.Weapon != "" {
			wk := weaponKey{k.KillerSteamID, k.Weapon}
			weaponKills[wk]++
			if k.IsHeadshot {
				weaponHS[wk]++
			}
		}
		if k.VictimSteamID != 0 && k.Weapon != "" {
			weaponDeaths[weaponKey{k.VictimSteamID, k.Weapon}]++
		}
		if k.AssisterSteamID != 0 && k.Weapon != "" {
			weaponAssist[weaponKey{k.AssisterSteamID, k.Weapon}]++
		}
	}

	// ---- Pass 17: Scan volatility — out-of-combat crosshair discipline. ----
	// (Implementation in computeScanVolatility; see the constants there for
	// the metric definitions.) Patch per-round rows, then roll up per player
	// below when building PlayerMatchStats.
	scanByPlayer := computeScanVolatility(raw)
	for i := range allRoundStats {
		rs := &allRoundStats[i]
		if res, ok := scanByPlayer[rs.SteamID][rs.RoundNumber]; ok {
			rs.ScanOOCSeconds = res.oocSeconds
			rs.ScanDwellPct = 100 * res.dwellSec / res.oocSeconds
			rs.ScanReversals = res.reversals
			rs.ScanAvgYawDegPerSec = res.absYawDeg / res.oocSeconds
		}
	}

	// ---- Pass 4: roll up into PlayerMatchStats. ----
	var matchStats []model.PlayerMatchStats
	for playerID, acc := range matchAccums {
		if acc.roundsPlayed == 0 {
			continue
		}
		ms := model.PlayerMatchStats{
			DemoHash:      raw.DemoHash,
			SteamID:       playerID,
			Name:          raw.PlayerNames[playerID],
			Team:          playerDominantTeam[playerID],
			Kills:         acc.kills,
			Assists:       acc.assists,
			Deaths:        acc.deaths,
			HeadshotKills: acc.headshotKills,
			FlashAssists:  acc.flashAssists,
			TotalDamage:   acc.totalDamage,
			UtilityDamage: acc.utilityDamage,
			RoundsPlayed:  acc.roundsPlayed,
			OpeningKills:  acc.openingKills,
			OpeningDeaths: acc.openingDeaths,
			TradeKills:    acc.tradeKills,
			TradeDeaths:   acc.tradeDeaths,
			KASTRounds:    acc.kastRounds,
			UnusedUtility: acc.unusedUtility,
			RoundsWon:     acc.roundsWon,
		}
		if delays := tradeKillDelays[playerID]; len(delays) > 0 {
			sort.Float64s(delays)
			ms.MedianTradeKillDelayMs = median(delays)
		}
		if delays := tradeDeathDelays[playerID]; len(delays) > 0 {
			sort.Float64s(delays)
			ms.MedianTradeDeathDelayMs = median(delays)
		}
		ms.SavedByTeammate = savedByTeammate[playerID]
		ms.SavedTeammate = savedTeammate[playerID]
		ms.AssistedKills = assistedKills[playerID]
		ms.HltvFlashAssists = hltvFlashAssists[playerID]
		ms.AliveSecondsTotal = aliveSeconds[playerID]
		ms.LastAliveServerRounds = lastAliveServer[playerID]
		// Pass 17 rollup: time-weighted across rounds.
		var scanOOC, scanDwell, scanYaw float64
		var scanRevs int
		for _, res := range scanByPlayer[playerID] {
			scanOOC += res.oocSeconds
			scanDwell += res.dwellSec
			scanYaw += res.absYawDeg
			scanRevs += res.reversals
		}
		if scanOOC > 0 {
			ms.ScanOOCSeconds = scanOOC
			ms.ScanDwellPct = 100 * scanDwell / scanOOC
			ms.ScanReversalsPerMin = float64(scanRevs) / scanOOC * 60
			ms.ScanAvgYawDegPerSec = scanYaw / scanOOC
		}
		matchStats = append(matchStats, ms)
	}

	// Sort by kills desc for stable output.
	sort.Slice(matchStats, func(i, j int) bool {
		return matchStats[i].Kills > matchStats[j].Kills
	})

	// ---- Pass 5: crosshair placement aggregation (total + pitch/yaw split). ----
	type xhairAccum struct {
		angles  []float64
		pitches []float64
		yaws    []float64
	}
	xhairByPlayer := make(map[uint64]*xhairAccum)
	for _, fs := range raw.FirstSights {
		acc := xhairByPlayer[fs.ObserverID]
		if acc == nil {
			acc = &xhairAccum{}
			xhairByPlayer[fs.ObserverID] = acc
		}
		acc.angles = append(acc.angles, fs.AngleDeg)
		acc.pitches = append(acc.pitches, fs.PitchDeg)
		acc.yaws = append(acc.yaws, fs.YawDeg)
	}
	for i := range matchStats {
		acc := xhairByPlayer[matchStats[i].SteamID]
		if acc == nil || len(acc.angles) == 0 {
			continue
		}
		sort.Float64s(acc.angles)
		sort.Float64s(acc.pitches)
		sort.Float64s(acc.yaws)
		n := len(acc.angles)
		matchStats[i].CrosshairEncounters = n
		matchStats[i].CrosshairMedianDeg = median(acc.angles)
		matchStats[i].CrosshairMedianPitchDeg = median(acc.pitches)
		matchStats[i].CrosshairMedianYawDeg = median(acc.yaws)
		under5 := 0
		for _, a := range acc.angles {
			if a < 5.0 {
				under5++
			}
		}
		matchStats[i].CrosshairPctUnder5 = float64(under5) / float64(n) * 100
	}

	// Build weapon stats from accumulated maps.
	// Collect all unique weapon keys.
	allWeaponKeys := make(map[weaponKey]struct{})
	for wk := range weaponKills {
		allWeaponKeys[wk] = struct{}{}
	}
	for wk := range weaponDeaths {
		allWeaponKeys[wk] = struct{}{}
	}
	for wk := range weaponAssist {
		allWeaponKeys[wk] = struct{}{}
	}
	for wk := range weaponDamage {
		allWeaponKeys[wk] = struct{}{}
	}

	// ---- Pass 18: shot accounting (accuracy + aimed/blind split) ----
	shotAcct := computeShotAccounting(raw)
	// A weapon can be fired without ever landing a hit or a kill, so shot
	// keys widen the weapon-row set rather than just annotating it.
	for sk := range shotAcct {
		allWeaponKeys[weaponKey{sk.playerID, sk.weapon}] = struct{}{}
	}

	var weaponStats []model.PlayerWeaponStats
	for wk := range allWeaponKeys {
		sa := shotAcct[shotKey{wk.playerID, wk.weapon}]
		if sa == nil {
			sa = &shotAccum{}
		}
		weaponStats = append(weaponStats, model.PlayerWeaponStats{
			DemoHash:        raw.DemoHash,
			SteamID:         wk.playerID,
			Weapon:          wk.weapon,
			Kills:           weaponKills[wk],
			HeadshotKills:   weaponHS[wk],
			Assists:         weaponAssist[wk],
			Deaths:          weaponDeaths[wk],
			Damage:          weaponDamage[wk],
			Hits:            weaponHits[wk],
			ShotsFired:      sa.shots,
			ShotsVisible:    sa.shotsVisible,
			HitsVisible:     sa.hitsVisible,
			HeadHits:        sa.headHits,
			HeadHitsVisible: sa.headHitsVis,
		})
	}
	sort.Slice(weaponStats, func(i, j int) bool {
		if weaponStats[i].Kills != weaponStats[j].Kills {
			return weaponStats[i].Kills > weaponStats[j].Kills
		}
		return weaponStats[i].Damage > weaponStats[j].Damage
	})

	// ---- Pass 6: Duel Engine ----

	// Build first-sight index: (observerID, enemyID, roundN) → first-sight tick.
	type sightKey struct {
		obsID, enemyID uint64
		roundN         int
	}
	firstSightIdx := make(map[sightKey]model.RawFirstSight)
	for _, fs := range raw.FirstSights {
		k := sightKey{fs.ObserverID, fs.EnemyID, fs.RoundNumber}
		if _, exists := firstSightIdx[k]; !exists {
			firstSightIdx[k] = fs
		}
	}

	// Build damage index: (roundN, atkID, vicID) → sorted slice of RawDamage (non-utility only).
	type duelDmgKey struct {
		roundN       int
		atkID, vicID uint64
	}
	duelDmgIdx := make(map[duelDmgKey][]model.RawDamage)
	for _, d := range raw.Damages {
		if d.IsUtility {
			continue
		}
		k := duelDmgKey{d.RoundNumber, d.AttackerSteamID, d.VictimSteamID}
		duelDmgIdx[k] = append(duelDmgIdx[k], d)
	}
	// Sort each slice by tick ascending.
	for k := range duelDmgIdx {
		sort.Slice(duelDmgIdx[k], func(i, j int) bool {
			return duelDmgIdx[k][i].Tick < duelDmgIdx[k][j].Tick
		})
	}

	// Build weapon-fire index: (shooterID, roundN) → sorted slice of RawWeaponFire.
	type wfKey struct {
		shooterID uint64
		roundN    int
	}
	wfIdx := make(map[wfKey][]model.RawWeaponFire)
	for _, wf := range raw.WeaponFires {
		k := wfKey{wf.ShooterID, wf.RoundNumber}
		wfIdx[k] = append(wfIdx[k], wf)
	}
	for k := range wfIdx {
		sort.Slice(wfIdx[k], func(i, j int) bool {
			return wfIdx[k][i].Tick < wfIdx[k][j].Tick
		})
	}

	// Duel accumulators per player.
	type duelAccum struct {
		winMs           []float64
		lossMs          []float64
		hitsToKill      []float64
		firstHitHSCount int
		firstHitTotal   int
		correctionDegs  []float64
	}
	duelAccums := make(map[uint64]*duelAccum)
	getDuelAccum := func(id uint64) *duelAccum {
		if duelAccums[id] == nil {
			duelAccums[id] = &duelAccum{}
		}
		return duelAccums[id]
	}

	// Segment accumulators: per (player, round, weapon_bucket, distance_bin).
	// The round dimension lets consumers slice FHHS by side / buy type /
	// round phase by joining player_round_stats; it also makes each cell
	// nearly one duel, so the medians below are effectively per-duel values.
	type segKey struct {
		playerID uint64
		round    int
		bucket   string
		bin      string
	}
	type segAccum struct {
		duelCount       int
		firstHitCount   int
		firstHitHSCount int
		corrDegs        []float64
		sightDegs       []float64
		expoWinMs       []float64
		shotDelayMs     []float64
	}
	segAccums := make(map[segKey]*segAccum)

	tps := raw.TicksPerSecond
	if tps == 0 {
		tps = 64.0
	}

	for _, kill := range raw.Kills {
		rn := kill.RoundNumber
		killerID := kill.KillerSteamID
		victimID := kill.VictimSteamID
		killTick := kill.Tick

		// Win accounting for killer.
		sk := sightKey{killerID, victimID, rn}
		if fs, ok := firstSightIdx[sk]; ok && fs.Tick <= killTick {
			sightTick := fs.Tick
			winMs := float64(killTick-sightTick) / tps * 1000

			// Count hits from killer→victim in [sightTick, killTick]; capture victim position from first hit.
			dmgKey := duelDmgKey{rn, killerID, victimID}
			damages := duelDmgIdx[dmgKey]
			hits := 0
			firstHitHS := false
			firstHitCounted := false
			victimPos := model.Vec3{}
			victimPosSet := false
			for _, d := range damages {
				if d.Tick < sightTick || d.Tick > killTick {
					continue
				}
				if !firstHitCounted {
					firstHitHS = d.HitGroup == "head"
					firstHitCounted = true
					victimPos = d.VictimPos
					victimPosSet = true
				}
				hits++
			}

			acc := getDuelAccum(killerID)
			acc.winMs = append(acc.winMs, winMs)
			if hits > 0 {
				acc.hitsToKill = append(acc.hitsToKill, float64(hits))
				acc.firstHitTotal++
				if firstHitHS {
					acc.firstHitHSCount++
				}
			}

			// Pre-shot correction and attacker position from first weapon fire in window.
			wfList := wfIdx[wfKey{killerID, rn}]
			corrDeg := 0.0
			shotDelayMs := 0.0
			corrComputed := false
			attackerPos := model.Vec3{}
			attackerPosSet := false
			for _, wf := range wfList {
				if wf.Tick < sightTick || wf.Tick > killTick {
					continue
				}
				corrDeg = angularDeltaDeg(fs.ObserverPitchDeg, fs.ObserverYawDeg, wf.PitchDeg, wf.YawDeg)
				shotDelayMs = float64(wf.Tick-sightTick) / tps * 1000
				corrComputed = true
				acc.correctionDegs = append(acc.correctionDegs, corrDeg)
				attackerPos = wf.AttackerPos
				attackerPosSet = true
				break
			}

			// Compute distance and segment.
			distM := -1.0
			if attackerPosSet && victimPosSet {
				dx := attackerPos.X - victimPos.X
				dy := attackerPos.Y - victimPos.Y
				dz := attackerPos.Z - victimPos.Z
				distM = math.Sqrt(dx*dx+dy*dy+dz*dz) * unitsToMeters
			}
			bucket := weaponBucket(kill.Weapon)
			bin := distanceBin(distM)

			sk2 := segKey{killerID, rn, bucket, bin}
			if segAccums[sk2] == nil {
				segAccums[sk2] = &segAccum{}
			}
			sa := segAccums[sk2]
			sa.duelCount++
			sa.sightDegs = append(sa.sightDegs, fs.AngleDeg)
			sa.expoWinMs = append(sa.expoWinMs, winMs)
			if firstHitCounted {
				sa.firstHitCount++
				if firstHitHS {
					sa.firstHitHSCount++
				}
			}
			if corrComputed {
				sa.corrDegs = append(sa.corrDegs, corrDeg)
				sa.shotDelayMs = append(sa.shotDelayMs, shotDelayMs)
			}
		}

		// Loss accounting for victim.
		// The sight key from killer's perspective: killer spotted victim → killer had sight of victim.
		// But we want: victim had sight of killer (victim was the "observer" of killer).
		// Use the victim's sight of the killer if available; otherwise just count the loss tick.
		sk2 := sightKey{victimID, killerID, rn}
		if fs2, ok := firstSightIdx[sk2]; ok && fs2.Tick <= killTick {
			sightTick2 := fs2.Tick
			lossMs := float64(killTick-sightTick2) / tps * 1000
			getDuelAccum(victimID).lossMs = append(getDuelAccum(victimID).lossMs, lossMs)
		} else {
			// Victim didn't spot killer; still count as a duel loss with 0ms exposure.
			getDuelAccum(victimID).lossMs = append(getDuelAccum(victimID).lossMs, 0)
		}

		// Increment win/loss counts.
		getDuelAccum(killerID).winMs = getDuelAccum(killerID).winMs // already appended above if sight found
		// Note: we count a win as "had sight of victim before the kill".
		// We count a loss as "victim died" regardless.
	}

	// Write duel stats into matchStats.
	// First build duel win/loss counts properly.
	// Reset and recompute from duelAccums (win = len(winMs), loss = len(lossMs)).
	for i := range matchStats {
		id := matchStats[i].SteamID
		acc := duelAccums[id]
		if acc == nil {
			continue
		}
		matchStats[i].DuelWins = len(acc.winMs)
		matchStats[i].DuelLosses = len(acc.lossMs)

		sort.Float64s(acc.winMs)
		sort.Float64s(acc.lossMs)
		sort.Float64s(acc.hitsToKill)
		sort.Float64s(acc.correctionDegs)

		matchStats[i].MedianExposureWinMs = median(acc.winMs)
		matchStats[i].MedianExposureLossMs = median(acc.lossMs)
		matchStats[i].MedianHitsToKill = median(acc.hitsToKill)
		if acc.firstHitTotal > 0 {
			matchStats[i].FirstHitHSRate = float64(acc.firstHitHSCount) / float64(acc.firstHitTotal) * 100
		}
		matchStats[i].MedianCorrectionDeg = median(acc.correctionDegs)
		if len(acc.correctionDegs) > 0 {
			under2 := 0
			for _, c := range acc.correctionDegs {
				if c < 2.0 {
					under2++
				}
			}
			matchStats[i].PctCorrectionUnder2Deg = float64(under2) / float64(len(acc.correctionDegs)) * 100
		}
	}

	// Convert segment accumulators to []PlayerDuelSegment.
	var duelSegments []model.PlayerDuelSegment
	for k, sa := range segAccums {
		sort.Float64s(sa.corrDegs)
		sort.Float64s(sa.sightDegs)
		sort.Float64s(sa.expoWinMs)
		sort.Float64s(sa.shotDelayMs)
		duelSegments = append(duelSegments, model.PlayerDuelSegment{
			DemoHash:          raw.DemoHash,
			SteamID:           k.playerID,
			RoundNumber:       k.round,
			WeaponBucket:      k.bucket,
			DistanceBin:       k.bin,
			DuelCount:         sa.duelCount,
			FirstHitCount:     sa.firstHitCount,
			FirstHitHSCount:   sa.firstHitHSCount,
			MedianCorrDeg:     median(sa.corrDegs),
			MedianSightDeg:    median(sa.sightDegs),
			MedianExpoWinMs:   median(sa.expoWinMs),
			MedianShotDelayMs: median(sa.shotDelayMs),
		})
	}

	// ---- Pass 7: AWP Death Classifier ----

	// Build flash index: victimID → []tick for flashes with FlashDuration > 0 per round.
	type flashVictimKey struct {
		victimID uint64
		roundN   int
	}
	flashTicksByVictim := make(map[flashVictimKey][]int)
	for _, fl := range raw.Flashes {
		if fl.FlashDuration <= 0 {
			continue
		}
		k := flashVictimKey{fl.VictimSteamID, fl.RoundNumber}
		flashTicksByVictim[k] = append(flashTicksByVictim[k], fl.Tick)
	}

	// Build prior-kill index: roundN → kills sorted by tick (reuse killsByRound).
	// (Already built above as killsByRound.)

	awpWindowTicks := int(3.0 * tps)

	for _, kill := range raw.Kills {
		if kill.Weapon != "AWP" {
			continue
		}
		victimID := kill.VictimSteamID
		rn := kill.RoundNumber
		killTick := kill.Tick

		// Find victim's matchStats index.
		victimIdx := -1
		for i := range matchStats {
			if matchStats[i].SteamID == victimID {
				victimIdx = i
				break
			}
		}
		if victimIdx < 0 {
			continue
		}

		matchStats[victimIdx].AWPDeaths++

		// DryPeek: no flash on victim in last 3s.
		isDry := true
		fKey := flashVictimKey{victimID, rn}
		for _, ft := range flashTicksByVictim[fKey] {
			if killTick-ft <= awpWindowTicks && ft <= killTick {
				isDry = false
				break
			}
		}
		if isDry {
			matchStats[victimIdx].AWPDeathsDry++
		}

		// RePeek: victim had a kill earlier this round.
		isRePeek := false
		for _, k := range killsByRound[rn] {
			if k.KillerSteamID == victimID && k.Tick < killTick {
				isRePeek = true
				break
			}
		}
		if isRePeek {
			matchStats[victimIdx].AWPDeathsRePeek++
		}

		// Isolated: NearbyVictimTeammates == 0.
		if kill.NearbyVictimTeammates == 0 {
			matchStats[victimIdx].AWPDeathsIsolated++
		}
	}

	// ---- Pass 8: Flash Quality Window ----

	// Build kill lookup: sorted by tick within round.
	// (killsByRound already built.)

	flashWindowTicks := int(1.5 * tps)

	effectiveFlashAccum := make(map[uint64]int)
	for _, fl := range raw.Flashes {
		if fl.AttackerTeam == fl.VictimTeam {
			continue // team flash — skip
		}
		if fl.FlashDuration <= 0 {
			continue
		}
		windowEnd := fl.Tick + flashWindowTicks
		rn := fl.RoundNumber
		// Check if any kill: victim == fl.VictimSteamID, killerTeam == fl.AttackerTeam, tick in window.
		for _, k := range killsByRound[rn] {
			if k.Tick < fl.Tick {
				continue
			}
			if k.Tick > windowEnd {
				break
			}
			if k.VictimSteamID == fl.VictimSteamID && k.KillerTeam == fl.AttackerTeam {
				effectiveFlashAccum[fl.AttackerSteamID]++
				break
			}
		}
	}
	for i := range matchStats {
		matchStats[i].EffectiveFlashes = effectiveFlashAccum[matchStats[i].SteamID]
	}

	// ---- Pass 9: Role classification ----
	for i := range matchStats {
		id := matchStats[i].SteamID
		totalKills := matchStats[i].Kills
		rounds := matchStats[i].RoundsPlayed
		awpKills := weaponKills[weaponKey{id, "AWP"}]

		switch {
		case totalKills > 0 && float64(awpKills)/float64(totalKills) > 0.30:
			matchStats[i].Role = "AWPer"
		case rounds > 0 && float64(matchStats[i].OpeningKills)/float64(rounds) > 0.12:
			matchStats[i].Role = "Entry"
		case rounds > 0 && (float64(matchStats[i].FlashAssists)/float64(rounds) > 0.08 ||
			float64(matchStats[i].UtilityDamage)/float64(rounds) > 15):
			matchStats[i].Role = "Support"
		default:
			matchStats[i].Role = "Rifler"
		}
	}

	// ---- Pass 10: TTK, TTD, and one-tap kills (WeaponFire-based, rolling 3s window) ----
	// TTK is measured from the first shot FIRED (not first hit) within 3s of the kill tick.
	// Including missed shots makes the numbers comparable to external tools like Refrag.
	// wfIdx is already built and sorted in Pass 6, keyed by {shooterID, roundN}.
	// One-taps (first shot in window fires at the same tick as the kill) are tracked
	// separately and excluded from TTK/TTD median samples.
	const ttkWindowSec = 3.0
	ttkWindowTicks := int(ttkWindowSec * tps)

	ttkSamples := make(map[uint64][]float64)
	ttdSamples := make(map[uint64][]float64)
	oneTapKills := make(map[uint64]int)
	for _, kill := range raw.Kills {
		if kill.KillerSteamID == 0 {
			continue
		}
		fires := wfIdx[wfKey{kill.KillerSteamID, kill.RoundNumber}]
		if len(fires) == 0 {
			continue // knife / fall / no weapon fires in this round
		}
		windowStart := kill.Tick - ttkWindowTicks
		// wfIdx entries are sorted ascending by Tick (Pass 6 sorts them).
		firstTick := -1
		for _, wf := range fires {
			if wf.Tick >= windowStart && wf.Tick <= kill.Tick {
				firstTick = wf.Tick
				break
			}
		}
		if firstTick == -1 {
			continue // no shot within the engagement window
		}
		if firstTick == kill.Tick {
			// One-tap: the killing shot was the first shot fired in the window.
			oneTapKills[kill.KillerSteamID]++
			continue
		}
		ms := float64(kill.Tick-firstTick) / tps * 1000
		ttkSamples[kill.KillerSteamID] = append(ttkSamples[kill.KillerSteamID], ms)
		ttdSamples[kill.VictimSteamID] = append(ttdSamples[kill.VictimSteamID], ms)
	}
	for i := range matchStats {
		id := matchStats[i].SteamID
		if s := ttkSamples[id]; len(s) > 0 {
			sort.Float64s(s)
			matchStats[i].MedianTTKMs = median(s)
		}
		if s := ttdSamples[id]; len(s) > 0 {
			sort.Float64s(s)
			matchStats[i].MedianTTDMs = median(s)
		}
		matchStats[i].OneTapKills = oneTapKills[id]
	}

	// ---- Counter-strafe % ----
	// A shot is counter-strafed when the shooter's horizontal speed at fire time is
	// at or below 34 Hammer units/s (≈14% of base walk speed). This threshold is
	// captured from the velocity field added to RawWeaponFire in the parser.
	const csThreshold = 34.0
	type csAccum struct{ total, strafed int }
	csMap := make(map[uint64]*csAccum)
	for _, wf := range raw.WeaponFires {
		if wf.ShooterID == 0 {
			continue
		}
		if _, ok := csMap[wf.ShooterID]; !ok {
			csMap[wf.ShooterID] = &csAccum{}
		}
		csMap[wf.ShooterID].total++
		if wf.HorizontalSpeed <= csThreshold {
			csMap[wf.ShooterID].strafed++
		}
	}
	for i := range matchStats {
		if acc, ok := csMap[matchStats[i].SteamID]; ok && acc.total > 0 {
			matchStats[i].CounterStrafePercent = float64(acc.strafed) / float64(acc.total) * 100
		}
	}

	// ---- Pass 12: death events. ----
	// Assemble one PlayerDeathEvent per kill, joining kill position/context
	// with trade annotation (pass 1), opening kill (pass 2), flash events,
	// and round phase. Denormalised match_date/map_name live on each row for
	// fast meta queries.
	const deathFlashWindowSec = 2.0
	deathFlashWindowTicks := int(deathFlashWindowSec * raw.TicksPerSecond)

	victimFlashes := make(map[uint64][]int) // victim SteamID → flash tick list
	for _, f := range raw.Flashes {
		victimFlashes[f.VictimSteamID] = append(victimFlashes[f.VictimSteamID], f.Tick)
	}

	roundByNumber := make(map[int]model.RawRound, len(raw.Rounds))
	for _, r := range raw.Rounds {
		roundByNumber[r.Number] = r
	}

	var deathEvents []model.PlayerDeathEvent
	for rn, kills := range killsByRound {
		round := roundByNumber[rn]
		for _, k := range kills {
			wasFlashed := false
			for _, ft := range victimFlashes[k.VictimSteamID] {
				if ft <= k.Tick && k.Tick-ft <= deathFlashWindowTicks {
					wasFlashed = true
					break
				}
			}

			dx := k.KillerPos.X - k.VictimPos.X
			dy := k.KillerPos.Y - k.VictimPos.Y
			dz := k.KillerPos.Z - k.VictimPos.Z
			dist := math.Sqrt(dx*dx+dy*dy+dz*dz) * unitsToMeters

			isOpening := openingByRound[rn].victimID == k.VictimSteamID

			// First-sight ticks for both parties, from the Pass 6 index.
			// -1 means that player never spotted the other this round.
			killerSight, victimSight := -1, -1
			if fs, ok := firstSightIdx[sightKey{k.KillerSteamID, k.VictimSteamID, rn}]; ok {
				killerSight = fs.Tick
			}
			if fs, ok := firstSightIdx[sightKey{k.VictimSteamID, k.KillerSteamID, rn}]; ok {
				victimSight = fs.Tick
			}
			bombPlanted := round.BombPlantTick > 0 && k.Tick >= round.BombPlantTick

			deathEvents = append(deathEvents, model.PlayerDeathEvent{
				DemoHash:        raw.DemoHash,
				MatchDate:       raw.MatchDate,
				MapName:         raw.MapName,
				RoundNumber:     rn,
				Tick:            k.Tick,
				VictimSteamID:   k.VictimSteamID,
				VictimTeam:      k.VictimTeam,
				KillerSteamID:   k.KillerSteamID,
				KillerTeam:      k.KillerTeam,
				Weapon:          k.Weapon,
				IsHeadshot:      k.IsHeadshot,
				VictimPos:       k.VictimPos,
				KillerPos:       k.KillerPos,
				VictimYawDeg:    k.VictimYawDeg,
				DistanceMeters:  dist,
				WasFlashed:      wasFlashed,
				WasTraded:       k.isTradeDeath,
				IsOpeningDeath:  isOpening,
				RoundPhase:      roundPhase(k.Tick, round, rn),
				KillerSightTick: killerSight,
				VictimSightTick: victimSight,
				BombPlanted:     bombPlanted,
			})
		}
	}

	// ---- Pass 13: flash events. ----
	// Enrich each RawFlash with the explosion position (from the closest
	// same-thrower same-round flash-type RawGrenadeEvent) and compute the angle
	// between the victim's view vector and the direction to the flash.
	// BlindAngleDeg = 0 when looking straight at the flash, 180 when facing away.
	// Flashes without a matching grenade event (e.g. older .csdem.gz files
	// without grenade capture, or flash throws split across the demo boundary)
	// emit BlindAngleDeg = 180 — they simply drop out of blind-angle analysis.
	type throwerRoundKey struct {
		thrower uint64
		round   int
	}
	flashGrenadesByKey := make(map[throwerRoundKey][]model.RawGrenadeEvent)
	for _, g := range raw.Grenades {
		if g.GrenadeType != "flash" {
			continue
		}
		k := throwerRoundKey{g.ThrowerSteamID, g.RoundNumber}
		flashGrenadesByKey[k] = append(flashGrenadesByKey[k], g)
	}

	var flashEvents []model.FlashEvent
	for _, f := range raw.Flashes {
		k := throwerRoundKey{f.AttackerSteamID, f.RoundNumber}
		var flashPos model.Vec3
		bestDelta := 1 << 30
		for _, g := range flashGrenadesByKey[k] {
			d := g.EndTick - f.Tick
			if d < 0 {
				d = -d
			}
			if d < bestDelta {
				bestDelta = d
				flashPos = g.LandPos
			}
		}

		blindDeg := 180.0
		var distM float64
		matched := flashPos.X != 0 || flashPos.Y != 0 || flashPos.Z != 0
		if matched {
			dx := flashPos.X - f.VictimPos.X
			dy := flashPos.Y - f.VictimPos.Y
			dz := flashPos.Z - f.VictimPos.Z
			dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
			distM = dist * unitsToMeters
			if dist > 1e-6 {
				yawR := f.VictimYawDeg * math.Pi / 180
				pitchR := f.VictimPitchDeg * math.Pi / 180 // Source 2: positive = looking down
				fwdX := math.Cos(pitchR) * math.Cos(yawR)
				fwdY := math.Cos(pitchR) * math.Sin(yawR)
				fwdZ := -math.Sin(pitchR)
				dot := (fwdX*dx + fwdY*dy + fwdZ*dz) / dist
				if dot > 1 {
					dot = 1
				} else if dot < -1 {
					dot = -1
				}
				blindDeg = math.Acos(dot) * 180 / math.Pi
			}
		}

		flashEvents = append(flashEvents, model.FlashEvent{
			DemoHash:       raw.DemoHash,
			MatchDate:      raw.MatchDate,
			MapName:        raw.MapName,
			RoundNumber:    f.RoundNumber,
			Tick:           f.Tick,
			ThrowerSteamID: f.AttackerSteamID,
			ThrowerTeam:    f.AttackerTeam,
			VictimSteamID:  f.VictimSteamID,
			VictimTeam:     f.VictimTeam,
			DurationSec:    f.FlashDuration.Seconds(),
			IsTeamFlash:    f.AttackerTeam == f.VictimTeam && f.AttackerSteamID != f.VictimSteamID,
			VictimPos:      f.VictimPos,
			VictimYawDeg:   f.VictimYawDeg,
			VictimPitchDeg: f.VictimPitchDeg,
			FlashPos:       flashPos,
			BlindAngleDeg:  blindDeg,
			DistanceMeters: distM,
		})
	}

	return matchStats, allRoundStats, weaponStats, duelSegments, deathEvents, flashEvents, nil
}

// clutchResult holds the clutch outcome for a single player in a round.
type clutchResult struct {
	isClutch   bool
	enemyCount int // max enemies alive when the clutch was detected
}

// computeClutch walks the kill list for a round and determines which players
// entered a clutch situation (last alive on their team facing ≥1 enemy).
// roundPlayers is the set of all player IDs who participated in the round.
// victimOrder is the ordered list of victim IDs (kill order by tick ascending).
// teamOf returns the team for a given player ID.
func computeClutch(
	roundPlayers map[uint64]struct{},
	victimOrder []uint64,
	teamOf func(uint64) model.Team,
) map[uint64]clutchResult {
	// Start with everyone alive.
	alive := make(map[uint64]bool, len(roundPlayers))
	for id := range roundPlayers {
		if id != 0 {
			alive[id] = true
		}
	}

	results := make(map[uint64]clutchResult, len(roundPlayers))

	checkClutch := func() {
		// Count alive players per team.
		teamAlive := make(map[model.Team]int)
		for id, isAlive := range alive {
			if isAlive {
				teamAlive[teamOf(id)]++
			}
		}
		// For each alive player, check if they are the sole survivor on their team
		// with at least one enemy alive.
		for id, isAlive := range alive {
			if !isAlive {
				continue
			}
			myTeam := teamOf(id)
			if myTeam == model.TeamUnknown {
				continue
			}
			myAlive := teamAlive[myTeam]
			// Count enemies alive (all teams except own).
			enemiesAlive := 0
			for t, cnt := range teamAlive {
				if t != myTeam && t != model.TeamUnknown && t != model.TeamSpectators {
					enemiesAlive += cnt
				}
			}
			if myAlive == 1 && enemiesAlive >= 1 {
				prev := results[id]
				prev.isClutch = true
				if enemiesAlive > prev.enemyCount {
					prev.enemyCount = enemiesAlive
				}
				results[id] = prev
			}
		}
	}

	for _, victimID := range victimOrder {
		alive[victimID] = false
		checkClutch()
	}

	return results
}

// median returns the median of a pre-sorted (ascending) slice of float64.
// For an even-length slice the average of the two middle values is returned.
// An empty slice returns 0.
func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// angularDeltaDeg computes the angle in degrees between two view directions
// given as (pitch, yaw) pairs in degrees. It reconstructs unit forward vectors
// from each pair using Source 2 conventions (positive pitch = looking down)
// and returns the arc-cosine of their dot product, clamped to [0, 180].
func angularDeltaDeg(pitch1, yaw1, pitch2, yaw2 float64) float64 {
	toRad := math.Pi / 180
	p1R := pitch1 * toRad
	y1R := yaw1 * toRad
	p2R := pitch2 * toRad
	y2R := yaw2 * toRad

	// Forward vector from pitch/yaw (Source2: positive pitch = looking down → negate for Z).
	fx1 := math.Cos(p1R) * math.Cos(y1R)
	fy1 := math.Cos(p1R) * math.Sin(y1R)
	fz1 := -math.Sin(p1R)

	fx2 := math.Cos(p2R) * math.Cos(y2R)
	fy2 := math.Cos(p2R) * math.Sin(y2R)
	fz2 := -math.Sin(p2R)

	dot := fx1*fx2 + fy1*fy2 + fz1*fz2
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	return math.Acos(dot) * 180 / math.Pi
}

// ---- Pass 17: Scan volatility ("panic swiping") ----
//
// Measures out-of-combat crosshair discipline from the 16 Hz view samples.
// "Out of combat" excludes any sample where an enemy is visible to the
// player, plus ±scanCombatRadiusSec around the player's own kills, deaths,
// damage dealt/taken, weapon fires, and grenade throws — utility lineups
// are deliberate aim, not scanning.
//
// Per qualifying sample pair (consecutive alive samples in the same round,
// gap ≤ scanMaxGapSec):
//   - dwell    — yaw speed below scanSettledDegPerSec counts as settled time
//   - reversal — a yaw-direction flip where both legs exceed
//     scanReversalDegPerSec ("crosshair never settles" swiping)
//   - travel   — absolute yaw degrees, for the average scan speed
//
// Yaw is unwrapped across the ±180° seam; per-step deltas above
// scanFlickClampDeg (a flick or teleport artifact at 16 Hz) are skipped.
const (
	scanSettledDegPerSec  = 25.0
	scanReversalDegPerSec = 60.0
	scanFlickClampDeg     = 160.0
	scanCombatRadiusSec   = 2.0
	scanMaxGapSec         = 0.3
)

// scanRoundResult holds Pass 17 accumulators for one (player, round).
type scanRoundResult struct {
	oocSeconds float64
	dwellSec   float64
	reversals  int
	absYawDeg  float64
}

func computeScanVolatility(raw *model.RawMatch) map[uint64]map[int]*scanRoundResult {
	out := map[uint64]map[int]*scanRoundResult{}
	if len(raw.ViewSamples) == 0 || raw.TicksPerSecond <= 0 {
		return out
	}
	radius := int(scanCombatRadiusSec * raw.TicksPerSecond)

	// Combat-event ticks per (player, round), sorted for window lookup.
	type prKey struct {
		id uint64
		rn int
	}
	combat := map[prKey][]int{}
	add := func(id uint64, rn, tick int) {
		if id != 0 {
			k := prKey{id, rn}
			combat[k] = append(combat[k], tick)
		}
	}
	for _, k := range raw.Kills {
		add(k.KillerSteamID, k.RoundNumber, k.Tick)
		add(k.VictimSteamID, k.RoundNumber, k.Tick)
	}
	for _, d := range raw.Damages {
		add(d.AttackerSteamID, d.RoundNumber, d.Tick)
		add(d.VictimSteamID, d.RoundNumber, d.Tick)
	}
	for _, w := range raw.WeaponFires {
		add(w.ShooterID, w.RoundNumber, w.Tick)
	}
	for _, g := range raw.Grenades {
		add(g.ThrowerSteamID, g.RoundNumber, g.ThrowTick)
	}
	for k := range combat {
		sort.Ints(combat[k])
	}
	inCombat := func(id uint64, rn, tick int) bool {
		ts := combat[prKey{id, rn}]
		i := sort.SearchInts(ts, tick-radius)
		return i < len(ts) && ts[i] <= tick+radius
	}

	freezeEnd := map[int]int{}
	for _, r := range raw.Rounds {
		freezeEnd[r.Number] = r.FreezeEndTick
	}

	// Group samples per (player, round), preserving tick order.
	grouped := map[prKey][]model.RawViewSample{}
	for _, s := range raw.ViewSamples {
		k := prKey{s.SteamID, s.RoundNumber}
		grouped[k] = append(grouped[k], s)
	}

	for k, samples := range grouped {
		sort.Slice(samples, func(i, j int) bool { return samples[i].Tick < samples[j].Tick })

		res := &scanRoundResult{}
		havePrev := false
		var prevTick int
		var prevYaw float64 // unwrapped
		lastDir := 0        // sign of the last leg exceeding the reversal threshold
		prevSettled := true

		for _, s := range samples {
			if s.HP == 0 || s.Tick < freezeEnd[k.rn] {
				havePrev = false
				lastDir = 0
				continue
			}
			if !havePrev {
				prevTick, prevYaw, havePrev = s.Tick, s.YawDeg, true
				continue
			}
			dt := float64(s.Tick-prevTick) / raw.TicksPerSecond
			dyaw := s.YawDeg - prevYaw
			for dyaw > 180 {
				dyaw -= 360
			}
			for dyaw < -180 {
				dyaw += 360
			}
			tick, lastTick := s.Tick, prevTick
			prevTick, prevYaw = s.Tick, s.YawDeg
			if dt <= 0 || dt > scanMaxGapSec || math.Abs(dyaw) > scanFlickClampDeg {
				lastDir = 0
				continue
			}
			if s.EnemiesVisible || inCombat(k.id, k.rn, tick) || inCombat(k.id, k.rn, lastTick) {
				lastDir = 0
				continue
			}

			rate := dyaw / dt
			res.oocSeconds += dt
			res.absYawDeg += math.Abs(dyaw)
			settled := math.Abs(rate) < scanSettledDegPerSec
			if settled {
				res.dwellSec += dt
			}
			if math.Abs(rate) >= scanReversalDegPerSec {
				dir := 1
				if rate < 0 {
					dir = -1
				}
				if lastDir != 0 && dir != lastDir {
					res.reversals++
				}
				lastDir = dir
			} else if settled && prevSettled {
				// Two consecutive settled steps break the swipe chain — a
				// later swing in the opposite direction is a new scan, not
				// a reversal.
				lastDir = 0
			}
			prevSettled = settled
		}

		if res.oocSeconds > 0 {
			if out[k.id] == nil {
				out[k.id] = map[int]*scanRoundResult{}
			}
			out[k.id][k.rn] = res
		}
	}
	return out
}

// ---- Pass 18: shot accounting (accuracy + aimed/blind split) -------------
//
// Raw accuracy (hits ÷ shots) is not comparable across skill tiers, because
// tiers differ enormously in how many shots they deliberately aim at nobody:
// smoke spam, prefire, wallbangs, suppressing fire. Measured on the pro
// corpus ~79% of rifle shots are fired with no enemy visible, against ~59%
// for a mid-tier player — enough to invert the raw-accuracy ranking.
//
// So every shot and every hit is tagged with the engine's own answer to
// "was an enemy visible to this player at this tick", taken from the nearest
// view sample (RawViewSample.EnemiesVisible, i.e. the spotted mask).
//
// Caveats, both inherent to the spotted mask rather than to this pass:
//   - The mask has latency and is FOV-gated, so the blind bucket is somewhat
//     over-inclusive. It is the same measure on both sides of any comparison.
//   - "Blind" cannot be decomposed into smoke / prefire / wallbang: the
//     converter never populates csraw2's in-smoke flag.

type shotKey struct {
	playerID uint64
	weapon   string
}

type shotAccum struct {
	shots, shotsVisible   int
	hitsVisible           int
	headHits, headHitsVis int
}

// shotSampleToleranceTicks returns how far from an event tick a view sample
// may sit and still describe that event. Baseline sampling is 16 Hz with
// 64 Hz bursts around events — and weapon fires *are* events, so in practice
// the nearest sample is 0–1 ticks away. The +2 is slack for the baseline
// case; anything beyond that is treated as "no sample" rather than guessed.
func shotSampleToleranceTicks(ticksPerSecond float64) int {
	if ticksPerSecond <= 0 {
		return 0
	}
	return int(math.Ceil(ticksPerSecond/16)) + 2
}

// computeShotAccounting tags every weapon fire and every enemy hit with
// whether an enemy was visible, keyed by (player, weapon).
//
// The damage filters here mirror the weapon-hit accumulator earlier in
// Aggregate exactly, so HitsVisible is always a subset of Hits.
func computeShotAccounting(raw *model.RawMatch) map[shotKey]*shotAccum {
	out := map[shotKey]*shotAccum{}
	tol := shotSampleToleranceTicks(raw.TicksPerSecond)
	if len(raw.ViewSamples) == 0 || tol == 0 {
		// No visibility data: still count shots, leave the split at zero so
		// callers can tell "no enemy visible" from "not measured".
		for _, w := range raw.WeaponFires {
			if w.ShooterID == 0 {
				continue
			}
			shotAccFor(out, shotKey{w.ShooterID, w.Weapon}).shots++
		}
		return out
	}

	// Per-player view samples, tick-sorted, for nearest-sample lookup.
	byPlayer := map[uint64][]model.RawViewSample{}
	for _, s := range raw.ViewSamples {
		byPlayer[s.SteamID] = append(byPlayer[s.SteamID], s)
	}
	for id := range byPlayer {
		ss := byPlayer[id]
		sort.Slice(ss, func(i, j int) bool { return ss[i].Tick < ss[j].Tick })
		byPlayer[id] = ss
	}

	// visible reports whether an enemy was in the player's spotted mask at
	// tick, using the nearest sample within tol. The second result is false
	// when no sample is close enough.
	visible := func(id uint64, tick int) (bool, bool) {
		ss := byPlayer[id]
		if len(ss) == 0 {
			return false, false
		}
		i := sort.Search(len(ss), func(i int) bool { return ss[i].Tick >= tick })
		best, bestDist := -1, math.MaxInt
		for _, j := range []int{i - 1, i} {
			if j < 0 || j >= len(ss) {
				continue
			}
			d := ss[j].Tick - tick
			if d < 0 {
				d = -d
			}
			if d < bestDist {
				best, bestDist = j, d
			}
		}
		if best < 0 || bestDist > tol {
			return false, false
		}
		return ss[best].EnemiesVisible, true
	}

	for _, w := range raw.WeaponFires {
		if w.ShooterID == 0 {
			continue
		}
		a := shotAccFor(out, shotKey{w.ShooterID, w.Weapon})
		a.shots++
		if vis, ok := visible(w.ShooterID, w.Tick); ok && vis {
			a.shotsVisible++
		}
	}

	for _, d := range raw.Damages {
		if d.AttackerSteamID == 0 {
			continue
		}
		if d.AttackerTeam != model.TeamUnknown && d.AttackerTeam == d.VictimTeam {
			continue
		}
		a := shotAccFor(out, shotKey{d.AttackerSteamID, d.Weapon})
		head := d.HitGroup == "head"
		if head {
			a.headHits++
		}
		if vis, ok := visible(d.AttackerSteamID, d.Tick); ok && vis {
			a.hitsVisible++
			if head {
				a.headHitsVis++
			}
		}
	}
	return out
}

// shotAccFor returns the accumulator for a (player, weapon) pair, creating it
// on first use.
func shotAccFor(m map[shotKey]*shotAccum, k shotKey) *shotAccum {
	a := m[k]
	if a == nil {
		a = &shotAccum{}
		m[k] = a
	}
	return a
}
