package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored demos",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	demos, err := db.ListDemos()
	if err != nil {
		return fmt.Errorf("list demos: %w", err)
	}
	if len(demos) == 0 {
		fmt.Fprintln(os.Stdout, "No demos stored yet. Run 'csmetrics parse <demo.dem>' to add one.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "%-14s  %-12s  %-10s  %-12s  %6s  %s\n",
		"HASH", "MAP", "DATE", "TYPE", "SCORE", "TICK")
	fmt.Fprintf(os.Stdout, "%-14s  %-12s  %-10s  %-12s  %6s  %s\n",
		"──────────────", "────────────", "──────────", "────────────", "──────", "────")
	for _, d := range demos {
		score := fmt.Sprintf("%d-%d", d.CTScore, d.TScore)
		fmt.Fprintf(os.Stdout, "%-14s  %-12s  %-10s  %-12s  %6s  %.0f\n",
			d.DemoHash[:12], d.MapName, d.MatchDate, d.MatchType, score, d.Tickrate)
	}
	return nil
}
