// Package roundquery provides the round data model and CEL-based query
// evaluation for the `query` command.
package roundquery

import "github.com/pable/go-cs-metrics/internal/model"

// QueryViewerData holds the raw per-round data needed by the 2D HTML viewer.
// Populated only when --html is requested.
type QueryViewerData struct {
	Players   []model.UnifiedPlayer
	RoundMeta model.UnifiedRound
	Tickrate  float64
	Frames    []model.UnifiedFrame
	Kills     []model.UnifiedKill
	Grenades  []model.UnifiedGrenade
	Trails    []model.UnifiedGrenadeTrail
	Shots     []model.UnifiedShot
	Bombs     []model.UnifiedBombAction
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
