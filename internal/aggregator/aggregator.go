package aggregator

import (
	"fmt"
	"math"
	"sort"

	"github.com/pable/go-cs-metrics/internal/model"
)

// Aggregate computes PlayerMatchStats, PlayerRoundStats, and PlayerWeaponStats from a RawMatch.
func Aggregate(raw *model.RawMatch) ([]model.PlayerMatchStats, []model.PlayerRoundStats, []model.PlayerWeaponStats, error) {
	if raw == nil {
		return nil, nil, nil, fmt.Errorf("nil RawMatch")
	}

	tradeWindowTicks := int(5.0 * raw.TicksPerSecond)

	// ---- Pass 1: annotate kills with trade flags. ----

	type annotatedKill struct {
		model.RawKill
		isTradeKill  bool // this kill trades a previous enemy kill
		isTradeDeath bool // this kill will be traded (victim traded the killer)
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
					break
				}
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
	type damageKey struct{ roundN int; attackerID, victimID uint64 }
	type flashKey struct{ roundN int; attackerID, victimID uint64 }

	// player x round → total damage dealt, utility damage, flash assists.
	type roundDamage struct {
		health  int
		utility int
	}
	damageLedger := make(map[damageKey]roundDamage)
	for _, d := range raw.Damages {
		k := damageKey{d.RoundNumber, d.AttackerSteamID, d.VictimSteamID}
		prev := damageLedger[k]
		prev.health += d.HealthDamage
		if d.IsUtility {
			prev.utility += d.HealthDamage
		}
		damageLedger[k] = prev
	}

	// Flash assists: attacker flashed victim who was killed by a teammate of attacker.
	// Strategy: for each kill with AssistedFlash=true, the assister is the flasher.
	// Track total health damage per (attacker, round).
	type playerRoundKey struct{ playerID uint64; roundN int }
	totalDmgByPlayerRound := make(map[playerRoundKey]int)
	utilDmgByPlayerRound := make(map[playerRoundKey]int)
	for _, d := range raw.Damages {
		pk := playerRoundKey{d.AttackerSteamID, d.RoundNumber}
		totalDmgByPlayerRound[pk] += d.HealthDamage
		if d.IsUtility {
			utilDmgByPlayerRound[pk] += d.HealthDamage
		}
	}

	// Weapon-level accumulators.
	type weaponKey struct {
		playerID uint64
		weapon   string
	}
	weaponKills  := make(map[weaponKey]int)
	weaponHS     := make(map[weaponKey]int)
	weaponDeaths := make(map[weaponKey]int)
	weaponAssist := make(map[weaponKey]int)
	weaponDamage := make(map[weaponKey]int)
	weaponHits   := make(map[weaponKey]int)

	for _, d := range raw.Damages {
		if d.AttackerSteamID == 0 {
			continue
		}
		wk := weaponKey{d.AttackerSteamID, d.Weapon}
		weaponDamage[wk] += d.HealthDamage
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

			// Surviving.
			if hasEndState {
				rs.Survived = endState.IsAlive
				rs.UnusedUtility = endState.GrenadeCount
			}

			// Opening kill/death.
			if opening.killerID == playerID {
				rs.IsOpeningKill = true
			}
			if opening.victimID == playerID {
				rs.IsOpeningDeath = true
			}

			// Damage.
			pk := playerRoundKey{playerID, rn}
			rs.Damage = totalDmgByPlayerRound[pk]

			// KAST: Kill, Assist, Survive, or Traded.
			rs.KASTEarned = rs.GotKill || rs.GotAssist || rs.Survived || rs.WasTraded

			allRoundStats = append(allRoundStats, rs)

			// Accumulate match-level stats.
			acc := matchAccums[playerID]
			acc.roundsPlayed++
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

	// ---- Pass 4: roll up into PlayerMatchStats. ----
	var matchStats []model.PlayerMatchStats
	for playerID, acc := range matchAccums {
		if acc.roundsPlayed == 0 {
			continue
		}
		matchStats = append(matchStats, model.PlayerMatchStats{
			DemoHash:       raw.DemoHash,
			SteamID:        playerID,
			Name:           raw.PlayerNames[playerID],
			Team:           playerDominantTeam[playerID],
			Kills:          acc.kills,
			Assists:        acc.assists,
			Deaths:         acc.deaths,
			HeadshotKills:  acc.headshotKills,
			FlashAssists:   acc.flashAssists,
			TotalDamage:    acc.totalDamage,
			UtilityDamage:  acc.utilityDamage,
			RoundsPlayed:   acc.roundsPlayed,
			OpeningKills:   acc.openingKills,
			OpeningDeaths:  acc.openingDeaths,
			TradeKills:     acc.tradeKills,
			TradeDeaths:    acc.tradeDeaths,
			KASTRounds:     acc.kastRounds,
			UnusedUtility:  acc.unusedUtility,
		})
	}

	// Sort by kills desc for stable output.
	sort.Slice(matchStats, func(i, j int) bool {
		return matchStats[i].Kills > matchStats[j].Kills
	})

	// ---- Pass 5: crosshair placement aggregation (total + pitch/yaw split). ----
	type xhairAccum struct {
		angles []float64
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

	var weaponStats []model.PlayerWeaponStats
	for wk := range allWeaponKeys {
		weaponStats = append(weaponStats, model.PlayerWeaponStats{
			DemoHash:      raw.DemoHash,
			SteamID:       wk.playerID,
			Weapon:        wk.weapon,
			Kills:         weaponKills[wk],
			HeadshotKills: weaponHS[wk],
			Assists:       weaponAssist[wk],
			Deaths:        weaponDeaths[wk],
			Damage:        weaponDamage[wk],
			Hits:          weaponHits[wk],
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
	type sightKey struct{ obsID, enemyID uint64; roundN int }
	firstSightIdx := make(map[sightKey]model.RawFirstSight)
	for _, fs := range raw.FirstSights {
		k := sightKey{fs.ObserverID, fs.EnemyID, fs.RoundNumber}
		if _, exists := firstSightIdx[k]; !exists {
			firstSightIdx[k] = fs
		}
	}

	// Build damage index: (roundN, atkID, vicID) → sorted slice of RawDamage (non-utility only).
	type duelDmgKey struct{ roundN int; atkID, vicID uint64 }
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
	type wfKey struct{ shooterID uint64; roundN int }
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
		winMs          []float64
		lossMs         []float64
		hitsToKill     []float64
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

			// Count hits from killer→victim in [sightTick, killTick].
			dmgKey := duelDmgKey{rn, killerID, victimID}
			damages := duelDmgIdx[dmgKey]
			hits := 0
			firstHitHS := false
			firstHitCounted := false
			for _, d := range damages {
				if d.Tick < sightTick || d.Tick > killTick {
					continue
				}
				if !firstHitCounted {
					firstHitHS = d.HitGroup == "head"
					firstHitCounted = true
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

			// Pre-shot correction: angle between observer's view at first-sight and at first shot.
			wfList := wfIdx[wfKey{killerID, rn}]
			for _, wf := range wfList {
				if wf.Tick < sightTick || wf.Tick > killTick {
					continue
				}
				corrDeg := angularDeltaDeg(fs.ObserverPitchDeg, fs.ObserverYawDeg, wf.PitchDeg, wf.YawDeg)
				acc.correctionDegs = append(acc.correctionDegs, corrDeg)
				break
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

	// ---- Pass 7: AWP Death Classifier ----

	// Build flash index: victimID → []tick for flashes with FlashDuration > 0 per round.
	type flashVictimKey struct{ victimID uint64; roundN int }
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

	return matchStats, allRoundStats, weaponStats, nil
}

// median returns the median of a pre-sorted (ascending) slice of float64.
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
// given as (pitch, yaw) pairs in degrees. Reconstructs forward vectors and uses acos(dot).
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
