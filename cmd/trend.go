package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var trendCmd = &cobra.Command{
	Use:   "trend <steamid64>",
	Short: "Chronological per-match performance trend for a player",
	Args:  cobra.ExactArgs(1),
	RunE:  runTrend,
}

func runTrend(cmd *cobra.Command, args []string) error {
	steamID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid steamid64: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	stats, err := db.GetAllPlayerMatchStats(steamID)
	if err != nil {
		return fmt.Errorf("query stats: %w", err)
	}
	if len(stats) == 0 {
		fmt.Println("no matches found")
		return nil
	}

	clutchMap, err := db.GetPlayerClutchStatsByMatch(steamID)
	if err != nil {
		return fmt.Errorf("query clutch stats: %w", err)
	}

	report.PrintTrendTable(os.Stdout, stats)
	report.PrintAimTrendTable(os.Stdout, stats)
	report.PrintClutchTrendTable(os.Stdout, stats, clutchMap)
	return nil
}

