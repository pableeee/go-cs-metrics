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
  demos(hash, quick_hash, map_name, match_date, match_type, tickrate, ct_score,
    t_score, tier, is_baseline)
  player_match_stats(demo_hash, steam_id TEXT, name, team, kills, assists, deaths,
    headshot_kills, total_damage, rounds_played, rounds_won, kast_rounds, role,
    median_ttk_ms, median_ttd_ms, one_tap_kills, counter_strafe_pct,
    median_trade_kill_delay_ms, median_trade_death_delay_ms,
    crosshair_encounters, crosshair_median_deg, crosshair_pct_under5,
    crosshair_median_yaw_deg, crosshair_median_pitch_deg,
    saved_by_teammate, saved_teammate, assisted_kills,     -- Pass 14
    hltv_flash_assists,                                    -- Pass 15
    alive_seconds_total, last_alive_server_rounds,         -- Pass 16
    scan_ooc_seconds, scan_dwell_pct, scan_reversals_per_min,
    scan_avg_yaw_deg_per_sec, ...)                         -- Pass 17
  player_round_stats(demo_hash, steam_id TEXT, round_number, team, kills, assists,
    damage, unused_utility, buy_type, won_round, got_kill, got_assist, survived,
    was_traded, kast_earned, is_opening_kill, is_opening_death, is_trade_kill,
    is_trade_death, is_post_plant, is_in_clutch, clutch_enemy_count,
    scan_ooc_seconds, scan_dwell_pct, scan_reversals, scan_avg_yaw_deg_per_sec)
  player_weapon_stats(demo_hash, steam_id TEXT, weapon, kills, headshot_kills,
    assists, deaths, damage, hits)
  player_duel_segments(demo_hash, steam_id TEXT, round_number, weapon_bucket, distance_bin,
    -- win-side duels only; median_shot_delay_ms = first sight -> first shot
    duel_count, first_hit_count, first_hit_hs_count, median_corr_deg,
    median_sight_deg, median_expo_win_ms)

Event tables (one row per event; see the "deaths" command for a ready-made
death drill-down). Populated by passes 12/13 — demos aggregated earlier have
no rows, so backfill with "replay --dir <event>/ --force":
  player_death_events(demo_hash, match_date, map_name, round_number, tick,
    victim_id TEXT, victim_team, killer_id TEXT, killer_team, weapon, is_headshot,
    victim_x/y/z, killer_x/y/z, victim_yaw, distance_m, was_flashed, was_traded,
    is_opening_death, round_phase)
  flash_events(demo_hash, match_date, map_name, round_number, tick,
    thrower_id TEXT, thrower_team, victim_id TEXT, victim_team, duration_s,
    is_team_flash, victim_x/y/z, victim_yaw, victim_pitch, flash_x/y/z,
    blind_angle_deg, distance_m)
  grenade_events(demo_hash, match_date, map_name, round_number, throw_tick,
    end_tick, thrower_id TEXT, thrower_team, grenade_type,
    throw_x/y/z, land_x/y/z)

Note: steam_id (and the event tables' victim_id / killer_id / thrower_id) are
stored as TEXT. Use quotes: WHERE steam_id = '76561198012345678'`,
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

