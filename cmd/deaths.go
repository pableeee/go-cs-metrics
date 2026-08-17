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

var (
	deathsMap        string
	deathsSince      string
	deathsBefore     string
	deathsPhase      string
	deathsTopWeapons int
)

var deathsCmd = &cobra.Command{
	Use:   "deaths <steamid64>",
	Short: "Death drill-down for a player: how, where, and when you die",
	Long: `Aggregate a player's deaths from player_death_events and break them down by
round phase, killing weapon, engagement distance, and map.

Answers "how do I die?": whether you die blind, whether teammates trade you,
how often you're the round's opening death, and at what range you lose fights.

Requires demos aggregated with Pass 12 (death events). Demos parsed before it
shipped contribute no rows — run "replay --dir <event>/ --force" to backfill.`,
	Args: cobra.ExactArgs(1),
	RunE: runDeaths,
}

func init() {
	deathsCmd.Flags().StringVar(&deathsMap, "map", "", "filter to one map (with or without the de_ prefix)")
	deathsCmd.Flags().StringVar(&deathsSince, "since", "", "only deaths on or after this date (YYYY-MM-DD)")
	deathsCmd.Flags().StringVar(&deathsBefore, "before", "", "only deaths strictly before this date (YYYY-MM-DD)")
	deathsCmd.Flags().StringVar(&deathsPhase, "phase", "",
		"restrict to one round phase: pistol, early, mid, late, post_plant")
	deathsCmd.Flags().IntVar(&deathsTopWeapons, "top-weapons", 12, "max rows in the weapon breakdown (0 = all)")
}

func runDeaths(cmd *cobra.Command, args []string) error {
	steamID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid steamid64: %w", err)
	}
	switch deathsPhase {
	case "", "pistol", "early", "mid", "late", "post_plant":
	default:
		return fmt.Errorf("--phase must be one of pistol, early, mid, late, post_plant; got %q", deathsPhase)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	filter := storage.DeathFilter{
		MapName: deathsMap,
		Since:   deathsSince,
		Before:  deathsBefore,
		Phase:   deathsPhase,
	}

	totals, err := db.DeathTotalsFor(steamID, filter)
	if err != nil {
		return fmt.Errorf("death totals: %w", err)
	}
	if totals.Deaths == 0 {
		fmt.Println("no death events found for this player under the current filter.")
		fmt.Println("if the player has matches in the DB, the demos likely predate Pass 12 —")
		fmt.Println(`backfill with: ./go-cs-metrics replay --dir <event>/ --force`)
		return nil
	}

	// Rounds played is only meaningful as a DPR denominator when the view
	// isn't restricted to a single phase (a phase covers a subset of rounds).
	roundsPlayed := 0
	if deathsPhase == "" {
		roundsPlayed, err = db.RoundsPlayedForDeathFilter(steamID, filter)
		if err != nil {
			return fmt.Errorf("rounds played: %w", err)
		}
	}

	name := playerNameFor(db, steamID)
	report.PrintDeathTotals(os.Stdout, totals, roundsPlayed, name)

	byPhase, err := db.DeathsByPhase(steamID, filter)
	if err != nil {
		return fmt.Errorf("deaths by phase: %w", err)
	}
	report.SortDeathsByPhase(byPhase)
	report.PrintDeathBreakdown(os.Stdout, "Deaths by Round Phase", "PHASE",
		"Round timeline: pistol rounds, then the post-freeze span split into thirds.\n"+
			"post_plant overrides the time-based label once the bomb is down.\n"+
			"A high OPENING% in early rounds means you lose the first duel; a high\n"+
			"late/post_plant share means you die in retakes and closing situations.",
		byPhase, totals.Deaths)

	byDist, err := db.DeathsByDistanceBin(steamID, filter)
	if err != nil {
		return fmt.Errorf("deaths by distance: %w", err)
	}
	report.SortDeathsByDistance(byDist)
	report.PrintDeathBreakdown(os.Stdout, "Deaths by Engagement Distance", "DISTANCE",
		"Distance from your killer, in the same bins as the FHHS duel table —\n"+
			"compare against your FHHS%/EXPO_WIN per bin to see which ranges you lose.",
		byDist, totals.Deaths)

	byWeapon, err := db.DeathsByWeapon(steamID, filter, deathsTopWeapons)
	if err != nil {
		return fmt.Errorf("deaths by weapon: %w", err)
	}
	weaponDesc := "What kills you, most frequent first."
	if deathsTopWeapons > 0 && len(byWeapon) == deathsTopWeapons {
		weaponDesc += fmt.Sprintf(" Showing the top %d — pass --top-weapons 0 for all.", deathsTopWeapons)
	}
	report.PrintDeathBreakdown(os.Stdout, "Deaths by Weapon", "WEAPON",
		weaponDesc, byWeapon, totals.Deaths)

	// Only worth a table when the filter spans more than one map.
	if deathsMap == "" && totals.Maps > 1 {
		byMap, err := db.DeathsByMap(steamID, filter)
		if err != nil {
			return fmt.Errorf("deaths by map: %w", err)
		}
		report.PrintDeathBreakdown(os.Stdout, "Deaths by Map", "MAP",
			"Per-map death profile. SHARE% reflects how much you played each map,\n"+
				"so read the rate columns (FLASHED%, TRADED%, OPENING%) rather than DEATHS.",
			byMap, totals.Deaths)
	}

	fmt.Fprintln(os.Stdout)
	return nil
}

// playerNameFor returns the player's most recent display name, falling back
// to the raw SteamID when no match stats exist.
func playerNameFor(db *storage.DB, steamID uint64) string {
	stats, err := db.GetAllPlayerMatchStats(steamID)
	if err != nil || len(stats) == 0 {
		return strconv.FormatUint(steamID, 10)
	}
	return stats[len(stats)-1].Name
}
