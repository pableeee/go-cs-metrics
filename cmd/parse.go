package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	playerSteamID uint64
	matchType     string
	parseTier     string
	parseBaseline bool
)

var parseCmd = &cobra.Command{
	Use:   "parse <demo.dem>",
	Short: "Parse a CS2 demo file and store metrics",
	Args:  cobra.ExactArgs(1),
	RunE:  runParse,
}

func init() {
	parseCmd.Flags().Uint64Var(&playerSteamID, "player", 0, "focus player SteamID64")
	parseCmd.Flags().StringVar(&matchType, "type", "Competitive", "match type label")
	parseCmd.Flags().StringVar(&parseTier, "tier", "", "tier label for baseline comparisons (e.g. faceit-5)")
	parseCmd.Flags().BoolVar(&parseBaseline, "baseline", false, "mark this demo as a baseline reference match")
}

func runParse(cmd *cobra.Command, args []string) error {
	demoPath := args[0]

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	fmt.Fprintf(os.Stdout, "Parsing %s...\n", demoPath)
	raw, err := parser.ParseDemo(demoPath, matchType)
	if err != nil {
		return fmt.Errorf("parse demo: %w", err)
	}

	exists, err := db.DemoExists(raw.DemoHash)
	if err != nil {
		return fmt.Errorf("check demo: %w", err)
	}
	if exists {
		fmt.Fprintf(os.Stdout, "Demo %s already stored — showing cached results.\n\n", raw.DemoHash[:12])
		return showByHash(db, raw.DemoHash)
	}

	matchStats, roundStats, weaponStats, err := aggregator.Aggregate(raw)
	if err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}

	// Compute CT/T scores from rounds.
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

	report.PrintMatchSummary(os.Stdout, summary)
	report.PrintPlayerTable(matchStats, playerSteamID)
	report.PrintDuelTable(os.Stdout, matchStats, playerSteamID)
	report.PrintAWPTable(os.Stdout, matchStats, playerSteamID)
	report.PrintWeaponTable(os.Stdout, weaponStats, matchStats, playerSteamID)
	return nil
}

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

func showByHash(db *storage.DB, hash string) error {
	demo, err := db.GetDemoByPrefix(hash)
	if err != nil || demo == nil {
		return fmt.Errorf("demo not found: %s", hash)
	}
	stats, err := db.GetPlayerMatchStats(hash)
	if err != nil {
		return err
	}
	weaponStats, err := db.GetPlayerWeaponStats(hash)
	if err != nil {
		return err
	}
	report.PrintMatchSummary(os.Stdout, *demo)
	report.PrintPlayerTable(stats, playerSteamID)
	report.PrintDuelTable(os.Stdout, stats, playerSteamID)
	report.PrintAWPTable(os.Stdout, stats, playerSteamID)
	report.PrintWeaponTable(os.Stdout, weaponStats, stats, playerSteamID)
	return nil
}
