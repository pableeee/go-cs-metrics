package storage

import (
	"database/sql"
	"path/filepath"
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

	if err := db.InsertDemo(summary, ""); err != nil {
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
		if err := db.InsertDemo(s, ""); err != nil {
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

	db.InsertDemo(model.MatchSummary{DemoHash: "deadbeef1234", MapName: "de_inferno", MatchDate: "2025-01-01", MatchType: "Wingman", Tickrate: 64}, "")

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

	db.InsertDemo(model.MatchSummary{DemoHash: "h1", MapName: "de_dust2", MatchDate: "2025-01-01", MatchType: "Competitive", Tickrate: 64}, "")

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

func TestMapNameNormalization(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"de_mirage", "Mirage"},
		{"de_ancient", "Ancient"},
		{"de_inferno", "Inferno"},
		{"de_nuke", "Nuke"},
		{"de_anubis", "Anubis"},
		{"de_vertigo", "Vertigo"},
		{"de_overpass", "Overpass"},
		{"de_dust2", "Dust2"},
		// Idempotent: already-normalized names are unchanged.
		{"Mirage", "Mirage"},
		{"Ancient", "Ancient"},
	}

	db := openMemDB(t)
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			hash := "hash_" + tc.raw
			if err := db.InsertDemo(model.MatchSummary{
				DemoHash:  hash,
				MapName:   tc.raw,
				MatchDate: "2025-01-01",
				MatchType: "pro",
				Tickrate:  128,
			}, ""); err != nil {
				t.Fatalf("InsertDemo: %v", err)
			}

			demo, err := db.GetDemoByPrefix(hash)
			if err != nil {
				t.Fatalf("GetDemoByPrefix: %v", err)
			}
			if demo == nil {
				t.Fatal("demo not found after insert")
			}
			if demo.MapName != tc.want {
				t.Errorf("MapName: got %q, want %q", demo.MapName, tc.want)
			}
		})
	}
}

func TestNormalizeMapName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"de_mirage", "Mirage"},
		{"de_dust2", "Dust2"},
		{"Mirage", "Mirage"},   // already normalized — idempotent
		{"Ancient", "Ancient"}, // already normalized — idempotent
		{"de_", "de_"},         // stripping leaves empty — original preserved
	}
	for _, tc := range cases {
		if got := normalizeMapName(tc.in); got != tc.want {
			t.Errorf("normalizeMapName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInsertIdempotency(t *testing.T) {
	db := openMemDB(t)

	s := model.MatchSummary{DemoHash: "idem1", MapName: "de_nuke", MatchDate: "2025-01-01", MatchType: "Competitive", Tickrate: 64}
	db.InsertDemo(s, "")
	// Second insert should not error (INSERT OR REPLACE).
	if err := db.InsertDemo(s, ""); err != nil {
		t.Errorf("second InsertDemo should succeed (idempotent): %v", err)
	}
}

// TestPlayerWeaponStatsRoundTrip covers the Pass 18 shot-accounting columns
// through both read paths: GetPlayerWeaponStats (per demo, used by `show`)
// and GetAllPlayerWeaponStats (per player, used by `player`).
func TestPlayerWeaponStatsRoundTrip(t *testing.T) {
	db := openMemDB(t)

	const hash = "wpn1"
	if err := db.InsertDemo(model.MatchSummary{
		DemoHash: hash, MapName: "de_mirage", MatchDate: "2025-01-01", Tickrate: 64,
	}, ""); err != nil {
		t.Fatalf("InsertDemo: %v", err)
	}

	want := model.PlayerWeaponStats{
		DemoHash: hash, SteamID: 76561198000000001, Weapon: "AK-47",
		Kills: 10, HeadshotKills: 4, Assists: 2, Deaths: 6, Damage: 1500, Hits: 40,
		ShotsFired: 200, ShotsVisible: 80, HitsVisible: 30,
		HeadHits: 8, HeadHitsVisible: 5,
	}
	if err := db.InsertPlayerWeaponStats([]model.PlayerWeaponStats{want}); err != nil {
		t.Fatalf("InsertPlayerWeaponStats: %v", err)
	}

	byDemo, err := db.GetPlayerWeaponStats(hash)
	if err != nil {
		t.Fatalf("GetPlayerWeaponStats: %v", err)
	}
	if len(byDemo) != 1 {
		t.Fatalf("GetPlayerWeaponStats returned %d rows, want 1", len(byDemo))
	}
	if byDemo[0] != want {
		t.Errorf("GetPlayerWeaponStats round trip:\n got %+v\nwant %+v", byDemo[0], want)
	}

	byPlayer, err := db.GetAllPlayerWeaponStats(want.SteamID)
	if err != nil {
		t.Fatalf("GetAllPlayerWeaponStats: %v", err)
	}
	if len(byPlayer) != 1 {
		t.Fatalf("GetAllPlayerWeaponStats returned %d rows, want 1", len(byPlayer))
	}
	if byPlayer[0] != want {
		t.Errorf("GetAllPlayerWeaponStats round trip:\n got %+v\nwant %+v", byPlayer[0], want)
	}

	// Derived rates come off the stored counters, not a recomputation.
	got := byPlayer[0]
	if v := got.AccuracyVisible(); v != 37.5 { // 30/80
		t.Errorf("AccuracyVisible() = %v, want 37.5", v)
	}
	if v := got.BlindShotPct(); v != 60 { // (200-80)/200
		t.Errorf("BlindShotPct() = %v, want 60", v)
	}
	if v := got.ShotsBlind(); v != 120 {
		t.Errorf("ShotsBlind() = %d, want 120", v)
	}
}

// TestDuelSegmentsRoundRoundTrip covers the round_number dimension end to end.
func TestDuelSegmentsRoundRoundTrip(t *testing.T) {
	db := openMemDB(t)
	const hash = "seg1"
	if err := db.InsertDemo(model.MatchSummary{
		DemoHash: hash, MapName: "de_nuke", MatchDate: "2025-01-01", Tickrate: 64,
	}, ""); err != nil {
		t.Fatalf("InsertDemo: %v", err)
	}

	segs := []model.PlayerDuelSegment{
		{DemoHash: hash, SteamID: 1, RoundNumber: 3, WeaponBucket: "AK", DistanceBin: "10-15m",
			DuelCount: 2, FirstHitCount: 2, FirstHitHSCount: 1,
			MedianCorrDeg: 4.5, MedianSightDeg: 5.5, MedianExpoWinMs: 900},
		// Same player/bucket/bin, different round: must be a distinct row now.
		{DemoHash: hash, SteamID: 1, RoundNumber: 7, WeaponBucket: "AK", DistanceBin: "10-15m",
			DuelCount: 1, FirstHitCount: 1, FirstHitHSCount: 0,
			MedianCorrDeg: 2.0, MedianSightDeg: 3.0, MedianExpoWinMs: 400},
	}
	if err := db.InsertPlayerDuelSegments(segs); err != nil {
		t.Fatalf("InsertPlayerDuelSegments: %v", err)
	}

	got, err := db.GetAllPlayerDuelSegments(1)
	if err != nil {
		t.Fatalf("GetAllPlayerDuelSegments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 — round must be part of the key", len(got))
	}
	rounds := map[int]bool{}
	for _, s := range got {
		rounds[s.RoundNumber] = true
	}
	if !rounds[3] || !rounds[7] {
		t.Errorf("round numbers not round-tripped: %v", rounds)
	}
}

// Re-inserting a demo's segments must replace them wholesale. Without the
// delete-before-insert, a segment that no longer exists (or that was written
// under an older key shape) would linger and double-count.
func TestInsertPlayerDuelSegments_ReplacesDemoWholesale(t *testing.T) {
	db := openMemDB(t)
	const hash = "seg2"
	if err := db.InsertDemo(model.MatchSummary{
		DemoHash: hash, MapName: "de_nuke", MatchDate: "2025-01-01", Tickrate: 64,
	}, ""); err != nil {
		t.Fatalf("InsertDemo: %v", err)
	}

	first := []model.PlayerDuelSegment{
		{DemoHash: hash, SteamID: 1, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "5-10m", DuelCount: 1, FirstHitCount: 1},
		{DemoHash: hash, SteamID: 1, RoundNumber: 2, WeaponBucket: "AK", DistanceBin: "5-10m", DuelCount: 1, FirstHitCount: 1},
	}
	if err := db.InsertPlayerDuelSegments(first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Re-aggregation now yields only one round.
	second := []model.PlayerDuelSegment{
		{DemoHash: hash, SteamID: 1, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "5-10m", DuelCount: 1, FirstHitCount: 1},
	}
	if err := db.InsertPlayerDuelSegments(second); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	got, err := db.GetAllPlayerDuelSegments(1)
	if err != nil {
		t.Fatalf("GetAllPlayerDuelSegments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 — round 2 should have been cleared, not left behind", len(got))
	}
	if got[0].RoundNumber != 1 {
		t.Errorf("surviving row is round %d, want 1", got[0].RoundNumber)
	}
}

// Segments for other demos must survive a re-insert of one demo.
func TestInsertPlayerDuelSegments_ScopedToItsOwnDemos(t *testing.T) {
	db := openMemDB(t)
	for _, h := range []string{"segA", "segB"} {
		if err := db.InsertDemo(model.MatchSummary{
			DemoHash: h, MapName: "de_nuke", MatchDate: "2025-01-01", Tickrate: 64,
		}, ""); err != nil {
			t.Fatalf("InsertDemo %s: %v", h, err)
		}
	}
	if err := db.InsertPlayerDuelSegments([]model.PlayerDuelSegment{
		{DemoHash: "segA", SteamID: 1, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "5-10m", DuelCount: 1},
		{DemoHash: "segB", SteamID: 1, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "5-10m", DuelCount: 1},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.InsertPlayerDuelSegments([]model.PlayerDuelSegment{
		{DemoHash: "segA", SteamID: 1, RoundNumber: 2, WeaponBucket: "AK", DistanceBin: "5-10m", DuelCount: 1},
	}); err != nil {
		t.Fatalf("re-insert segA: %v", err)
	}
	got, err := db.GetAllPlayerDuelSegments(1)
	if err != nil {
		t.Fatalf("GetAllPlayerDuelSegments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (segA round 2 + untouched segB)", len(got))
	}
	for _, s := range got {
		if s.DemoHash == "segB" && s.RoundNumber != 1 {
			t.Errorf("segB was modified: %+v", s)
		}
	}
}

// TestMigrateDuelSegmentsRound builds a database with the pre-round table
// shape, then opens it through Open and checks the rebuild happened: the
// column exists, old rows survive marked as round -1, and the new UNIQUE key
// admits two rounds that the old key would have collapsed into one row.
func TestMigrateDuelSegmentsRound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// Build the old shape by hand — no round_number, old UNIQUE key.
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	_, err = raw.Exec(`
		CREATE TABLE demos (hash TEXT PRIMARY KEY, map_name TEXT, match_date TEXT,
			match_type TEXT, tickrate REAL, ct_score INT, t_score INT);
		INSERT INTO demos(hash) VALUES ('old1');
		CREATE TABLE player_duel_segments (
			demo_hash          TEXT NOT NULL REFERENCES demos(hash),
			steam_id           TEXT NOT NULL,
			weapon_bucket      TEXT NOT NULL,
			distance_bin       TEXT NOT NULL,
			duel_count         INTEGER NOT NULL DEFAULT 0,
			first_hit_count    INTEGER NOT NULL DEFAULT 0,
			first_hit_hs_count INTEGER NOT NULL DEFAULT 0,
			median_corr_deg    REAL    NOT NULL DEFAULT 0,
			median_sight_deg   REAL    NOT NULL DEFAULT 0,
			median_expo_win_ms REAL    NOT NULL DEFAULT 0,
			UNIQUE(demo_hash, steam_id, weapon_bucket, distance_bin)
		);
		INSERT INTO player_duel_segments VALUES
			('old1','1','AK','10-15m',5,5,2,4.0,5.0,900);`)
	if err != nil {
		t.Fatalf("seed old schema: %v", err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (should migrate): %v", err)
	}
	defer db.Close()

	got, err := db.GetAllPlayerDuelSegments(1)
	if err != nil {
		t.Fatalf("GetAllPlayerDuelSegments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 carried over", len(got))
	}
	if got[0].RoundNumber != -1 {
		t.Errorf("carried-over row has round %d, want -1 (unknown)", got[0].RoundNumber)
	}
	if got[0].DuelCount != 5 || got[0].MedianCorrDeg != 4.0 {
		t.Errorf("payload not preserved: %+v", got[0])
	}

	// The new key must allow the same (player, bucket, bin) in two rounds.
	if err := db.InsertPlayerDuelSegments([]model.PlayerDuelSegment{
		{DemoHash: "old1", SteamID: 1, RoundNumber: 1, WeaponBucket: "AK", DistanceBin: "10-15m", DuelCount: 1},
		{DemoHash: "old1", SteamID: 1, RoundNumber: 2, WeaponBucket: "AK", DistanceBin: "10-15m", DuelCount: 1},
	}); err != nil {
		t.Fatalf("insert after migration: %v", err)
	}
	got, err = db.GetAllPlayerDuelSegments(1)
	if err != nil {
		t.Fatalf("GetAllPlayerDuelSegments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 distinct rounds", len(got))
	}

	// Re-opening an already-migrated DB must be a no-op, not a second rebuild.
	db.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer db2.Close()
	again, err := db2.GetAllPlayerDuelSegments(1)
	if err != nil {
		t.Fatalf("GetAllPlayerDuelSegments after reopen: %v", err)
	}
	if len(again) != 2 {
		t.Errorf("got %d rows after reopen, want 2 — migration is not idempotent", len(again))
	}
}
