// Package csraw2bridge converts a csraw2.Match (the v2 intermediate
// format) into a model.RawMatch suitable for the existing aggregator.
//
// The bridge exists so v2 can ship without re-writing the 11-pass
// aggregator. Once v2 is the only format on disk and the aggregator is
// rewritten to consume csraw2 directly, this package can be deleted.
package csraw2bridge

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pable/go-cs-metrics/internal/csraw2"
	"github.com/pable/go-cs-metrics/internal/model"
)

// ToRawMatch converts m into a model.RawMatch. The returned value is
// owned by the caller; m is not retained.
func ToRawMatch(m *csraw2.Match) (*model.RawMatch, error) {
	if m == nil {
		return nil, fmt.Errorf("csraw2bridge: nil match")
	}
	if m.Header.CSRawVersion != csraw2.Version {
		return nil, fmt.Errorf("csraw2bridge: unsupported csraw_version %d", m.Header.CSRawVersion)
	}

	raw := &model.RawMatch{
		DemoHash:       stripPrefix(m.Header.DemoHash, "sha256:"),
		MapName:        m.Header.Map,
		MatchDate:      m.Header.MatchDate,
		MatchType:      m.Header.MatchType,
		Tier:           m.Header.Tier,
		Tickrate:       m.Header.Tickrate,
		TicksPerSecond: m.Header.Tickrate,
		PlayerNames:    map[uint64]string{},
		PlayerTeams:    map[uint64]model.Team{},
	}

	// Resolve roster: slot → steamID, slot → name, slot → starting team.
	slotIDs, err := buildSlotIDs(m.Header.Players)
	if err != nil {
		return nil, err
	}
	for _, p := range m.Header.Players {
		id, _ := strconv.ParseUint(p.SteamID, 10, 64)
		if id != 0 {
			raw.PlayerNames[id] = p.Name
		}
	}

	// Build a per-round team map: round_number → slot → model.Team. We
	// derive this from PlayerSamples — every alive player emits a sample
	// each round at a known team, so we can read the most recent value per
	// (round, slot). For rounds with no samples we fall back to the
	// player's StartingTeam.
	roundTeams := buildRoundTeams(m)
	startingTeams := buildStartingTeams(m.Header.Players)

	// Helper: resolve the team of slot s in round r, falling back to
	// starting team if no samples. Returns model.TeamUnknown if the slot
	// is unknown.
	teamAt := func(round int16, slot uint8) model.Team {
		if round > 0 {
			if rt, ok := roundTeams[int(round)]; ok {
				if t, ok := rt[slot]; ok {
					return t
				}
			}
		}
		if t, ok := startingTeams[slot]; ok {
			return t
		}
		return model.TeamUnknown
	}

	// Helper: slot → steamID (0 if slot is invalid / unknown).
	idOf := func(slot uint8) uint64 { return slotIDs[slot] }

	// PlayerTeams (global): use the team from the LAST round the player
	// appeared in. Mirrors v1's UnifiedMatch.ToRawMatch behaviour.
	for slot, id := range slotIDs {
		if id == 0 {
			continue
		}
		// Walk rounds in reverse to find the latest team.
		for i := len(m.Header.Rounds) - 1; i >= 0; i-- {
			r := m.Header.Rounds[i]
			if t := teamAt(int16(r.N), slot); t != model.TeamUnknown {
				raw.PlayerTeams[id] = t
				break
			}
		}
		if _, ok := raw.PlayerTeams[id]; !ok {
			raw.PlayerTeams[id] = startingTeams[slot]
		}
	}

	weaponName := func(id uint8) string {
		return m.Header.WeaponTable[strconv.FormatUint(uint64(id), 10)]
	}

	// ── Rounds ─────────────────────────────────────────────────────────
	raw.Rounds = make([]model.RawRound, len(m.Header.Rounds))
	// Index final samples per (round, slot) so we can build PlayerEndState
	// without scanning all samples per round.
	lastSampleInRound := indexLastSamples(m.PlayerSamples)
	freezeEndSamples := indexFreezeEndSamples(m)

	for i, r := range m.Header.Rounds {
		endState := map[uint64]model.PlayerRoundEndState{}
		equipVals := map[uint64]int{}

		// PlayerEndState gate: iterate the engine-truth "playing at round
		// end" list (schema 2.2.0). A slot present in samples but absent
		// from this list disconnected mid-round and must NOT count toward
		// RoundsPlayed — v1's parser snapshots Participants().Playing() at
		// RoundEnd and would not include them.
		for _, slot := range r.PlayersAtEnd {
			id := idOf(slot)
			if id == 0 {
				continue
			}
			// Pull IsAlive/Team from the last sample if we have one;
			// otherwise the slot was Playing at end but never sampled in
			// this round (e.g. died before the first baseline tick) — fall
			// back to dead + best-effort team from the round teams map.
			if last, ok := lastSampleInRound[roundSlot{round: r.N, slot: slot}]; ok {
				endState[id] = model.PlayerRoundEndState{
					SteamID64: id,
					IsAlive:   last.HP > 0,
					Team:      teamFromSample(last.Team),
					// GrenadeCount left at 0 — not tracked by csraw2 yet.
				}
			} else {
				endState[id] = model.PlayerRoundEndState{
					SteamID64: id,
					IsAlive:   false,
					Team:      teamAt(int16(r.N), slot),
				}
			}
		}

		// PlayerEquipValues — separate concern from PlayerEndState; keep
		// scanning all 16 slots since equip values are read at freeze-end
		// regardless of whether the player finished the round.
		for s := range 16 {
			slot := uint8(s)
			id := idOf(slot)
			if id == 0 {
				continue
			}
			if fe, ok := freezeEndSamples[roundSlot{round: r.N, slot: slot}]; ok {
				equipVals[id] = int(fe.EquipValue)
			}
		}
		raw.Rounds[i] = model.RawRound{
			Number:            r.N,
			StartTick:         r.StartTick,
			FreezeEndTick:     r.FreezeEndTick,
			EndTick:           r.EndTick,
			WinnerTeam:        teamFromLabel(r.Winner),
			PlayerEndState:    endState,
			PlayerEquipValues: equipVals,
			BombPlantTick:     r.BombPlantTick,
		}
	}

	// ── Kills ──────────────────────────────────────────────────────────
	// Mirror v1's filter: skip world-deaths / suicides (killer_slot < 0).
	// v2 captures them losslessly in kills.parquet so a future metric can
	// study suicides; the existing aggregator wasn't designed to count
	// them, so we drop them here.
	raw.Kills = make([]model.RawKill, 0, len(m.Kills))
	for _, k := range m.Kills {
		if k.KillerSlot < 0 {
			continue
		}
		var assistID uint64
		if k.AssisterSlot >= 0 {
			assistID = idOf(uint8(k.AssisterSlot))
		}
		raw.Kills = append(raw.Kills, model.RawKill{
			Tick:                  int(k.Tick),
			RoundNumber:           int(k.Round),
			KillerSteamID:         idOf(uint8(k.KillerSlot)),
			VictimSteamID:         idOf(k.VictimSlot),
			AssisterSteamID:       assistID,
			KillerTeam:            teamAt(k.Round, uint8(k.KillerSlot)),
			VictimTeam:            teamAt(k.Round, k.VictimSlot),
			Weapon:                weaponName(k.WeaponID),
			IsHeadshot:            k.IsHeadshot,
			AssistedFlash:         k.FlashAssist,
			NearbyVictimTeammates: int(k.NearbyVictimTeammates),
			KillerPos:             vec3From(k.KillerPosX, k.KillerPosY, k.KillerPosZ),
			VictimPos:             vec3From(k.VictimPosX, k.VictimPosY, k.VictimPosZ),
			VictimYawDeg:          yaw0to360(k.VictimYawDeg),
		})
	}

	// ── Damages ────────────────────────────────────────────────────────
	// Mirror v1's parser-level filtering: skip world damage (attacker_slot < 0)
	// and self-damage (attacker == victim). v2 captures both losslessly in
	// damages.parquet; the aggregator's metrics were tuned against the
	// filtered set, so we drop them here for parity.
	raw.Damages = make([]model.RawDamage, 0, len(m.Damages))
	for _, d := range m.Damages {
		if d.AttackerSlot < 0 {
			continue
		}
		if uint8(d.AttackerSlot) == d.VictimSlot {
			continue
		}
		// Capped damage = health actually lost (HLTV-style). The parquet
		// rows carry pre/post HP snapshots, so derive it here rather than
		// trusting raw HealthDamage, which is NOT capped at remaining HP.
		taken := int(d.PreDamageHP) - int(d.PostDamageHP)
		if taken < 0 {
			taken = 0
		}
		if taken > int(d.HealthDamage) {
			taken = int(d.HealthDamage)
		}
		raw.Damages = append(raw.Damages, model.RawDamage{
			Tick:              int(d.Tick),
			RoundNumber:       int(d.Round),
			AttackerSteamID:   idOf(uint8(d.AttackerSlot)),
			VictimSteamID:     idOf(d.VictimSlot),
			AttackerTeam:      teamAt(d.Round, uint8(d.AttackerSlot)),
			VictimTeam:        teamAt(d.Round, d.VictimSlot),
			HealthDamage:      int(d.HealthDamage),
			HealthDamageTaken: taken,
			Weapon:          weaponName(d.WeaponID),
			IsUtility:       d.IsUtility,
			HitGroup:        hitGroupName(d.HitGroup),
			VictimPos:       vec3FromF32(d.VictimPosX, d.VictimPosY, d.VictimPosZ),
		})
	}

	// ── Weapon fires ───────────────────────────────────────────────────
	// Mirror v1's filter: skip utility (HE/molotov/incendiary/flash/smoke/
	// decoy) and knife "fires", which the aggregator's counter-strafe and
	// duel logic isn't designed to count.
	raw.WeaponFires = make([]model.RawWeaponFire, 0, len(m.WeaponFires))
	for _, w := range m.WeaponFires {
		name := weaponName(w.WeaponID)
		if isUtilityOrKnifeName(name) {
			continue
		}
		hSpeed := math.Hypot(float64(w.VelX), float64(w.VelY))
		raw.WeaponFires = append(raw.WeaponFires, model.RawWeaponFire{
			Tick:            int(w.Tick),
			RoundNumber:     int(w.Round),
			ShooterID:       idOf(w.ShooterSlot),
			Weapon:          name,
			PitchDeg:        float64(w.PitchDeg),
			YawDeg:          yaw0to360F32(w.YawDeg),
			AttackerPos:     vec3FromF32(w.PosX, w.PosY, w.PosZ),
			HorizontalSpeed: hSpeed,
		})
	}

	// ── Flashes ────────────────────────────────────────────────────────
	raw.Flashes = make([]model.RawFlash, len(m.Flashes))
	for i, f := range m.Flashes {
		raw.Flashes[i] = model.RawFlash{
			Tick:            int(f.Tick),
			RoundNumber:     int(f.Round),
			AttackerSteamID: idOf(f.AttackerSlot),
			VictimSteamID:   idOf(f.VictimSlot),
			AttackerTeam:    teamAt(f.Round, f.AttackerSlot),
			VictimTeam:      teamAt(f.Round, f.VictimSlot),
			FlashDuration:   time.Duration(f.FlashDurationMS) * time.Millisecond,
			VictimPos:       vec3From(f.VictimPosX, f.VictimPosY, f.VictimPosZ),
			VictimYawDeg:    yaw0to360(f.VictimYawDeg),
			VictimPitchDeg:  float64(f.VictimPitchDeg) / 100,
		}
	}

	// ── First sights ───────────────────────────────────────────────────
	raw.FirstSights = make([]model.RawFirstSight, len(m.FirstSights))
	for i, fs := range m.FirstSights {
		raw.FirstSights[i] = model.RawFirstSight{
			Tick:             int(fs.Tick),
			RoundNumber:      int(fs.Round),
			ObserverID:       idOf(fs.ObserverSlot),
			EnemyID:          idOf(fs.EnemySlot),
			AngleDeg:         float64(fs.AngleDeg) / 100,
			PitchDeg:         float64(fs.PitchDeg) / 100,
			YawDeg:           float64(fs.YawDeg) / 100,
			ObserverPitchDeg: float64(fs.ObserverPitchDeg),
			ObserverYawDeg:   yaw0to360F32(fs.ObserverYawDeg),
		}
	}

	// ── Grenades (throw-to-land summary) ───────────────────────────────
	// Match GrenadeThrow → GrenadeDetonate by ProjectileID. Throws without
	// a matching detonation (e.g. a smoke that expires after the destroy
	// event) get EndTick = ThrowTick to keep the row well-formed.
	detoByPID := map[uint32]int32{}
	for _, d := range m.GrenadeDetonations {
		detoByPID[d.ProjectileID] = d.Tick
	}
	raw.Grenades = make([]model.RawGrenadeEvent, len(m.GrenadeThrows))
	for i, g := range m.GrenadeThrows {
		end := g.Tick
		if t, ok := detoByPID[g.ProjectileID]; ok {
			end = t
		}
		landX, landY, landZ := g.ThrowPosX, g.ThrowPosY, g.ThrowPosZ
		if d, ok := findDetonation(m.GrenadeDetonations, g.ProjectileID); ok {
			landX, landY, landZ = d.PosX, d.PosY, d.PosZ
		}
		raw.Grenades[i] = model.RawGrenadeEvent{
			ThrowTick:      int(g.Tick),
			EndTick:        int(end),
			RoundNumber:    int(g.Round),
			ThrowerSteamID: idOf(g.ThrowerSlot),
			ThrowerTeam:    teamAt(g.Round, g.ThrowerSlot),
			GrenadeType:    grenadeTypeName(g.GrenadeType),
			ThrowPos:       vec3From(g.ThrowPosX, g.ThrowPosY, g.ThrowPosZ),
			LandPos:        vec3From(landX, landY, landZ),
		}
	}

	// ── View samples (for the scan-volatility pass) ────────────────────
	// Reduce PlayerSamples to the view-state fields Pass 17 needs. Rows for
	// slots without a resolvable steamID are dropped.
	raw.ViewSamples = make([]model.RawViewSample, 0, len(m.PlayerSamples))
	for _, s := range m.PlayerSamples {
		id := idOf(s.PlayerSlot)
		if id == 0 {
			continue
		}
		raw.ViewSamples = append(raw.ViewSamples, model.RawViewSample{
			Tick:           int(s.Tick),
			RoundNumber:    int(s.Round),
			SteamID:        id,
			YawDeg:         float64(s.YawDeg) / 100,
			HP:             int(s.HP),
			EnemiesVisible: s.VisibleEnemiesMask != 0,
		})
	}

	return raw, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

type roundSlot struct {
	round int
	slot  uint8
}

func buildSlotIDs(players []csraw2.Player) (map[uint8]uint64, error) {
	out := map[uint8]uint64{}
	for _, p := range players {
		id, err := strconv.ParseUint(p.SteamID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("csraw2bridge: bad steam_id %q for slot %d: %w", p.SteamID, p.Slot, err)
		}
		out[p.Slot] = id
	}
	return out, nil
}

func buildStartingTeams(players []csraw2.Player) map[uint8]model.Team {
	out := map[uint8]model.Team{}
	for _, p := range players {
		out[p.Slot] = teamFromLabel(p.StartingTeam)
	}
	return out
}

// buildRoundTeams scans player_samples to find each (round, slot)'s most
// recent team value. Teams flip at halftime; this captures the actual
// post-flip team for any sample-emitting round without needing a heuristic.
func buildRoundTeams(m *csraw2.Match) map[int]map[uint8]model.Team {
	out := map[int]map[uint8]model.Team{}
	for _, s := range m.PlayerSamples {
		round := int(s.Round)
		if _, ok := out[round]; !ok {
			out[round] = map[uint8]model.Team{}
		}
		// Last write wins — samples are emitted in tick order so the
		// final value for a round is whatever team the player ended on.
		out[round][s.PlayerSlot] = teamFromSample(s.Team)
	}
	return out
}

// indexLastSamples returns the highest-tick PlayerSample per (round,slot).
func indexLastSamples(samples []csraw2.PlayerSample) map[roundSlot]csraw2.PlayerSample {
	out := map[roundSlot]csraw2.PlayerSample{}
	for _, s := range samples {
		k := roundSlot{round: int(s.Round), slot: s.PlayerSlot}
		if cur, ok := out[k]; !ok || s.Tick > cur.Tick {
			out[k] = s
		}
	}
	return out
}

// indexFreezeEndSamples returns the first PlayerSample at-or-after each
// round's freeze_end_tick per (round, slot). That's the canonical "freeze
// end" equipment-value snapshot the aggregator uses.
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

func teamFromSample(t uint8) model.Team {
	switch t {
	case csraw2.TeamT:
		return model.TeamT
	case csraw2.TeamCT:
		return model.TeamCT
	case csraw2.TeamSpectators:
		return model.TeamSpectators
	}
	return model.TeamUnknown
}

func teamFromLabel(s string) model.Team {
	switch s {
	case "T":
		return model.TeamT
	case "CT":
		return model.TeamCT
	}
	return model.TeamUnknown
}

func vec3From(x, y, z int16) model.Vec3 {
	return model.Vec3{X: float64(x), Y: float64(y), Z: float64(z)}
}

func vec3FromF32(x, y, z float32) model.Vec3 {
	return model.Vec3{X: float64(x), Y: float64(y), Z: float64(z)}
}

// yaw0to360 converts a signed-1/100° int16 yaw back to the 0–360° float
// the v1 RawKill / RawWeaponFire / RawFlash structs expect.
func yaw0to360(q int16) float64 {
	d := float64(q) / 100
	if d < 0 {
		d += 360
	}
	return d
}

// yaw0to360F32 normalises a float32 yaw (already in degrees, may be in
// either [-180, 180] or [0, 360)) into the canonical [0, 360) range that
// downstream v1 structs expect. Used for the schema 2.4.0 float32 angle
// fields that bypass int16 quantisation.
func yaw0to360F32(f float32) float64 {
	d := float64(f)
	if d < 0 {
		d += 360
	}
	return d
}

func hitGroupName(hg uint8) string {
	switch hg {
	case csraw2.HitGroupHead:
		return "head"
	case csraw2.HitGroupChest:
		return "chest"
	case csraw2.HitGroupStomach:
		return "stomach"
	case csraw2.HitGroupLeftArm:
		return "left_arm"
	case csraw2.HitGroupRightArm:
		return "right_arm"
	case csraw2.HitGroupLeftLeg:
		return "left_leg"
	case csraw2.HitGroupRightLeg:
		return "right_leg"
	case csraw2.HitGroupNeck:
		return "neck"
	case csraw2.HitGroupGear:
		return "gear"
	}
	return "other"
}

func grenadeTypeName(g uint8) string {
	switch g {
	case csraw2.GrenadeSmoke:
		return "smoke"
	case csraw2.GrenadeFlash:
		return "flash"
	case csraw2.GrenadeHE:
		return "he"
	case csraw2.GrenadeMolotov:
		return "molotov"
	case csraw2.GrenadeDecoy:
		return "decoy"
	}
	return ""
}

func findDetonation(detos []csraw2.GrenadeDetonate, pid uint32) (csraw2.GrenadeDetonate, bool) {
	for _, d := range detos {
		if d.ProjectileID == pid {
			return d, true
		}
	}
	return csraw2.GrenadeDetonate{}, false
}

func stripPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

// isUtilityOrKnifeName mirrors v1 parser's isUtilityOrKnifeWeapon filter,
// expressed against the lowercase weapon name string we get from
// EquipmentType.String() in Header.WeaponTable.
func isUtilityOrKnifeName(name string) bool {
	switch strings.ToLower(name) {
	case "knife", "knife_t", "knife_ct", "bayonet",
		"he grenade", "molotov", "incendiary grenade",
		"flashbang", "smoke grenade", "decoy grenade":
		return true
	}
	// Source-2 demoinfocs sometimes returns lowercase short forms too.
	return false
}
