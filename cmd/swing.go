package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
	"github.com/pable/go-cs-metrics/internal/swing"
)

var (
	swingTier     string
	swingSince    string
	swingBefore   string
	swingByMap    bool
	swingBySide   bool
	swingByWeapon bool
	swingFloorC   bool
	swingMinDemos int
	swingRoster   string
	swingContext  bool
	swingMinRnds  int
	swingTables   bool
	swingTop      int
)

// swingWeaponMinDuels hides weapon classes with too few duels to mean anything.
const swingWeaponMinDuels = 20

// swingFloorMinRoundsPerDemo excludes fragment appearances (short overtime
// stand-ins) from the per-demo rate distribution.
const swingFloorMinRoundsPerDemo = 10

// swingLeaderMinRounds is the round floor for entering the reference
// population. Round swing per round is a small number divided by a small
// number early on, so an unfiltered leaderboard is pure sampling noise.
const swingLeaderMinRounds = 300

var swingCmd = &cobra.Command{
	Use:   "swing <steamid64> [<steamid64>...]",
	Short: "Round swing and duel swing (win-probability added) for one or more players",
	Long: `Round swing and duel swing, both computed from empirical probability
tables counted over the corpus rather than a fitted model.

  Round swing — how much the player moved their team's chance of winning the
                round, attributed at every kill from P(win | alive counts, plant).
  Duel swing  — how much the player beat or missed the expected outcome of the
                duels they took, from P(win | first-sight advantage, range, weapon).

The two answer different questions. A player can win duels they should have lost
(positive duel swing) while spending them in spots that barely move the round
(low round swing), or the reverse. The comparison is the point.

Both are zero-sum across all players, which is checked before anything prints.

--by-side splits both metrics by the side the player was on. That cut is what
separates the two things a combined number blends: holding a site above
expectation and taking space below it average out to nothing. A side on its own
is not zero-sum, so read a CT number against other players' CT numbers.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSwing,
}

func init() {
	swingCmd.Flags().StringVar(&swingTier, "tier", "", "restrict to a tier ('personal', 'pro'); default all")
	swingCmd.Flags().StringVar(&swingSince, "since", "", "only matches on or after this date (YYYY-MM-DD)")
	swingCmd.Flags().StringVar(&swingBefore, "before", "", "only matches strictly before this date (YYYY-MM-DD); with --since, bounds a point-in-time window")
	swingCmd.Flags().BoolVar(&swingByMap, "by-map", false, "break the player's swing down per map; combine with --by-side for CT/T rows per map")
	swingCmd.Flags().BoolVar(&swingByWeapon, "by-weapon", false, "break the player's swing down by the weapon class that resolved each duel")
	swingCmd.Flags().BoolVar(&swingFloorC, "floor-ceiling", false, "per-demo rate distribution (p25/p50/p75); with --top, a leaderboard by round-swing floor")
	swingCmd.Flags().IntVar(&swingMinDemos, "min-demos", 8, "floor/ceiling: omit players with fewer qualifying demos")
	swingCmd.Flags().StringVar(&swingRoster, "roster", "", "roster JSON ({\"team\":...,\"players\":[...]}); its players are added to the query")
	swingCmd.Flags().BoolVar(&swingContext, "context", false, "per-side resource/positioning context: good-gun %, pack distance, contact timing")
	swingCmd.Flags().BoolVar(&swingBySide, "by-side", false, "break the player's swing down by CT/T side; adds per-side duel swing columns to --top")
	swingCmd.Flags().IntVar(&swingMinRnds, "min-rounds", 20, "hide per-map rows below this many rounds")
	swingCmd.Flags().BoolVar(&swingTables, "show-tables", false, "print the empirical probability tables that back the numbers")
	swingCmd.Flags().IntVar(&swingTop, "top", 0, "also print the top N players by round swing, plus where the queried players rank")
}

func runSwing(cmd *cobra.Command, args []string) error {
	ids := make([]uint64, 0, len(args))
	for _, a := range args {
		id, err := strconv.ParseUint(a, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid SteamID64 %q: %w", a, err)
		}
		ids = append(ids, id)
	}
	teamName := ""
	if swingRoster != "" {
		team, rosterIDs, err := loadRosterIDs(swingRoster)
		if err != nil {
			return err
		}
		teamName = team
		seen := map[uint64]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		for _, id := range rosterIDs {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	if len(ids) == 0 {
		return fmt.Errorf("need at least one SteamID64 argument or --roster")
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	base := storage.SwingFilter{Tier: swingTier, Since: swingSince, Before: swingBefore}

	// The probability tables are built once over the whole filtered corpus.
	// Building them per map would shrink every cell for no gain: what a 3v2
	// is worth does not change with the map anywhere near as much as the
	// sample cost of splitting.
	rounds, kills, err := loadSwingCorpus(db, base)
	if err != nil {
		return err
	}
	if len(rounds) == 0 {
		return fmt.Errorf("no rounds matched the filter")
	}
	rt := swing.BuildRoundTable(rounds, kills)
	dt := swing.BuildDuelTable(kills)

	fmt.Fprintf(os.Stdout, "Corpus: %d rounds, %d duels", len(rounds), len(kills))
	if swingTier != "" {
		fmt.Fprintf(os.Stdout, " (tier=%s)", swingTier)
	}
	if swingSince != "" {
		fmt.Fprintf(os.Stdout, " (since=%s)", swingSince)
	}
	if swingBefore != "" {
		fmt.Fprintf(os.Stdout, " (before=%s)", swingBefore)
	}
	fmt.Fprintln(os.Stdout)

	res := swing.Compute(rounds, kills, rt, dt)
	if err := swing.Validate(res); err != nil {
		return fmt.Errorf("refusing to print: %w", err)
	}

	if swingTables {
		report.PrintSwingTables(os.Stdout, rt, dt)
	}

	overall := make([]*swing.PlayerSwing, 0, len(ids))
	for _, id := range ids {
		p := res[id]
		if p == nil {
			fmt.Fprintf(os.Stderr, "No data for SteamID64 %d\n", id)
			continue
		}
		overall = append(overall, p)
	}
	if len(overall) == 0 {
		return nil
	}
	names := nameLookup(db, ids)
	report.PrintSwingOverview(os.Stdout, overall, names)
	if swingBySide {
		report.PrintSwingBySide(os.Stdout, overall, names)
	}
	if swingByWeapon {
		for _, p := range overall {
			report.PrintSwingByWeapon(os.Stdout, names[p.SteamID], p, swingWeaponMinDuels)
		}
	}
	// Team shape only makes sense over an actual set of teammates: an explicit
	// roster, or enough queried ids to look like one.
	if swingRoster != "" || len(overall) >= 3 {
		report.PrintSwingTeamShape(os.Stdout, teamName, overall, names)
	}
	if swingContext {
		ctxRows, err := db.LoadPlayerContext(ids, base)
		if err != nil {
			return fmt.Errorf("load context: %w", err)
		}
		popCtx, err := db.LoadPopulationContext(base)
		if err != nil {
			return fmt.Errorf("load population context: %w", err)
		}
		report.PrintSwingContext(os.Stdout, ids, ctxRows, popCtx, names)
	}
	if swingFloorC {
		byDemo := swing.ComputeByDemo(rounds, kills, rt, dt)
		fc := swing.FloorCeilings(byDemo, swingMinDemos, swingFloorMinRoundsPerDemo)
		var queried []swing.PlayerFloorCeiling
		for _, id := range ids {
			if v, ok := fc[id]; ok {
				queried = append(queried, v)
			} else if res[id] != nil {
				fmt.Fprintf(os.Stderr, "%s: fewer than %d qualifying demos, not in floor/ceiling\n", names[id], swingMinDemos)
			}
		}
		var pop []swing.PlayerFloorCeiling
		fcNames, fcTiers := names, map[uint64]string{}
		if swingTop > 0 {
			for _, v := range fc {
				pop = append(pop, v)
			}
			var err error
			if fcNames, err = db.PlayerNames(base); err != nil {
				return fmt.Errorf("player names: %w", err)
			}
			if fcTiers, err = db.PlayerTiers(base); err != nil {
				return fmt.Errorf("player tiers: %w", err)
			}
		}
		report.PrintSwingFloorCeiling(os.Stdout, queried, pop, fcNames, fcTiers, swingTop, swingMinDemos)
	}

	if swingTop > 0 {
		popNames, err := db.PlayerNames(base)
		if err != nil {
			return fmt.Errorf("player names: %w", err)
		}
		tiers, err := db.PlayerTiers(base)
		if err != nil {
			return fmt.Errorf("player tiers: %w", err)
		}
		// The reference population is everyone with enough rounds to have a
		// stable number. Below that a handful of lucky rounds tops the board.
		var pop []*swing.PlayerSwing
		for _, p := range res {
			if p.Rounds >= swingLeaderMinRounds {
				pop = append(pop, p)
			}
		}
		report.PrintSwingLeaderboard(os.Stdout, pop, overall, popNames, tiers, swingTop, swingLeaderMinRounds, swingBySide)
	}

	if swingByMap {
		for _, id := range ids {
			maps, err := db.PlayerMapsPlayed(id, base)
			if err != nil {
				return fmt.Errorf("maps for %d: %w", id, err)
			}
			var rowsOut []report.SwingMapRow
			for _, m := range maps {
				f := base
				f.MapName = m
				mr, mk, err := loadSwingCorpus(db, f)
				if err != nil {
					return err
				}
				if len(mr) == 0 {
					continue
				}
				// Tables stay corpus-wide; only the attribution is per map.
				mres := swing.Compute(mr, mk, rt, dt)
				p := mres[id]
				if p == nil || p.Rounds < swingMinRnds {
					continue
				}
				rowsOut = append(rowsOut, report.SwingMapRow{MapName: m, Swing: p})
			}
			sort.Slice(rowsOut, func(i, j int) bool {
				return rowsOut[i].Swing.Rounds > rowsOut[j].Swing.Rounds
			})
			if len(rowsOut) > 0 {
				report.PrintSwingByMap(os.Stdout, nameFor(db, id), rowsOut, swingMinRnds, swingBySide)
			}
		}
	}
	return nil
}

func loadSwingCorpus(db *storage.DB, f storage.SwingFilter) ([]swing.Round, []swing.Kill, error) {
	rounds, err := db.LoadSwingRounds(f)
	if err != nil {
		return nil, nil, fmt.Errorf("load rounds: %w", err)
	}
	kills, err := db.LoadSwingKills(f)
	if err != nil {
		return nil, nil, fmt.Errorf("load kills: %w", err)
	}
	return rounds, kills, nil
}

func nameLookup(db *storage.DB, ids []uint64) map[uint64]string {
	out := map[uint64]string{}
	for _, id := range ids {
		out[id] = nameFor(db, id)
	}
	return out
}

func nameFor(db *storage.DB, id uint64) string {
	stats, err := db.GetAllPlayerMatchStats(id)
	if err != nil || len(stats) == 0 {
		return strconv.FormatUint(id, 10)
	}
	// Most recent non-empty name.
	for i := len(stats) - 1; i >= 0; i-- {
		if strings.TrimSpace(stats[i].Name) != "" {
			return stats[i].Name
		}
	}
	return strconv.FormatUint(id, 10)
}

// loadRosterIDs reads an export-style roster JSON and parses its players.
func loadRosterIDs(path string) (team string, ids []uint64, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read roster: %w", err)
	}
	var rf rosterFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return "", nil, fmt.Errorf("parse roster %s: %w", path, err)
	}
	for _, p := range rf.Players {
		id, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return "", nil, fmt.Errorf("roster %s: invalid SteamID64 %q: %w", path, p, err)
		}
		ids = append(ids, id)
	}
	return rf.Team, ids, nil
}
