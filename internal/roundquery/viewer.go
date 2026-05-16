package roundquery

import (
	"sort"
	"strconv"

	"github.com/pable/go-cs-metrics/internal/csraw2"
)

// buildViewerData composes the per-round payload that powers the 2D HTML
// viewer. All inputs are already filtered to the target round.
func buildViewerData(
	m *csraw2.Match,
	r csraw2.Round,
	teamAt func(round int, slot uint8) uint8,
	samples []csraw2.PlayerSample,
	kills []csraw2.Kill,
	throws []csraw2.GrenadeThrow,
	detos []csraw2.GrenadeDetonate,
	bombs []csraw2.BombAction,
	fires []csraw2.WeaponFire,
	projSamples []csraw2.ProjectileSample,
	weaponNameForID func(uint8) string,
) *QueryViewerData {
	players := make([]QueryPlayer, len(m.Header.Players))
	for i, p := range m.Header.Players {
		id, _ := strconv.ParseUint(p.SteamID, 10, 64)
		players[i] = QueryPlayer{Slot: p.Slot, SteamID: id, Name: p.Name}
	}

	meta := QueryRoundMeta{
		Number:        r.N,
		WinnerTeam:    r.Winner,
		CTScore:       r.CTScoreAfter,
		TScore:        r.TScoreAfter,
		FreezeEndTick: r.FreezeEndTick,
		EndTick:       r.EndTick,
	}

	return &QueryViewerData{
		Players:   players,
		RoundMeta: meta,
		Tickrate:  m.Header.Tickrate,
		Frames:    buildFrames(samples, teamAt, r.N, weaponNameForID),
		Kills:     buildKills(kills, weaponNameForID),
		Grenades:  buildGrenades(throws, detos, teamAt, r.N),
		Trails:    buildTrails(throws, projSamples),
		Shots:     buildShots(fires),
		Bombs:     buildBombs(bombs),
	}
}

// buildFrames groups per-tick PlayerSamples into one frame per unique tick,
// emitting the compact viewer state for each alive player at that tick.
func buildFrames(samples []csraw2.PlayerSample, teamAt func(int, uint8) uint8, round int, _ func(uint8) string) []QueryFrame {
	byTick := map[int32][]QueryFrameState{}
	for _, s := range samples {
		flags := 0
		if s.HP == 0 {
			flags |= 1 << 0 // dead
		}
		if teamAt(int(s.Round), s.PlayerSlot) == csraw2.TeamT {
			flags |= 1 << 1 // T
		}
		if s.Flags&csraw2.FlagIsBombCarrier != 0 {
			flags |= 1 << 2
		}
		if s.Armor > 0 {
			flags |= 1 << 3
		}
		if s.Flags&csraw2.FlagHasHelmet != 0 {
			flags |= 1 << 4
		}
		byTick[s.Tick] = append(byTick[s.Tick], QueryFrameState{
			Slot:    s.PlayerSlot,
			Flags:   flags,
			HP:      int(s.HP),
			X:       int(s.PosX),
			Y:       int(s.PosY),
			Z:       int(s.PosZ),
			Yaw:     int(s.YawDeg),
			Weapon:  "", // weapon name lookup omitted from frames; viewer reads kills/shots
			Utility: 0,  // grenade inventory not exposed in player_samples today
			Money:   int(s.Money),
		})
	}
	ticks := make([]int32, 0, len(byTick))
	for t := range byTick {
		ticks = append(ticks, t)
	}
	sort.Slice(ticks, func(i, j int) bool { return ticks[i] < ticks[j] })
	frames := make([]QueryFrame, 0, len(ticks))
	for _, t := range ticks {
		states := byTick[t]
		sort.Slice(states, func(i, j int) bool { return states[i].Slot < states[j].Slot })
		frames = append(frames, QueryFrame{Tick: int(t), States: states})
	}
	return frames
}

func buildKills(kills []csraw2.Kill, weaponNameForID func(uint8) string) []QueryKill {
	out := make([]QueryKill, 0, len(kills))
	for _, k := range kills {
		out = append(out, QueryKill{
			Tick:          int(k.Tick),
			KillerSlot:    k.KillerSlot,
			VictimSlot:    int8(k.VictimSlot),
			AssisterSlot:  k.AssisterSlot,
			Weapon:        weaponNameForID(k.WeaponID),
			IsHeadshot:    k.IsHeadshot,
			AssistedFlash: k.FlashAssist,
			KillerX:       int(k.KillerPosX),
			KillerY:       int(k.KillerPosY),
			VictimX:       int(k.VictimPosX),
			VictimY:       int(k.VictimPosY),
		})
	}
	return out
}

func buildGrenades(throws []csraw2.GrenadeThrow, detos []csraw2.GrenadeDetonate, teamAt func(int, uint8) uint8, round int) []QueryGrenade {
	detoByPID := map[uint32]csraw2.GrenadeDetonate{}
	for _, d := range detos {
		detoByPID[d.ProjectileID] = d
	}
	out := make([]QueryGrenade, 0, len(throws))
	for _, g := range throws {
		viewerType := mapGrenadeType(g.GrenadeType, teamAt(round, g.ThrowerSlot))
		endTick := int(g.Tick)
		x, y := int(g.ThrowPosX), int(g.ThrowPosY)
		if d, ok := detoByPID[g.ProjectileID]; ok {
			endTick = int(d.Tick)
			x, y = int(d.PosX), int(d.PosY)
		}
		out = append(out, QueryGrenade{
			StartTick:  int(g.Tick),
			EndTick:    endTick,
			Type:       viewerType,
			X:          x,
			Y:          y,
			ThrowerIdx: int(g.ThrowerSlot),
		})
	}
	return out
}

// buildTrails groups ProjectileSamples (which are emitted for in-flight
// grenades) into per-throw arcs.
func buildTrails(throws []csraw2.GrenadeThrow, projSamples []csraw2.ProjectileSample) []QueryTrail {
	if len(throws) == 0 || len(projSamples) == 0 {
		return nil
	}
	pointsByPID := map[uint32][][3]int{}
	for _, s := range projSamples {
		if s.Kind != csraw2.ProjectileKindGrenade {
			continue
		}
		pointsByPID[s.ProjectileID] = append(pointsByPID[s.ProjectileID], [3]int{
			int(s.Tick), int(s.PosX), int(s.PosY),
		})
	}
	out := make([]QueryTrail, 0, len(throws))
	for _, g := range throws {
		pts := pointsByPID[g.ProjectileID]
		if len(pts) == 0 {
			continue
		}
		sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] })
		start := pts[0][0]
		end := pts[len(pts)-1][0]
		// Switch from absolute tick to tick offset from throw, matching the
		// viewer's expected encoding.
		for i := range pts {
			pts[i][0] -= int(g.Tick)
		}
		out = append(out, QueryTrail{
			StartTick:  start,
			EndTick:    end,
			Type:       mapGrenadeType(g.GrenadeType, csraw2.TeamUnknown),
			ThrowerIdx: int(g.ThrowerSlot),
			Points:     pts,
		})
	}
	return out
}

// buildShots emits one entry per recorded shot. Weapon fires are already
// per-shot in csraw2, so no dedup window is needed.
func buildShots(fires []csraw2.WeaponFire) []QueryShot {
	out := make([]QueryShot, len(fires))
	for i, f := range fires {
		out[i] = QueryShot{Tick: int(f.Tick), Slot: f.ShooterSlot}
	}
	return out
}

func buildBombs(actions []csraw2.BombAction) []QueryBomb {
	out := make([]QueryBomb, 0, len(actions))
	for _, a := range actions {
		viewerAction, ok := mapBombAction(a.Action)
		if !ok {
			continue
		}
		out = append(out, QueryBomb{
			Tick:   int(a.Tick),
			Action: viewerAction,
			X:      int(a.PosX),
			Y:      int(a.PosY),
			Site:   bombSiteLabel(a.Site),
		})
	}
	return out
}

func bombSiteLabel(s uint8) string {
	switch s {
	case csraw2.BombSiteA:
		return "A"
	case csraw2.BombSiteB:
		return "B"
	}
	return ""
}

// mapGrenadeType converts the csraw2 grenade-type constant into the viewer
// encoding, which splits smokes by throwing side.
func mapGrenadeType(g uint8, throwerTeam uint8) int {
	switch g {
	case csraw2.GrenadeSmoke:
		if throwerTeam == csraw2.TeamCT {
			return ViewerGrenadeCTSmoke
		}
		if throwerTeam == csraw2.TeamT {
			return ViewerGrenadeTSmoke
		}
		return ViewerGrenadeSmoke
	case csraw2.GrenadeFlash:
		return ViewerGrenadeFlash
	case csraw2.GrenadeHE:
		return ViewerGrenadeHE
	case csraw2.GrenadeMolotov:
		return ViewerGrenadeMolotov
	}
	return ViewerGrenadeSmoke
}

// mapBombAction translates the csraw2 bomb-action enum into the viewer
// encoding. Returns ok=false for any action the viewer does not render.
func mapBombAction(a uint8) (int, bool) {
	switch a {
	case csraw2.BombActionPlantBegin:
		return ViewerBombPlantBegin, true
	case csraw2.BombActionPlantComplete:
		return ViewerBombPlanted, true
	case csraw2.BombActionDefuseBegin:
		return ViewerBombDefuseBegin, true
	case csraw2.BombActionDefuseComplete:
		return ViewerBombDefused, true
	case csraw2.BombActionExplode:
		return ViewerBombExploded, true
	}
	return 0, false
}
