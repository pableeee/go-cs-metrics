package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
)

// MatchMapSpec describes one map in the playoff-matches spec file.
type MatchMapSpec struct {
	Map      string `json:"map"`
	Picker   string `json:"picker"`               // "A", "B", or "D"
	AStartCT *bool  `json:"a_start_ct,omitempty"` // true=A started CT, false=A started T, nil=unknown
	AWon     *bool  `json:"a_won,omitempty"`       // true=A won the map, false=B won, nil=unknown
}

// MatchSpec is one historical match entry in the JSON spec file.
type MatchSpec struct {
	MatchID     string         `json:"match_id"`
	EventDate   string         `json:"event_date"`    // "YYYY-MM-DD"; stats computed from demos strictly before this date
	Format      string         `json:"format"`        // "bo3" or "bo5"
	TeamARoster string         `json:"team_a_roster"` // path to roster JSON
	TeamBRoster string         `json:"team_b_roster"` // path to roster JSON
	Maps        []MatchMapSpec `json:"maps"`
	AWonSeries  bool           `json:"a_won_series"`
}

// btMapStats matches the simbo3 MapStats JSON schema.
type btMapStats struct {
	MapWinPct     float64 `json:"map_win_pct"`
	CTRoundWinPct float64 `json:"ct_round_win_pct"`
	TRoundWinPct  float64 `json:"t_round_win_pct"`
	Matches3m     int     `json:"matches_3m"`
}

// btTeamStats matches the simbo3 TeamStats JSON schema.
type btTeamStats struct {
	Team              string                `json:"team"`
	PlayersRating2_3m []float64             `json:"players_rating2_3m"`
	Maps              map[string]btMapStats `json:"maps"`
}

// btMapRecord is one map in the output MatchRecord.
type btMapRecord struct {
	Map      string `json:"map"`
	Picker   string `json:"picker"`
	AStartCT *bool  `json:"a_start_ct,omitempty"`
	AWon     *bool  `json:"a_won,omitempty"`
}

// btMatchRecord is a simbo3-compatible MatchRecord for the backtest dataset.
type btMatchRecord struct {
	MatchID    string        `json:"match_id,omitempty"`
	Format     string        `json:"format,omitempty"`
	TeamA      btTeamStats   `json:"team_a"`
	TeamB      btTeamStats   `json:"team_b"`
	Maps       []btMapRecord `json:"maps"`
	AWonSeries bool          `json:"a_won_series"`
}

var (
	bdsSpec     string
	bdsOut      string
	bdsWindow   int
	bdsQuorum   int
	bdsHalfLife float64
)

var backtestDatasetCmd = &cobra.Command{
	Use:   "backtest-dataset",
	Short: "Build a simbo3 backtest dataset from a match spec file",
	Long: `Reads a JSON spec of historical matches and outputs a MatchRecord array
for use with "simbo3 backtest". Each match's team stats are computed from
demos dated strictly before event_date, eliminating temporal lookahead bias.

Example:
  csmetrics backtest-dataset \
    --spec backtest/playoff-matches.json \
    --out  backtest/playoffs21.json \
    --window 90`,
	RunE: runBacktestDataset,
}

func init() {
	backtestDatasetCmd.Flags().StringVar(&bdsSpec, "spec", "", "match spec JSON file (required)")
	backtestDatasetCmd.Flags().StringVar(&bdsOut, "out", "", "output file path (stdout if omitted)")
	backtestDatasetCmd.Flags().IntVar(&bdsWindow, "window", 90, "look-back window in days before event_date")
	backtestDatasetCmd.Flags().IntVar(&bdsQuorum, "quorum", 3, "min roster players per demo to include it")
	backtestDatasetCmd.Flags().Float64Var(&bdsHalfLife, "half-life", 35,
		"temporal decay half-life in days (0 = uniform weights)")
	_ = backtestDatasetCmd.MarkFlagRequired("spec")
}

func runBacktestDataset(_ *cobra.Command, _ []string) error {
	raw, err := os.ReadFile(bdsSpec)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}
	var specs []MatchSpec
	if err := json.Unmarshal(raw, &specs); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	var records []btMatchRecord
	skipped := 0
	for _, spec := range specs {
		fmt.Fprintf(os.Stderr, "\n=== %s ===\n", spec.MatchID)

		eventDate, err := time.Parse("2006-01-02", spec.EventDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP: invalid event_date %q: %v\n", spec.EventDate, err)
			skipped++
			continue
		}
		since := eventDate.AddDate(0, 0, -bdsWindow)

		teamA, err := buildBTTeamStats(db, spec.TeamARoster, since, eventDate, bdsQuorum, "A", bdsHalfLife)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP: team A: %v\n", err)
			skipped++
			continue
		}
		teamB, err := buildBTTeamStats(db, spec.TeamBRoster, since, eventDate, bdsQuorum, "B", bdsHalfLife)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP: team B: %v\n", err)
			skipped++
			continue
		}

		maps := make([]btMapRecord, len(spec.Maps))
		for i, m := range spec.Maps {
			maps[i] = btMapRecord{Map: m.Map, Picker: m.Picker, AStartCT: m.AStartCT, AWon: m.AWon}
		}

		records = append(records, btMatchRecord{
			MatchID:    spec.MatchID,
			Format:     spec.Format,
			TeamA:      *teamA,
			TeamB:      *teamB,
			Maps:       maps,
			AWonSeries: spec.AWonSeries,
		})
		fmt.Fprintf(os.Stderr, "  OK: %s vs %s\n", teamA.Team, teamB.Team)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	if bdsOut == "" {
		fmt.Println(string(data))
		return nil
	}
	if err := os.WriteFile(bdsOut, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write %s: %w", bdsOut, err)
	}
	fmt.Fprintf(os.Stderr, "\nWrote %d record(s) to %s (%d skipped)\n", len(records), bdsOut, skipped)
	return nil
}

// buildBTTeamStats loads a roster file and computes team stats from demos in
// the window [since, before) where before=event_date eliminates lookahead bias.
func buildBTTeamStats(db *storage.DB, rosterPath string, since, before time.Time, quorum int, label string, halfLife float64) (*btTeamStats, error) {
	raw, err := os.ReadFile(rosterPath)
	if err != nil {
		return nil, fmt.Errorf("read roster %s: %w", rosterPath, err)
	}
	var rf rosterFile
	if err := json.Unmarshal(raw, &rf); err != nil {
		return nil, fmt.Errorf("parse roster %s: %w", rosterPath, err)
	}
	if len(rf.Players) == 0 {
		return nil, fmt.Errorf("roster %s has no players", rosterPath)
	}

	fmt.Fprintf(os.Stderr, "  [%s] %s — window [%s, %s) quorum=%d\n",
		label, rf.Team, since.Format("2006-01-02"), before.Format("2006-01-02"), quorum)

	demos, err := db.QualifyingDemosWindow(rf.Players, since, before, quorum)
	if err != nil {
		return nil, fmt.Errorf("qualifying demos: %w", err)
	}
	if len(demos) == 0 {
		return nil, fmt.Errorf("no qualifying demos for %s in [%s, %s) quorum=%d",
			rf.Team, since.Format("2006-01-02"), before.Format("2006-01-02"), quorum)
	}
	fmt.Fprintf(os.Stderr, "    %d qualifying demo(s)\n", len(demos))

	byMap := make(map[string][]string)
	allHashes := make([]string, 0, len(demos))
	for _, d := range demos {
		byMap[d.MapName] = append(byMap[d.MapName], d.Hash)
		allHashes = append(allHashes, d.Hash)
	}

	weights := demoWeights(demos, before, halfLife)

	maps := make(map[string]btMapStats, len(byMap))
	for mapName, hashes := range byMap {
		outcomes, err := db.MapWinOutcomes(rf.Players, hashes)
		if err != nil {
			return nil, fmt.Errorf("map win outcomes %s: %w", mapName, err)
		}
		mapWinPct := weightedMapWinPct(outcomes, weights)
		n := len(outcomes)

		sidesByDemo, err := db.RoundSideStatsByDemo(rf.Players, hashes)
		if err != nil {
			return nil, fmt.Errorf("round side stats %s: %w", mapName, err)
		}
		ctPct, tPct := weightedSideStats(sidesByDemo, weights)

		maps[mapName] = btMapStats{
			MapWinPct:     roundTo2dp(mapWinPct),
			CTRoundWinPct: roundTo2dp(ctPct),
			TRoundWinPct:  roundTo2dp(tPct),
			Matches3m:     n,
		}
		fmt.Fprintf(os.Stderr, "    %-12s %2d match(es)  win=%.2f CT=%.2f T=%.2f\n",
			mapName, n, mapWinPct, ctPct, tPct)
	}

	byDemo, err := db.RosterMatchTotalsByDemo(rf.Players, allHashes)
	if err != nil {
		return nil, fmt.Errorf("roster match totals: %w", err)
	}
	ratings := buildWeightedRatings(byDemo, weights)

	return &btTeamStats{
		Team:              rf.Team,
		PlayersRating2_3m: ratings,
		Maps:              maps,
	}, nil
}
