package csraw2

// Header is the top-level metadata for one converted match. Serialised as
// header.json inside the .csraw2.tar archive. All time fields are RFC 3339;
// match_date is the calendar date in YYYY-MM-DD.
type Header struct {
	CSRawVersion       int    `json:"csraw_version"`
	CSRawSchemaVersion string `json:"csraw_schema_version"`
	CS2ProtocolVersion int    `json:"cs2_protocol_version,omitempty"`
	Writer             string `json:"writer"`
	DemoinfocsVersion  string `json:"writer_demoinfocs_version,omitempty"`

	DemoHash           string `json:"demo_hash"`
	QuickHash          string `json:"quick_hash,omitempty"`
	SourceDemSizeBytes int64  `json:"source_dem_size_bytes,omitempty"`
	SourceDemMtime     string `json:"source_dem_mtime,omitempty"`

	Tier      string  `json:"tier"`
	MatchType string  `json:"match_type"`
	Map       string  `json:"map"`
	Tickrate  float64 `json:"tickrate"`
	MatchDate string  `json:"match_date"`

	Sampling Sampling `json:"sampling"`

	Players []Player `json:"players"`
	Rounds  []Round  `json:"rounds"`

	// WeaponTable maps the uint8 weapon_id used in event/sample rows to a
	// stable lowercase weapon name. Keys are stringified ids ("0", "1", ...)
	// so the JSON object is portable; readers parse them back to uint8.
	WeaponTable map[string]string `json:"weapon_table"`
}

// Sampling captures the rules under which player_samples and
// projectile_samples were emitted, so future readers know exactly what
// resolution is available without inspecting the parquet contents.
type Sampling struct {
	BaselineHz             int  `json:"baseline_hz"`
	EventWindowHz          int  `json:"event_window_hz"`
	EventWindowRadiusTicks int  `json:"event_window_radius_ticks"`
	FreezeTimeSampled      bool `json:"freeze_time_sampled"`
	ProjectileHz           int  `json:"projectile_hz"`
}

// Player is a static identity row. Slot is 0..9 and matches the
// player_slot column used in every event and sample row.
type Player struct {
	Slot         uint8  `json:"slot"`
	SteamID      string `json:"steam_id"` // string to avoid JS uint64 precision loss
	Name         string `json:"name"`
	StartingTeam string `json:"starting_team"` // "T" | "CT"
	IsBot        bool   `json:"is_bot,omitempty"`
}

// Round is the metadata for one round.
type Round struct {
	N             int    `json:"n"`
	StartTick     int    `json:"start_tick"`
	FreezeEndTick int    `json:"freeze_end_tick"`
	FirstKillTick int    `json:"first_kill_tick,omitempty"`
	BombPlantTick int    `json:"bomb_plant_tick"` // 0 if no plant
	EndTick       int    `json:"end_tick"`
	Winner        string `json:"winner"`               // "T" | "CT" | "DRAW"
	WinReason     string `json:"win_reason,omitempty"` // engine reason string
	CTScoreAfter  int    `json:"ct_score_after"`
	TScoreAfter   int    `json:"t_score_after"`
	Phase         string `json:"phase"` // "regulation" | "ot1" | "ot2" | …
}

// ── Event row types ─────────────────────────────────────────────────────────
// Each event type lives in its own parquet stream inside the archive. All
// position columns are int16 in Hammer units (1u ≈ 1.905 cm). View angles
// are int16 quantised to 1/100°. Velocities are int16 in u/s.

// Kill is one row per kill event.
type Kill struct {
	Tick                      int32  `parquet:"tick"`
	Round                     int16  `parquet:"round"`
	KillerSlot                int8   `parquet:"killer_slot"` // -1 = world / suicide
	VictimSlot                uint8  `parquet:"victim_slot"`
	AssisterSlot              int8   `parquet:"assister_slot"` // -1 = none
	WeaponID                  uint8  `parquet:"weapon_id"`
	IsHeadshot                bool   `parquet:"is_headshot"`
	IsWallbang                bool   `parquet:"is_wallbang"`
	PenetratedCount           uint8  `parquet:"penetrated_count"`
	IsNoScope                 bool   `parquet:"is_no_scope"`
	IsThroughSmoke            bool   `parquet:"is_through_smoke"`
	IsAttackerBlind           bool   `parquet:"is_attacker_blind"`
	FlashAssist               bool   `parquet:"flash_assist"`
	AssistedFlashAttackerSlot int8   `parquet:"assisted_flash_attacker_slot"`
	NearbyVictimTeammates     uint8  `parquet:"nearby_victim_teammates"`
	KillerPosX                int16  `parquet:"killer_pos_x"`
	KillerPosY                int16  `parquet:"killer_pos_y"`
	KillerPosZ                int16  `parquet:"killer_pos_z"`
	VictimPosX                int16  `parquet:"victim_pos_x"`
	VictimPosY                int16  `parquet:"victim_pos_y"`
	VictimPosZ                int16  `parquet:"victim_pos_z"`
	KillerYawDeg              int16  `parquet:"killer_yaw_deg"`
	KillerPitchDeg            int16  `parquet:"killer_pitch_deg"`
	VictimYawDeg              int16  `parquet:"victim_yaw_deg"`
	VictimPitchDeg            int16  `parquet:"victim_pitch_deg"`
	VictimViewDistance        uint16 `parquet:"victim_view_distance"`
	KillerActiveWeaponID      uint8  `parquet:"killer_active_weapon_id"`
}

// Damage is one row per player_hurt event.
type Damage struct {
	Tick            int32  `parquet:"tick"`
	Round           int16  `parquet:"round"`
	AttackerSlot    int8   `parquet:"attacker_slot"` // -1 = world / fall damage
	VictimSlot      uint8  `parquet:"victim_slot"`
	WeaponID        uint8  `parquet:"weapon_id"`
	HealthDamage    uint16 `parquet:"health_damage"`
	ArmorDamage     uint16 `parquet:"armor_damage"`
	PreDamageHP     uint8  `parquet:"pre_damage_hp"`
	PreDamageArmor  uint8  `parquet:"pre_damage_armor"`
	PostDamageHP    uint8  `parquet:"post_damage_hp"`
	PostDamageArmor uint8  `parquet:"post_damage_armor"`
	HitGroup        uint8  `parquet:"hit_group"`
	IsUtility       bool   `parquet:"is_utility"`
	AttackerPosX    int16  `parquet:"attacker_pos_x"`
	AttackerPosY    int16  `parquet:"attacker_pos_y"`
	AttackerPosZ    int16  `parquet:"attacker_pos_z"`
	VictimPosX      int16  `parquet:"victim_pos_x"`
	VictimPosY      int16  `parquet:"victim_pos_y"`
	VictimPosZ      int16  `parquet:"victim_pos_z"`
}

// WeaponFire is one row per shot fired (continuous fire = many rows).
type WeaponFire struct {
	Tick          int32 `parquet:"tick"`
	Round         int16 `parquet:"round"`
	ShooterSlot   uint8 `parquet:"shooter_slot"`
	WeaponID      uint8 `parquet:"weapon_id"`
	PosX          int16 `parquet:"pos_x"`
	PosY          int16 `parquet:"pos_y"`
	PosZ          int16 `parquet:"pos_z"`
	YawDeg        int16 `parquet:"yaw_deg"`
	PitchDeg      int16 `parquet:"pitch_deg"`
	VelX          int16 `parquet:"vel_x"`
	VelY          int16 `parquet:"vel_y"`
	VelZ          int16 `parquet:"vel_z"`
	IsScoped      bool  `parquet:"is_scoped"`
	ClipAmmoAfter uint8 `parquet:"clip_ammo_after"`
	IsSilenced    bool  `parquet:"is_silenced"`
	RecoilIndex   uint8 `parquet:"recoil_index"`
}

// Flash is one row per effective flash (positive duration).
type Flash struct {
	Tick               int32  `parquet:"tick"`
	Round              int16  `parquet:"round"`
	AttackerSlot       uint8  `parquet:"attacker_slot"`
	VictimSlot         uint8  `parquet:"victim_slot"`
	FlashDurationMS    uint16 `parquet:"flash_duration_ms"`
	VictimPosX         int16  `parquet:"victim_pos_x"`
	VictimPosY         int16  `parquet:"victim_pos_y"`
	VictimPosZ         int16  `parquet:"victim_pos_z"`
	VictimYawDeg       int16  `parquet:"victim_yaw_deg"`
	VictimPitchDeg     int16  `parquet:"victim_pitch_deg"`
	VictimThroughSmoke bool   `parquet:"victim_through_smoke"`
}

// FirstSight is one row per crosshair-placement event.
type FirstSight struct {
	Tick              int32 `parquet:"tick"`
	Round             int16 `parquet:"round"`
	ObserverSlot      uint8 `parquet:"observer_slot"`
	EnemySlot         uint8 `parquet:"enemy_slot"`
	AngleDeg          int16 `parquet:"angle_deg"`
	PitchDeg          int16 `parquet:"pitch_deg"`
	YawDeg            int16 `parquet:"yaw_deg"`
	ObserverPitchDeg  int16 `parquet:"observer_pitch_deg"`
	ObserverYawDeg    int16 `parquet:"observer_yaw_deg"`
	ObserverPosX      int16 `parquet:"observer_pos_x"`
	ObserverPosY      int16 `parquet:"observer_pos_y"`
	ObserverPosZ      int16 `parquet:"observer_pos_z"`
	EnemyPosX         int16 `parquet:"enemy_pos_x"`
	EnemyPosY         int16 `parquet:"enemy_pos_y"`
	EnemyPosZ         int16 `parquet:"enemy_pos_z"`
	WasAlreadyVisible bool  `parquet:"was_already_visible"`
}

// GrenadeThrow is one row per grenade throw. ProjectileID joins to the
// matching GrenadeDetonate row and to ProjectileSample rows during flight.
type GrenadeThrow struct {
	Tick          int32  `parquet:"tick"`
	Round         int16  `parquet:"round"`
	ThrowerSlot   uint8  `parquet:"thrower_slot"`
	GrenadeType   uint8  `parquet:"grenade_type"`
	ThrowPosX     int16  `parquet:"throw_pos_x"`
	ThrowPosY     int16  `parquet:"throw_pos_y"`
	ThrowPosZ     int16  `parquet:"throw_pos_z"`
	ThrowYawDeg   int16  `parquet:"throw_yaw_deg"`
	ThrowPitchDeg int16  `parquet:"throw_pitch_deg"`
	ThrowVelX     int16  `parquet:"throw_vel_x"`
	ThrowVelY     int16  `parquet:"throw_vel_y"`
	ThrowVelZ     int16  `parquet:"throw_vel_z"`
	IsJumpThrow   bool   `parquet:"is_jumpthrow"`
	ProjectileID  uint32 `parquet:"projectile_id"`
}

// GrenadeDetonate is one row per smoke pop / HE detonate / flash detonate /
// molotov ignition.
type GrenadeDetonate struct {
	Tick         int32  `parquet:"tick"`
	Round        int16  `parquet:"round"`
	ProjectileID uint32 `parquet:"projectile_id"`
	GrenadeType  uint8  `parquet:"grenade_type"`
	PosX         int16  `parquet:"pos_x"`
	PosY         int16  `parquet:"pos_y"`
	PosZ         int16  `parquet:"pos_z"`
}

// BombAction covers plant_begin, plant_complete, defuse_begin,
// defuse_complete, and explode events. Site is BombSiteNA for explode rows.
type BombAction struct {
	Tick       int32 `parquet:"tick"`
	Round      int16 `parquet:"round"`
	Action     uint8 `parquet:"action"`
	PlayerSlot int8  `parquet:"player_slot"` // -1 for explode
	PosX       int16 `parquet:"pos_x"`
	PosY       int16 `parquet:"pos_y"`
	PosZ       int16 `parquet:"pos_z"`
	Site       uint8 `parquet:"site"`
	HadKit     bool  `parquet:"had_kit"`
}

// ItemPickup is one row per pickup of a weapon, kit, armor, grenade, or
// dropped C4.
type ItemPickup struct {
	Tick       int32 `parquet:"tick"`
	Round      int16 `parquet:"round"`
	PlayerSlot uint8 `parquet:"player_slot"`
	ItemID     uint8 `parquet:"item_id"` // weapon_id or special id
	PosX       int16 `parquet:"pos_x"`
	PosY       int16 `parquet:"pos_y"`
	PosZ       int16 `parquet:"pos_z"`
	FromSlot   int8  `parquet:"from_slot"` // -1 = floor / spawn
}

// ItemDrop is one row per item drop (death drop, manual drop, c4 drop).
type ItemDrop struct {
	Tick       int32 `parquet:"tick"`
	Round      int16 `parquet:"round"`
	PlayerSlot uint8 `parquet:"player_slot"`
	ItemID     uint8 `parquet:"item_id"`
	PosX       int16 `parquet:"pos_x"`
	PosY       int16 `parquet:"pos_y"`
	PosZ       int16 `parquet:"pos_z"`
}

// EquipChange is one row per active-weapon switch.
type EquipChange struct {
	Tick         int32 `parquet:"tick"`
	Round        int16 `parquet:"round"`
	PlayerSlot   uint8 `parquet:"player_slot"`
	WeaponID     uint8 `parquet:"weapon_id"`
	PrevWeaponID uint8 `parquet:"prev_weapon_id"`
}

// ChatCommand is one row per chat / radio command.
type ChatCommand struct {
	Tick       int32  `parquet:"tick"`
	Round      int16  `parquet:"round"`
	PlayerSlot uint8  `parquet:"player_slot"`
	Text       string `parquet:"text"`
	IsTeamOnly bool   `parquet:"is_team_only"`
}

// PlayerSample is one tick-sampled state row for one player.
// Sampling rules and tier definitions are documented in the spec.
type PlayerSample struct {
	Tick                 int32  `parquet:"tick"`
	Round                int16  `parquet:"round"`
	PlayerSlot           uint8  `parquet:"player_slot"`
	DensityTier          uint8  `parquet:"density_tier"`
	PosX                 int16  `parquet:"pos_x"`
	PosY                 int16  `parquet:"pos_y"`
	PosZ                 int16  `parquet:"pos_z"`
	YawDeg               int16  `parquet:"yaw_deg"`
	PitchDeg             int16  `parquet:"pitch_deg"`
	VelX                 int16  `parquet:"vel_x"`
	VelY                 int16  `parquet:"vel_y"`
	VelZ                 int16  `parquet:"vel_z"`
	HP                   uint8  `parquet:"hp"`
	Armor                uint8  `parquet:"armor"`
	Flags                uint16 `parquet:"flags"`
	ActiveWeaponID       uint8  `parquet:"active_weapon_id"`
	ClipAmmo             uint8  `parquet:"clip_ammo"`
	ReserveAmmo          uint16 `parquet:"reserve_ammo"`
	FlashRemainingMS     uint16 `parquet:"flash_remaining_ms"`
	Money                uint16 `parquet:"money"`
	EquipValue           uint16 `parquet:"equip_value"`
	VisibleEnemiesMask   uint16 `parquet:"visible_enemies_mask"`
	LastShotTickOffset   uint8  `parquet:"last_shot_tick_offset"`
	LastDamageTickOffset uint8  `parquet:"last_damage_tick_offset"`
}

// ProjectileSample is one tick-sampled state row for an in-flight grenade
// or the bomb. FuseRemainingMS is -1 when N/A; DefuseProgressPct is 0 unless
// Kind=ProjectileKindBomb and Subtype=BombStateDefusing.
type ProjectileSample struct {
	Tick              int32  `parquet:"tick"`
	Round             int16  `parquet:"round"`
	ProjectileID      uint32 `parquet:"projectile_id"`
	Kind              uint8  `parquet:"kind"`
	Subtype           uint8  `parquet:"subtype"`
	OwnerSlot         int8   `parquet:"owner_slot"`
	PosX              int16  `parquet:"pos_x"`
	PosY              int16  `parquet:"pos_y"`
	PosZ              int16  `parquet:"pos_z"`
	VelX              int16  `parquet:"vel_x"`
	VelY              int16  `parquet:"vel_y"`
	VelZ              int16  `parquet:"vel_z"`
	FuseRemainingMS   int16  `parquet:"fuse_remaining_ms"`
	DefuseProgressPct uint8  `parquet:"defuse_progress_pct"`
}

// Match aggregates everything that goes into one .csraw2.tar archive.
// All slices may be nil/empty; the writer still emits a parquet file with
// the schema for each stream so readers always see a consistent layout.
type Match struct {
	Header             Header
	Kills              []Kill
	Damages            []Damage
	WeaponFires        []WeaponFire
	Flashes            []Flash
	FirstSights        []FirstSight
	GrenadeThrows      []GrenadeThrow
	GrenadeDetonations []GrenadeDetonate
	BombActions        []BombAction
	ItemPickups        []ItemPickup
	ItemDrops          []ItemDrop
	EquipChanges       []EquipChange
	ChatCommands       []ChatCommand
	PlayerSamples      []PlayerSample
	ProjectileSamples  []ProjectileSample
}
