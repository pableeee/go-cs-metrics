package aggregator

import (
	"fmt"
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

	return matchStats, allRoundStats, weaponStats, nil
}
