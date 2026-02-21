// Package parser converts Counter-Strike 2 demo (.dem) files into structured
// RawMatch data by walking each frame, extracting kills, damage, flashes,
// weapon fires, and first-sight crosshair angles.
package parser

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

// pairKey identifies a (observer, enemy) pair for spotted-state deduplication.
type pairKey struct{ obs, enemy uint64 }

// Source 2 player model eye-height and head-hitbox offsets (in Hammer units).
// Used to reconstruct eye and head positions when PositionEyes() is unavailable.
const (
	standingEyeHeight = 64.0625 // eye height above origin when standing
	crouchEyeHeight   = 46.0469 // eye height above origin when crouching
	headAboveEye      = 8.0     // vertical offset from eye level to head-hitbox center
)

// headZ returns the world-space Z coordinate of an enemy's head center.
// PositionEyes() panics on Source 2 demos, so eye height is computed manually.
func headZ(p *common.Player) float64 {
	eyeOffset := standingEyeHeight
	if p.IsDucking() {
		eyeOffset = crouchEyeHeight
	}
	return p.Position().Z + eyeOffset + headAboveEye
}

// crosshairAngles returns total angular deviation, pitch deviation, and yaw deviation
// between the observer's crosshair direction and the direction to the enemy's head.
//
// Coordinate convention (Source 2 / CS2):
//   - ViewDirectionX() = yaw,   0–360°, 0=East (+X), 90=North (+Y)
//   - ViewDirectionY() = pitch, 270–90°, where 270 ≡ −90 (looking down);
//     normalize by subtracting 360 when > 180
//   - Forward vector: fwdX = cos(pitch)*cos(yaw), fwdY = cos(pitch)*sin(yaw),
//     fwdZ = -sin(pitch)  (positive pitch → looking down → Z component negative)
//
// NOTE: This formula should be validated against a known demo with independently
// verifiable crosshair data before treating the absolute values as ground truth.
func crosshairAngles(observer, enemy *common.Player) (total, pitch, yaw float64) {
	// Observer eye position (PositionEyes() panics on Source 2 — compute manually).
	eyePos := observer.Position()
	if observer.IsDucking() {
		eyePos.Z += crouchEyeHeight
	} else {
		eyePos.Z += standingEyeHeight
	}

	// Enemy head position.
	headPos := enemy.Position()
	headPos.Z = headZ(enemy)

	// Raw direction from eye to head (not yet normalized — we need raw for atan2).
	dxRaw := headPos.X - eyePos.X
	dyRaw := headPos.Y - eyePos.Y
	dzRaw := headPos.Z - eyePos.Z
	distXY := math.Sqrt(dxRaw*dxRaw + dyRaw*dyRaw)
	dist := math.Sqrt(dxRaw*dxRaw + dyRaw*dyRaw + dzRaw*dzRaw)
	if dist < 1e-6 {
		return 0, 0, 0
	}

	// Yaw and pitch to enemy (world-space angles).
	yawToEnemy := math.Atan2(dyRaw, dxRaw) * 180 / math.Pi
	if yawToEnemy < 0 {
		yawToEnemy += 360
	}
	pitchToEnemy := math.Atan2(dzRaw, distXY) * 180 / math.Pi // positive = upward

	// Observer angles.
	observerYaw := float64(observer.ViewDirectionX())
	observerPitch := float64(observer.ViewDirectionY())
	if observerPitch > 180 {
		observerPitch -= 360 // normalize: 270 → −90 (looking down)
	}
	// Source2 convention: positive pitch = looking down → negate for math
	observerPitch = -observerPitch

	// Yaw deviation wrapped to [0, 180].
	yawDev := math.Abs(yawToEnemy - observerYaw)
	if yawDev > 180 {
		yawDev = 360 - yawDev
	}

	// Pitch deviation (absolute).
	pitchDev := math.Abs(pitchToEnemy - observerPitch)

	// Total angular deviation via dot product of unit forward vectors.
	dx := dxRaw / dist
	dy := dyRaw / dist
	dz := dzRaw / dist

	yawR := observerYaw * math.Pi / 180
	pitchR := (-observerPitch) * math.Pi / 180 // undo our negation for vector math
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

// hitGroupName maps a demoinfocs HitGroup to a string label.
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

// isUtilityOrKnifeWeapon returns true for weapons that should be skipped in WeaponFire handling.
func isUtilityOrKnifeWeapon(t common.EquipmentType) bool {
	return t == common.EqHE || t == common.EqMolotov || t == common.EqIncendiary ||
		t == common.EqFlash || t == common.EqSmoke || t == common.EqDecoy ||
		t == common.EqKnife
}

// ParseDemo parses the demo at path and returns a RawMatch.
func ParseDemo(path, matchType string) (*model.RawMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	// Hash file for idempotency key.
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hash demo: %w", err)
	}
	demoHash := fmt.Sprintf("%x", h.Sum(nil))

	// Seek back to start for the parser.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek demo: %w", err)
	}

	p := demoinfocs.NewParser(f)
	defer p.Close()

	raw := &model.RawMatch{
		DemoHash:    demoHash,
		MatchType:   matchType,
		PlayerNames: make(map[uint64]string),
		PlayerTeams: make(map[uint64]model.Team),
	}

	var (
		roundNumber       int
		roundStartTick    int
		freezeEndTick     int
		currentEquipVals  map[uint64]int
	)

	// seenThisRound tracks (observer, enemy) pairs already recorded in the current round
	// so each pair only generates one RawFirstSight event per round.
	seenThisRound := make(map[pairKey]bool)

	// RoundStart: record start tick, bump round counter, reset spotted tracking.
	p.RegisterEventHandler(func(e events.RoundStart) {
		if p.GameState().IsWarmupPeriod() {
			return
		}
		roundNumber++
		roundStartTick = p.GameState().IngameTick()
		freezeEndTick = roundStartTick // will be updated by RoundFreezetimeEnd
		seenThisRound = make(map[pairKey]bool)
		currentEquipVals = nil
	})

	// RoundFreezetimeEnd: record the tick after freeze ends and snapshot equipment values.
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

	// RoundEnd: snapshot state, record round metadata.
	p.RegisterEventHandler(func(e events.RoundEnd) {
		if roundNumber == 0 {
			return
		}
		endTick := p.GameState().IngameTick()
		winnerTeam := teamFromCommon(e.Winner)

		endState := make(map[uint64]model.PlayerRoundEndState)
		for _, pl := range p.GameState().Participants().Playing() {
			if pl == nil || pl.SteamID64 == 0 {
				continue
			}
			grenCount := 0
			for _, weap := range pl.Weapons() {
				if weap != nil && weap.Type.Class() == common.EqClassGrenade &&
					weap.Type != common.EqFlash { // flashes counted separately
					grenCount++
				}
			}
			endState[pl.SteamID64] = model.PlayerRoundEndState{
				SteamID64:    pl.SteamID64,
				IsAlive:      pl.IsAlive(),
				Team:         teamFromCommon(pl.Team),
				GrenadeCount: grenCount,
			}
			// Update name/team maps.
			raw.PlayerNames[pl.SteamID64] = pl.Name
			raw.PlayerTeams[pl.SteamID64] = teamFromCommon(pl.Team)
		}

		raw.Rounds = append(raw.Rounds, model.RawRound{
			Number:            roundNumber,
			StartTick:         roundStartTick,
			FreezeEndTick:     freezeEndTick,
			EndTick:           endTick,
			WinnerTeam:        winnerTeam,
			PlayerEndState:    endState,
			PlayerEquipValues: currentEquipVals,
		})
	})

	// Kill events.
	p.RegisterEventHandler(func(e events.Kill) {
		if roundNumber == 0 {
			return
		}
		if e.Killer == nil || e.Victim == nil {
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

		kill := model.RawKill{
			Tick:            p.GameState().IngameTick(),
			RoundNumber:     roundNumber,
			KillerSteamID:   e.Killer.SteamID64,
			VictimSteamID:   e.Victim.SteamID64,
			AssisterSteamID: assisterID,
			KillerTeam:      teamFromCommon(e.Killer.Team),
			VictimTeam:      teamFromCommon(e.Victim.Team),
			Weapon:          weapName,
			IsHeadshot:      e.IsHeadshot,
			AssistedFlash:   e.AssistedFlash,
		}

		// Count alive teammates of victim within 512 units for AWP death classifier.
		if e.Weapon != nil && e.Weapon.Type == common.EqAWP {
			victimPos := e.Victim.Position()
			count := 0
			for _, pl := range p.GameState().Participants().Playing() {
				if pl == nil || !pl.IsAlive() || pl.Team != e.Victim.Team || pl.SteamID64 == e.Victim.SteamID64 {
					continue
				}
				d := pl.Position().Sub(victimPos)
				if math.Sqrt(float64(d.X*d.X+d.Y*d.Y+d.Z*d.Z)) <= 512 {
					count++
				}
			}
			kill.NearbyVictimTeammates = count
		}

		raw.Kills = append(raw.Kills, kill)

		// Update player name/team.
		raw.PlayerNames[e.Killer.SteamID64] = e.Killer.Name
		raw.PlayerNames[e.Victim.SteamID64] = e.Victim.Name
		raw.PlayerTeams[e.Killer.SteamID64] = teamFromCommon(e.Killer.Team)
		raw.PlayerTeams[e.Victim.SteamID64] = teamFromCommon(e.Victim.Team)
	})

	// PlayerHurt (damage) events.
	p.RegisterEventHandler(func(e events.PlayerHurt) {
		if roundNumber == 0 {
			return
		}
		if e.Attacker == nil || e.Player == nil {
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
		raw.Damages = append(raw.Damages, model.RawDamage{
			Tick:            p.GameState().IngameTick(),
			RoundNumber:     roundNumber,
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

	// PlayerFlashed events.
	p.RegisterEventHandler(func(e events.PlayerFlashed) {
		if roundNumber == 0 {
			return
		}
		if e.Attacker == nil || e.Player == nil {
			return
		}
		dur := e.FlashDuration()
		if dur <= 0 {
			return
		}

		raw.Flashes = append(raw.Flashes, model.RawFlash{
			Tick:            p.GameState().IngameTick(),
			RoundNumber:     roundNumber,
			AttackerSteamID: e.Attacker.SteamID64,
			VictimSteamID:   e.Player.SteamID64,
			AttackerTeam:    teamFromCommon(e.Attacker.Team),
			VictimTeam:      teamFromCommon(e.Player.Team),
			FlashDuration:   dur,
		})
	})

	// WeaponFire events (for pre-shot correction).
	p.RegisterEventHandler(func(e events.WeaponFire) {
		if roundNumber == 0 {
			return
		}
		if p.GameState().IsWarmupPeriod() {
			return
		}
		if e.Shooter == nil || e.Shooter.SteamID64 == 0 {
			return
		}
		if e.Weapon == nil || isUtilityOrKnifeWeapon(e.Weapon.Type) {
			return
		}

		yaw := float64(e.Shooter.ViewDirectionX())
		pitch := float64(e.Shooter.ViewDirectionY())
		if pitch > 180 {
			pitch -= 360 // normalize
		}

		sp := e.Shooter.Position()
		vel := e.Shooter.Velocity()
		shooterVelocity := math.Sqrt(vel.X*vel.X + vel.Y*vel.Y)
		raw.WeaponFires = append(raw.WeaponFires, model.RawWeaponFire{
			Tick:            p.GameState().IngameTick(),
			RoundNumber:     roundNumber,
			ShooterID:       e.Shooter.SteamID64,
			Weapon:          e.Weapon.Type.String(),
			PitchDeg:        pitch,
			YawDeg:          yaw,
			AttackerPos:     model.Vec3{X: sp.X, Y: sp.Y, Z: sp.Z},
			ShooterVelocity: shooterVelocity,
		})
	})

	// Frame-walk loop: fires registered event handlers each frame AND lets us
	// inspect live game state for spotted-flag transitions every tick.
	for {
		ok, err := p.ParseNextFrame()
		if err != nil {
			return nil, fmt.Errorf("parse demo: %w", err)
		}

		if roundNumber > 0 {
			tick := p.GameState().IngameTick()
			players := p.GameState().Participants().Playing()
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
						raw.FirstSights = append(raw.FirstSights, model.RawFirstSight{
							Tick:             tick,
							RoundNumber:      roundNumber,
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

	// Extract header metadata.
	header := p.Header()
	raw.MapName = header.MapName
	raw.MatchDate = demoFileDate(path)
	raw.Tickrate = p.TickRate()
	raw.TicksPerSecond = p.TickRate()

	return raw, nil
}

// teamFromCommon converts a demoinfocs common.Team value to the internal model.Team enum.
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

// isUtilityWeapon returns true for grenade-type equipment (HE, molotov, incendiary)
// that should be flagged as utility damage in PlayerHurt events.
func isUtilityWeapon(t common.EquipmentType) bool {
	return t == common.EqHE || t == common.EqMolotov || t == common.EqIncendiary
}

// demoFileDate returns the file's modification time as "YYYY-MM-DD".
// CS2 writes the demo to disk when the match ends, so mtime is a reliable
// proxy for the match date. Falls back to today if stat fails.
func demoFileDate(path string) string {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().UTC().Format("2006-01-02")
	}
	return time.Now().UTC().Format("2006-01-02")
}
