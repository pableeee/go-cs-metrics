package storage

import (
	"strconv"
	"testing"
)

// insertDeath writes one player_death_events row directly. The `deaths`
// command only reads this table, so the test doesn't need a full aggregator
// run — just representative rows.
func insertDeath(t *testing.T, db *DB, demoHash, date, mapName, phase, weapon string,
	victimID uint64, distM float64, headshot, flashed, traded, opening int,
) {
	t.Helper()
	_, err := db.conn.Exec(`
		INSERT INTO player_death_events (
			demo_hash, match_date, map_name, round_number, tick,
			victim_id, victim_team, killer_id, killer_team, weapon,
			is_headshot, victim_x, victim_y, victim_z, killer_x, killer_y, killer_z,
			victim_yaw, distance_m, was_flashed, was_traded, is_opening_death, round_phase
		) VALUES (?, ?, ?, 1, 1000, ?, 'CT', '99', 'T', ?,
			?, 0,0,0, 0,0,0, 0, ?, ?, ?, ?, ?)`,
		demoHash, date, mapName, strconv.FormatUint(victimID, 10), weapon,
		headshot, distM, flashed, traded, opening, phase)
	if err != nil {
		t.Fatalf("insert death event: %v", err)
	}
}

func seedDeaths(t *testing.T, db *DB) {
	t.Helper()
	const victim = uint64(42)
	// Two maps, two dates, three phases, mixed flags.
	insertDeath(t, db, "d1", "2026-01-10", "de_mirage", "pistol", "USP-S", victim, 10, 1, 0, 1, 1)
	insertDeath(t, db, "d1", "2026-01-10", "de_mirage", "mid", "AK-47", victim, 20, 0, 1, 0, 0)
	insertDeath(t, db, "d2", "2026-02-20", "de_nuke", "late", "AWP", victim, 30, 0, 0, 1, 0)
	insertDeath(t, db, "d2", "2026-02-20", "de_nuke", "mid", "AK-47", victim, 40, 1, 0, 0, 0)
	// A different player's death — must never appear in results.
	insertDeath(t, db, "d2", "2026-02-20", "de_nuke", "mid", "AK-47", 999, 50, 1, 1, 1, 1)
}

func TestDeathTotalsFor_FiltersToVictim(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	got, err := db.DeathTotalsFor(42, DeathFilter{})
	if err != nil {
		t.Fatalf("DeathTotalsFor: %v", err)
	}
	if got.Deaths != 4 {
		t.Errorf("Deaths = %d, want 4 (the 5th row belongs to another player)", got.Deaths)
	}
	if got.Headshots != 2 || got.Flashed != 1 || got.Traded != 2 || got.OpeningDeal != 1 {
		t.Errorf("flag sums = hs %d, flashed %d, traded %d, opening %d; want 2/1/2/1",
			got.Headshots, got.Flashed, got.Traded, got.OpeningDeal)
	}
	if got.Demos != 2 || got.Maps != 2 {
		t.Errorf("Demos/Maps = %d/%d, want 2/2", got.Demos, got.Maps)
	}
	if got.FirstDate != "2026-01-10" || got.LastDate != "2026-02-20" {
		t.Errorf("date range = %s → %s, want 2026-01-10 → 2026-02-20", got.FirstDate, got.LastDate)
	}
	if want := 25.0; got.AvgDistM != want {
		t.Errorf("AvgDistM = %v, want %v", got.AvgDistM, want)
	}
}

func TestDeathTotalsFor_MapFilterIgnoresDePrefix(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	for _, m := range []string{"mirage", "de_mirage", "MIRAGE"} {
		got, err := db.DeathTotalsFor(42, DeathFilter{MapName: m})
		if err != nil {
			t.Fatalf("DeathTotalsFor(%q): %v", m, err)
		}
		if got.Deaths != 2 {
			t.Errorf("map %q: Deaths = %d, want 2", m, got.Deaths)
		}
	}
}

func TestDeathTotalsFor_DateWindowIsHalfOpen(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	// Since is inclusive, Before is exclusive.
	got, err := db.DeathTotalsFor(42, DeathFilter{Since: "2026-01-10", Before: "2026-02-20"})
	if err != nil {
		t.Fatalf("DeathTotalsFor: %v", err)
	}
	if got.Deaths != 2 {
		t.Errorf("Deaths = %d, want 2 (Jan rows only; Before excludes the Feb date)", got.Deaths)
	}
}

func TestDeathTotalsFor_PhaseFilter(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	got, err := db.DeathTotalsFor(42, DeathFilter{Phase: "mid"})
	if err != nil {
		t.Fatalf("DeathTotalsFor: %v", err)
	}
	if got.Deaths != 2 {
		t.Errorf("Deaths = %d, want 2 mid-round deaths", got.Deaths)
	}
}

func TestDeathsByWeapon_OrdersByCountAndRespectsLimit(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	rows, err := db.DeathsByWeapon(42, DeathFilter{}, 0)
	if err != nil {
		t.Fatalf("DeathsByWeapon: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d weapon rows, want 3", len(rows))
	}
	if rows[0].Key != "AK-47" || rows[0].Deaths != 2 {
		t.Errorf("first row = %s (%d), want AK-47 (2) — most frequent first", rows[0].Key, rows[0].Deaths)
	}

	limited, err := db.DeathsByWeapon(42, DeathFilter{}, 1)
	if err != nil {
		t.Fatalf("DeathsByWeapon limited: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limit 1 returned %d rows", len(limited))
	}
}

func TestDeathsByDistanceBin(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	rows, err := db.DeathsByDistanceBin(42, DeathFilter{})
	if err != nil {
		t.Fatalf("DeathsByDistanceBin: %v", err)
	}
	// Distances 10, 20, 30, 40 land in 10-15m, 20-30m, 30m+, 30m+.
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Key] = r.Deaths
	}
	if counts["10-15m"] != 1 || counts["20-30m"] != 1 || counts["30m+"] != 2 {
		t.Errorf("distance bins = %v, want 10-15m:1, 20-30m:1, 30m+:2", counts)
	}
}

func TestDeathQueries_EmptyPlayerIsNotAnError(t *testing.T) {
	db := openMemDB(t)
	seedDeaths(t, db)

	got, err := db.DeathTotalsFor(12345, DeathFilter{})
	if err != nil {
		t.Fatalf("DeathTotalsFor on unknown player: %v", err)
	}
	if got.Deaths != 0 {
		t.Errorf("Deaths = %d, want 0", got.Deaths)
	}
	rows, err := db.DeathsByPhase(12345, DeathFilter{})
	if err != nil {
		t.Fatalf("DeathsByPhase on unknown player: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want none", len(rows))
	}
}
