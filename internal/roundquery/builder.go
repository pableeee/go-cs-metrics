package roundquery

import (
	"github.com/pable/go-cs-metrics/internal/model"
)

// Grenade type constants (from model.UnifiedGrenade.Type).
const (
	grenadeFlash   = 1
	grenadeHE      = 2
	grenadeMolotov = 3
	grenadeCTSmoke = 4
	grenadeTSmoke  = 5
)

// BombAction constants (from model.UnifiedBombAction.Action).
const (
	bombPlanted  = 1
	bombDefused  = 3
	bombExploded = 4
)

// BuildRecords converts a UnifiedMatch into a slice of RoundRecords,
// one per round.
func BuildRecords(m *model.UnifiedMatch, matchFile, date string) []RoundRecord {
	// Build player index → SteamID slice.
	idxToSteam := make([]uint64, len(m.Players))
	for i, p := range m.Players {
		idxToSteam[i] = p.SteamID
	}

	// Index events by round number for O(1) lookup.
	grenadesByRound := indexGrenades(m.Grenades)
	damagesByRound := indexDamages(m.Damages)
	killsByRound := indexKills(m.Kills)
	bombsByRound := indexBombs(m.Bombs)
	framesByRound := indexFrames(m.Frames)

	records := make([]RoundRecord, 0, len(m.Rounds))
	for _, round := range m.Rounds {
		rec := buildRecord(m, round, idxToSteam,
			grenadesByRound[round.Number],
			damagesByRound[round.Number],
			killsByRound[round.Number],
			bombsByRound[round.Number],
			framesByRound[round.Number],
			matchFile, date)
		records = append(records, rec)
	}
	return records
}

func buildRecord(
	m *model.UnifiedMatch,
	round model.UnifiedRound,
	idxToSteam []uint64,
	grenades []model.UnifiedGrenade,
	damages []model.UnifiedDamage,
	kills []model.UnifiedKill,
	bombs []model.UnifiedBombAction,
	frames []model.UnifiedFrame,
	matchFile, date string,
) RoundRecord {
	// Build player index → team map from end-of-round state.
	// Teams don't switch mid-round so end state is authoritative.
	idxTeam := make(map[int]model.Team, len(round.PlayerEndState))
	for i, steamID := range idxToSteam {
		if state, ok := round.PlayerEndState[steamID]; ok {
			idxTeam[i] = state.Team
		}
	}

	// Equipment values and buy types per side.
	equipT, equipCT := sumEquip(round.PlayerEquipValues, round.PlayerEndState)

	// Util counts per side.
	smokesT, smokesCT, flashesT, flashesCT, molotovsT, molotovsCT, hesT, hesCT :=
		countGrenades(grenades, idxTeam)

	// HE damage per side.
	heDmgT, heDmgCT := sumHEDamage(damages)

	// Flash-assisted kills per side.
	flashKillsT, flashKillsCT := countFlashKills(kills)

	// Alive counts at round start and at plant.
	aliveStartT, aliveStartCT := countAliveAtTick(frames, round.FreezeEndTick)
	var alivePlantT, alivePlantCT int
	planted, defused, exploded, plantTick := bombEvents(bombs)
	if planted {
		alivePlantT, alivePlantCT = countAliveAtTick(frames, plantTick)
	}

	// Entry side: killer team on the first kill of the round.
	entrySide := entrySide(kills)

	// Winner.
	winner := ""
	switch round.WinnerTeam {
	case model.TeamT:
		winner = "T"
	case model.TeamCT:
		winner = "CT"
	}

	return RoundRecord{
		MatchFile:    matchFile,
		Date:         date,
		Map:          m.MapName,
		RoundNum:     round.Number,
		Winner:       winner,
		TypeT:        classifyBuy(equipT),
		TypeCT:       classifyBuy(equipCT),
		EquipT:       equipT,
		EquipCT:      equipCT,
		SmokesT:      smokesT,
		SmokesCT:     smokesCT,
		FlashesT:     flashesT,
		FlashesCT:    flashesCT,
		MolotovsT:    molotovsT,
		MolotovsCT:   molotovsCT,
		HEsT:         hesT,
		HEsCT:        hesCT,
		HEDamageT:    heDmgT,
		HEDamageCT:   heDmgCT,
		FlashKillsT:  flashKillsT,
		FlashKillsCT: flashKillsCT,
		AliveStartT:  aliveStartT,
		AliveStartCT: aliveStartCT,
		AlivePlantT:  alivePlantT,
		AlivePlantCT: alivePlantCT,
		Planted:      planted,
		Defused:      defused,
		Exploded:     exploded,
		EntrySide:    entrySide,
	}
}

// sumEquip returns total equipment value in USD for each side.
func sumEquip(equipValues map[uint64]int, endState map[uint64]model.PlayerRoundEndState) (t, ct int) {
	for steamID, val := range equipValues {
		state, ok := endState[steamID]
		if !ok {
			continue
		}
		switch state.Team {
		case model.TeamT:
			t += val
		case model.TeamCT:
			ct += val
		}
	}
	return
}

// classifyBuy returns a buy type label based on total team equipment value.
// Thresholds are per-team totals (5 players):
//
//	≤ 4 000  → "pistol"   (~$800/player)
//	≤ 9 000  → "eco"      (~$1 800/player)
//	≤ 20 000 → "force"    (~$4 000/player)
//	else     → "full"
func classifyBuy(total int) string {
	switch {
	case total <= 4000:
		return "pistol"
	case total <= 9000:
		return "eco"
	case total <= 20000:
		return "force"
	default:
		return "full"
	}
}

// countGrenades tallies utility counts by side.
// Grenade types: 0=smoke 1=flash 2=HE 3=molotov 4=CT-smoke 5=T-smoke
func countGrenades(grenades []model.UnifiedGrenade, idxTeam map[int]model.Team) (
	smokesT, smokesCT, flashesT, flashesCT, molotovsT, molotovsCT, hesT, hesCT int,
) {
	for _, g := range grenades {
		side := sideFromGrenadeType(g.Type, g.ThrowerIdx, idxTeam)
		switch g.Type {
		case grenadeTSmoke:
			smokesT++
		case grenadeCTSmoke:
			smokesCT++
		case grenadeFlash:
			if side == model.TeamT {
				flashesT++
			} else if side == model.TeamCT {
				flashesCT++
			}
		case grenadeMolotov:
			if side == model.TeamT {
				molotovsT++
			} else if side == model.TeamCT {
				molotovsCT++
			}
		case grenadeHE:
			if side == model.TeamT {
				hesT++
			} else if side == model.TeamCT {
				hesCT++
			}
		}
	}
	return
}

// sideFromGrenadeType returns the throwing team. For smoke grenades (type 4/5)
// the side is encoded in the type; for all others use the thrower's team.
func sideFromGrenadeType(gType, throwerIdx int, idxTeam map[int]model.Team) model.Team {
	switch gType {
	case grenadeTSmoke:
		return model.TeamT
	case grenadeCTSmoke:
		return model.TeamCT
	default:
		if throwerIdx >= 0 {
			return idxTeam[throwerIdx]
		}
		return model.TeamUnknown
	}
}

// sumHEDamage returns total HP damage dealt by HE grenades per side.
func sumHEDamage(damages []model.UnifiedDamage) (t, ct int) {
	for _, d := range damages {
		if d.Weapon != "he_grenade" {
			continue
		}
		switch d.AttackerTeam {
		case model.TeamT:
			t += d.HealthDamage
		case model.TeamCT:
			ct += d.HealthDamage
		}
	}
	return
}

// countFlashKills returns kills where the victim was flashed, per killer side.
func countFlashKills(kills []model.UnifiedKill) (t, ct int) {
	for _, k := range kills {
		if !k.AssistedFlash {
			continue
		}
		switch k.KillerTeam {
		case model.TeamT:
			t++
		case model.TeamCT:
			ct++
		}
	}
	return
}

// bombEvents returns plant/defuse/explode flags, the plant tick, and site.
func bombEvents(bombs []model.UnifiedBombAction) (planted, defused, exploded bool, plantTick int) {
	for _, b := range bombs {
		switch b.Action {
		case bombPlanted:
			planted = true
			plantTick = b.Tick
		case bombDefused:
			defused = true
		case bombExploded:
			exploded = true
		}
	}
	return
}

// countAliveAtTick returns alive player counts per side at the frame at or
// just before targetTick. Frame player flags: 0=CT+alive 1=CT+dead 2=T+alive 3=T+dead.
func countAliveAtTick(frames []model.UnifiedFrame, targetTick int) (aliveT, aliveCT int) {
	best := -1
	for i, f := range frames {
		if f.Tick <= targetTick {
			best = i
		} else {
			break
		}
	}
	if best < 0 {
		return
	}
	for _, s := range frames[best].States {
		flags := s.Flags & 3
		switch flags {
		case 2: // T + alive
			aliveT++
		case 0: // CT + alive
			aliveCT++
		}
	}
	return
}

// entrySide returns "T" or "CT" based on who got the first kill in the round,
// or "" if there were no kills.
func entrySide(kills []model.UnifiedKill) string {
	if len(kills) == 0 {
		return ""
	}
	first := kills[0]
	for _, k := range kills[1:] {
		if k.Tick < first.Tick {
			first = k
		}
	}
	switch first.KillerTeam {
	case model.TeamT:
		return "T"
	case model.TeamCT:
		return "CT"
	}
	return ""
}

// --- indexers ---

func indexGrenades(grenades []model.UnifiedGrenade) map[int][]model.UnifiedGrenade {
	m := make(map[int][]model.UnifiedGrenade)
	for _, g := range grenades {
		m[g.Round] = append(m[g.Round], g)
	}
	return m
}

func indexDamages(damages []model.UnifiedDamage) map[int][]model.UnifiedDamage {
	m := make(map[int][]model.UnifiedDamage)
	for _, d := range damages {
		m[d.Round] = append(m[d.Round], d)
	}
	return m
}

func indexKills(kills []model.UnifiedKill) map[int][]model.UnifiedKill {
	m := make(map[int][]model.UnifiedKill)
	for _, k := range kills {
		m[k.Round] = append(m[k.Round], k)
	}
	return m
}

func indexBombs(bombs []model.UnifiedBombAction) map[int][]model.UnifiedBombAction {
	m := make(map[int][]model.UnifiedBombAction)
	for _, b := range bombs {
		m[b.Round] = append(m[b.Round], b)
	}
	return m
}

func indexFrames(frames []model.UnifiedFrame) map[int][]model.UnifiedFrame {
	m := make(map[int][]model.UnifiedFrame)
	for _, f := range frames {
		m[f.Round] = append(m[f.Round], f)
	}
	return m
}
