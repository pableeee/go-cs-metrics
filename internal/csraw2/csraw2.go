// Package csraw2 implements the CSRaw v2 intermediate format for parsed
// CS2 demo data. The on-disk format is a tar archive containing a
// header.json plus one parquet file per event stream; see
// docs/csraw-v2-spec.md for the canonical schema.
package csraw2

const (
	Version       = 2
	SchemaVersion = "2.0.0"
)

// File names inside the .csraw2.tar archive.
const (
	HeaderName = "header.json"

	StreamKills              = "kills.parquet"
	StreamDamages            = "damages.parquet"
	StreamWeaponFires        = "weapon_fires.parquet"
	StreamFlashes            = "flashes.parquet"
	StreamFirstSights        = "first_sights.parquet"
	StreamGrenadeThrows      = "grenade_throws.parquet"
	StreamGrenadeDetonations = "grenade_detonations.parquet"
	StreamBombActions        = "bomb_actions.parquet"
	StreamItemPickups        = "item_pickups.parquet"
	StreamItemDrops          = "item_drops.parquet"
	StreamEquipChanges       = "equip_changes.parquet"
	StreamChatCommands       = "chat_commands.parquet"
	StreamPlayerSamples      = "player_samples.parquet"
	StreamProjectileSamples  = "projectile_samples.parquet"
)

// Grenade type enum, used by GrenadeThrow / GrenadeDetonate / ProjectileSample.
const (
	GrenadeSmoke    uint8 = 1
	GrenadeFlash    uint8 = 2
	GrenadeHE       uint8 = 3
	GrenadeMolotov  uint8 = 4 // includes incendiary
	GrenadeDecoy    uint8 = 5
)

// Bomb action enum.
const (
	BombActionPlantBegin     uint8 = 1
	BombActionPlantComplete  uint8 = 2
	BombActionDefuseBegin    uint8 = 3
	BombActionDefuseComplete uint8 = 4
	BombActionExplode        uint8 = 5
)

// Bomb site enum (uint8). 255 means N/A (e.g. for explode rows).
const (
	BombSiteA   uint8 = 0
	BombSiteB   uint8 = 1
	BombSiteNA  uint8 = 255
)

// Hit group enum, mirrors demoinfocs HitGroup values.
const (
	HitGroupGeneric  uint8 = 0
	HitGroupHead     uint8 = 1
	HitGroupChest    uint8 = 2
	HitGroupStomach  uint8 = 3
	HitGroupLeftArm  uint8 = 4
	HitGroupRightArm uint8 = 5
	HitGroupLeftLeg  uint8 = 6
	HitGroupRightLeg uint8 = 7
	HitGroupNeck     uint8 = 8
	HitGroupGear     uint8 = 10
)

// Player flag bit positions (PlayerSample.Flags is a bitfield).
const (
	FlagAlive         = 1 << 0
	FlagHasKit        = 1 << 1
	FlagHasHelmet     = 1 << 2
	FlagIsScoped      = 1 << 3
	FlagIsReloading   = 1 << 4
	FlagIsWalking     = 1 << 5
	FlagIsDucking     = 1 << 6
	FlagIsOnGround    = 1 << 7
	FlagIsInAir       = 1 << 8
	FlagIsBombCarrier = 1 << 9
	FlagIsPlanting    = 1 << 10
	FlagIsDefusing    = 1 << 11
	FlagIsInSmoke     = 1 << 12
	FlagIsInFlames    = 1 << 13
	FlagIsJumping     = 1 << 14
	FlagIsBlind       = 1 << 15
)

// ProjectileSample.Kind enum.
const (
	ProjectileKindGrenade uint8 = 0
	ProjectileKindBomb    uint8 = 1
)

// Bomb subtype values for ProjectileSample.Subtype when Kind=ProjectileKindBomb.
const (
	BombStateIdle      uint8 = 0
	BombStatePlanted   uint8 = 1
	BombStateDefusing  uint8 = 2
	BombStateExploded  uint8 = 3
)

// PlayerSample.DensityTier enum.
const (
	DensityBaseline    uint8 = 0
	DensityEventWindow uint8 = 1
)
