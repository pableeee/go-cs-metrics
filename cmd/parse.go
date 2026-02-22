package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// parse command flags.
var (
	// playerSteamID is the optional focus player SteamID64 for highlighted output.
	playerSteamID uint64
	// matchType is the label stored alongside the demo (e.g. "Competitive", "FACEIT").
	matchType string
	// parseTier is the tier label for baseline comparisons (e.g. "faceit-5").
	parseTier string
	// parseBaseline marks the demo as a baseline reference match when true.
	parseBaseline bool
	// parseDir is an optional directory path; all *.dem files inside are parsed.
	parseDir string
)

// parseCmd is the cobra command for parsing a CS2 demo file and storing its metrics.
var parseCmd = &cobra.Command{
	Use:   "parse [<demo.dem>...] [--dir <directory>]",
	Short: "Parse one or more CS2 demo files and store metrics",
	Long: `Parse CS2 demo files and store all metrics in the database.

Single file:
  parse match.dem

Multiple files (shell glob):
  parse /replays/*.dem

Whole directory:
  parse --dir /path/to/replays

When more than one demo is provided, full tables are suppressed and a
brief status line is printed per demo instead.`,
	Args: cobra.ArbitraryArgs,
	RunE: runParse,
}

func init() {
	parseCmd.Flags().Uint64Var(&playerSteamID, "player", 0, "focus player SteamID64")
	parseCmd.Flags().StringVar(&matchType, "type", "Competitive", "match type label")
	parseCmd.Flags().StringVar(&parseTier, "tier", "", "tier label for baseline comparisons (e.g. faceit-5)")
	parseCmd.Flags().BoolVar(&parseBaseline, "baseline", false, "mark this demo as a baseline reference match")
	parseCmd.Flags().StringVar(&parseDir, "dir", "", "directory containing .dem files to parse in bulk")
}

// runParse parses one or more demo files, aggregates metrics, stores them in
// the database, and prints report tables. When more than one demo is provided
// (via args or --dir), full tables are suppressed and a brief status line is
// printed per demo instead.
func runParse(cmd *cobra.Command, args []string) error {
	// Collect demo paths from positional args and --dir.
	paths := append([]string(nil), args...)
	if parseDir != "" {
		entries, err := os.ReadDir(parseDir)
		if err != nil {
			return fmt.Errorf("read dir: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".dem" {
				paths = append(paths, filepath.Join(parseDir, e.Name()))
			}
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("no demo files specified; provide file args or --dir")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	bulk := len(paths) > 1
	var stored, skipped, failed int

	for i, demoPath := range paths {
		if bulk {
			fmt.Fprintf(os.Stdout, "[%d/%d] %s\n", i+1, len(paths), filepath.Base(demoPath))
		} else {
			fmt.Fprintf(os.Stdout, "Parsing %s...\n", demoPath)
		}

		t0 := time.Now()
		raw, err := parser.ParseDemo(demoPath, matchType)
		parseElapsed := time.Since(t0)
		if err != nil {
			if bulk {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				failed++
				continue
			}
			return fmt.Errorf("parse demo: %w", err)
		}

		exists, err := db.DemoExists(raw.DemoHash)
		if err != nil {
			return fmt.Errorf("check demo: %w", err)
		}
		if exists {
			if bulk {
				fmt.Fprintf(os.Stdout, "  already stored — skipped\n")
				skipped++
				continue
			}
			fmt.Fprintf(os.Stdout, "Demo %s already stored — showing cached results.\n\n", raw.DemoHash[:12])
			return showByHash(db, raw.DemoHash)
		}

		t1 := time.Now()
		matchStats, roundStats, weaponStats, duelSegs, err := aggregator.Aggregate(raw)
		aggElapsed := time.Since(t1)
		if err != nil {
			if bulk {
				fmt.Fprintf(os.Stderr, "  aggregate error: %v\n", err)
				failed++
				continue
			}
			return fmt.Errorf("aggregate: %w", err)
		}

		ctScore, tScore := computeScore(raw.Rounds)
		summary := model.MatchSummary{
			DemoHash:   raw.DemoHash,
			MapName:    raw.MapName,
			MatchDate:  raw.MatchDate,
			MatchType:  raw.MatchType,
			Tickrate:   raw.Tickrate,
			CTScore:    ctScore,
			TScore:     tScore,
			Tier:       parseTier,
			IsBaseline: parseBaseline,
		}

		if err := db.InsertDemo(summary); err != nil {
			return fmt.Errorf("insert demo: %w", err)
		}
		if err := db.InsertPlayerMatchStats(matchStats); err != nil {
			return fmt.Errorf("insert player stats: %w", err)
		}
		if err := db.InsertPlayerRoundStats(roundStats); err != nil {
			return fmt.Errorf("insert round stats: %w", err)
		}
		if err := db.InsertPlayerWeaponStats(weaponStats); err != nil {
			return fmt.Errorf("insert weapon stats: %w", err)
		}
		if err := db.InsertPlayerDuelSegments(duelSegs); err != nil {
			return fmt.Errorf("insert duel segments: %w", err)
		}

		if bulk {
			fmt.Fprintf(os.Stdout, "  stored: %s  %s  %d–%d  %d players  %d rounds  (parse %s  agg %s  total %s)\n",
				summary.MapName, summary.MatchDate, ctScore, tScore,
				len(matchStats), len(raw.Rounds),
				parseElapsed.Round(time.Millisecond),
				aggElapsed.Round(time.Millisecond),
				(parseElapsed+aggElapsed).Round(time.Millisecond))
			stored++
			continue
		}

		fmt.Fprintf(os.Stdout, "  parse: %s  aggregate: %s  total: %s\n\n",
			parseElapsed.Round(time.Millisecond),
			aggElapsed.Round(time.Millisecond),
			(parseElapsed+aggElapsed).Round(time.Millisecond))

		clutch, err := db.GetClutchStatsByDemo(summary.DemoHash)
		if err != nil {
			return fmt.Errorf("get clutch stats: %w", err)
		}
		report.PrintMatchSummary(os.Stdout, summary)
		report.PrintPlayerRosterTable(os.Stdout, matchStats)
		report.PrintPlayerTable(matchStats, playerSteamID)
		report.PrintDuelTable(os.Stdout, matchStats, playerSteamID)
		report.PrintAWPTable(os.Stdout, matchStats, playerSteamID)
		report.PrintWeaponTable(os.Stdout, weaponStats, matchStats, playerSteamID)
		report.PrintAimTimingTable(os.Stdout, matchStats, playerSteamID)
		report.PrintMatchClutchTable(os.Stdout, matchStats, clutch)
	}

	if bulk {
		fmt.Fprintf(os.Stdout, "\nDone: %d stored, %d skipped, %d failed (total %d)\n",
			stored, skipped, failed, len(paths))
	}
	return nil
}

// computeScore tallies the CT and T round wins from the parsed round data.
func computeScore(rounds []model.RawRound) (ctScore, tScore int) {
	for _, r := range rounds {
		switch r.WinnerTeam {
		case model.TeamCT:
			ctScore++
		case model.TeamT:
			tScore++
		}
	}
	return
}

// showByHash loads a previously stored demo by its full hash and prints all
// report tables. Used when a re-parsed demo is already in the database.
func showByHash(db *storage.DB, hash string) error {
	demo, err := db.GetDemoByPrefix(hash)
	if err != nil || demo == nil {
		return fmt.Errorf("demo not found: %s", hash)
	}
	stats, err := db.GetPlayerMatchStats(hash)
	if err != nil {
		return err
	}
	sideStats, err := db.GetPlayerSideStats(hash)
	if err != nil {
		return err
	}
	weaponStats, err := db.GetPlayerWeaponStats(hash)
	if err != nil {
		return err
	}
	clutch, err := db.GetClutchStatsByDemo(hash)
	if err != nil {
		return fmt.Errorf("get clutch stats: %w", err)
	}
	report.PrintMatchSummary(os.Stdout, *demo)
	report.PrintPlayerRosterTable(os.Stdout, stats)
	report.PrintPlayerTable(stats, playerSteamID)
	report.PrintPlayerSideTable(os.Stdout, sideStats, playerSteamID)
	report.PrintDuelTable(os.Stdout, stats, playerSteamID)
	report.PrintAWPTable(os.Stdout, stats, playerSteamID)
	report.PrintWeaponTable(os.Stdout, weaponStats, stats, playerSteamID)
	report.PrintAimTimingTable(os.Stdout, stats, playerSteamID)
	report.PrintMatchClutchTable(os.Stdout, stats, clutch)
	return nil
}
