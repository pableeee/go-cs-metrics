package csraw2bridge

import (
	"testing"
	"time"

	"github.com/pable/go-cs-metrics/internal/csraw2"
	"github.com/pable/go-cs-metrics/internal/model"
)

func TestToRawMatch_HappyPath(t *testing.T) {
	m := newSyntheticMatch()
	raw, err := ToRawMatch(m)
	if err != nil {
		t.Fatalf("ToRawMatch: %v", err)
	}

	// Top-level metadata.
	if raw.MapName != "de_nuke" {
		t.Errorf("MapName=%q want de_nuke", raw.MapName)
	}
	if raw.DemoHash != "abc123" {
		t.Errorf("DemoHash=%q (sha256 prefix should be stripped) want abc123", raw.DemoHash)
	}
	if raw.Tickrate != 64 {
		t.Errorf("Tickrate=%v want 64", raw.Tickrate)
	}

	// Roster: PlayerNames and PlayerTeams populated from header + samples.
	if name := raw.PlayerNames[1001]; name != "alice" {
		t.Errorf("PlayerNames[1001]=%q want alice", name)
	}
	if raw.PlayerTeams[1001] != model.TeamT {
		t.Errorf("PlayerTeams[1001]=%v want T", raw.PlayerTeams[1001])
	}

	// Single round propagated; PlayerEndState alive=true (sample HP=100),
	// equip values from the freeze-end sample.
	if len(raw.Rounds) != 1 {
		t.Fatalf("rounds=%d want 1", len(raw.Rounds))
	}
	r := raw.Rounds[0]
	if r.WinnerTeam != model.TeamT {
		t.Errorf("winner=%v want T", r.WinnerTeam)
	}
	if r.PlayerEndState[1001].IsAlive != true {
		t.Errorf("alice IsAlive=%v want true", r.PlayerEndState[1001].IsAlive)
	}
	if r.PlayerEquipValues[1001] != 4750 {
		t.Errorf("alice equip=%d want 4750", r.PlayerEquipValues[1001])
	}

	// Kill conversion: weapon_id=7 → "ak47", yaw -13500 (1/100°) → 225°.
	if len(raw.Kills) != 1 {
		t.Fatalf("kills=%d want 1", len(raw.Kills))
	}
	k := raw.Kills[0]
	if k.Weapon != "ak47" {
		t.Errorf("kill weapon=%q want ak47", k.Weapon)
	}
	if k.KillerSteamID != 1001 || k.VictimSteamID != 1002 {
		t.Errorf("kill steam ids=(%d,%d) want (1001,1002)", k.KillerSteamID, k.VictimSteamID)
	}
	if k.VictimYawDeg != 225 {
		t.Errorf("victim yaw=%v want 225 (-135 wrapped to 0–360)", k.VictimYawDeg)
	}
	if k.KillerTeam != model.TeamT || k.VictimTeam != model.TeamCT {
		t.Errorf("kill teams=(%v,%v) want (T,CT)", k.KillerTeam, k.VictimTeam)
	}

	// Damage: HitGroup id=2 → "chest"; world damage filtered.
	if len(raw.Damages) != 1 {
		t.Fatalf("damages=%d want 1 (world damage filtered)", len(raw.Damages))
	}
	if raw.Damages[0].HitGroup != "chest" {
		t.Errorf("hit_group=%q want chest", raw.Damages[0].HitGroup)
	}

	// WeaponFire: knife fire filtered, ak47 fire kept; speed = sqrt(34² + 0²) = 34.
	if len(raw.WeaponFires) != 1 {
		t.Fatalf("weapon_fires=%d want 1 (knife filtered)", len(raw.WeaponFires))
	}
	if raw.WeaponFires[0].Weapon != "ak47" {
		t.Errorf("kept fire weapon=%q want ak47", raw.WeaponFires[0].Weapon)
	}
	if raw.WeaponFires[0].HorizontalSpeed != 34 {
		t.Errorf("h_speed=%v want 34", raw.WeaponFires[0].HorizontalSpeed)
	}

	// Flash: duration converted ms → time.Duration.
	if len(raw.Flashes) != 1 {
		t.Fatalf("flashes=%d want 1", len(raw.Flashes))
	}
	if raw.Flashes[0].FlashDuration != 1850*time.Millisecond {
		t.Errorf("flash duration=%v want 1850ms", raw.Flashes[0].FlashDuration)
	}

	// Grenade throw → RawGrenadeEvent: end_tick = matching detonation tick,
	// land_pos = detonation pos.
	if len(raw.Grenades) != 1 {
		t.Fatalf("grenades=%d want 1", len(raw.Grenades))
	}
	if raw.Grenades[0].EndTick != 290 {
		t.Errorf("end_tick=%d want 290 (detonation tick)", raw.Grenades[0].EndTick)
	}
	if raw.Grenades[0].LandPos.X != 105 {
		t.Errorf("land_pos.X=%v want 105 (detonation x)", raw.Grenades[0].LandPos.X)
	}
	if raw.Grenades[0].GrenadeType != "flash" {
		t.Errorf("grenade type=%q want flash", raw.Grenades[0].GrenadeType)
	}
}

func TestToRawMatch_FilterSelfDamage(t *testing.T) {
	m := newSyntheticMatch()
	// Add a self-damage row (attacker == victim).
	m.Damages = append(m.Damages, csraw2.Damage{
		Tick: 400, Round: 1, AttackerSlot: 0, VictimSlot: 0, WeaponID: 1,
		HealthDamage: 25, HitGroup: csraw2.HitGroupChest,
	})
	raw, err := ToRawMatch(m)
	if err != nil {
		t.Fatalf("ToRawMatch: %v", err)
	}
	for _, d := range raw.Damages {
		if d.AttackerSteamID == d.VictimSteamID {
			t.Errorf("self-damage row leaked: %+v", d)
		}
	}
}

func TestToRawMatch_VersionGuard(t *testing.T) {
	m := newSyntheticMatch()
	m.Header.CSRawVersion = 99
	if _, err := ToRawMatch(m); err == nil {
		t.Fatalf("expected version mismatch error")
	}
}

func TestToRawMatch_NilMatch(t *testing.T) {
	if _, err := ToRawMatch(nil); err == nil {
		t.Fatalf("expected error for nil match")
	}
}

func TestYaw0to360(t *testing.T) {
	cases := []struct {
		in   int16
		want float64
	}{
		{0, 0},
		{4500, 45},
		{18000, 180},
		{-9000, 270}, // -90° → 270°
		{-13500, 225},
	}
	for _, c := range cases {
		if got := yaw0to360(c.in); got != c.want {
			t.Errorf("yaw0to360(%d)=%v want %v", c.in, got, c.want)
		}
	}
}

func newSyntheticMatch() *csraw2.Match {
	return &csraw2.Match{
		Header: csraw2.Header{
			CSRawVersion:       csraw2.Version,
			CSRawSchemaVersion: csraw2.SchemaVersion,
			DemoHash:           "sha256:abc123",
			Map:                "de_nuke",
			Tickrate:           64,
			MatchType:          "competitive",
			MatchDate:          "2026-04-29",
			Tier:               "personal",
			Players: []csraw2.Player{
				{Slot: 0, SteamID: "1001", Name: "alice", StartingTeam: "T"},
				{Slot: 1, SteamID: "1002", Name: "bob", StartingTeam: "CT"},
			},
			Rounds: []csraw2.Round{
				{N: 1, StartTick: 100, FreezeEndTick: 200, EndTick: 1000,
					Winner: "T", CTScoreAfter: 0, TScoreAfter: 1, Phase: "regulation"},
			},
			WeaponTable: map[string]string{"1": "knife", "7": "ak47"},
			Sampling: csraw2.Sampling{BaselineHz: 16, EventWindowHz: 64,
				EventWindowRadiusTicks: 32, FreezeTimeSampled: true, ProjectileHz: 32},
		},
		Kills: []csraw2.Kill{
			{Tick: 350, Round: 1, KillerSlot: 0, VictimSlot: 1, AssisterSlot: -1,
				WeaponID: 7, IsHeadshot: true,
				KillerPosX: 100, VictimPosX: 110, VictimYawDeg: -13500},
		},
		Damages: []csraw2.Damage{
			{Tick: 348, Round: 1, AttackerSlot: 0, VictimSlot: 1, WeaponID: 7,
				HealthDamage: 100, HitGroup: csraw2.HitGroupChest},
			// World damage (attacker_slot = -1) — should be filtered.
			{Tick: 360, Round: 1, AttackerSlot: -1, VictimSlot: 1, WeaponID: 0,
				HealthDamage: 5, HitGroup: csraw2.HitGroupGeneric},
		},
		WeaponFires: []csraw2.WeaponFire{
			{Tick: 345, Round: 1, ShooterSlot: 0, WeaponID: 7, VelX: 34, VelY: 0},
			// Knife "fire" — should be filtered.
			{Tick: 100, Round: 1, ShooterSlot: 0, WeaponID: 1},
		},
		Flashes: []csraw2.Flash{
			{Tick: 320, Round: 1, AttackerSlot: 0, VictimSlot: 1,
				FlashDurationMS: 1850, VictimPosX: 110, VictimYawDeg: -13500},
		},
		GrenadeThrows: []csraw2.GrenadeThrow{
			{Tick: 250, Round: 1, ThrowerSlot: 0, GrenadeType: csraw2.GrenadeFlash,
				ThrowPosX: 90, ProjectileID: 1001},
		},
		GrenadeDetonations: []csraw2.GrenadeDetonate{
			{Tick: 290, Round: 1, ProjectileID: 1001, GrenadeType: csraw2.GrenadeFlash,
				PosX: 105, PosY: 205, PosZ: 70},
		},
		PlayerSamples: []csraw2.PlayerSample{
			// Round 1 baseline (just before/at freeze end).
			{Tick: 200, Round: 1, PlayerSlot: 0, Team: csraw2.TeamT,
				HP: 100, EquipValue: 4750, Money: 800,
				PosX: 90, PosY: 190, PosZ: 64},
			{Tick: 200, Round: 1, PlayerSlot: 1, Team: csraw2.TeamCT,
				HP: 100, EquipValue: 4750, Money: 800,
				PosX: 110, PosY: 210, PosZ: 64},
			// End of round.
			{Tick: 999, Round: 1, PlayerSlot: 0, Team: csraw2.TeamT,
				HP: 100, EquipValue: 4750, Money: 3500,
				PosX: 200, PosY: 300, PosZ: 64},
			{Tick: 999, Round: 1, PlayerSlot: 1, Team: csraw2.TeamCT,
				HP: 0, EquipValue: 0, Money: 100,
				PosX: 110, PosY: 210, PosZ: 64},
		},
	}
}
