package parserv2

import (
	"testing"

	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"
)

func TestQuantPos(t *testing.T) {
	cases := []struct {
		in   float64
		want int16
	}{
		{0, 0},
		{1234.6, 1235},
		{-1234.6, -1235},
		{40000, 32767},  // saturate at int16 max
		{-40000, -32768},
		{0.4, 0},
		{0.5, 1},
	}
	for _, c := range cases {
		if got := quantPos(c.in); got != c.want {
			t.Errorf("quantPos(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestQuantAngleWraps(t *testing.T) {
	// Yaw of 270° folds to -90° → -9000 in 1/100°.
	if got := quantAngle(270); got != -9000 {
		t.Errorf("quantAngle(270)=%d, want -9000", got)
	}
	// Yaw of -270° folds to +90° → 9000.
	if got := quantAngle(-270); got != 9000 {
		t.Errorf("quantAngle(-270)=%d, want 9000", got)
	}
	// 180° stays at 18000 (boundary).
	if got := quantAngle(180); got != 18000 {
		t.Errorf("quantAngle(180)=%d, want 18000", got)
	}
	// -180° wraps to +180° → 18000 (folding rule keeps it positive).
	if got := quantAngle(-180); got != -18000 {
		t.Errorf("quantAngle(-180)=%d, want -18000", got)
	}
	// 45.67° → 4567 (round to nearest 1/100°).
	if got := quantAngle(45.67); got != 4567 {
		t.Errorf("quantAngle(45.67)=%d, want 4567", got)
	}
}

func TestNormalisePitch(t *testing.T) {
	if got := normalisePitch(45); got != 45 {
		t.Errorf("normalisePitch(45)=%v, want 45", got)
	}
	// 270° (Source 2 "looking down 90°") → -90°.
	if got := normalisePitch(270); got != -90 {
		t.Errorf("normalisePitch(270)=%v, want -90", got)
	}
}

func TestTickOffset(t *testing.T) {
	if got := tickOffset(100, 0); got != 255 {
		t.Errorf("tickOffset never-fired=%d, want 255", got)
	}
	if got := tickOffset(100, 90); got != 10 {
		t.Errorf("tickOffset 100-90=%d, want 10", got)
	}
	if got := tickOffset(100, 1000); got != 0 {
		t.Errorf("tickOffset negative-clamps-to-0=%d, want 0", got)
	}
	if got := tickOffset(100000, 1); got != 255 {
		t.Errorf("tickOffset large-saturates=%d, want 255", got)
	}
}

func TestSlotAllocator(t *testing.T) {
	s := newState()

	// Build minimal common.Player stand-ins via the struct literal —
	// SteamID64, Name, Team, IsBot are all exported and that's all
	// slotFor reads.
	a := &common.Player{SteamID64: 1, Name: "a", Team: common.TeamTerrorists}
	b := &common.Player{SteamID64: 2, Name: "b", Team: common.TeamCounterTerrorists}

	// First sight allocates slot 0; second player gets slot 1.
	if slot, ok := s.slotFor(a); !ok || slot != 0 {
		t.Errorf("first slotFor(a) = (%d,%v), want (0,true)", slot, ok)
	}
	if slot, ok := s.slotFor(b); !ok || slot != 1 {
		t.Errorf("first slotFor(b) = (%d,%v), want (1,true)", slot, ok)
	}
	// Repeat lookups are stable.
	if slot, ok := s.slotFor(a); !ok || slot != 0 {
		t.Errorf("repeat slotFor(a) = (%d,%v), want (0,true)", slot, ok)
	}
	// Roster appended in allocation order.
	if len(s.match.Header.Players) != 2 {
		t.Fatalf("roster len=%d, want 2", len(s.match.Header.Players))
	}
	if s.match.Header.Players[0].SteamID != "1" || s.match.Header.Players[0].StartingTeam != "T" {
		t.Errorf("roster[0]=%+v", s.match.Header.Players[0])
	}
	if s.match.Header.Players[1].StartingTeam != "CT" {
		t.Errorf("roster[1].team=%s, want CT", s.match.Header.Players[1].StartingTeam)
	}

	// Nil and unconnected players → not allocated.
	if _, ok := s.slotFor(nil); ok {
		t.Errorf("slotFor(nil) returned ok=true")
	}
	if _, ok := s.slotFor(&common.Player{SteamID64: 0}); ok {
		t.Errorf("slotFor(unconnected) returned ok=true")
	}
}

func TestSlotForOptional(t *testing.T) {
	s := newState()
	if got := s.slotForOptional(nil); got != -1 {
		t.Errorf("slotForOptional(nil)=%d, want -1", got)
	}
	a := &common.Player{SteamID64: 99, Name: "x", Team: common.TeamTerrorists}
	if got := s.slotForOptional(a); got != 0 {
		t.Errorf("slotForOptional(a)=%d, want 0", got)
	}
}

func TestWeaponIDFor(t *testing.T) {
	s := newState()
	// Unknown weapon always maps to 0.
	if id := s.weaponIDFor(common.EqUnknown); id != 0 {
		t.Errorf("weaponIDFor(unknown)=%d, want 0", id)
	}
	// First real weapon allocates id 0.
	if id := s.weaponIDFor(common.EqAK47); id != 0 {
		t.Errorf("weaponIDFor(AK47)=%d, want 0", id)
	}
	// Same weapon returns stable id.
	if id := s.weaponIDFor(common.EqAK47); id != 0 {
		t.Errorf("repeat weaponIDFor(AK47)=%d, want 0", id)
	}
	// Second weapon allocates id 1.
	if id := s.weaponIDFor(common.EqM4A4); id != 1 {
		t.Errorf("weaponIDFor(M4A4)=%d, want 1", id)
	}
	if len(s.match.Header.WeaponTable) != 2 {
		t.Errorf("weapon_table len=%d, want 2", len(s.match.Header.WeaponTable))
	}
}
