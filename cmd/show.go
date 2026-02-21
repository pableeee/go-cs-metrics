package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var showPlayerID uint64

var showCmd = &cobra.Command{
	Use:   "show <hash-prefix>",
	Short: "Show stored match stats by hash prefix",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	showCmd.Flags().Uint64Var(&showPlayerID, "player", 0, "highlight player SteamID64")
}

func runShow(cmd *cobra.Command, args []string) error {
	prefix := args[0]

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

	stats, err := db.GetPlayerMatchStats(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("get player stats: %w", err)
	}
	sideStats, err := db.GetPlayerSideStats(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("get side stats: %w", err)
	}
	weaponStats, err := db.GetPlayerWeaponStats(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("get weapon stats: %w", err)
	}
	duelSegs, err := db.GetPlayerDuelSegments(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("get duel segments: %w", err)
	}

	report.PrintMatchSummary(os.Stdout, *demo)
	report.PrintPlayerTable(stats, showPlayerID)
	report.PrintPlayerSideTable(os.Stdout, sideStats, showPlayerID)
	report.PrintDuelTable(os.Stdout, stats, showPlayerID)
	report.PrintAWPTable(os.Stdout, stats, showPlayerID)
	report.PrintFHHSTable(os.Stdout, duelSegs, stats, showPlayerID)
	report.PrintWeaponTable(os.Stdout, weaponStats, stats, showPlayerID)
	return nil
}
