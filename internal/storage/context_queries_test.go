package storage

import (
	"math"
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

// Round-trip the Pass 19 columns and confirm the loader's aggregation rules:
// unmeasured rounds don't count toward Rounds, and -1 sentinels are excluded
// from every average.
func TestLoadPlayerContext_RoundTrip(t *testing.T) {
	db := openMemDB(t)
	if err := db.InsertDemo(model.MatchSummary{
		DemoHash: "h1", MapName: "de_nuke", MatchDate: "2026-01-10",
		MatchType: "Competitive", Tickrate: 64, Tier: "pro",
	}, ""); err != nil {
		t.Fatalf("InsertDemo: %v", err)
	}

	const id = uint64(76561198000000001)
	rows := []model.PlayerRoundStats{
		{DemoHash: "h1", SteamID: id, RoundNumber: 1, Team: model.TeamCT,
			GunSamples: 100, GunSamplesRifle: 60, GunSamplesSniper: 20,
			PackDistAvgM: 10, FirstContactSec: 20, DeathSec: 30},
		{DemoHash: "h1", SteamID: id, RoundNumber: 2, Team: model.TeamCT,
			GunSamples: 100, GunSamplesRifle: 40, GunSamplesSniper: 0,
			PackDistAvgM: 20, FirstContactSec: -1, DeathSec: -1}, // no contact, survived
		// Pre-backfill row: zero samples, all sentinels. Must not count.
		{DemoHash: "h1", SteamID: id, RoundNumber: 3, Team: model.TeamCT,
			PackDistAvgM: -1, FirstContactSec: -1, DeathSec: -1},
		{DemoHash: "h1", SteamID: id, RoundNumber: 4, Team: model.TeamT,
			GunSamples: 50, GunSamplesRifle: 50,
			PackDistAvgM: 5, FirstContactSec: 10, DeathSec: 15},
	}
	if err := db.InsertPlayerRoundStats(rows); err != nil {
		t.Fatalf("InsertPlayerRoundStats: %v", err)
	}

	got, err := db.LoadPlayerContext([]uint64{id}, SwingFilter{Tier: "pro"})
	if err != nil {
		t.Fatalf("LoadPlayerContext: %v", err)
	}
	ct := got[id]["CT"]
	if ct == nil {
		t.Fatal("no CT row")
	}
	if ct.Rounds != 2 {
		t.Errorf("CT rounds = %d, want 2 (unmeasured round excluded)", ct.Rounds)
	}
	if ct.GunSamples != 200 || ct.GunRifle != 100 || ct.GunSniper != 20 {
		t.Errorf("CT gun sums = %d/%d/%d, want 200/100/20", ct.GunSamples, ct.GunRifle, ct.GunSniper)
	}
	if math.Abs(ct.PackDistAvgM-15) > 1e-9 {
		t.Errorf("CT pack dist = %v, want 15", ct.PackDistAvgM)
	}
	// Only round 1 has valid contact/death values: the -1s must not drag the mean.
	if math.Abs(ct.FirstContactSec-20) > 1e-9 || math.Abs(ct.DeathSec-30) > 1e-9 {
		t.Errorf("CT contact/death = %v/%v, want 20/30", ct.FirstContactSec, ct.DeathSec)
	}

	tt := got[id]["T"]
	if tt == nil || tt.Rounds != 1 || tt.GunSamples != 50 {
		t.Fatalf("T row = %+v, want 1 round, 50 samples", tt)
	}

	pop, err := db.LoadPopulationContext(SwingFilter{Tier: "pro"})
	if err != nil {
		t.Fatalf("LoadPopulationContext: %v", err)
	}
	if pop["CT"] == nil || pop["CT"].GunSamples != 200 {
		t.Errorf("population CT = %+v, want the same 200 samples", pop["CT"])
	}

	// A player with only sentinel rounds yields -1 means, not zeros.
	if r := pop["CT"]; math.Abs(r.PackDistAvgM-15) > 1e-9 {
		t.Errorf("population CT pack dist = %v, want 15", r.PackDistAvgM)
	}
}
