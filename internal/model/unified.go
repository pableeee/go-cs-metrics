package model

import (
	"encoding/json"
	"time"
)

// UnifiedMatchFileVersion is the current file format version.
// Bump this on any breaking schema change.
const UnifiedMatchFileVersion = 1

// UnifiedMatchFile is the top-level structure serialised to a .csdem.gz file.
// It wraps UnifiedMatch with version and tier metadata.
type UnifiedMatchFile struct {
	Version int           `json:"version"` // = UnifiedMatchFileVersion
	Tier    string        `json:"tier"`    // "pro", "faceit-5" — set at conversion time
	Match   *UnifiedMatch `json:"match"`
}

// UnifiedMatch is the single intermediate representation of a CS2 demo.
// One conversion pass produces it; both go-cs-metrics and cs-demo-viewer consume it.
// All event slices are flat with round numbers embedded — both tools filter/group as needed.
type UnifiedMatch struct {
	DemoHash  string  `json:"demo_hash"`
	MapName   string  `json:"map"`
	MatchDate string  `json:"match_date"` // from .dem file mtime at conversion time
	MatchType string  `json:"match_type"`
	Tickrate  float64 `json:"tickrate"`

	// Players roster. Index into this slice == player idx used in viewer structs
	// (Frames, Grenades, Trails, Shots). SteamID is used in all metrics event structs.
	Players []UnifiedPlayer `json:"players"`

	// Round structure (used by both tools).
	Rounds []UnifiedRound `json:"rounds"`

	// Flat metrics event streams — go-cs-metrics reads all of these.
	// cs-demo-viewer reads only Kills (for positions and kill markers).
	Kills         []UnifiedKill         `json:"kills"`
	Damages       []UnifiedDamage       `json:"damages"`
	Flashes       []UnifiedFlash        `json:"flashes"`
	FirstSights   []UnifiedFirstSight   `json:"first_sights"`
	WeaponFires   []UnifiedWeaponFire   `json:"weapon_fires"`
	GrenadeEvents []UnifiedGrenadeEvent `json:"grenade_events,omitempty"`

	// Viewer-only streams — go-cs-metrics ignores these entirely.
	Frames   []UnifiedFrame        `json:"frames"`
	Bombs    []UnifiedBombAction   `json:"bombs"`
	Grenades []UnifiedGrenade      `json:"grenades"`
	Trails   []UnifiedGrenadeTrail `json:"trails"`
	Shots    []UnifiedShot         `json:"shots"` // deduplicated WeaponFire for muzzle flash
}

// UnifiedPlayer is the static identity of a match participant.
// Position in Match.Players defines the player idx used in all viewer structs.
type UnifiedPlayer struct {
	SteamID uint64 `json:"id,string"` // string encoding avoids JS uint64 precision loss
	Name    string `json:"name"`
}

// UnifiedRound holds metadata for a single round.
type UnifiedRound struct {
	Number        int  `json:"n"`
	StartTick     int  `json:"start_tick"`
	FreezeEndTick int  `json:"freeze_end_tick"`
	EndTick       int  `json:"end_tick"`
	WinnerTeam    Team `json:"winner"` // TeamCT | TeamT | TeamUnknown

	// Running score after this round resolves (viewer scoreboard).
	CTScore int `json:"ct_score"`
	TScore  int `json:"t_score"`

	// Metrics: per-player state at round end.
	BombPlantTick     int                            `json:"bomb_plant_tick"` // 0 = no plant
	PlayerEndState    map[uint64]PlayerRoundEndState `json:"player_end_state"`
	PlayerEquipValues map[uint64]int                 `json:"player_equip_values"` // USD at freeze-end
}

// UnifiedKill is used by both tools: metrics needs SteamIDs and flag fields;
// viewer needs kill positions for markers and scoreboard updates.
type UnifiedKill struct {
	Tick            int    `json:"tick"`
	Round           int    `json:"round"`
	KillerSteamID   uint64 `json:"killer,string"`
	VictimSteamID   uint64 `json:"victim,string"`
	AssisterSteamID uint64 `json:"assister,string,omitempty"` // 0 = no assist
	KillerTeam      Team   `json:"killer_team"`
	VictimTeam      Team   `json:"victim_team"`
	Weapon          string `json:"weapon"`

	IsHeadshot            bool `json:"hs,omitempty"`
	AssistedFlash         bool `json:"flash_assist,omitempty"`
	NoScope               bool `json:"no_scope,omitempty"`
	ThroughSmoke          bool `json:"through_smoke,omitempty"`
	AttackerBlind         bool `json:"attacker_blind,omitempty"`
	NearbyVictimTeammates int  `json:"nearby_teammates,omitempty"` // AWP death classifier

	// Positions — viewer kill markers; also available for future heatmaps.
	KillerX int `json:"kx"`
	KillerY int `json:"ky"`
	VictimX int `json:"vx"`
	VictimY int `json:"vy"`

	// Extended metrics-side death context (added after initial v1 format):
	// full Z + victim yaw. omitempty so old .csdem.gz files remain valid.
	KillerZ      int     `json:"kz,omitempty"`
	VictimZ      int     `json:"vz,omitempty"`
	VictimYawDeg float64 `json:"v_yaw,omitempty"`
}

// UnifiedDamage — go-cs-metrics only; viewer ignores.
type UnifiedDamage struct {
	Tick            int    `json:"tick"`
	Round           int    `json:"round"`
	AttackerSteamID uint64 `json:"attacker,string"`
	VictimSteamID   uint64 `json:"victim,string"`
	AttackerTeam    Team   `json:"attacker_team"`
	HealthDamage    int    `json:"hp_dmg"`
	Weapon          string `json:"weapon"`
	IsUtility       bool   `json:"utility,omitempty"`
	HitGroup        string `json:"hit_group"`
	VictimPos       Vec3   `json:"victim_pos"`
}

// UnifiedFlash — go-cs-metrics only (flash quality, KAST, blind-angle analytics).
// The VictimPos/Yaw/Pitch fields were added after initial v1 and use omitempty
// so older .csdem.gz files remain readable (flash_events for those will have
// zero blind angles).
type UnifiedFlash struct {
	Tick            int           `json:"tick"`
	Round           int           `json:"round"`
	AttackerSteamID uint64        `json:"attacker,string"`
	VictimSteamID   uint64        `json:"victim,string"`
	AttackerTeam    Team          `json:"attacker_team"`
	VictimTeam      Team          `json:"victim_team"`
	FlashDuration   time.Duration `json:"duration_ns"`
	VictimPos       Vec3          `json:"victim_pos"`         // zero Vec3 in older .csdem.gz
	VictimYawDeg    float64       `json:"v_yaw,omitempty"`
	VictimPitchDeg  float64       `json:"v_pitch,omitempty"`
}

// UnifiedFirstSight — go-cs-metrics only (crosshair placement).
type UnifiedFirstSight struct {
	Tick             int     `json:"tick"`
	Round            int     `json:"round"`
	ObserverID       uint64  `json:"observer,string"`
	EnemyID          uint64  `json:"enemy,string"`
	AngleDeg         float64 `json:"angle_deg"`
	PitchDeg         float64 `json:"pitch_deg"`
	YawDeg           float64 `json:"yaw_deg"`
	ObserverPitchDeg float64 `json:"obs_pitch_deg"`
	ObserverYawDeg   float64 `json:"obs_yaw_deg"`
}

// UnifiedWeaponFire — go-cs-metrics only (counter-strafe %, duel engine, TTK).
type UnifiedWeaponFire struct {
	Tick            int     `json:"tick"`
	Round           int     `json:"round"`
	ShooterID       uint64  `json:"shooter,string"`
	Weapon          string  `json:"weapon"`
	PitchDeg        float64 `json:"pitch_deg"`
	YawDeg          float64 `json:"yaw_deg"`
	AttackerPos     Vec3    `json:"pos"`
	HorizontalSpeed float64 `json:"h_speed"`
}

// UnifiedGrenadeEvent — go-cs-metrics only (lineup clustering, utility usage).
// Slim throw-to-land summary; viewer still uses the richer UnifiedGrenadeTrail.
type UnifiedGrenadeEvent struct {
	ThrowTick      int    `json:"t0"`
	EndTick        int    `json:"t1"`
	Round          int    `json:"round"`
	ThrowerSteamID uint64 `json:"thrower,string"`
	ThrowerTeam    Team   `json:"thrower_team"`
	GrenadeType    string `json:"type"` // "smoke" | "flash" | "he" | "molotov" | "decoy"
	ThrowPos       Vec3   `json:"throw_pos"`
	LandPos        Vec3   `json:"land_pos"`
}

// UnifiedFrame is one 16-tick position keyframe (viewer only).
// Sampled after freeze-end only; JS viewer interpolates to smooth 60 fps playback.
type UnifiedFrame struct {
	Tick   int                  `json:"tick"`
	Round  int                  `json:"round"`
	States []UnifiedPlayerState `json:"p"`
}

// UnifiedPlayerState encodes one player's state at a sampled tick.
// Serialised as a compact 10-element JSON array to minimise frame data size.
// flags bits: 0=dead, 1=T(vs CT), 2=bomb carrier, 3=has kevlar, 4=has helmet
// utility bits: 0=smoke, 1=HE, 2-3=flash count (0-2), 4=molotov, 5=decoy
type UnifiedPlayerState struct {
	Idx     int    // index into Match.Players
	Flags   int
	HP      int
	X, Y, Z int
	Yaw     int
	Weapon  string
	Utility int
	Money   int
}

// MarshalJSON serialises UnifiedPlayerState as a compact 10-element array.
func (ps UnifiedPlayerState) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{ps.Idx, ps.Flags, ps.HP, ps.X, ps.Y, ps.Z,
		ps.Yaw, ps.Weapon, ps.Utility, ps.Money})
}

// UnmarshalJSON deserialises a compact 10-element array back into UnifiedPlayerState.
func (ps *UnifiedPlayerState) UnmarshalJSON(data []byte) error {
	var arr [10]any
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	ps.Idx = int(toFloat64(arr[0]))
	ps.Flags = int(toFloat64(arr[1]))
	ps.HP = int(toFloat64(arr[2]))
	ps.X = int(toFloat64(arr[3]))
	ps.Y = int(toFloat64(arr[4]))
	ps.Z = int(toFloat64(arr[5]))
	ps.Yaw = int(toFloat64(arr[6]))
	if s, ok := arr[7].(string); ok {
		ps.Weapon = s
	}
	ps.Utility = int(toFloat64(arr[8]))
	ps.Money = int(toFloat64(arr[9]))
	return nil
}

func toFloat64(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// UnifiedBombAction — viewer bomb overlay.
// action: 0=plant_begin 1=planted 2=defuse_begin 3=defused 4=exploded 5=dropped 6=pickup
type UnifiedBombAction struct {
	Tick   int    `json:"tick"`
	Round  int    `json:"round"`
	Action int    `json:"action"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Site   string `json:"site,omitempty"`
}

// UnifiedGrenade — viewer grenade overlay.
// Type: 0=smoke 1=flash 2=HE 3=molotov 4=CT-smoke 5=T-smoke; EndTick=0 means instant.
type UnifiedGrenade struct {
	StartTick  int    `json:"t0"`
	EndTick    int    `json:"t1"`
	Round      int    `json:"round"`
	Type       int    `json:"type"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	ThrowerIdx int    `json:"thrower"` // index into Match.Players; -1 if unknown
}

// UnifiedGrenadeTrail — viewer throw arc. Max 80 sample points.
type UnifiedGrenadeTrail struct {
	StartTick  int      `json:"t0"`
	EndTick    int      `json:"t1"`
	Round      int      `json:"round"`
	Type       int      `json:"type"`
	ThrowerIdx int      `json:"thrower"` // index into Match.Players; -1 if unknown
	Points     [][3]int `json:"pts"`     // [tickOffset, x, y]
}

// UnifiedShot — deduplicated weapon-fire event for viewer muzzle-flash rendering.
// One entry per player per 16-tick window (pre-computed at conversion time).
type UnifiedShot struct {
	Tick  int `json:"tick"`
	Round int `json:"round"`
	Idx   int `json:"i"` // index into Match.Players
}

// ToRawMatch converts a UnifiedMatch back to a *RawMatch for the aggregator.
// Viewer-only fields (Frames, Bombs, Grenades, Trails, Shots) are discarded.
func (u *UnifiedMatch) ToRawMatch() *RawMatch {
	raw := &RawMatch{
		DemoHash:       u.DemoHash,
		MapName:        u.MapName,
		MatchDate:      u.MatchDate,
		MatchType:      u.MatchType,
		Tickrate:       u.Tickrate,
		TicksPerSecond: u.Tickrate,
		PlayerNames:    make(map[uint64]string, len(u.Players)),
		PlayerTeams:    make(map[uint64]Team, len(u.Players)),
	}

	// Build PlayerNames from the Players roster.
	for _, p := range u.Players {
		raw.PlayerNames[p.SteamID] = p.Name
	}

	// Convert rounds.
	raw.Rounds = make([]RawRound, len(u.Rounds))
	for i, r := range u.Rounds {
		raw.Rounds[i] = RawRound{
			Number:            r.Number,
			StartTick:         r.StartTick,
			FreezeEndTick:     r.FreezeEndTick,
			EndTick:           r.EndTick,
			WinnerTeam:        r.WinnerTeam,
			PlayerEndState:    r.PlayerEndState,
			PlayerEquipValues: r.PlayerEquipValues,
			BombPlantTick:     r.BombPlantTick,
		}
		// Build PlayerTeams from end-state; last round's value wins per player.
		for id, state := range r.PlayerEndState {
			raw.PlayerTeams[id] = state.Team
		}
	}

	// Convert kills.
	raw.Kills = make([]RawKill, len(u.Kills))
	for i, k := range u.Kills {
		raw.Kills[i] = RawKill{
			Tick:                  k.Tick,
			RoundNumber:           k.Round,
			KillerSteamID:         k.KillerSteamID,
			VictimSteamID:         k.VictimSteamID,
			AssisterSteamID:       k.AssisterSteamID,
			KillerTeam:            k.KillerTeam,
			VictimTeam:            k.VictimTeam,
			Weapon:                k.Weapon,
			IsHeadshot:            k.IsHeadshot,
			AssistedFlash:         k.AssistedFlash,
			NearbyVictimTeammates: k.NearbyVictimTeammates,
			KillerPos:             Vec3{X: float64(k.KillerX), Y: float64(k.KillerY), Z: float64(k.KillerZ)},
			VictimPos:             Vec3{X: float64(k.VictimX), Y: float64(k.VictimY), Z: float64(k.VictimZ)},
			VictimYawDeg:          k.VictimYawDeg,
		}
	}

	// Convert damages.
	raw.Damages = make([]RawDamage, len(u.Damages))
	for i, d := range u.Damages {
		raw.Damages[i] = RawDamage{
			Tick:            d.Tick,
			RoundNumber:     d.Round,
			AttackerSteamID: d.AttackerSteamID,
			VictimSteamID:   d.VictimSteamID,
			AttackerTeam:    d.AttackerTeam,
			HealthDamage:    d.HealthDamage,
			Weapon:          d.Weapon,
			IsUtility:       d.IsUtility,
			HitGroup:        d.HitGroup,
			VictimPos:       d.VictimPos,
		}
	}

	// Convert flashes.
	raw.Flashes = make([]RawFlash, len(u.Flashes))
	for i, f := range u.Flashes {
		raw.Flashes[i] = RawFlash{
			Tick:            f.Tick,
			RoundNumber:     f.Round,
			AttackerSteamID: f.AttackerSteamID,
			VictimSteamID:   f.VictimSteamID,
			AttackerTeam:    f.AttackerTeam,
			VictimTeam:      f.VictimTeam,
			FlashDuration:   f.FlashDuration,
			VictimPos:       f.VictimPos,
			VictimYawDeg:    f.VictimYawDeg,
			VictimPitchDeg:  f.VictimPitchDeg,
		}
	}

	// Convert first sights.
	raw.FirstSights = make([]RawFirstSight, len(u.FirstSights))
	for i, fs := range u.FirstSights {
		raw.FirstSights[i] = RawFirstSight{
			Tick:             fs.Tick,
			RoundNumber:      fs.Round,
			ObserverID:       fs.ObserverID,
			EnemyID:          fs.EnemyID,
			AngleDeg:         fs.AngleDeg,
			PitchDeg:         fs.PitchDeg,
			YawDeg:           fs.YawDeg,
			ObserverPitchDeg: fs.ObserverPitchDeg,
			ObserverYawDeg:   fs.ObserverYawDeg,
		}
	}

	// Convert weapon fires.
	raw.WeaponFires = make([]RawWeaponFire, len(u.WeaponFires))
	for i, wf := range u.WeaponFires {
		raw.WeaponFires[i] = RawWeaponFire{
			Tick:            wf.Tick,
			RoundNumber:     wf.Round,
			ShooterID:       wf.ShooterID,
			Weapon:          wf.Weapon,
			PitchDeg:        wf.PitchDeg,
			YawDeg:          wf.YawDeg,
			AttackerPos:     wf.AttackerPos,
			HorizontalSpeed: wf.HorizontalSpeed,
		}
	}

	// Convert grenade events (omitted in older .csdem.gz files).
	raw.Grenades = make([]RawGrenadeEvent, len(u.GrenadeEvents))
	for i, g := range u.GrenadeEvents {
		raw.Grenades[i] = RawGrenadeEvent{
			ThrowTick:      g.ThrowTick,
			EndTick:        g.EndTick,
			RoundNumber:    g.Round,
			ThrowerSteamID: g.ThrowerSteamID,
			ThrowerTeam:    g.ThrowerTeam,
			GrenadeType:    g.GrenadeType,
			ThrowPos:       g.ThrowPos,
			LandPos:        g.LandPos,
		}
	}

	return raw
}
