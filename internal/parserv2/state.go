package parserv2

import (
	"strconv"

	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"

	"github.com/pable/go-cs-metrics/internal/csraw2"
)

// firstSightPairKey deduplicates (observer, enemy) pairs within a round
// so each pair only emits a single FirstSight row per round.
type firstSightPairKey struct{ obsSlot, enemySlot uint8 }

// activeProjectile tracks an in-flight grenade projectile so we can emit
// ProjectileSample rows from the frame-walk loop. Cleared on
// GrenadeProjectileDestroy.
type activeProjectile struct {
	projectileID uint32
	grenadeType  uint8
	ownerSlot    int8
	roundNumber  int
}

// state holds everything mutated during a v2 parse — slot assignments,
// weapon-id table, projectile tracking, round-level scratch state, and the
// match being built.
type state struct {
	match *csraw2.Match

	// Player slot allocation. Slots are assigned in the order players are
	// first seen and remain stable for the rest of the match. A player who
	// disconnects keeps their slot; a fresh joiner gets the next free one.
	slotByID map[uint64]uint8
	nextSlot uint8

	// Weapon table. Built lazily as new EquipmentTypes are observed.
	weaponID   map[common.EquipmentType]uint8
	nextWeapon uint8

	// Current weapon held per player slot, used to drive equip_change rows
	// when the active weapon changes.
	currentWeapon map[uint8]uint8

	// Round-scoped state.
	roundNumber       int
	roundStartTick    int
	freezeEndTick     int
	bombPlantTick     int
	firstKillTick     int
	seenFirstSight    map[firstSightPairKey]bool

	// Projectile tracking — keyed by demoinfocs entity ID, which is unique
	// for the lifetime of an in-flight projectile (entity IDs are recycled
	// only after the entity is destroyed).
	nextProjectileID  uint32
	activeProjectiles map[int]*activeProjectile

	// Per-player last-event tick (for last_shot_tick_offset / last_damage_tick_offset).
	lastShotTick   map[uint8]int
	lastDamageTick map[uint8]int

	// Sampling parameters.
	tickrate        float64
	baselineEvery   int // ticks between baseline player samples
	projectileEvery int // ticks between projectile samples
}

func newState() *state {
	return &state{
		match:             &csraw2.Match{Header: csraw2.Header{WeaponTable: map[string]string{}}},
		slotByID:          map[uint64]uint8{},
		weaponID:          map[common.EquipmentType]uint8{},
		nextWeapon:        0,
		currentWeapon:     map[uint8]uint8{},
		seenFirstSight:    map[firstSightPairKey]bool{},
		nextProjectileID:  1,
		activeProjectiles: map[int]*activeProjectile{},
		lastShotTick:      map[uint8]int{},
		lastDamageTick:    map[uint8]int{},
	}
}

// maxPlayerSlots caps slot allocation. The visibility mask is uint16, so
// 16 is the natural ceiling; standard 5v5 matches only ever fill 10 slots
// even with mid-match substitutions reusing prior slots.
const maxPlayerSlots = 16

// slotFor returns a stable uint8 slot index for a steam id, allocating a
// new one (and recording the player in the header roster) on first sight.
// Returns false if we've already exhausted maxPlayerSlots — call sites
// should treat that as "skip this row" rather than panic.
func (s *state) slotFor(p *common.Player) (uint8, bool) {
	if p == nil || p.SteamID64 == 0 {
		return 0, false
	}
	if slot, ok := s.slotByID[p.SteamID64]; ok {
		return slot, true
	}
	if s.nextSlot >= maxPlayerSlots {
		return 0, false
	}
	slot := s.nextSlot
	s.slotByID[p.SteamID64] = slot
	s.nextSlot++

	s.match.Header.Players = append(s.match.Header.Players, csraw2.Player{
		Slot:         slot,
		SteamID:      strconv.FormatUint(p.SteamID64, 10),
		Name:         p.Name,
		StartingTeam: teamLabel(p.Team),
		IsBot:        p.IsBot,
	})
	return slot, true
}

// slotForOptional is slotFor's "may be world" variant: returns -1 (as int8)
// when the player is nil or unconnected, otherwise the assigned slot.
func (s *state) slotForOptional(p *common.Player) int8 {
	slot, ok := s.slotFor(p)
	if !ok {
		return -1
	}
	return int8(slot)
}

// weaponIDFor returns the v2 weapon_id for the given equipment type,
// allocating a fresh id (and adding it to the header weapon_table) on
// first sight. EqUnknown maps to 0.
func (s *state) weaponIDFor(t common.EquipmentType) uint8 {
	if t == common.EqUnknown {
		return 0
	}
	if id, ok := s.weaponID[t]; ok {
		return id
	}
	id := s.nextWeapon
	s.weaponID[t] = id
	s.nextWeapon++
	s.match.Header.WeaponTable[strconv.FormatUint(uint64(id), 10)] = weaponName(t)
	return id
}

// weaponName returns a stable lowercase identifier for an EquipmentType
// (the engine's String() preserved as-is, lowercased). Used as the value
// side of the weapon_table.
func weaponName(t common.EquipmentType) string {
	return t.String()
}
