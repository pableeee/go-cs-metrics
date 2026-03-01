// Package converter performs a single demoinfocs parse pass that produces a
// UnifiedMatchFile — the .csdem.gz intermediate format. It merges the metrics
// extraction logic (parser package) with the viewer extraction logic
// (cs-demo-viewer parser) in one pass, so the .dem is read only once.
package converter

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"

	"github.com/pable/go-cs-metrics/internal/model"
)

// sampleTicks is the number of ticks between sampled position frames.
// At 64 ticks/sec, 16 ticks = 4 fps keyframes, interpolated to 60 fps in the viewer.
const sampleTicks = 16

// pairKey identifies a (observer, enemy) pair for first-sight deduplication.
type pairKey struct{ obs, enemy uint64 }

// iround rounds a float64 to the nearest int.
func iround(f float64) int { return int(math.Round(f)) }

// ConvertDemo parses the demo at path and returns a UnifiedMatchFile containing
// all data needed by both go-cs-metrics (aggregation) and cs-demo-viewer (2D replay).
// matchType is the label stored in the file (e.g. "Competitive").
func ConvertDemo(path, matchType string) (*model.UnifiedMatchFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	// Full SHA-256 hash for idempotency key.
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hash demo: %w", err)
	}
	demoHash := fmt.Sprintf("%x", h.Sum(nil))

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek demo: %w", err)
	}

	p := demoinfocs.NewParser(f)
	defer p.Close()

	match := &model.UnifiedMatch{
		DemoHash:  demoHash,
		MatchType: matchType,
	}

	// ── Player index tracking (viewer convention) ─────────────────────────────
	// pidx maps SteamID64 → index in match.Players.
	// Used for all viewer structs (frames, grenades, trails, shots).
	pidx := make(map[uint64]int)
	getIdx := func(pl *common.Player) int {
		if pl == nil || pl.SteamID64 == 0 {
			return -1
		}
		if i, ok := pidx[pl.SteamID64]; ok {
			match.Players[i].Name = pl.Name // update name in place
			return i
		}
		i := len(match.Players)
		pidx[pl.SteamID64] = i
		match.Players = append(match.Players, model.UnifiedPlayer{
			SteamID: pl.SteamID64,
			Name:    pl.Name,
		})
		return i
	}

	// ── Round state ───────────────────────────────────────────────────────────
	var (
		roundNumber          int
		roundStartTick       int
		freezeEndTick        int
		currentEquipVals     map[uint64]int
		currentBombPlantTick int
		ctScore, tScore      int
		lastSampledTick      int
		inRound              bool
	)

	// lastShot tracks per-player last shot tick for deduplication of UnifiedShot.
	lastShot := map[int]int{}

	// seenThisRound tracks (observer, enemy) pairs for first-sight deduplication.
	seenThisRound := make(map[pairKey]bool)

	// bombX/Y/Site hold the last-known bomb position for bomb action events.
	var bombX, bombY int
	var bombSite string

	// lastMolotovThrowerIdx carries the thrower idx from GrenadeProjectileDestroy
	// to the subsequent InfernoStart event.
	lastMolotovThrowerIdx := -1

	// pendingThrows maps grenade uniqueID → throw info for trail building.
	pendingThrows := map[int64]struct{ tick, throwerIdx int }{}

	// ── Round lifecycle ───────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.RoundStart) {
		if p.GameState().IsWarmupPeriod() {
			return
		}
		roundNumber++
		roundStartTick = p.GameState().IngameTick()
		freezeEndTick = roundStartTick
		lastSampledTick = 0
		inRound = true
		seenThisRound = make(map[pairKey]bool)
		currentEquipVals = nil
		currentBombPlantTick = 0
		lastShot = map[int]int{}
		pendingThrows = map[int64]struct{ tick, throwerIdx int }{}
		lastMolotovThrowerIdx = -1
	})

	p.RegisterEventHandler(func(e events.RoundFreezetimeEnd) {
		if roundNumber == 0 {
			return
		}
		freezeEndTick = p.GameState().IngameTick()
		equipVals := make(map[uint64]int)
		for _, pl := range p.GameState().Participants().Playing() {
			if pl == nil || pl.SteamID64 == 0 {
				continue
			}
			equipVals[pl.SteamID64] = pl.EquipmentValueFreezeTimeEnd()
		}
		currentEquipVals = equipVals
	})

	p.RegisterEventHandler(func(e events.RoundEnd) {
		if roundNumber == 0 {
			return
		}
		endTick := p.GameState().IngameTick()
		winnerTeam := teamFromCommon(e.Winner)

		// Update running score.
		switch winnerTeam {
		case model.TeamCT:
			ctScore++
		case model.TeamT:
			tScore++
		}

		// Capture final frame at round-end tick (same as viewer).
		if frame := captureFrame(p, getIdx, roundNumber, endTick); len(frame.States) > 0 {
			match.Frames = append(match.Frames, frame)
		}

		endState := make(map[uint64]model.PlayerRoundEndState)
		for _, pl := range p.GameState().Participants().Playing() {
			if pl == nil || pl.SteamID64 == 0 {
				continue
			}
			grenCount := 0
			for _, weap := range pl.Weapons() {
				if weap != nil && weap.Type.Class() == common.EqClassGrenade &&
					weap.Type != common.EqFlash {
					grenCount++
				}
			}
			endState[pl.SteamID64] = model.PlayerRoundEndState{
				SteamID64:    pl.SteamID64,
				IsAlive:      pl.IsAlive(),
				Team:         teamFromCommon(pl.Team),
				GrenadeCount: grenCount,
			}
			// Keep player roster up to date.
			getIdx(pl)
		}

		match.Rounds = append(match.Rounds, model.UnifiedRound{
			Number:            roundNumber,
			StartTick:         roundStartTick,
			FreezeEndTick:     freezeEndTick,
			EndTick:           endTick,
			WinnerTeam:        winnerTeam,
			CTScore:           ctScore,
			TScore:            tScore,
			BombPlantTick:     currentBombPlantTick,
			PlayerEndState:    endState,
			PlayerEquipValues: currentEquipVals,
		})
		inRound = false
	})

	// ── Kill events ───────────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.Kill) {
		if roundNumber == 0 || e.Killer == nil || e.Victim == nil {
			return
		}
		var assisterID uint64
		if e.Assister != nil {
			assisterID = e.Assister.SteamID64
		}
		var weapName string
		if e.Weapon != nil {
			weapName = e.Weapon.Type.String()
		}
		tick := p.GameState().IngameTick()
		ap := e.Killer.Position()
		vp := e.Victim.Position()

		kill := model.UnifiedKill{
			Tick:            tick,
			Round:           roundNumber,
			KillerSteamID:   e.Killer.SteamID64,
			VictimSteamID:   e.Victim.SteamID64,
			AssisterSteamID: assisterID,
			KillerTeam:      teamFromCommon(e.Killer.Team),
			VictimTeam:      teamFromCommon(e.Victim.Team),
			Weapon:          weapName,
			IsHeadshot:      e.IsHeadshot,
			AssistedFlash:   e.AssistedFlash,
			NoScope:         e.NoScope,
			ThroughSmoke:    e.ThroughSmoke,
			AttackerBlind:   e.AttackerBlind,
			KillerX:         iround(ap.X),
			KillerY:         iround(ap.Y),
			VictimX:         iround(vp.X),
			VictimY:         iround(vp.Y),
		}

		// Count alive teammates of victim within 512 units for AWP death classifier.
		if e.Weapon != nil && e.Weapon.Type == common.EqAWP {
			count := 0
			for _, pl := range p.GameState().Participants().Playing() {
				if pl == nil || !pl.IsAlive() || pl.Team != e.Victim.Team ||
					pl.SteamID64 == e.Victim.SteamID64 {
					continue
				}
				d := pl.Position().Sub(vp)
				if math.Sqrt(float64(d.X*d.X+d.Y*d.Y+d.Z*d.Z)) <= 512 {
					count++
				}
			}
			kill.NearbyVictimTeammates = count
		}

		match.Kills = append(match.Kills, kill)
		getIdx(e.Killer)
		getIdx(e.Victim)
	})

	// ── Damage events ─────────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.PlayerHurt) {
		if roundNumber == 0 || e.Attacker == nil || e.Player == nil {
			return
		}
		if e.Attacker.SteamID64 == e.Player.SteamID64 {
			return // ignore self-damage
		}
		var weapName string
		isUtil := false
		if e.Weapon != nil {
			weapName = e.Weapon.Type.String()
			isUtil = isUtilityWeapon(e.Weapon.Type)
		}
		vp := e.Player.Position()
		match.Damages = append(match.Damages, model.UnifiedDamage{
			Tick:            p.GameState().IngameTick(),
			Round:           roundNumber,
			AttackerSteamID: e.Attacker.SteamID64,
			VictimSteamID:   e.Player.SteamID64,
			AttackerTeam:    teamFromCommon(e.Attacker.Team),
			HealthDamage:    e.HealthDamage,
			Weapon:          weapName,
			IsUtility:       isUtil,
			HitGroup:        hitGroupName(e.HitGroup),
			VictimPos:       model.Vec3{X: vp.X, Y: vp.Y, Z: vp.Z},
		})
	})

	// ── Flash events ──────────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.PlayerFlashed) {
		if roundNumber == 0 || e.Attacker == nil || e.Player == nil {
			return
		}
		dur := e.FlashDuration()
		if dur <= 0 {
			return
		}
		match.Flashes = append(match.Flashes, model.UnifiedFlash{
			Tick:            p.GameState().IngameTick(),
			Round:           roundNumber,
			AttackerSteamID: e.Attacker.SteamID64,
			VictimSteamID:   e.Player.SteamID64,
			AttackerTeam:    teamFromCommon(e.Attacker.Team),
			VictimTeam:      teamFromCommon(e.Player.Team),
			FlashDuration:   dur,
		})
	})

	// ── WeaponFire events ─────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.WeaponFire) {
		if roundNumber == 0 || p.GameState().IsWarmupPeriod() {
			return
		}
		if e.Shooter == nil || e.Shooter.SteamID64 == 0 {
			return
		}
		if e.Weapon == nil || isUtilityOrKnifeWeapon(e.Weapon.Type) {
			return
		}

		tick := p.GameState().IngameTick()
		yaw := float64(e.Shooter.ViewDirectionX())
		pitch := float64(e.Shooter.ViewDirectionY())
		if pitch > 180 {
			pitch -= 360
		}
		sp := e.Shooter.Position()
		vel := e.Shooter.Velocity()
		hSpeed := math.Sqrt(vel.X*vel.X + vel.Y*vel.Y)

		match.WeaponFires = append(match.WeaponFires, model.UnifiedWeaponFire{
			Tick:            tick,
			Round:           roundNumber,
			ShooterID:       e.Shooter.SteamID64,
			Weapon:          e.Weapon.Type.String(),
			PitchDeg:        pitch,
			YawDeg:          yaw,
			AttackerPos:     model.Vec3{X: sp.X, Y: sp.Y, Z: sp.Z},
			HorizontalSpeed: hSpeed,
		})

		// Deduplicated shot for viewer muzzle flash.
		pi := getIdx(e.Shooter)
		if pi >= 0 {
			if last, ok := lastShot[pi]; !ok || tick-last >= sampleTicks {
				match.Shots = append(match.Shots, model.UnifiedShot{
					Tick:  tick,
					Round: roundNumber,
					Idx:   pi,
				})
				lastShot[pi] = tick
			}
		}
	})

	// ── Bomb events ───────────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.BombPlantBegin) {
		if roundNumber == 0 || e.Player == nil {
			return
		}
		tick := p.GameState().IngameTick()
		pos := e.Player.Position()
		bombX, bombY = iround(pos.X), iround(pos.Y)
		bombSite = string(rune(e.Site))
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 0, X: bombX, Y: bombY, Site: bombSite,
		})
	})

	p.RegisterEventHandler(func(e events.BombPlanted) {
		if roundNumber == 0 || e.Player == nil {
			return
		}
		tick := p.GameState().IngameTick()
		pos := e.Player.Position()
		bombX, bombY = iround(pos.X), iround(pos.Y)
		bombSite = string(rune(e.Site))
		currentBombPlantTick = p.CurrentFrame()
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 1, X: bombX, Y: bombY, Site: bombSite,
		})
	})

	p.RegisterEventHandler(func(e events.BombDefuseStart) {
		if roundNumber == 0 || e.Player == nil {
			return
		}
		tick := p.GameState().IngameTick()
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 2, X: bombX, Y: bombY, Site: bombSite,
		})
	})

	p.RegisterEventHandler(func(e events.BombDefused) {
		if roundNumber == 0 || e.Player == nil {
			return
		}
		tick := p.GameState().IngameTick()
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 3, X: bombX, Y: bombY, Site: string(rune(e.Site)),
		})
	})

	p.RegisterEventHandler(func(e events.BombExplode) {
		if roundNumber == 0 {
			return
		}
		tick := p.GameState().IngameTick()
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 4, X: bombX, Y: bombY, Site: bombSite,
		})
	})

	p.RegisterEventHandler(func(e events.BombDropped) {
		if roundNumber == 0 || e.Player == nil {
			return
		}
		tick := p.GameState().IngameTick()
		pos := e.Player.Position()
		bombX, bombY = iround(pos.X), iround(pos.Y)
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 5, X: bombX, Y: bombY, Site: bombSite,
		})
	})

	p.RegisterEventHandler(func(e events.BombPickup) {
		if roundNumber == 0 {
			return
		}
		tick := p.GameState().IngameTick()
		match.Bombs = append(match.Bombs, model.UnifiedBombAction{
			Tick: tick, Round: roundNumber, Action: 6, X: bombX, Y: bombY, Site: bombSite,
		})
	})

	// ── Grenade events ────────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.SmokeStart) {
		if roundNumber == 0 {
			return
		}
		tick := p.GameState().IngameTick()
		smokeType := 0
		if e.Thrower != nil {
			if e.Thrower.Team == common.TeamCounterTerrorists {
				smokeType = 4
			} else if e.Thrower.Team == common.TeamTerrorists {
				smokeType = 5
			}
		}
		match.Grenades = append(match.Grenades, model.UnifiedGrenade{
			StartTick:  tick,
			EndTick:    tick + 1152, // ~18 s at 64 ticks/s
			Round:      roundNumber,
			Type:       smokeType,
			X:          iround(e.Position.X),
			Y:          iround(e.Position.Y),
			ThrowerIdx: getIdx(e.Thrower),
		})
	})

	p.RegisterEventHandler(func(e events.HeExplode) {
		if roundNumber == 0 {
			return
		}
		tick := p.GameState().IngameTick()
		match.Grenades = append(match.Grenades, model.UnifiedGrenade{
			StartTick:  tick,
			EndTick:    0,
			Round:      roundNumber,
			Type:       2,
			X:          iround(e.Position.X),
			Y:          iround(e.Position.Y),
			ThrowerIdx: getIdx(e.Thrower),
		})
	})

	p.RegisterEventHandler(func(e events.FlashExplode) {
		if roundNumber == 0 {
			return
		}
		tick := p.GameState().IngameTick()
		match.Grenades = append(match.Grenades, model.UnifiedGrenade{
			StartTick:  tick,
			EndTick:    0,
			Round:      roundNumber,
			Type:       1,
			X:          iround(e.Position.X),
			Y:          iround(e.Position.Y),
			ThrowerIdx: getIdx(e.Thrower),
		})
	})

	p.RegisterEventHandler(func(e events.InfernoStart) {
		if roundNumber == 0 {
			return
		}
		tick := p.GameState().IngameTick()
		pos := e.Inferno.Entity.Position()
		match.Grenades = append(match.Grenades, model.UnifiedGrenade{
			StartTick:  tick,
			EndTick:    tick + 448, // ~7 s at 64 ticks/s
			Round:      roundNumber,
			Type:       3,
			X:          iround(pos.X),
			Y:          iround(pos.Y),
			ThrowerIdx: lastMolotovThrowerIdx,
		})
		lastMolotovThrowerIdx = -1
	})

	// ── Grenade trails ────────────────────────────────────────────────────────

	p.RegisterEventHandler(func(e events.GrenadeProjectileThrow) {
		if roundNumber == 0 || e.Projectile == nil {
			return
		}
		pi := -1
		if e.Projectile.Thrower != nil {
			pi = getIdx(e.Projectile.Thrower)
		}
		pendingThrows[e.Projectile.UniqueID()] = struct{ tick, throwerIdx int }{
			p.GameState().IngameTick(), pi,
		}
	})

	p.RegisterEventHandler(func(e events.GrenadeProjectileDestroy) {
		if roundNumber == 0 || e.Projectile == nil || e.Projectile.WeaponInstance == nil {
			return
		}
		gt := equipToGrenadeType(e.Projectile.WeaponInstance.Type)
		if gt < 0 {
			return
		}
		// Team-coloured smokes.
		if gt == 0 && e.Projectile.Thrower != nil {
			if e.Projectile.Thrower.Team == common.TeamCounterTerrorists {
				gt = 4
			} else if e.Projectile.Thrower.Team == common.TeamTerrorists {
				gt = 5
			}
		}
		// Track molotov thrower for the subsequent InfernoStart event.
		if gt == 3 && e.Projectile.Thrower != nil {
			lastMolotovThrowerIdx = getIdx(e.Projectile.Thrower)
		}

		uid := e.Projectile.UniqueID()
		info, ok := pendingThrows[uid]
		if !ok {
			return
		}
		delete(pendingThrows, uid)

		traj := e.Projectile.Trajectory2
		if len(traj) < 2 {
			return
		}
		step := 1
		if len(traj) > 80 {
			step = len(traj) / 80
		}
		points := make([][3]int, 0, 80)
		for i := 0; i < len(traj); i += step {
			te := traj[i]
			tickOff := int(math.Round(te.Time.Seconds()*64)) - info.tick
			if tickOff < 0 {
				tickOff = 0
			}
			points = append(points, [3]int{tickOff, iround(te.Position.X), iround(te.Position.Y)})
		}
		last := traj[len(traj)-1]
		lastOff := int(math.Round(last.Time.Seconds()*64)) - info.tick
		if lastOff < 0 {
			lastOff = 0
		}
		if len(points) == 0 || points[len(points)-1][0] != lastOff {
			points = append(points, [3]int{lastOff, iround(last.Position.X), iround(last.Position.Y)})
		}

		match.Trails = append(match.Trails, model.UnifiedGrenadeTrail{
			StartTick:  info.tick,
			EndTick:    p.GameState().IngameTick(),
			Round:      roundNumber,
			Type:       gt,
			ThrowerIdx: info.throwerIdx,
			Points:     points,
		})
	})

	// ── Main frame-walk loop ──────────────────────────────────────────────────
	// Handles both position frame sampling (viewer) and first-sight detection (metrics).

	for {
		ok, err := p.ParseNextFrame()
		if err != nil {
			return nil, fmt.Errorf("parse demo: %w", err)
		}

		if roundNumber > 0 {
			tick := p.GameState().IngameTick()
			players := p.GameState().Participants().Playing()

			// Position frame sampling (viewer): every sampleTicks after freeze ends.
			// Skip duplicate ticks from full-snapshot packets (DEM_FullPacket).
			if inRound && freezeEndTick > 0 && tick >= freezeEndTick &&
				tick > lastSampledTick && tick%sampleTicks == 0 {
				if frame := captureFrame(p, getIdx, roundNumber, tick); len(frame.States) > 0 {
					match.Frames = append(match.Frames, frame)
					lastSampledTick = tick
				}
			}

			// First-sight detection (metrics): spotted-flag transitions per frame.
			for _, observer := range players {
				if observer == nil || observer.SteamID64 == 0 || !observer.IsAlive() {
					continue
				}
				for _, enemy := range players {
					if enemy == nil || enemy.SteamID64 == 0 || !enemy.IsAlive() {
						continue
					}
					if enemy.Team == observer.Team {
						continue
					}
					key := pairKey{observer.SteamID64, enemy.SteamID64}
					if seenThisRound[key] {
						continue
					}
					if enemy.IsSpottedBy(observer) {
						totalDeg, pitchDeg, yawDeg := crosshairAngles(observer, enemy)
						obsPitch := float64(observer.ViewDirectionY())
						if obsPitch > 180 {
							obsPitch -= 360
						}
						match.FirstSights = append(match.FirstSights, model.UnifiedFirstSight{
							Tick:             tick,
							Round:            roundNumber,
							ObserverID:       observer.SteamID64,
							EnemyID:          enemy.SteamID64,
							AngleDeg:         totalDeg,
							PitchDeg:         pitchDeg,
							YawDeg:           yawDeg,
							ObserverPitchDeg: obsPitch,
							ObserverYawDeg:   float64(observer.ViewDirectionX()),
						})
						seenThisRound[key] = true
					}
				}
			}
		}

		if !ok {
			break
		}
	}

	// ── Finalise match metadata ───────────────────────────────────────────────

	header := p.Header()
	match.MapName = header.MapName
	match.MatchDate = demoFileDate(path)
	match.Tickrate = p.TickRate()

	return &model.UnifiedMatchFile{
		Version: model.UnifiedMatchFileVersion,
		Match:   match,
	}, nil
}

// captureFrame samples the current player state at the given tick.
func captureFrame(
	p demoinfocs.Parser,
	getIdx func(*common.Player) int,
	round, tick int,
) model.UnifiedFrame {
	frame := model.UnifiedFrame{Tick: tick, Round: round}
	bomb := p.GameState().Bomb()
	var carrierID uint64
	if bomb != nil && bomb.Carrier != nil {
		carrierID = bomb.Carrier.SteamID64
	}
	for _, pl := range p.GameState().Participants().Playing() {
		if pl == nil || pl.SteamID64 == 0 {
			continue
		}
		pos := pl.Position()
		flags := 2 // T+alive
		if pl.Team == common.TeamCounterTerrorists {
			flags = 0 // CT+alive
		}
		if !pl.IsAlive() {
			flags++ // CT+dead=1, T+dead=3
		}
		if pl.SteamID64 == carrierID {
			flags |= 4
		}
		if pl.Armor() > 0 {
			flags |= 8
			if pl.HasHelmet() {
				flags |= 16
			}
		}
		var wep string
		if aw := pl.ActiveWeapon(); aw != nil && aw.Type != common.EqUnknown {
			wep = aw.Type.String()
		}
		var util int
		for _, w := range pl.Weapons() {
			if w == nil {
				continue
			}
			switch w.Type {
			case common.EqSmoke:
				util |= 1
			case common.EqHE:
				util |= 2
			case common.EqFlash:
				if fl := (util >> 2) & 3; fl < 2 {
					util = (util &^ (3 << 2)) | ((fl + 1) << 2)
				}
			case common.EqMolotov, common.EqIncendiary:
				util |= 16
			case common.EqDecoy:
				util |= 32
			}
		}
		idx := getIdx(pl)
		if idx < 0 {
			continue
		}
		frame.States = append(frame.States, model.UnifiedPlayerState{
			Idx:     idx,
			Flags:   flags,
			HP:      pl.Health(),
			X:       iround(pos.X),
			Y:       iround(pos.Y),
			Z:       iround(pos.Z),
			Yaw:     iround(float64(pl.ViewDirectionX())),
			Weapon:  wep,
			Utility: util,
			Money:   pl.Money(),
		})
	}
	return frame
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func teamFromCommon(t common.Team) model.Team {
	switch t {
	case common.TeamTerrorists:
		return model.TeamT
	case common.TeamCounterTerrorists:
		return model.TeamCT
	case common.TeamSpectators:
		return model.TeamSpectators
	default:
		return model.TeamUnknown
	}
}

func isUtilityWeapon(t common.EquipmentType) bool {
	return t == common.EqHE || t == common.EqMolotov || t == common.EqIncendiary
}

func isUtilityOrKnifeWeapon(t common.EquipmentType) bool {
	return t == common.EqHE || t == common.EqMolotov || t == common.EqIncendiary ||
		t == common.EqFlash || t == common.EqSmoke || t == common.EqDecoy ||
		t == common.EqKnife
}

func hitGroupName(hg events.HitGroup) string {
	switch hg {
	case events.HitGroupHead:
		return "head"
	case events.HitGroupChest:
		return "chest"
	case events.HitGroupStomach:
		return "stomach"
	case events.HitGroupLeftArm:
		return "left_arm"
	case events.HitGroupRightArm:
		return "right_arm"
	case events.HitGroupLeftLeg:
		return "left_leg"
	case events.HitGroupRightLeg:
		return "right_leg"
	default:
		return "other"
	}
}

func equipToGrenadeType(t common.EquipmentType) int {
	switch t {
	case common.EqSmoke:
		return 0
	case common.EqFlash:
		return 1
	case common.EqHE:
		return 2
	case common.EqMolotov, common.EqIncendiary:
		return 3
	}
	return -1
}

// demoFileDate returns the file's modification time as "YYYY-MM-DD".
func demoFileDate(path string) string {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UTC().Format("2006-01-02")
	}
	return time.Now().UTC().Format("2006-01-02")
}

// Source 2 player model eye-height and head-hitbox offsets (Hammer units).
const (
	standingEyeHeight = 64.0625
	crouchEyeHeight   = 46.0469
	headAboveEye      = 8.0
)

func headZ(pl *common.Player) float64 {
	eyeOffset := standingEyeHeight
	if pl.IsDucking() {
		eyeOffset = crouchEyeHeight
	}
	return pl.Position().Z + eyeOffset + headAboveEye
}

func crosshairAngles(observer, enemy *common.Player) (total, pitch, yaw float64) {
	eyePos := observer.Position()
	if observer.IsDucking() {
		eyePos.Z += crouchEyeHeight
	} else {
		eyePos.Z += standingEyeHeight
	}
	headPos := enemy.Position()
	headPos.Z = headZ(enemy)

	dxRaw := headPos.X - eyePos.X
	dyRaw := headPos.Y - eyePos.Y
	dzRaw := headPos.Z - eyePos.Z
	distXY := math.Sqrt(dxRaw*dxRaw + dyRaw*dyRaw)
	dist := math.Sqrt(dxRaw*dxRaw + dyRaw*dyRaw + dzRaw*dzRaw)
	if dist < 1e-6 {
		return 0, 0, 0
	}

	yawToEnemy := math.Atan2(dyRaw, dxRaw) * 180 / math.Pi
	if yawToEnemy < 0 {
		yawToEnemy += 360
	}
	pitchToEnemy := math.Atan2(dzRaw, distXY) * 180 / math.Pi

	observerYaw := float64(observer.ViewDirectionX())
	observerPitch := float64(observer.ViewDirectionY())
	if observerPitch > 180 {
		observerPitch -= 360
	}
	observerPitch = -observerPitch

	yawDev := math.Abs(yawToEnemy - observerYaw)
	if yawDev > 180 {
		yawDev = 360 - yawDev
	}
	pitchDev := math.Abs(pitchToEnemy - observerPitch)

	dx := dxRaw / dist
	dy := dyRaw / dist
	dz := dzRaw / dist
	yawR := observerYaw * math.Pi / 180
	pitchR := (-observerPitch) * math.Pi / 180
	fwdX := math.Cos(pitchR) * math.Cos(yawR)
	fwdY := math.Cos(pitchR) * math.Sin(yawR)
	fwdZ := -math.Sin(pitchR)

	dot := fwdX*dx + fwdY*dy + fwdZ*dz
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	total = math.Acos(dot) * 180 / math.Pi
	return total, pitchDev, yawDev
}
