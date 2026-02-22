package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// showPlayerID is the optional SteamID64 used to highlight a player in the show output.
var showPlayerID uint64

// showCmd is the cobra command that re-displays stored match stats by hash prefix.
var showCmd = &cobra.Command{
	Use:   "show <hash-prefix>",
	Short: "Show stored match stats by hash prefix",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	showCmd.Flags().Uint64Var(&showPlayerID, "player", 0, "highlight player SteamID64")
}

// runShow looks up a demo by hash prefix and prints all its report tables.
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
	report.PrintMatchSummary(os.Stdout, *demo)
	report.PrintPlayerRosterTable(os.Stdout, stats)
	report.PrintPlayerTable(stats, showPlayerID)
	report.PrintPlayerSideTable(os.Stdout, sideStats, showPlayerID)
	report.PrintDuelTable(os.Stdout, stats, showPlayerID)
	report.PrintAWPTable(os.Stdout, stats, showPlayerID)
	report.PrintWeaponTable(os.Stdout, weaponStats, stats, showPlayerID)
	report.PrintAimTimingTable(os.Stdout, stats, showPlayerID)
	return nil
}
