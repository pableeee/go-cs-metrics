package aggregator

import (
	"math"
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

// ---- Pass 19 tests: round context ----

const ctxFreezeEnd = 1000

func ctxRaw() *model.RawMatch {
	return &model.RawMatch{
		DemoHash:       "h",
		TicksPerSecond: 64,
		Rounds: []model.RawRound{
			{Number: 1, StartTick: 0, FreezeEndTick: ctxFreezeEnd, EndTick: 10000},
		},
	}
}

func ctxSample(id uint64, team model.Team, tick int, weapon string, hp int, x, y, z float64) model.RawViewSample {
	return model.RawViewSample{
		Tick: tick, RoundNumber: 1, SteamID: id, Team: team,
		HP: hp, Weapon: weapon,
		Pos: model.Vec3{X: x, Y: y, Z: z},
	}
}

// Gun-sample counting: alive in-round samples only, classified on the same
// weapon taxonomy the duel table uses.
func TestPass19_GunSampleCounts(t *testing.T) {
	raw := ctxRaw()
	raw.ViewSamples = []model.RawViewSample{
		ctxSample(1, model.TeamCT, 1100, "AK-47", 100, 0, 0, 0),    // rifle
		ctxSample(1, model.TeamCT, 1200, "M4A1", 100, 0, 0, 0),     // rifle (silenced name)
		ctxSample(1, model.TeamCT, 1300, "AWP", 100, 0, 0, 0),      // sniper
		ctxSample(1, model.TeamCT, 1400, "Glock-18", 100, 0, 0, 0), // pistol: counts, not rifle/sniper
		ctxSample(1, model.TeamCT, 1500, "", 100, 0, 0, 0),         // no weapon
		ctxSample(1, model.TeamCT, 1600, "AK-47", 0, 0, 0, 0),      // dead → excluded
		ctxSample(1, model.TeamCT, 900, "AK-47", 100, 0, 0, 0),     // freeze time → excluded
	}
	res := computeRoundContext(raw)[1][1]
	if res == nil {
		t.Fatal("no result for player 1 round 1")
	}
	if res.gunSamples != 5 || res.gunRifle != 2 || res.gunSniper != 1 {
		t.Errorf("gun samples = %d/%d/%d, want 5 total, 2 rifle, 1 sniper",
			res.gunSamples, res.gunRifle, res.gunSniper)
	}
}

// Pack distance: nearest alive teammate, opponents ignored, sole-survivor
// ticks contribute nothing.
func TestPass19_PackDistance(t *testing.T) {
	raw := ctxRaw()
	raw.ViewSamples = []model.RawViewSample{
		// Tick 1100: teammates at 100u and 300u, opponent at 50u.
		ctxSample(1, model.TeamT, 1100, "AK-47", 100, 0, 0, 0),
		ctxSample(2, model.TeamT, 1100, "AK-47", 100, 100, 0, 0),
		ctxSample(3, model.TeamT, 1100, "AK-47", 100, 300, 0, 0),
		ctxSample(9, model.TeamCT, 1100, "AK-47", 100, 50, 0, 0),
		// Tick 1200: player 1 alone on their team → no contribution.
		ctxSample(1, model.TeamT, 1200, "AK-47", 100, 500, 0, 0),
		ctxSample(9, model.TeamCT, 1200, "AK-47", 100, 501, 0, 0),
	}
	out := computeRoundContext(raw)

	p1 := out[1][1]
	if p1.packDistN != 1 {
		t.Fatalf("player 1 packDistN = %d, want 1 (sole-survivor tick excluded)", p1.packDistN)
	}
	want := 100 * unitsToMeters // nearest teammate is at 100u, NOT the CT at 50u
	if math.Abs(p1.packDistSumM-want) > 1e-9 {
		t.Errorf("player 1 pack dist = %.4f, want %.4f", p1.packDistSumM, want)
	}
	// Player 3's nearest teammate is player 2 at 200u.
	p3 := out[3][1]
	if math.Abs(p3.packDistSumM-200*unitsToMeters) > 1e-9 {
		t.Errorf("player 3 pack dist = %.4f, want %.4f", p3.packDistSumM, 200*unitsToMeters)
	}
}

// First contact: earliest of kill/death/damage-given/damage-taken; freeze-time
// events clamp to zero; team damage and suicides are not contact.
func TestPass19_FirstContact(t *testing.T) {
	raw := ctxRaw()
	raw.Kills = []model.RawKill{
		{RoundNumber: 1, Tick: ctxFreezeEnd + 640, KillerSteamID: 1, VictimSteamID: 6,
			KillerTeam: model.TeamT, VictimTeam: model.TeamCT},
		// Teamkill: not enemy contact for either party (but a death for 3).
		{RoundNumber: 1, Tick: ctxFreezeEnd + 64, KillerSteamID: 2, VictimSteamID: 3,
			KillerTeam: model.TeamT, VictimTeam: model.TeamT},
	}
	raw.Damages = []model.RawDamage{
		// Player 1 took damage BEFORE their kill: that is the first contact.
		{RoundNumber: 1, Tick: ctxFreezeEnd + 320, AttackerSteamID: 6, VictimSteamID: 1,
			AttackerTeam: model.TeamCT, VictimTeam: model.TeamT},
		// Freeze-time damage for player 4 clamps to 0.
		{RoundNumber: 1, Tick: ctxFreezeEnd - 100, AttackerSteamID: 4, VictimSteamID: 7,
			AttackerTeam: model.TeamT, VictimTeam: model.TeamCT},
	}
	out := computeRoundContext(raw)

	if got := out[1][1].firstContactSec; math.Abs(got-5.0) > 1e-9 {
		t.Errorf("player 1 first contact = %v, want 5.0 (damage taken at +320 ticks)", got)
	}
	if got := out[4][1].firstContactSec; got != 0 {
		t.Errorf("player 4 first contact = %v, want 0 (freeze-time clamp)", got)
	}
	// The teamkiller never touched an enemy: either no entry at all or a -1.
	if r := out[2][1]; r != nil && r.firstContactSec != -1 {
		t.Errorf("player 2 first contact = %v, want none (teamkill is not contact)", r.firstContactSec)
	}
	// The teamkill victim still died.
	if got := out[3][1].deathSec; math.Abs(got-1.0) > 1e-9 {
		t.Errorf("player 3 death = %v, want 1.0", got)
	}
	// Victim-side involvement counts as contact.
	if got := out[6][1].firstContactSec; math.Abs(got-5.0) > 1e-9 {
		t.Errorf("player 6 first contact = %v, want 5.0", got)
	}
}

// Death timing: survivors stay -1; a death 64 ticks after freeze end at 64
// ticks/second is exactly one second.
func TestPass19_DeathSec(t *testing.T) {
	raw := ctxRaw()
	raw.Kills = []model.RawKill{
		{RoundNumber: 1, Tick: ctxFreezeEnd + 64, KillerSteamID: 1, VictimSteamID: 6,
			KillerTeam: model.TeamT, VictimTeam: model.TeamCT},
	}
	raw.ViewSamples = []model.RawViewSample{
		ctxSample(1, model.TeamT, 1100, "AK-47", 100, 0, 0, 0),
	}
	out := computeRoundContext(raw)
	if got := out[6][1].deathSec; math.Abs(got-1.0) > 1e-9 {
		t.Errorf("victim death = %v, want 1.0", got)
	}
	if got := out[1][1].deathSec; got != -1 {
		t.Errorf("survivor death = %v, want -1", got)
	}
}
