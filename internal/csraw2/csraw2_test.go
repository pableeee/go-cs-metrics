package csraw2

import (
	"bytes"
	"reflect"
	"testing"
)

// TestRoundTrip writes a synthetic Match with at least one row in every
// event stream + samples, reads it back, and checks every slice survived
// the parquet/zstd/tar round-trip byte-for-byte.
func TestRoundTrip(t *testing.T) {
	src := newSyntheticMatch()

	var buf bytes.Buffer
	if err := Write(&buf, src); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !reflect.DeepEqual(src.Header, got.Header) {
		t.Errorf("header mismatch:\n want: %+v\n got:  %+v", src.Header, got.Header)
	}

	checkSlice(t, "kills", src.Kills, got.Kills)
	checkSlice(t, "damages", src.Damages, got.Damages)
	checkSlice(t, "weapon_fires", src.WeaponFires, got.WeaponFires)
	checkSlice(t, "flashes", src.Flashes, got.Flashes)
	checkSlice(t, "first_sights", src.FirstSights, got.FirstSights)
	checkSlice(t, "grenade_throws", src.GrenadeThrows, got.GrenadeThrows)
	checkSlice(t, "grenade_detonations", src.GrenadeDetonations, got.GrenadeDetonations)
	checkSlice(t, "bomb_actions", src.BombActions, got.BombActions)
	checkSlice(t, "item_pickups", src.ItemPickups, got.ItemPickups)
	checkSlice(t, "item_drops", src.ItemDrops, got.ItemDrops)
	checkSlice(t, "equip_changes", src.EquipChanges, got.EquipChanges)
	checkSlice(t, "chat_commands", src.ChatCommands, got.ChatCommands)
	checkSlice(t, "player_samples", src.PlayerSamples, got.PlayerSamples)
	checkSlice(t, "projectile_samples", src.ProjectileSamples, got.ProjectileSamples)
}

// TestEmptyStreams confirms that a Match with no event rows still writes
// and reads back cleanly — important because real demos have empty streams
// (no chat, no decoys, etc.) and we don't want to special-case those.
func TestEmptyStreams(t *testing.T) {
	src := &Match{Header: minimalHeader()}

	var buf bytes.Buffer
	if err := Write(&buf, src); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(src.Header, got.Header) {
		t.Errorf("header mismatch")
	}
	if len(got.Kills) != 0 || len(got.Damages) != 0 || len(got.PlayerSamples) != 0 {
		t.Errorf("expected empty streams, got kills=%d damages=%d samples=%d",
			len(got.Kills), len(got.Damages), len(got.PlayerSamples))
	}
}

// TestVersionMismatch ensures readers reject archives written by an
// incompatible major version rather than silently producing garbage.
func TestVersionMismatch(t *testing.T) {
	src := &Match{Header: minimalHeader()}
	src.Header.CSRawVersion = 99

	var buf bytes.Buffer
	if err := Write(&buf, src); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Read(bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatalf("expected version mismatch error, got nil")
	}
}

func checkSlice[T any](t *testing.T, name string, want, got []T) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("%s: length mismatch: want %d got %d", name, len(want), len(got))
		return
	}
	for i := range want {
		if !reflect.DeepEqual(want[i], got[i]) {
			t.Errorf("%s[%d] mismatch:\n want: %+v\n got:  %+v", name, i, want[i], got[i])
		}
	}
}

func minimalHeader() Header {
	return Header{
		CSRawVersion:       Version,
		CSRawSchemaVersion: SchemaVersion,
		Writer:             "csraw2-test",
		DemoHash:           "sha256:test",
		Tier:               "pro",
		MatchType:          "competitive",
		Map:                "de_nuke",
		Tickrate:           64,
		MatchDate:          "2026-04-28",
		Sampling: Sampling{
			BaselineHz:             16,
			EventWindowHz:          64,
			EventWindowRadiusTicks: 32,
			FreezeTimeSampled:      true,
			ProjectileHz:           32,
		},
		Players: []Player{
			{Slot: 0, SteamID: "76561198000000001", Name: "p0", StartingTeam: "T"},
			{Slot: 1, SteamID: "76561198000000002", Name: "p1", StartingTeam: "CT"},
		},
		Rounds: []Round{
			{N: 1, StartTick: 100, FreezeEndTick: 200, EndTick: 1000,
				Winner: "T", CTScoreAfter: 0, TScoreAfter: 1, Phase: "regulation"},
		},
		WeaponTable: map[string]string{"0": "knife", "1": "glock", "7": "ak47"},
	}
}

func newSyntheticMatch() *Match {
	return &Match{
		Header: minimalHeader(),
		Kills: []Kill{
			{
				Tick: 350, Round: 1, KillerSlot: 0, VictimSlot: 1, AssisterSlot: -1,
				WeaponID: 7, IsHeadshot: true, IsWallbang: false, PenetratedCount: 0,
				NearbyVictimTeammates: 2,
				KillerPosX:            100, KillerPosY: 200, KillerPosZ: 64,
				VictimPosX:            110, VictimPosY: 210, VictimPosZ: 64,
				KillerYawDeg:          4500, KillerPitchDeg: -100,
				VictimYawDeg:          -13500, VictimPitchDeg: 200,
				VictimViewDistance:    150, KillerActiveWeaponID: 7,
			},
		},
		Damages: []Damage{
			{
				Tick: 348, Round: 1, AttackerSlot: 0, VictimSlot: 1, WeaponID: 7,
				HealthDamage: 36, ArmorDamage: 9, PreDamageHP: 100, PostDamageHP: 64,
				PreDamageArmor: 100, PostDamageArmor: 91, HitGroup: HitGroupChest,
				AttackerPosX: 100, VictimPosX: 110,
			},
		},
		WeaponFires: []WeaponFire{
			{Tick: 345, Round: 1, ShooterSlot: 0, WeaponID: 7,
				PosX: 100, PosY: 200, YawDeg: 4500, ClipAmmoAfter: 29,
				VelX: -34, VelY: 0, RecoilIndex: 0},
		},
		Flashes: []Flash{
			{Tick: 320, Round: 1, AttackerSlot: 0, VictimSlot: 1,
				FlashDurationMS: 1850, VictimPosX: 110, VictimPosY: 210,
				VictimYawDeg: -13500},
		},
		FirstSights: []FirstSight{
			{Tick: 340, Round: 1, ObserverSlot: 0, EnemySlot: 1,
				AngleDeg: 12, YawDeg: 4500, ObserverYawDeg: 4488,
				ObserverPosX: 100, EnemyPosX: 110, WasAlreadyVisible: false},
		},
		GrenadeThrows: []GrenadeThrow{
			{Tick: 250, Round: 1, ThrowerSlot: 0, GrenadeType: GrenadeFlash,
				ThrowPosX: 90, ThrowPosY: 190, ThrowVelX: 100, ThrowVelY: 50,
				IsJumpThrow: true, ProjectileID: 1001},
		},
		GrenadeDetonations: []GrenadeDetonate{
			{Tick: 290, Round: 1, ProjectileID: 1001, GrenadeType: GrenadeFlash,
				PosX: 105, PosY: 205, PosZ: 70},
		},
		BombActions: []BombAction{
			{Tick: 600, Round: 1, Action: BombActionPlantBegin, PlayerSlot: 0,
				PosX: 200, PosY: 300, Site: BombSiteA},
			{Tick: 720, Round: 1, Action: BombActionPlantComplete, PlayerSlot: 0,
				PosX: 200, PosY: 300, Site: BombSiteA},
		},
		ItemPickups: []ItemPickup{
			{Tick: 210, Round: 1, PlayerSlot: 0, ItemID: 7, PosX: 95, FromSlot: -1},
		},
		ItemDrops: []ItemDrop{
			{Tick: 350, Round: 1, PlayerSlot: 1, ItemID: 7, PosX: 110},
		},
		EquipChanges: []EquipChange{
			{Tick: 215, Round: 1, PlayerSlot: 0, WeaponID: 7, PrevWeaponID: 0},
		},
		ChatCommands: []ChatCommand{
			{Tick: 225, Round: 1, PlayerSlot: 0, Text: "going A", IsTeamOnly: true},
		},
		PlayerSamples: []PlayerSample{
			{Tick: 200, Round: 1, PlayerSlot: 0, DensityTier: DensityBaseline,
				PosX: 90, PosY: 190, PosZ: 64, YawDeg: 4500, HP: 100, Armor: 100,
				Flags: FlagAlive | FlagHasHelmet, ActiveWeaponID: 7,
				ClipAmmo: 30, ReserveAmmo: 90, Money: 800, EquipValue: 4700,
				VisibleEnemiesMask: 0b0010, LastShotTickOffset: 255, LastDamageTickOffset: 255},
			{Tick: 348, Round: 1, PlayerSlot: 1, DensityTier: DensityEventWindow,
				PosX: 110, PosY: 210, PosZ: 64, YawDeg: -13500, HP: 100, Armor: 100,
				Flags: FlagAlive | FlagHasHelmet | FlagIsBlind,
				ActiveWeaponID:       7, ClipAmmo: 30, ReserveAmmo: 90,
				FlashRemainingMS:     1500, Money: 3250, EquipValue: 4750,
				VisibleEnemiesMask:   0,
				LastShotTickOffset:   255, LastDamageTickOffset: 0},
		},
		ProjectileSamples: []ProjectileSample{
			{Tick: 260, Round: 1, ProjectileID: 1001, Kind: ProjectileKindGrenade,
				Subtype: GrenadeFlash, OwnerSlot: 0,
				PosX: 95, PosY: 195, PosZ: 70, VelX: 100, VelY: 50, VelZ: 30,
				FuseRemainingMS: 1500},
		},
	}
}
