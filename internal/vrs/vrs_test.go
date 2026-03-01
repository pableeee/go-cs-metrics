package vrs

import (
	"os"
	"testing"
)

const sampleMarkdown = `
# Counter-Strike Regional Standings — Live

### Standings as of 2026_02_02

| Rank | Points | Team | Roster | Details |
|------|--------|------|--------|---------|
| 1 | 1951 | FURIA | FalleN, KSCERATO, molodoy, YEKINDAR, yuurih | [details](https://example.com) |
| 2 | 1843 | G2 | NiKo, huNter-, jks, nexa, ropz | [details](https://example.com) |
| 3 | 1721 | FaZe | broky, karrigan, rain, ropz, twistzz | [details](https://example.com) |
| 4 | 1610 | Vitality | apEX, dupreeh, flameZ, mezii, ZywOo | [details](https://example.com) |
| 5 | 1520 | NAVI | B1T, iM, jL, Perfecto, s1mple | [details](https://example.com) |
`

func TestParseGlobalStandings(t *testing.T) {
	snap, err := ParseGlobalStandings(sampleMarkdown)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Date != "2026-02-02" {
		t.Errorf("date: got %q, want %q", snap.Date, "2026-02-02")
	}
	if len(snap.Entries) != 5 {
		t.Fatalf("entries: got %d, want 5", len(snap.Entries))
	}

	g2 := snap.Entries[1]
	if g2.TeamName != "G2" {
		t.Errorf("G2 team name: got %q", g2.TeamName)
	}
	if g2.Rank != 2 {
		t.Errorf("G2 rank: got %d, want 2", g2.Rank)
	}
	if g2.Points != 1843 {
		t.Errorf("G2 points: got %f, want 1843", g2.Points)
	}
	if len(g2.Roster) != 5 {
		t.Errorf("G2 roster len: got %d, want 5", len(g2.Roster))
	}
}

func TestParseGlobalStandingsNoDate(t *testing.T) {
	_, err := ParseGlobalStandings("| 1 | 100 | Team | Player1, Player2, Player3 |\n")
	if err == nil {
		t.Error("expected error for missing date, got nil")
	}
}

func TestParseGlobalStandingsNoEntries(t *testing.T) {
	_, err := ParseGlobalStandings("### Standings as of 2026_01_01\n\nNo table here.\n")
	if err == nil {
		t.Error("expected error for no entries, got nil")
	}
}

func TestMatchOpponentToVRS(t *testing.T) {
	path := t.TempDir() + "/vrs_test.db"
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	snap, _ := ParseGlobalStandings(sampleMarkdown)
	if err := store.InsertSnapshot(snap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	tests := []struct {
		name      string
		opponents []string
		date      string
		wantOK    bool
		wantTeam  string
		wantRank  int
	}{
		{
			name:      "exact 5-match",
			opponents: []string{"NiKo", "huNter-", "jks", "nexa", "ropz"},
			date:      "2026-02-05",
			wantOK:    true,
			wantTeam:  "G2",
			wantRank:  2,
		},
		{
			name:      "case insensitive",
			opponents: []string{"niko", "hunter-", "JKS", "NEXA", "ropz"},
			date:      "2026-02-05",
			wantOK:    true,
			wantTeam:  "G2",
			wantRank:  2,
		},
		{
			name:      "4-of-5 match (1 stand-in)",
			opponents: []string{"NiKo", "huNter-", "jks", "nexa", "STANDIN"},
			date:      "2026-02-05",
			wantOK:    true,
			wantTeam:  "G2",
			wantRank:  2,
		},
		{
			name:      "3-of-5 match",
			opponents: []string{"NiKo", "huNter-", "jks", "STANDIN1", "STANDIN2"},
			date:      "2026-02-05",
			wantOK:    true,
			wantTeam:  "G2",
			wantRank:  2,
		},
		{
			name:      "2-of-5 match borderline unique",
			opponents: []string{"NiKo", "huNter-", "X1", "X2", "X3"},
			date:      "2026-02-05",
			wantOK:    true, // borderline: unique team with 2 matches
			wantTeam:  "G2",
			wantRank:  2,
		},
		{
			name:      "no match",
			opponents: []string{"PlayerA", "PlayerB", "PlayerC", "PlayerD", "PlayerE"},
			date:      "2026-02-05",
			wantOK:    false,
		},
		{
			name:      "date before snapshot",
			opponents: []string{"NiKo", "huNter-", "jks", "nexa", "ropz"},
			date:      "2020-01-01",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTeam, gotRank, gotOK := MatchOpponentToVRS(tc.opponents, tc.date, store)
			if gotOK != tc.wantOK {
				t.Errorf("ok: got %v, want %v", gotOK, tc.wantOK)
			}
			if tc.wantOK {
				if gotTeam != tc.wantTeam {
					t.Errorf("team: got %q, want %q", gotTeam, tc.wantTeam)
				}
				if gotRank != tc.wantRank {
					t.Errorf("rank: got %d, want %d", gotRank, tc.wantRank)
				}
			}
		})
	}
}

func TestVRSStoreRoundtrip(t *testing.T) {
	path := t.TempDir() + "/vrs_rt.db"
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	snap, _ := ParseGlobalStandings(sampleMarkdown)
	if err := store.InsertSnapshot(snap); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Idempotent re-insert.
	if err := store.InsertSnapshot(snap); err != nil {
		t.Fatalf("re-insert: %v", err)
	}

	entries, err := store.LatestSnapshotBefore("2026-03-01")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("entries: got %d, want 5", len(entries))
	}
	if entries[0].TeamName != "FURIA" || entries[0].Rank != 1 {
		t.Errorf("first entry: got %+v", entries[0])
	}

	dates, err := store.ExistingDates()
	if err != nil {
		t.Fatalf("existing dates: %v", err)
	}
	if len(dates) != 1 || dates[0] != "2026-02-02" {
		t.Errorf("existing dates: got %v", dates)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
