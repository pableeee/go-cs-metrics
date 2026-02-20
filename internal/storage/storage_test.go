package storage

import (
	"testing"

	"github.com/pable/go-cs-metrics/internal/model"
)

func openMemDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDemoInsertAndExists(t *testing.T) {
	db := openMemDB(t)

	summary := model.MatchSummary{
		DemoHash:  "abc123",
		MapName:   "de_dust2",
		MatchDate: "2025-01-01",
		MatchType: "Competitive",
		Tickrate:  64,
		CTScore:   16,
		TScore:    10,
	}

	if err := db.InsertDemo(summary); err != nil {
		t.Fatalf("InsertDemo: %v", err)
	}

	exists, err := db.DemoExists("abc123")
	if err != nil {
		t.Fatalf("DemoExists: %v", err)
	}
	if !exists {
		t.Error("expected demo to exist after insert")
	}

	exists2, _ := db.DemoExists("nonexistent")
	if exists2 {
		t.Error("expected non-existent demo to not exist")
	}
}

func TestListDemos(t *testing.T) {
	db := openMemDB(t)

	summaries := []model.MatchSummary{
		{DemoHash: "h1", MapName: "de_dust2", MatchDate: "2025-01-01", MatchType: "Competitive", Tickrate: 64},
		{DemoHash: "h2", MapName: "de_mirage", MatchDate: "2025-02-01", MatchType: "Premier", Tickrate: 128},
	}
	for _, s := range summaries {
		if err := db.InsertDemo(s); err != nil {
			t.Fatalf("InsertDemo: %v", err)
		}
	}

	list, err := db.ListDemos()
	if err != nil {
		t.Fatalf("ListDemos: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 demos, got %d", len(list))
	}
	// Ordered by match_date DESC — h2 should be first.
	if list[0].DemoHash != "h2" {
		t.Errorf("expected h2 first (newest), got %s", list[0].DemoHash)
	}
}

func TestGetDemoByPrefix(t *testing.T) {
	db := openMemDB(t)

	db.InsertDemo(model.MatchSummary{DemoHash: "deadbeef1234", MapName: "de_inferno", MatchDate: "2025-01-01", MatchType: "Wingman", Tickrate: 64})

	s, err := db.GetDemoByPrefix("deadb")
	if err != nil {
		t.Fatalf("GetDemoByPrefix: %v", err)
	}
	if s == nil {
		t.Fatal("expected match for prefix 'deadb'")
	}
	if s.DemoHash != "deadbeef1234" {
		t.Errorf("unexpected hash %s", s.DemoHash)
	}

	s2, err := db.GetDemoByPrefix("ffffffff")
	if err != nil {
		t.Fatalf("GetDemoByPrefix no-match: %v", err)
	}
	if s2 != nil {
		t.Error("expected nil for unknown prefix")
	}
}

func TestPlayerMatchStatsRoundTrip(t *testing.T) {
	db := openMemDB(t)

	db.InsertDemo(model.MatchSummary{DemoHash: "h1", MapName: "de_dust2", MatchDate: "2025-01-01", MatchType: "Competitive", Tickrate: 64})

	stats := []model.PlayerMatchStats{
		{
			DemoHash: "h1", SteamID: 76561198000000001, Name: "Alice", Team: model.TeamCT,
			Kills: 20, Assists: 3, Deaths: 15, HeadshotKills: 10, FlashAssists: 2,
			TotalDamage: 2500, UtilityDamage: 200, RoundsPlayed: 25,
			OpeningKills: 4, OpeningDeaths: 2, TradeKills: 3, TradeDeaths: 1,
			KASTRounds: 18, UnusedUtility: 5,
			CrosshairEncounters: 12, CrosshairMedianDeg: 4.3, CrosshairPctUnder5: 58.3,
		},
		{
			DemoHash: "h1", SteamID: 76561198000000002, Name: "Bob", Team: model.TeamT,
			Kills: 15, Assists: 1, Deaths: 18, HeadshotKills: 5, FlashAssists: 0,
			TotalDamage: 1800, UtilityDamage: 0, RoundsPlayed: 25,
			OpeningKills: 1, OpeningDeaths: 3, TradeKills: 1, TradeDeaths: 2,
			KASTRounds: 12, UnusedUtility: 2,
			CrosshairEncounters: 0, CrosshairMedianDeg: 0, CrosshairPctUnder5: 0,
		},
	}

	if err := db.InsertPlayerMatchStats(stats); err != nil {
		t.Fatalf("InsertPlayerMatchStats: %v", err)
	}

	got, err := db.GetPlayerMatchStats("h1")
	if err != nil {
		t.Fatalf("GetPlayerMatchStats: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 player rows, got %d", len(got))
	}

	// Find Alice in results.
	var alice *model.PlayerMatchStats
	for i := range got {
		if got[i].SteamID == 76561198000000001 {
			alice = &got[i]
		}
	}
	if alice == nil {
		t.Fatal("Alice not found in results")
	}
	if alice.Kills != 20 || alice.Deaths != 15 || alice.KASTRounds != 18 {
		t.Errorf("Alice stats mismatch: kills=%d deaths=%d kast=%d", alice.Kills, alice.Deaths, alice.KASTRounds)
	}
	if alice.Team != model.TeamCT {
		t.Errorf("Alice team: expected CT, got %v", alice.Team)
	}
	if alice.CrosshairEncounters != 12 {
		t.Errorf("Alice CrosshairEncounters: want 12, got %d", alice.CrosshairEncounters)
	}
	if alice.CrosshairMedianDeg != 4.3 {
		t.Errorf("Alice CrosshairMedianDeg: want 4.3, got %f", alice.CrosshairMedianDeg)
	}
	if alice.CrosshairPctUnder5 != 58.3 {
		t.Errorf("Alice CrosshairPctUnder5: want 58.3, got %f", alice.CrosshairPctUnder5)
	}
}

func TestInsertIdempotency(t *testing.T) {
	db := openMemDB(t)

	s := model.MatchSummary{DemoHash: "idem1", MapName: "de_nuke", MatchDate: "2025-01-01", MatchType: "Competitive", Tickrate: 64}
	db.InsertDemo(s)
	// Second insert should not error (INSERT OR REPLACE).
	if err := db.InsertDemo(s); err != nil {
		t.Errorf("second InsertDemo should succeed (idempotent): %v", err)
	}
}
