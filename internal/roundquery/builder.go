package roundquery

import (
	"strconv"
	"strings"

	"github.com/pable/go-cs-metrics/internal/csraw2"
)

// BuildRecords converts a csraw2.Match into a slice of RoundRecords, one
// per round. If collectData is true, each record's ViewerData field is
// populated with the per-round data needed by the 2D HTML viewer.
func BuildRecords(m *csraw2.Match, matchFile, date string, collectData bool) []RoundRecord {
	if m == nil {
		return nil
	}

	// Per-round, per-slot team resolution. Same rule as csraw2bridge: last
	// sample in the round wins; fall back to header.players starting team if
	// no samples were emitted for that slot.
	roundTeams := buildRoundTeams(m)
	startingTeams := buildStartingTeams(m.Header.Players)

	teamAt := func(round int, slot uint8) uint8 {
		if rt, ok := roundTeams[round]; ok {
			if t, ok := rt[slot]; ok {
				return t
			}
		}
		if t, ok := startingTeams[slot]; ok {
			return t
		}
		return csraw2.TeamUnknown
	}

	// Index events by round number for O(1) lookup per round.
	kills := indexByRound(m.Kills, func(k csraw2.Kill) int { return int(k.Round) })
	damages := indexByRound(m.Damages, func(d csraw2.Damage) int { return int(d.Round) })
	throws := indexByRound(m.GrenadeThrows, func(g csraw2.GrenadeThrow) int { return int(g.Round) })
	detos := indexByRound(m.GrenadeDetonations, func(d csraw2.GrenadeDetonate) int { return int(d.Round) })
	bombs := indexByRound(m.BombActions, func(b csraw2.BombAction) int { return int(b.Round) })

	// freeze-end samples per (round, slot) for equip values.
	freezeEnd := indexFreezeEndSamples(m)

	// Last sample at-or-before each tick for alive-count queries. Built once
	// per round on demand inside the builder.
	samplesByRound := indexSamplesByRound(m.PlayerSamples)

	// Viewer-only data sources.
	var firesByRound map[int][]csraw2.WeaponFire
	var projByRound map[int][]csraw2.ProjectileSample
	if collectData {
		firesByRound = indexByRound(m.WeaponFires, func(f csraw2.WeaponFire) int { return int(f.Round) })
		projByRound = indexByRound(m.ProjectileSamples, func(s csraw2.ProjectileSample) int { return int(s.Round) })
	}

	weaponNameForID := func(id uint8) string {
		return m.Header.WeaponTable[strconv.FormatUint(uint64(id), 10)]
	}

	records := make([]RoundRecord, 0, len(m.Header.Rounds))
	for _, r := range m.Header.Rounds {
		rec := buildRecord(
			m, r, teamAt, freezeEnd, samplesByRound[r.N],
			kills[r.N], damages[r.N], throws[r.N], bombs[r.N],
			weaponNameForID, matchFile, date,
		)
		if collectData {
			rec.ViewerData = buildViewerData(
				m, r, teamAt,
				samplesByRound[r.N],
				kills[r.N], throws[r.N], detos[r.N], bombs[r.N],
				firesByRound[r.N], projByRound[r.N],
				weaponNameForID,
			)
		}
		records = append(records, rec)
	}
	return records
}

func buildRecord(
	m *csraw2.Match,
	r csraw2.Round,
	teamAt func(round int, slot uint8) uint8,
	freezeEnd map[roundSlot]csraw2.PlayerSample,
	samples []csraw2.PlayerSample,
	kills []csraw2.Kill,
	damages []csraw2.Damage,
	throws []csraw2.GrenadeThrow,
	bombs []csraw2.BombAction,
	weaponNameForID func(uint8) string,
	matchFile, date string,
) RoundRecord {
	// Equipment values per side from the freeze-end snapshot.
	var equipT, equipCT int
	for slot := uint8(0); slot < 10; slot++ {
		fe, ok := freezeEnd[roundSlot{round: r.N, slot: slot}]
		if !ok {
			continue
		}
		switch teamAt(r.N, slot) {
		case csraw2.TeamT:
			equipT += int(fe.EquipValue)
		case csraw2.TeamCT:
			equipCT += int(fe.EquipValue)
		}
	}

	smokesT, smokesCT, flashesT, flashesCT, molotovsT, molotovsCT, hesT, hesCT :=
		countGrenades(throws, teamAt, r.N)

	heDmgT, heDmgCT := sumHEDamage(damages, teamAt, weaponNameForID)
	flashKillsT, flashKillsCT := countFlashKills(kills, teamAt)

	aliveStartT, aliveStartCT := countAliveAtTick(samples, r.FreezeEndTick, teamAt, r.N)
	planted, defused, exploded, plantTick := bombEvents(bombs)
	var alivePlantT, alivePlantCT int
	if planted {
		alivePlantT, alivePlantCT = countAliveAtTick(samples, plantTick, teamAt, r.N)
	}

	return RoundRecord{
		MatchFile:    matchFile,
		Date:         date,
		Map:          m.Header.Map,
		RoundNum:     r.N,
		Winner:       r.Winner,
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
		EntrySide:    entrySideFromKills(kills, teamAt),
	}
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

// countGrenades tallies utility throws by side.
func countGrenades(throws []csraw2.GrenadeThrow, teamAt func(int, uint8) uint8, round int) (
	smokesT, smokesCT, flashesT, flashesCT, molotovsT, molotovsCT, hesT, hesCT int,
) {
	for _, g := range throws {
		side := teamAt(round, g.ThrowerSlot)
		switch g.GrenadeType {
		case csraw2.GrenadeSmoke:
			if side == csraw2.TeamT {
				smokesT++
			} else if side == csraw2.TeamCT {
				smokesCT++
			}
		case csraw2.GrenadeFlash:
			if side == csraw2.TeamT {
				flashesT++
			} else if side == csraw2.TeamCT {
				flashesCT++
			}
		case csraw2.GrenadeMolotov:
			if side == csraw2.TeamT {
				molotovsT++
			} else if side == csraw2.TeamCT {
				molotovsCT++
			}
		case csraw2.GrenadeHE:
			if side == csraw2.TeamT {
				hesT++
			} else if side == csraw2.TeamCT {
				hesCT++
			}
		}
	}
	return
}

// sumHEDamage returns total HP damage dealt by HE grenades per side.
func sumHEDamage(damages []csraw2.Damage, teamAt func(int, uint8) uint8, weaponNameForID func(uint8) string) (t, ct int) {
	for _, d := range damages {
		if d.AttackerSlot < 0 {
			continue
		}
		if !isHEGrenadeName(weaponNameForID(d.WeaponID)) {
			continue
		}
		atkTeam := teamAt(int(d.Round), uint8(d.AttackerSlot))
		// Exclude team damage (HE on own teammates is not side output).
		if atkTeam != csraw2.TeamUnknown && atkTeam == teamAt(int(d.Round), d.VictimSlot) {
			continue
		}
		// Cap at health actually lost (raw HealthDamage overshoots at low HP).
		dmg := int(d.PreDamageHP) - int(d.PostDamageHP)
		if dmg < 0 {
			dmg = 0
		}
		if dmg > int(d.HealthDamage) {
			dmg = int(d.HealthDamage)
		}
		switch atkTeam {
		case csraw2.TeamT:
			t += dmg
		case csraw2.TeamCT:
			ct += dmg
		}
	}
	return
}

// isHEGrenadeName reports whether the weapon name (as recorded in
// header.weapon_table) refers to a HE grenade. The string is whatever
// demoinfocs EquipmentType.String() returns at parse time, so accept the
// common variants.
func isHEGrenadeName(name string) bool {
	switch strings.ToLower(name) {
	case "he grenade", "he_grenade", "hegrenade":
		return true
	}
	return false
}

// countFlashKills returns kills where the killer's victim was flashed.
func countFlashKills(kills []csraw2.Kill, teamAt func(int, uint8) uint8) (t, ct int) {
	for _, k := range kills {
		if !k.FlashAssist {
			continue
		}
		if k.KillerSlot < 0 {
			continue
		}
		switch teamAt(int(k.Round), uint8(k.KillerSlot)) {
		case csraw2.TeamT:
			t++
		case csraw2.TeamCT:
			ct++
		}
	}
	return
}

// bombEvents returns plant/defuse/explode flags plus the plant tick.
func bombEvents(actions []csraw2.BombAction) (planted, defused, exploded bool, plantTick int) {
	for _, a := range actions {
		switch a.Action {
		case csraw2.BombActionPlantComplete:
			planted = true
			plantTick = int(a.Tick)
		case csraw2.BombActionDefuseComplete:
			defused = true
		case csraw2.BombActionExplode:
			exploded = true
		}
	}
	return
}

// countAliveAtTick returns alive player counts per side using the most
// recent sample for each slot at-or-before targetTick. Round-scoped: only
// samples whose Round matches round are considered.
func countAliveAtTick(samples []csraw2.PlayerSample, targetTick int, teamAt func(int, uint8) uint8, round int) (aliveT, aliveCT int) {
	target := int32(targetTick)
	type latest struct {
		hp   uint8
		tick int32
		ok   bool
	}
	var perSlot [10]latest
	for _, s := range samples {
		if s.Tick > target {
			continue
		}
		if s.PlayerSlot >= uint8(len(perSlot)) {
			continue
		}
		cur := &perSlot[s.PlayerSlot]
		if !cur.ok || s.Tick > cur.tick {
			cur.hp = s.HP
			cur.tick = s.Tick
			cur.ok = true
		}
	}
	for slot, l := range perSlot {
		if !l.ok || l.hp == 0 {
			continue
		}
		switch teamAt(round, uint8(slot)) {
		case csraw2.TeamT:
			aliveT++
		case csraw2.TeamCT:
			aliveCT++
		}
	}
	return
}

// entrySideFromKills returns the side of whoever scored the first kill in
// the round, or "" if there were none.
func entrySideFromKills(kills []csraw2.Kill, teamAt func(int, uint8) uint8) string {
	if len(kills) == 0 {
		return ""
	}
	first := kills[0]
	for _, k := range kills[1:] {
		if k.Tick < first.Tick {
			first = k
		}
	}
	if first.KillerSlot < 0 {
		return ""
	}
	return teamLabel(teamAt(int(first.Round), uint8(first.KillerSlot)))
}

// ── Indexers ────────────────────────────────────────────────────────────

type roundSlot struct {
	round int
	slot  uint8
}

func indexByRound[T any](rows []T, roundOf func(T) int) map[int][]T {
	out := map[int][]T{}
	for _, row := range rows {
		r := roundOf(row)
		out[r] = append(out[r], row)
	}
	return out
}

func indexSamplesByRound(samples []csraw2.PlayerSample) map[int][]csraw2.PlayerSample {
	out := map[int][]csraw2.PlayerSample{}
	for _, s := range samples {
		out[int(s.Round)] = append(out[int(s.Round)], s)
	}
	return out
}

// indexFreezeEndSamples returns the first PlayerSample at-or-after each
// round's freeze_end_tick per (round, slot). Mirrors the csraw2bridge helper
// so equip values agree with the aggregator.
func indexFreezeEndSamples(m *csraw2.Match) map[roundSlot]csraw2.PlayerSample {
	type bestEntry struct {
		s    csraw2.PlayerSample
		diff int32
	}
	freezeEnds := map[int]int32{}
	for _, r := range m.Header.Rounds {
		freezeEnds[r.N] = int32(r.FreezeEndTick)
	}
	best := map[roundSlot]bestEntry{}
	for _, s := range m.PlayerSamples {
		fe, ok := freezeEnds[int(s.Round)]
		if !ok || s.Tick < fe {
			continue
		}
		diff := s.Tick - fe
		k := roundSlot{round: int(s.Round), slot: s.PlayerSlot}
		if cur, ok := best[k]; !ok || diff < cur.diff {
			best[k] = bestEntry{s: s, diff: diff}
		}
	}
	out := map[roundSlot]csraw2.PlayerSample{}
	for k, v := range best {
		out[k] = v.s
	}
	return out
}

// buildRoundTeams produces (round → slot → team) using the latest sample
// per (round, slot).
func buildRoundTeams(m *csraw2.Match) map[int]map[uint8]uint8 {
	out := map[int]map[uint8]uint8{}
	for _, s := range m.PlayerSamples {
		round := int(s.Round)
		if _, ok := out[round]; !ok {
			out[round] = map[uint8]uint8{}
		}
		out[round][s.PlayerSlot] = s.Team
	}
	return out
}

// buildStartingTeams resolves header.players[].starting_team into csraw2 enum
// values for fallback team lookups.
func buildStartingTeams(players []csraw2.Player) map[uint8]uint8 {
	out := map[uint8]uint8{}
	for _, p := range players {
		switch p.StartingTeam {
		case "T":
			out[p.Slot] = csraw2.TeamT
		case "CT":
			out[p.Slot] = csraw2.TeamCT
		default:
			out[p.Slot] = csraw2.TeamUnknown
		}
	}
	return out
}
