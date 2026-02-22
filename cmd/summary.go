package cmd

import (
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
)

// summaryCmd is the cobra command for displaying a high-level database overview.
var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show a high-level overview of the database",
	Long: `Display aggregate statistics about all demos stored in the database:
total match count, date range, map breakdown, most active players,
and match type distribution.`,
	Args: cobra.NoArgs,
	RunE: runSummary,
}

func runSummary(cmd *cobra.Command, args []string) error {
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	ov, err := db.GetDBOverview()
	if err != nil {
		return fmt.Errorf("get overview: %w", err)
	}
	if ov.TotalMatches == 0 {
		fmt.Fprintln(os.Stdout, "No demos stored yet. Run 'csmetrics parse <demo.dem>' to add one.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "\n=== Database Summary ===\n\n")
	fmt.Fprintf(os.Stdout, "  Demos stored  : %d\n", ov.TotalMatches)
	fmt.Fprintf(os.Stdout, "  Date range    : %s → %s\n", ov.EarliestMatch, ov.LatestMatch)
	fmt.Fprintf(os.Stdout, "  Unique maps   : %d\n", ov.UniqueMaps)
	fmt.Fprintf(os.Stdout, "  Players seen  : %d\n", ov.UniquePlayers)
	fmt.Fprintf(os.Stdout, "  Total rounds  : %d\n", ov.TotalRounds)

	// Map breakdown.
	maps, err := db.GetMapStats()
	if err != nil {
		return fmt.Errorf("get map stats: %w", err)
	}
	fmt.Fprintf(os.Stdout, "\n--- Maps ---\n\n")
	mt := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	mt.Header("MAP", "MATCHES", "CT WINS", "T WINS", "CT WIN%")
	for _, m := range maps {
		total := m.CTWins + m.TWins
		ctPct := 0.0
		if total > 0 {
			ctPct = 100.0 * float64(m.CTWins) / float64(total)
		}
		mt.Append(
			m.MapName,
			fmt.Sprintf("%d", m.Matches),
			fmt.Sprintf("%d", m.CTWins),
			fmt.Sprintf("%d", m.TWins),
			fmt.Sprintf("%.0f%%", ctPct),
		)
	}
	mt.Render()

	// Most active players.
	players, err := db.GetTopPlayersByMatches(10)
	if err != nil {
		return fmt.Errorf("get top players: %w", err)
	}
	fmt.Fprintf(os.Stdout, "\n--- Most Active Players ---\n\n")
	pt := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	pt.Header("NAME", "STEAM ID", "MATCHES", "AVG K/D", "AVG ADR", "AVG KAST%")
	for _, p := range players {
		pt.Append(
			p.Name,
			p.SteamID,
			fmt.Sprintf("%d", p.Matches),
			fmt.Sprintf("%.2f", p.AvgKD),
			fmt.Sprintf("%.1f", p.AvgADR),
			fmt.Sprintf("%.0f%%", p.AvgKAST),
		)
	}
	pt.Render()

	// Match type breakdown — only shown when more than one type is present.
	types, err := db.GetMatchTypeCounts()
	if err != nil {
		return fmt.Errorf("get match types: %w", err)
	}
	if len(types) > 1 {
		fmt.Fprintf(os.Stdout, "\n--- Match Types ---\n\n")
		tt := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(tablewriter.Config{
			Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
			Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
		}))
		tt.Header("TYPE", "MATCHES")
		for _, t := range types {
			tt.Append(t.MatchType, fmt.Sprintf("%d", t.Matches))
		}
		tt.Render()
	}

	return nil
}
