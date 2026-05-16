// Package roundquery provides the round data model and CEL-based query
// evaluation for the `query` command.
package roundquery

import "github.com/pable/go-cs-metrics/internal/csraw2"

// QueryPlayer is the slim per-player identity exposed to the HTML viewer.
type QueryPlayer struct {
	Slot    uint8
	SteamID uint64
	Name    string
}

// QueryRoundMeta is the round-level metadata exposed to the HTML viewer.
type QueryRoundMeta struct {
	Number        int
	WinnerTeam    string // "T" | "CT" | ""
	CTScore       int
	TScore        int
	FreezeEndTick int
	EndTick       int
}

// QueryFrameState is one player's sampled state at a single tick. Indices,
// flags, and field order match cs-demo-viewer's compact JSON tuple:
// [idx, flags, hp, x, y, z, yaw, weapon, utility, money].
//
// Flags bits: 0=dead, 1=T (vs CT), 2=bomb_carrier, 3=has_kevlar, 4=has_helmet.
// Utility bits: 0=smoke, 1=HE, 2-3=flash count (0..2), 4=molotov, 5=decoy.
type QueryFrameState struct {
	Slot    uint8
	Flags   int
	HP      int
	X, Y, Z int
	Yaw     int
	Weapon  string
	Utility int
	Money   int
}

// QueryFrame is one sampled tick with per-player state across the round.
type QueryFrame struct {
	Tick   int
	States []QueryFrameState
}

// QueryKill is the kill-marker row consumed by the HTML viewer.
type QueryKill struct {
	Tick           int
	KillerSlot     int8
	VictimSlot     int8
	AssisterSlot   int8 // -1 if no assist
	Weapon         string
	IsHeadshot     bool
	AssistedFlash  bool
	KillerX        int
	KillerY        int
	VictimX        int
	VictimY        int
}

// QueryGrenade is the grenade-marker row. Type uses the viewer encoding:
// 0=smoke, 1=flash, 2=HE, 3=molotov, 4=CT-smoke, 5=T-smoke.
type QueryGrenade struct {
	StartTick  int
	EndTick    int
	Type       int
	X, Y       int
	ThrowerIdx int // slot index; -1 if unknown
}

// QueryTrail is the in-flight projectile path; Points = [tickOffset, x, y].
// Type encoding matches QueryGrenade.
type QueryTrail struct {
	StartTick  int
	EndTick    int
	Type       int
	ThrowerIdx int
	Points     [][3]int
}

// QueryShot is one weapon-fire event used to render muzzle flashes.
type QueryShot struct {
	Tick int
	Slot uint8
}

// QueryBomb is one bomb action row. Action uses the viewer encoding:
// 0=plant_begin, 1=planted, 2=defuse_begin, 3=defused, 4=exploded.
type QueryBomb struct {
	Tick   int
	Action int
	X, Y   int
	Site   string
}

// QueryViewerData holds the raw per-round data needed by the 2D HTML viewer.
// Populated only when --html is requested.
type QueryViewerData struct {
	Players   []QueryPlayer
	RoundMeta QueryRoundMeta
	Tickrate  float64
	Frames    []QueryFrame
	Kills     []QueryKill
	Grenades  []QueryGrenade
	Trails    []QueryTrail
	Shots     []QueryShot
	Bombs     []QueryBomb
}

// RoundRecord holds all queryable statistics for a single round.
// Field names match the CEL variable names used in query expressions.
type RoundRecord struct {
	// Identity (not exposed to CEL; used for output only).
	MatchFile string
	Date      string

	// Queryable identity fields.
	Map      string // e.g. "de_inferno"
	RoundNum int    // 1-based

	// Outcome.
	Winner string // "T" | "CT" | ""

	// Buy type classification per side.
	TypeT  string // "pistol" | "eco" | "force" | "full"
	TypeCT string

	// Total equipment value per side (USD sum at freeze-end).
	EquipT  int
	EquipCT int

	// Utility thrown per side (count of grenades of each type).
	SmokesT    int
	SmokesCT   int
	FlashesT   int
	FlashesCT  int
	MolotovsT  int
	MolotovsCT int
	HEsT       int
	HEsCT      int

	// HE grenade damage dealt per side.
	HEDamageT  int
	HEDamageCT int

	// Flash-assisted kills (victim was blind when killed) per side.
	FlashKillsT  int
	FlashKillsCT int

	// Alive player counts.
	AliveStartT  int // alive at freeze-end (round start)
	AliveStartCT int
	AlivePlantT  int // alive at bomb plant tick; 0 if no plant
	AlivePlantCT int

	// Bomb events.
	Planted  bool
	Defused  bool
	Exploded bool

	// Which side got the first kill of the round.
	EntrySide string // "T" | "CT" | ""

	// Viewer data — populated only when --html is requested.
	ViewerData *QueryViewerData
}

// celVars returns the variable map consumed by the CEL evaluator.
func (r RoundRecord) celVars() map[string]any {
	return map[string]any{
		"map":            r.Map,
		"round_num":      int64(r.RoundNum),
		"winner":         r.Winner,
		"type_t":         r.TypeT,
		"type_ct":        r.TypeCT,
		"equip_t":        int64(r.EquipT),
		"equip_ct":       int64(r.EquipCT),
		"smokes_t":       int64(r.SmokesT),
		"smokes_ct":      int64(r.SmokesCT),
		"flashes_t":      int64(r.FlashesT),
		"flashes_ct":     int64(r.FlashesCT),
		"molotovs_t":     int64(r.MolotovsT),
		"molotovs_ct":    int64(r.MolotovsCT),
		"hes_t":          int64(r.HEsT),
		"hes_ct":         int64(r.HEsCT),
		"he_damage_t":    int64(r.HEDamageT),
		"he_damage_ct":   int64(r.HEDamageCT),
		"flash_kills_t":  int64(r.FlashKillsT),
		"flash_kills_ct": int64(r.FlashKillsCT),
		"alive_start_t":  int64(r.AliveStartT),
		"alive_start_ct": int64(r.AliveStartCT),
		"alive_plant_t":  int64(r.AlivePlantT),
		"alive_plant_ct": int64(r.AlivePlantCT),
		"planted":        r.Planted,
		"defused":        r.Defused,
		"exploded":       r.Exploded,
		"entry_side":     r.EntrySide,
	}
}

// Viewer grenade-type encoding shared with cs-demo-viewer.
const (
	ViewerGrenadeSmoke   = 0
	ViewerGrenadeFlash   = 1
	ViewerGrenadeHE      = 2
	ViewerGrenadeMolotov = 3
	ViewerGrenadeCTSmoke = 4
	ViewerGrenadeTSmoke  = 5
)

// Viewer bomb-action encoding shared with cs-demo-viewer.
const (
	ViewerBombPlantBegin  = 0
	ViewerBombPlanted     = 1
	ViewerBombDefuseBegin = 2
	ViewerBombDefused     = 3
	ViewerBombExploded    = 4
)

// teamLabel converts a csraw2.Team* constant to the "T"/"CT" string used in
// records and viewer output.
func teamLabel(t uint8) string {
	switch t {
	case csraw2.TeamT:
		return "T"
	case csraw2.TeamCT:
		return "CT"
	}
	return ""
}
