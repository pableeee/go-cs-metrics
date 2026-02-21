package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// roundsCmd is the cobra command for per-round drill-down for one player in one match.
var roundsCmd = &cobra.Command{
	Use:   "rounds <hash-prefix> <steamid64>",
	Short: "Per-round drill-down for one player in one match",
	Args:  cobra.ExactArgs(2),
	RunE:  runRounds,
}

// runRounds loads per-round stats for a player in a match and prints the drill-down table.
func runRounds(cmd *cobra.Command, args []string) error {
	prefix := args[0]
	steamID, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid SteamID64 %q: %w", args[1], err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	demo, err := db.GetDemoByPrefix(prefix)
	if err != nil {
		return fmt.Errorf("query demo: %w", err)
	}
	if demo == nil {
		fmt.Fprintf(os.Stderr, "No demo found with hash prefix %q\n", prefix)
		return nil
	}

	roundStats, err := db.GetPlayerRoundStats(demo.DemoHash, steamID)
	if err != nil {
		return fmt.Errorf("get round stats: %w", err)
	}
	if len(roundStats) == 0 {
		fmt.Fprintf(os.Stderr, "No round data found for player %d in demo %s\n", steamID, prefix)
		return nil
	}

	// Get player name from match stats.
	matchStats, err := db.GetPlayerMatchStats(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("get match stats: %w", err)
	}
	playerName := strconv.FormatUint(steamID, 10)
	for _, ms := range matchStats {
		if ms.SteamID == steamID {
			playerName = ms.Name
			break
		}
	}

	report.PrintRoundDetailTable(os.Stdout, roundStats, playerName, demo.MapName)
	return nil
}
