package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
)

var sqlCmd = &cobra.Command{
	Use:   "sql <query>",
	Short: "Run a raw SQL query against the metrics database",
	Long: `Run an arbitrary SQL query against the metrics database and print results as a table.

Schema overview:
  demos(hash, map_name, match_date, match_type, tickrate, ct_score, t_score, tier, is_baseline)
  player_match_stats(demo_hash, steam_id TEXT, name, team, kills, assists, deaths,
    headshot_kills, total_damage, rounds_played, kast_rounds, role, median_ttk_ms,
    median_ttd_ms, one_tap_kills, ...)
  player_round_stats(demo_hash, steam_id TEXT, round_number, team, kills, assists,
    damage, buy_type, is_post_plant, is_in_clutch, clutch_enemy_count, ...)
  player_weapon_stats(demo_hash, steam_id TEXT, weapon, kills, headshot_kills, damage, hits)
  player_duel_segments(demo_hash, steam_id TEXT, weapon_bucket, distance_bin,
    duel_count, first_hit_count, first_hit_hs_count, median_corr_deg, median_expo_win_ms)

Note: steam_id is stored as TEXT. Use quotes: WHERE steam_id = '76561198031906602'`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSQL,
}

func runSQL(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	cols, rows, err := db.QueryRaw(query)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("(no rows)")
		return nil
	}

	table := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))

	colsAny := make([]any, len(cols))
	for i, c := range cols {
		colsAny[i] = c
	}
	table.Header(colsAny...)

	for _, row := range rows {
		rowAny := make([]any, len(row))
		for i, v := range row {
			rowAny[i] = v
		}
		table.Append(rowAny...)
	}
	table.Render()
	fmt.Fprintf(os.Stdout, "\n(%d rows)\n", len(rows))
	return nil
}

