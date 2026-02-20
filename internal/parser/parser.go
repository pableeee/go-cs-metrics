package parser

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"time"

	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"

	"github.com/pable/go-cs-metrics/internal/model"
)

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
		roundNumber    int
		roundStartTick int
		freezeEndTick  int
	)

	// RoundStart: record start tick, bump round counter.
	p.RegisterEventHandler(func(e events.RoundStart) {
		if p.GameState().IsWarmupPeriod() {
			return
		}
		roundNumber++
		roundStartTick = p.GameState().IngameTick()
		freezeEndTick = roundStartTick // will be updated by RoundFreezetimeEnd
	})

	// RoundFreezetimeEnd: record the tick after freeze ends.
	p.RegisterEventHandler(func(e events.RoundFreezetimeEnd) {
		if roundNumber == 0 {
			return
		}
		freezeEndTick = p.GameState().IngameTick()
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
			Number:        roundNumber,
			StartTick:     roundStartTick,
			FreezeEndTick: freezeEndTick,
			EndTick:       endTick,
			WinnerTeam:    winnerTeam,
			PlayerEndState: endState,
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

		raw.Kills = append(raw.Kills, model.RawKill{
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
		})

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

		raw.Damages = append(raw.Damages, model.RawDamage{
			Tick:            p.GameState().IngameTick(),
			RoundNumber:     roundNumber,
			AttackerSteamID: e.Attacker.SteamID64,
			VictimSteamID:   e.Player.SteamID64,
			AttackerTeam:    teamFromCommon(e.Attacker.Team),
			HealthDamage:    e.HealthDamage,
			Weapon:          weapName,
			IsUtility:       isUtil,
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

	if err := p.ParseToEnd(); err != nil {
		return nil, fmt.Errorf("parse demo: %w", err)
	}

	// Extract header metadata.
	header := p.Header()
	raw.MapName = header.MapName
	raw.MatchDate = time.Now().Format("2006-01-02") // demos rarely embed wall-clock time
	raw.Tickrate = p.TickRate()
	raw.TicksPerSecond = p.TickRate()

	return raw, nil
}

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
