package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
	"github.com/pable/go-cs-metrics/internal/vrs"
)

var (
	exportTeam         string
	exportPlayers      string
	exportRoster       string
	exportSince        int
	exportBefore       string
	exportQuorum       int
	exportOut          string
	exportHalfLife     float64
	exportVRSDB        string
	exportShrinkRounds float64
	exportRatingPrior  float64
)

// rosterFile is the schema for --roster JSON files.
type rosterFile struct {
	Team    string   `json:"team"`
	Players []string `json:"players"`
}

// simbo3TeamStats is the top-level JSON schema expected by cs2-pro-match-simulator.
//
// players_rating2_3m and matches_3m use the "_3m" naming convention from HLTV's
// standard 3-month rolling window. The actual window is recorded in window_days;
// the field names are kept as-is for compatibility with simbo3, which ignores
// the provenance fields (generated_at, window_days, latest_match_date, demo_count)
// via standard JSON unmarshalling.
type simbo3TeamStats struct {
	Team              string                    `json:"team"`
	PlayersRating2_3m []float64                 `json:"players_rating2_3m"`
	Maps              map[string]simbo3MapStats `json:"maps"`
	GeneratedAt       string                    `json:"generated_at"`
	WindowDays        int                       `json:"window_days"`
	LatestMatchDate   string                    `json:"latest_match_date"`
	DemoCount         int                       `json:"demo_count"`
	TradeNetRate      float64                   `json:"trade_net_rate,omitempty"`
	EcoWinPct         float64                   `json:"eco_win_pct,omitempty"`
	ForceWinPct       float64                   `json:"force_win_pct,omitempty"`
	RatingFloor       float64                   `json:"rating_floor,omitempty"`

	// VRS-stratified player ratings — only present when enough demos matched.
	PlayersRatingVsTop30 []float64 `json:"players_rating_vs_top30,omitempty"`
	PlayersRatingVsTop20 []float64 `json:"players_rating_vs_top20,omitempty"`
	PlayersRatingVsTop10 []float64 `json:"players_rating_vs_top10,omitempty"`
	DemoCountVsTop30     int       `json:"demo_count_vs_top30,omitempty"`
	DemoCountVsTop20     int       `json:"demo_count_vs_top20,omitempty"`
	DemoCountVsTop10     int       `json:"demo_count_vs_top10,omitempty"`

	// Own VRS rank (matched by roster player names against the latest snapshot).
	VRSGlobalRank   int    `json:"vrs_global_rank,omitempty"`
	VRSSnapshotDate string `json:"vrs_snapshot_date,omitempty"`
}

// simbo3MapStats is the per-map block within the simbo3 team JSON.
type simbo3MapStats struct {
	MapWinPct        float64 `json:"map_win_pct"`
	CTRoundWinPct    float64 `json:"ct_round_win_pct"`
	TRoundWinPct     float64 `json:"t_round_win_pct"`
	Matches3m        int     `json:"matches_3m"`
	EntryKillRate    float64 `json:"entry_kill_rate,omitempty"`
	EntryDeathRate   float64 `json:"entry_death_rate,omitempty"`
	PostPlantTWinPct float64 `json:"post_plant_t_win_pct,omitempty"`

	// VRS-stratified map stats — only present when ≥2 matches exist in the stratum.
	MapWinPctVsTop30     float64 `json:"map_win_pct_vs_top30,omitempty"`
	CTRoundWinPctVsTop30 float64 `json:"ct_round_win_pct_vs_top30,omitempty"`
	TRoundWinPctVsTop30  float64 `json:"t_round_win_pct_vs_top30,omitempty"`
	Matches3mVsTop30     int     `json:"matches_3m_vs_top30,omitempty"`
	MapWinPctVsTop20     float64 `json:"map_win_pct_vs_top20,omitempty"`
	CTRoundWinPctVsTop20 float64 `json:"ct_round_win_pct_vs_top20,omitempty"`
	TRoundWinPctVsTop20  float64 `json:"t_round_win_pct_vs_top20,omitempty"`
	Matches3mVsTop20     int     `json:"matches_3m_vs_top20,omitempty"`
	MapWinPctVsTop10     float64 `json:"map_win_pct_vs_top10,omitempty"`
	CTRoundWinPctVsTop10 float64 `json:"ct_round_win_pct_vs_top10,omitempty"`
	TRoundWinPctVsTop10  float64 `json:"t_round_win_pct_vs_top10,omitempty"`
	Matches3mVsTop10     int     `json:"matches_3m_vs_top10,omitempty"`
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export team stats as a simbo3-compatible JSON file",
	Long: `Queries the metrics database for a team roster and produces a JSON file
in the format expected by cs2-pro-match-simulator (simbo3).

Specify the roster via --players (comma-separated SteamID64s) or
--roster (path to a JSON file). If both are provided, --players takes precedence.
If --team is set alongside --roster, it overrides the name from the roster file.

Player ratings are estimated using the community approximation of HLTV Rating 2.0:
  Rating ≈ 0.0073*KAST% + 0.3591*KPR - 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
  Impact  = 2.13*KPR + 0.42*APR - 0.41

Example:
  csmetrics export --team "NaVi" --players "76561198XXXXXXXXX,76561198XXXXXXXXX,..." --out navi.json
  csmetrics export --roster navi.json --out navi-simbo3.json`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportTeam, "team", "", "team name for the output JSON")
	exportCmd.Flags().StringVar(&exportPlayers, "players", "", "comma-separated SteamID64s")
	exportCmd.Flags().StringVar(&exportRoster, "roster", "", `roster JSON file: {"team":"...","players":["...",...]}`)
	exportCmd.Flags().IntVar(&exportSince, "since", 90, "look-back window in days")
	exportCmd.Flags().StringVar(&exportBefore, "before", "", "exclude demos on or after this date (YYYY-MM-DD); --since becomes relative to this date")
	exportCmd.Flags().IntVar(&exportQuorum, "quorum", 3, "min roster players per demo to include it")
	exportCmd.Flags().StringVar(&exportOut, "out", "", "output file path (default: stdout)")
	exportCmd.Flags().Float64Var(&exportHalfLife, "half-life", 35,
		"temporal decay half-life in days (0 = uniform weights)")
	defaultVRSDB := filepath.Join(mustUserHome(), ".csmetrics", "vrs.db")
	exportCmd.Flags().StringVar(&exportVRSDB, "vrs-db", defaultVRSDB,
		"path to VRS database for stratified stats (skip if absent)")
	exportCmd.Flags().Float64Var(&exportShrinkRounds, "rating-shrink-rounds", 0,
		"empirical-Bayes shrinkage strength for player ratings, in weighted rounds "+
			"(0 = off; e.g. 200 pulls thin-sample teams toward the population mean)")
	exportCmd.Flags().Float64Var(&exportRatingPrior, "rating-prior", -1,
		"prior mean rating to shrink toward (<0 = auto: population mean over the window)")
}

func runExport(_ *cobra.Command, _ []string) error {
	teamName, steamIDs, err := resolveRoster()
	if err != nil {
		return err
	}
	if len(steamIDs) == 0 {
		return fmt.Errorf("no players specified: use --players or --roster")
	}
	if teamName == "" {
		return fmt.Errorf("no team name specified: use --team or include it in the roster file")
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	// refDate is today unless --before is set, in which case it anchors the
	// look-back window and the temporal decay to the cutoff date.
	refDate := time.Now()
	if exportBefore != "" {
		refDate, err = time.Parse("2006-01-02", exportBefore)
		if err != nil {
			return fmt.Errorf("parse --before date %q: %w", exportBefore, err)
		}
	}

	since := refDate.AddDate(0, 0, -exportSince)

	var demos []storage.DemoRef
	if exportBefore != "" {
		fmt.Fprintf(os.Stderr, "Querying demos for %d players [%s, %s) (quorum=%d)...\n",
			len(steamIDs), since.Format("2006-01-02"), refDate.Format("2006-01-02"), exportQuorum)
		demos, err = db.QualifyingDemosWindow(steamIDs, since, refDate, exportQuorum)
	} else {
		fmt.Fprintf(os.Stderr, "Querying demos for %d players since %s (quorum=%d)...\n",
			len(steamIDs), since.Format("2006-01-02"), exportQuorum)
		demos, err = db.QualifyingDemos(steamIDs, since, exportQuorum)
	}
	if err != nil {
		return fmt.Errorf("query qualifying demos: %w", err)
	}
	if len(demos) == 0 {
		// Run a diagnostic query to explain why: show per-player demo counts
		// without the quorum filter so the user knows what data exists.
		counts, diagErr := db.PlayerDemoCounts(steamIDs, since)
		if diagErr == nil {
			if len(counts) == 0 {
				fmt.Fprintf(os.Stderr, "hint: none of the %d roster players appear in any demo in the last %d days — parse more demos first\n",
					len(steamIDs), exportSince)
			} else {
				fmt.Fprintf(os.Stderr, "Per-player demo counts (last %d days, no quorum filter):\n", exportSince)
				for _, c := range counts {
					fmt.Fprintf(os.Stderr, "  %-20s  %d demo(s)\n", c.Name, c.Count)
				}
				if counts[0].Count < exportQuorum {
					fmt.Fprintf(os.Stderr, "hint: most active roster player has only %d demo(s); try --quorum 1 or parse more team demos\n",
						counts[0].Count)
				} else {
					fmt.Fprintf(os.Stderr, "hint: players exist individually but no single demo has %d+ of them together; try --quorum %d\n",
						exportQuorum, exportQuorum-1)
				}
			}
		}
		return fmt.Errorf("no qualifying demos found in the last %d days with quorum=%d", exportSince, exportQuorum)
	}
	fmt.Fprintf(os.Stderr, "Found %d qualifying demos\n", len(demos))

	// Group demo hashes by map name and collect all hashes for the rating query.
	// Map names are already normalized at storage time (e.g. "Mirage" not "de_mirage").
	byMap := make(map[string][]string)
	allHashes := make([]string, 0, len(demos))
	for _, d := range demos {
		byMap[d.MapName] = append(byMap[d.MapName], d.Hash)
		allHashes = append(allHashes, d.Hash)
	}

	weights := demoWeights(demos, refDate, exportHalfLife)

	tierFactor := computeTierFactor(demos)
	fmt.Fprintf(os.Stderr, "  tier factor: %.3f  (%d top-tier / %d total demos)\n",
		tierFactor, func() int {
			n := 0
			for _, d := range demos {
				if isTopTier(d.EventID) {
					n++
				}
			}
			return n
		}(), len(demos))

	// normPct shrinks a win-rate deviation from 0.50 by tierFactor.
	// A team whose demos are all from top-tier events (tierFactor=1.0) is
	// unaffected; a team from all regional events (tierFactor=0.5) has its
	// map win-rate deviations halved toward 0.50.
	normPct := func(pct float64) float64 {
		return roundTo2dp(0.50 + (pct-0.50)*tierFactor)
	}

	// Compute per-map stats.
	maps := make(map[string]simbo3MapStats, len(byMap))
	for mapName, hashes := range byMap {
		outcomes, err := db.MapWinOutcomes(steamIDs, hashes)
		if err != nil {
			return fmt.Errorf("map win outcomes for %s: %w", mapName, err)
		}

		mapWinPct := weightedMapWinPct(outcomes, weights)
		n := len(outcomes)

		sidesByDemo, err := db.RoundSideStatsByDemo(steamIDs, hashes)
		if err != nil {
			return fmt.Errorf("round side stats for %s: %w", mapName, err)
		}
		ctPct, tPct := weightedSideStats(sidesByDemo, weights)

		maps[mapName] = simbo3MapStats{
			MapWinPct:     normPct(mapWinPct),
			CTRoundWinPct: normPct(ctPct),
			TRoundWinPct:  normPct(tPct),
			Matches3m:     n,
		}
		fmt.Fprintf(os.Stderr, "  %-12s  %2d matches  win=%.2f→%.2f  CT=%.2f→%.2f  T=%.2f→%.2f\n",
			mapName, n, mapWinPct, normPct(mapWinPct), ctPct, normPct(ctPct), tPct, normPct(tPct))
	}

	// Resolve the empirical-Bayes shrinkage prior once for this export. When
	// --rating-shrink-rounds > 0, small-sample roster ratings are regressed toward
	// priorMean with weight rounds/(rounds+shrinkRounds). The prior is the
	// population mean rating over the same window (or --rating-prior if set), so it
	// tracks whatever the rating scale is rather than assuming 1.0.
	priorMean := exportRatingPrior
	if exportShrinkRounds > 0 && priorMean < 0 {
		const priorMinRounds = 100 // ~2 maps; excludes one-off stand-ins from the prior
		mean, nPlayers, perr := db.PopulationMeanRating(since, refDate, priorMinRounds)
		if perr != nil {
			return fmt.Errorf("population mean rating: %w", perr)
		}
		if nPlayers == 0 {
			fmt.Fprintf(os.Stderr, "  shrinkage: no players with >=%d rounds in window — disabled\n", priorMinRounds)
			priorMean = -1 // signal disabled to buildWeightedRatings
		} else {
			priorMean = mean
			fmt.Fprintf(os.Stderr, "  shrinkage: prior=%.3f over %d players (shrink-rounds=%.0f)\n",
				priorMean, nPlayers, exportShrinkRounds)
		}
	}
	shrinkRounds := exportShrinkRounds
	if priorMean < 0 {
		shrinkRounds = 0 // prior unavailable -> no shrinkage
	}

	// Compute HLTV Rating 2.0 proxies for the top 5 players by activity.
	byDemo, err := db.RosterMatchTotalsByDemo(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("roster match totals: %w", err)
	}
	ratings, namedGlobal := buildWeightedRatings(byDemo, weights, priorMean, shrinkRounds)

	// Normalise ratings for opponent quality.
	oppByDemo, err := db.OpponentMatchTotalsByDemo(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("opponent match totals: %w", err)
	}
	normFactor := computeOpponentNormFactor(oppByDemo, weights)
	for i := range ratings {
		ratings[i] = roundTo2dp(ratings[i] * normFactor)
	}
	// Print compact global rating summary (one line).
	parts := make([]string, len(namedGlobal))
	for i, n := range namedGlobal {
		parts[i] = fmt.Sprintf("%s=%.2f", n.name, roundTo2dp(n.rating*normFactor))
	}
	fmt.Fprintf(os.Stderr, "  ratings: %s\n", strings.Join(parts, "  "))

	// Populate per-map entry kill/death rates.
	entryByMap, err := db.MapEntryStats(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("map entry stats: %w", err)
	}
	for mapName, es := range entryByMap {
		ms, ok := maps[mapName]
		if !ok {
			continue
		}
		if es.RoundsPlayed > 0 {
			ms.EntryKillRate = roundTo2dp(float64(es.OpeningKills) / float64(es.RoundsPlayed))
			ms.EntryDeathRate = roundTo2dp(float64(es.OpeningDeaths) / float64(es.RoundsPlayed))
		}
		maps[mapName] = ms
	}

	// Populate per-map T-side post-plant win rates.
	postPlantByMap, err := db.MapPostPlantTWinRates(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("map post-plant stats: %w", err)
	}
	const postPlantPrior = 0.75
	const postPlantMinRounds = 5
	for mapName, ms := range maps {
		pp, ok := postPlantByMap[mapName]
		if ok && pp.TTotal >= postPlantMinRounds {
			ms.PostPlantTWinPct = roundTo2dp(float64(pp.TWins) / float64(pp.TTotal))
		} else {
			ms.PostPlantTWinPct = postPlantPrior
		}
		maps[mapName] = ms
	}

	// Compute team-level trade net rate.
	tradeStats, err := db.TeamTradeStats(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("team trade stats: %w", err)
	}
	var tradeNetRate float64
	if tradeStats.RoundsPlayed > 0 {
		tradeNetRate = roundTo2dp(float64(tradeStats.TradeKills-tradeStats.TradeDeaths) / float64(tradeStats.RoundsPlayed))
	}

	// Compute eco and force buy-type win rates.
	buyRates, err := db.BuyTypeWinRates(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("buy type win rates: %w", err)
	}
	const buyTypeMinRounds = 10
	ecoWinPct := 0.50
	if buyRates.EcoTotal >= buyTypeMinRounds {
		ecoWinPct = roundTo2dp(float64(buyRates.EcoWins) / float64(buyRates.EcoTotal))
	}
	forceWinPct := 0.50
	if buyRates.ForceTotal >= buyTypeMinRounds {
		forceWinPct = roundTo2dp(float64(buyRates.ForceWins) / float64(buyRates.ForceTotal))
	}

	// Rating floor: ratings is sorted descending; index 4 is the 5th player (lowest).
	ratingFloor := ratings[4]

	// --- VRS-stratified stats (optional; skipped when --vrs-db is absent) ---
	// Identify each demo's opponent team by matching opponent player names to
	// VRS roster data, then partition hashes into top30/top20 strata and
	// compute per-stratum map and rating stats.
	var (
		vrsRatingsTop30, vrsRatingsTop20, vrsRatingsTop10       []float64
		vrsDemoCountTop30, vrsDemoCountTop20, vrsDemoCountTop10 int
		vrsOwnRank                                              int
		vrsOwnSnapshotDate                                      string
	)
	vrsStore := openVRSStore(exportVRSDB)
	if vrsStore != nil {
		defer vrsStore.Close()

		// Build helper maps from existing data (no extra DB queries needed).
		demoDate := make(map[string]string, len(demos))
		for _, d := range demos {
			demoDate[d.Hash] = d.MatchDate
		}
		oppNamesByDemo := groupNamesByDemo(oppByDemo)

		// Tag each demo with its opponent's VRS rank.
		opponentRank := tagDemosWithVRSRank(oppNamesByDemo, demoDate, vrsStore)

		// Partition into strata.
		top30HashSet := make(map[string]bool)
		top20HashSet := make(map[string]bool)
		top10HashSet := make(map[string]bool)
		matchedCount := 0
		for hash, rank := range opponentRank {
			matchedCount++
			if rank <= 30 {
				top30HashSet[hash] = true
			}
			if rank <= 20 {
				top20HashSet[hash] = true
			}
			if rank <= 10 {
				top10HashSet[hash] = true
			}
		}
		unmatchedCount := len(demos) - matchedCount
		fmt.Fprintf(os.Stderr,
			"  VRS matching: %d/%d demos matched (top30: %d, top20: %d, top10: %d, unmatched: %d)\n",
			matchedCount, len(demos), len(top30HashSet), len(top20HashSet), len(top10HashSet), unmatchedCount)

		// Compute own team's VRS rank from roster player names.
		rosterNames := playerNamesFromTotals(byDemo)
		// Use today's date (or refDate) as the anchor for own-team rank lookup.
		ownTeam, ownRank, ownOK := vrs.MatchOpponentToVRS(rosterNames, refDate.Format("2006-01-02"), vrsStore)
		if ownOK {
			vrsOwnRank = ownRank
			// Retrieve snapshot date from the store for informational purposes.
			if entries, _ := vrsStore.LatestSnapshotBefore(refDate.Format("2006-01-02")); len(entries) > 0 {
				vrsOwnSnapshotDate = entries[0].SnapshotDate
			}
			fmt.Fprintf(os.Stderr, "  Own VRS rank: %d (%s)\n", vrsOwnRank, ownTeam)
		} else {
			fmt.Fprintf(os.Stderr, "  Own VRS rank: unmatched (no VRS rank; stratified stat selection disabled for this team)\n")
		}

		// top30 stratum.
		if len(top30HashSet) > 0 {
			top30Hashes := hashSetSlice(top30HashSet)
			vrsDemoCountTop30 = len(top30Hashes)
			if err := addStratifiedMapStats(db, steamIDs, byMap, top30HashSet, weights, normPct, maps, "top30"); err != nil {
				return fmt.Errorf("stratified top30 map stats: %w", err)
			}
			if len(top30Hashes) >= exportQuorum {
				byDemoTop30, err := db.RosterMatchTotalsByDemo(steamIDs, top30Hashes)
				if err != nil {
					return fmt.Errorf("roster match totals (top30): %w", err)
				}
				oppTop30, err := db.OpponentMatchTotalsByDemo(steamIDs, top30Hashes)
				if err != nil {
					return fmt.Errorf("opponent match totals (top30): %w", err)
				}
				r30, _ := buildWeightedRatings(byDemoTop30, weights, priorMean, shrinkRounds)
				nf30 := computeOpponentNormFactor(oppTop30, weights)
				for i := range r30 {
					r30[i] = roundTo2dp(r30[i] * nf30)
				}
				vrsRatingsTop30 = r30
				fmt.Fprintf(os.Stderr, "  vs top30 (%d demos): %s\n", vrsDemoCountTop30, formatRatings(r30))
			}
		}

		// top20 stratum.
		if len(top20HashSet) > 0 {
			top20Hashes := hashSetSlice(top20HashSet)
			vrsDemoCountTop20 = len(top20Hashes)
			if err := addStratifiedMapStats(db, steamIDs, byMap, top20HashSet, weights, normPct, maps, "top20"); err != nil {
				return fmt.Errorf("stratified top20 map stats: %w", err)
			}
			if len(top20Hashes) >= exportQuorum {
				byDemoTop20, err := db.RosterMatchTotalsByDemo(steamIDs, top20Hashes)
				if err != nil {
					return fmt.Errorf("roster match totals (top20): %w", err)
				}
				oppTop20, err := db.OpponentMatchTotalsByDemo(steamIDs, top20Hashes)
				if err != nil {
					return fmt.Errorf("opponent match totals (top20): %w", err)
				}
				r20, _ := buildWeightedRatings(byDemoTop20, weights, priorMean, shrinkRounds)
				nf20 := computeOpponentNormFactor(oppTop20, weights)
				for i := range r20 {
					r20[i] = roundTo2dp(r20[i] * nf20)
				}
				vrsRatingsTop20 = r20
				fmt.Fprintf(os.Stderr, "  vs top20 (%d demos): %s\n", vrsDemoCountTop20, formatRatings(r20))
			}
		}

		// top10 stratum.
		if len(top10HashSet) > 0 {
			top10Hashes := hashSetSlice(top10HashSet)
			vrsDemoCountTop10 = len(top10Hashes)
			if err := addStratifiedMapStats(db, steamIDs, byMap, top10HashSet, weights, normPct, maps, "top10"); err != nil {
				return fmt.Errorf("stratified top10 map stats: %w", err)
			}
			if len(top10Hashes) >= exportQuorum {
				byDemoTop10, err := db.RosterMatchTotalsByDemo(steamIDs, top10Hashes)
				if err != nil {
					return fmt.Errorf("roster match totals (top10): %w", err)
				}
				oppTop10, err := db.OpponentMatchTotalsByDemo(steamIDs, top10Hashes)
				if err != nil {
					return fmt.Errorf("opponent match totals (top10): %w", err)
				}
				r10, _ := buildWeightedRatings(byDemoTop10, weights, priorMean, shrinkRounds)
				nf10 := computeOpponentNormFactor(oppTop10, weights)
				for i := range r10 {
					r10[i] = roundTo2dp(r10[i] * nf10)
				}
				vrsRatingsTop10 = r10
				fmt.Fprintf(os.Stderr, "  vs top10 (%d demos): %s\n", vrsDemoCountTop10, formatRatings(r10))
			}
		}
	}
	// --- end VRS-stratified stats ---

	out := simbo3TeamStats{
		Team:              teamName,
		PlayersRating2_3m: ratings,
		Maps:              maps,
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
		WindowDays:        exportSince,
		LatestMatchDate:   demos[0].MatchDate,
		DemoCount:         len(demos),
		TradeNetRate:      tradeNetRate,
		EcoWinPct:         ecoWinPct,
		ForceWinPct:       forceWinPct,
		RatingFloor:       ratingFloor,

		PlayersRatingVsTop30: vrsRatingsTop30,
		PlayersRatingVsTop20: vrsRatingsTop20,
		PlayersRatingVsTop10: vrsRatingsTop10,
		DemoCountVsTop30:     vrsDemoCountTop30,
		DemoCountVsTop20:     vrsDemoCountTop20,
		DemoCountVsTop10:     vrsDemoCountTop10,
		VRSGlobalRank:        vrsOwnRank,
		VRSSnapshotDate:      vrsOwnSnapshotDate,
	}
	if exportSince != 90 {
		fmt.Fprintf(os.Stderr,
			"note: window_days=%d — players_rating2_3m and matches_3m use the conventional _3m names but cover your %d-day window\n",
			exportSince, exportSince)
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	if exportOut == "" {
		fmt.Println(string(data))
		return nil
	}
	if err := os.WriteFile(exportOut, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write %s: %w", exportOut, err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", exportOut)
	return nil
}

// resolveRoster returns the team name and SteamID list from flags.
// --players takes precedence over --roster; --team always overrides the roster file name.
func resolveRoster() (teamName string, steamIDs []string, err error) {
	if exportPlayers != "" {
		for _, raw := range strings.Split(exportPlayers, ",") {
			if id := strings.TrimSpace(raw); id != "" {
				steamIDs = append(steamIDs, id)
			}
		}
		return exportTeam, steamIDs, nil
	}
	if exportRoster != "" {
		data, readErr := os.ReadFile(exportRoster)
		if readErr != nil {
			return "", nil, fmt.Errorf("read roster file: %w", readErr)
		}
		var rf rosterFile
		if jsonErr := json.Unmarshal(data, &rf); jsonErr != nil {
			return "", nil, fmt.Errorf("parse roster file: %w", jsonErr)
		}
		name := rf.Team
		if exportTeam != "" {
			name = exportTeam
		}
		return name, rf.Players, nil
	}
	return exportTeam, nil, nil
}

// demoWeights returns exp(-ln(2)/halfLife * days_before_ref) per demo hash.
// halfLife <= 0 returns uniform weights of 1.0.
func demoWeights(demos []storage.DemoRef, refDate time.Time, halfLife float64) map[string]float64 {
	weights := make(map[string]float64, len(demos))
	if halfLife <= 0 {
		for _, d := range demos {
			weights[d.Hash] = 1.0
		}
		return weights
	}
	lambda := math.Log(2) / halfLife
	for _, d := range demos {
		matchDate, err := time.Parse("2006-01-02", d.MatchDate)
		if err != nil {
			weights[d.Hash] = 1.0
			continue
		}
		days := refDate.Sub(matchDate).Hours() / 24
		if days < 0 {
			days = 0
		}
		weights[d.Hash] = math.Exp(-lambda * days)
	}
	return weights
}

// weightedMapWinPct returns weighted win% from a WinOutcome slice.
func weightedMapWinPct(outcomes []storage.WinOutcome, weights map[string]float64) float64 {
	var winSum, totalW float64
	for _, o := range outcomes {
		if o.RoundsPlayed == 0 {
			continue
		}
		w := weights[o.Hash]
		totalW += w
		switch {
		case o.RoundsWon*2 > o.RoundsPlayed:
			winSum += w
		case o.RoundsWon*2 == o.RoundsPlayed:
			winSum += 0.5 * w
		}
	}
	if totalW == 0 {
		return 0
	}
	return winSum / totalW
}

// weightedSideStats returns weighted CT/T win% from per-demo DemoSideStats.
// Returns 0.50/0.50 when no data is available.
func weightedSideStats(byDemo []storage.DemoSideStats, weights map[string]float64) (ctPct, tPct float64) {
	var ctWinW, ctTotalW, tWinW, tTotalW float64
	for _, d := range byDemo {
		w := weights[d.Hash]
		ctWinW += w * float64(d.CTWins)
		ctTotalW += w * float64(d.CTTotal)
		tWinW += w * float64(d.TWins)
		tTotalW += w * float64(d.TTotal)
	}
	ctPct, tPct = 0.50, 0.50
	if ctTotalW > 0 {
		ctPct = ctWinW / ctTotalW
	}
	if tTotalW > 0 {
		tPct = tWinW / tTotalW
	}
	return
}

// namedRating pairs a player name with their computed Rating 2.0 proxy.
type namedRating struct {
	name   string
	rating float64
}

// buildWeightedRatings groups PlayerDemoTotals by player, accumulates
// weighted stat sums, computes KPR/DPR/APR/KAST/ADR from weighted totals.
// Returns a 5-element slice sorted descending (padded with 1.00) and the top
// players by rounds in rounds-descending order for informational logging.
//
// When shrinkRounds > 0 each player's rating is regressed toward priorMean by the
// empirical-Bayes factor rounds/(rounds+shrinkRounds): a player with few weighted
// rounds is pulled toward the population mean, so thin-sample teams stop receiving
// inflated/noisy ratings. shrinkRounds <= 0 (or priorMean < 0) disables shrinkage.
func buildWeightedRatings(byDemo []storage.PlayerDemoTotals, weights map[string]float64, priorMean, shrinkRounds float64) ([]float64, []namedRating) {
	type acc struct {
		name        string
		kills       float64
		deaths      float64
		assists     float64
		kastRounds  float64
		rounds      float64
		totalDamage float64
	}

	players := make(map[string]*acc)
	for _, d := range byDemo {
		w := weights[d.DemoHash]
		a, ok := players[d.SteamID]
		if !ok {
			a = &acc{name: d.Name}
			players[d.SteamID] = a
		}
		a.kills += w * float64(d.Kills)
		a.deaths += w * float64(d.Deaths)
		a.assists += w * float64(d.Assists)
		a.kastRounds += w * float64(d.KastRounds)
		a.rounds += w * float64(d.RoundsPlayed)
		a.totalDamage += w * float64(d.TotalDamage)
	}

	type namedAcc struct {
		steamID string
		*acc
	}
	sorted := make([]namedAcc, 0, len(players))
	for id, a := range players {
		sorted = append(sorted, namedAcc{id, a})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].rounds > sorted[j].rounds })

	ratings := make([]float64, 5)
	for i := range ratings {
		ratings[i] = 1.00
	}

	top := sorted
	if len(top) > 5 {
		top = top[:5]
	}

	named := make([]namedRating, 0, len(top))
	for i, p := range top {
		if p.rounds == 0 {
			continue
		}
		kpr := p.kills / p.rounds
		dpr := p.deaths / p.rounds
		apr := p.assists / p.rounds
		kast := 100.0 * p.kastRounds / p.rounds
		adr := p.totalDamage / p.rounds
		impact := 2.13*kpr + 0.42*apr - 0.41
		r := 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impact + 0.0032*adr + 0.1587
		if shrinkRounds > 0 && priorMean >= 0 {
			f := p.rounds / (p.rounds + shrinkRounds)
			r = priorMean + (r-priorMean)*f
		}
		ratings[i] = roundTo2dp(r)
		named = append(named, namedRating{p.name, roundTo2dp(r)})
	}

	if len(top) < 5 {
		fmt.Fprintf(os.Stderr, "warn: only %d player(s) found, padding remaining %d slot(s) with 1.00\n",
			len(top), 5-len(top))
	}

	sort.Slice(ratings, func(i, j int) bool { return ratings[i] > ratings[j] })
	return ratings, named
}

func roundTo2dp(v float64) float64 {
	return math.Round(v*100) / 100
}

// formatRatings renders a []float64 as space-separated two-decimal values.
func formatRatings(r []float64) string {
	parts := make([]string, len(r))
	for i, v := range r {
		parts[i] = fmt.Sprintf("%.2f", v)
	}
	return strings.Join(parts, " ")
}

// topTierPrefixes lists event_id prefixes that indicate a top-tier international
// event. Matches the list in cs2-trade/pipeline.py.
var topTierPrefixes = []string{
	"iem_", "esl_pro_league_", "esl_one_", "blast_", "pgl_",
	"faceit_major_", "major_", "starladder_",
}

func isTopTier(eventID string) bool {
	for _, p := range topTierPrefixes {
		if strings.HasPrefix(eventID, p) {
			return true
		}
	}
	return false
}

// computeTierFactor returns a [0,1] multiplier that shrinks win-rate deviations
// toward 0.50 when a team's qualifying demos are predominantly from lower-tier
// or regional events.
//
// Formula:
//
//	tierFactor = topFrac + nonTopFrac * nonTopWeight
//
// With nonTopWeight=0.5: a team with all top-tier demos gets tierFactor=1.0
// (no adjustment). A team with all regional demos gets tierFactor=0.5
// (deviations from 0.50 are halved). Mixed teams interpolate linearly.
//
// The adjusted win rate is then: 0.50 + (raw - 0.50) * tierFactor
func computeTierFactor(demos []storage.DemoRef) float64 {
	const nonTopWeight = 0.5
	if len(demos) == 0 {
		return 1.0
	}
	var topCount int
	for _, d := range demos {
		if isTopTier(d.EventID) {
			topCount++
		}
	}
	topFrac := float64(topCount) / float64(len(demos))
	nonTopFrac := 1.0 - topFrac
	f := topFrac + nonTopFrac*nonTopWeight
	return f
}

// computeOpponentNormFactor computes the temporally-weighted average HLTV
// Rating 2.0 of all opponent players across the qualifying demos.
//
// Rationale: a team that exclusively faces weak regional opposition earns
// inflated raw ratings. Multiplying their ratings by the opponent average
// (e.g. 0.85 for a low-tier field) scales them back toward a fair baseline.
// A team that faces strong international opponents (avg ~1.05) gets a small
// upward adjustment. The global baseline is 1.0.
//
// Returns 1.0 (no adjustment) if opponent data is empty.
func computeOpponentNormFactor(oppByDemo []storage.PlayerDemoTotals, weights map[string]float64) float64 {
	byDemo := make(map[string][]storage.PlayerDemoTotals)
	for _, p := range oppByDemo {
		byDemo[p.DemoHash] = append(byDemo[p.DemoHash], p)
	}

	var sumW, sumRW float64
	for hash, players := range byDemo {
		w := weights[hash]
		if w == 0 {
			continue
		}
		var totalR float64
		var n int
		for _, p := range players {
			if p.RoundsPlayed == 0 {
				continue
			}
			kpr := float64(p.Kills) / float64(p.RoundsPlayed)
			dpr := float64(p.Deaths) / float64(p.RoundsPlayed)
			apr := float64(p.Assists) / float64(p.RoundsPlayed)
			kast := 100.0 * float64(p.KastRounds) / float64(p.RoundsPlayed)
			adr := float64(p.TotalDamage) / float64(p.RoundsPlayed)
			impact := 2.13*kpr + 0.42*apr - 0.41
			r := 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impact + 0.0032*adr + 0.1587
			totalR += r
			n++
		}
		if n == 0 {
			continue
		}
		sumRW += w * (totalR / float64(n))
		sumW += w
	}

	if sumW == 0 {
		return 1.0
	}
	// Cap the factor to [0.80, 1.20] to prevent extreme adjustments.
	// Without this, teams that consistently lose to strong opponents get
	// an inflated normFactor (opponents' stats are inflated in lopsided wins).
	const normMin, normMax = 0.80, 1.20
	f := sumRW / sumW
	if f < normMin {
		f = normMin
	} else if f > normMax {
		f = normMax
	}
	return f
}

// --- VRS helper functions ---

// openVRSStore opens the VRS database at path. Returns nil (with a warning to
// stderr) if the file does not exist or cannot be opened.
func openVRSStore(path string) *vrs.VRSStore {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr,
			"  VRS: db not found at %s — run 'go-cs-metrics vrs-sync' to enable stratified stats\n", path)
		return nil
	}
	store, err := vrs.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  VRS: cannot open %s: %v — stratified stats skipped\n", path, err)
		return nil
	}
	return store
}

// groupNamesByDemo groups player names from PlayerDemoTotals by demo hash.
func groupNamesByDemo(totals []storage.PlayerDemoTotals) map[string][]string {
	out := make(map[string][]string)
	for _, p := range totals {
		out[p.DemoHash] = append(out[p.DemoHash], p.Name)
	}
	return out
}

// tagDemosWithVRSRank returns a map of demo hash → opponent VRS rank for demos
// where the opponent could be identified. Demos where matching fails are omitted.
func tagDemosWithVRSRank(
	oppNamesByDemo map[string][]string,
	demoDate map[string]string,
	store *vrs.VRSStore,
) map[string]int {
	out := make(map[string]int, len(oppNamesByDemo))
	for hash, names := range oppNamesByDemo {
		date, ok := demoDate[hash]
		if !ok {
			continue
		}
		_, rank, matched := vrs.MatchOpponentToVRS(names, date, store)
		if matched {
			out[hash] = rank
		}
	}
	return out
}

// playerNamesFromTotals extracts one canonical name per player from
// RosterMatchTotalsByDemo results. Used to identify own team's VRS rank.
func playerNamesFromTotals(totals []storage.PlayerDemoTotals) []string {
	// Keep a map of steamID → most-recently-seen name (arbitrary choice; names
	// rarely change mid-window and the VRS matcher is tolerant of 2 mismatches).
	seen := make(map[string]string)
	for _, p := range totals {
		if p.Name != "" {
			seen[p.SteamID] = p.Name
		}
	}
	names := make([]string, 0, len(seen))
	for _, n := range seen {
		names = append(names, n)
	}
	return names
}

// hashSetSlice converts a set (map[string]bool) to a slice.
func hashSetSlice(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for h := range s {
		out = append(out, h)
	}
	return out
}

// addStratifiedMapStats computes per-map win/side stats for demos in hashSet
// and stores the results in the appropriate stratum fields of maps.
// stratum must be "top30" or "top20".
func addStratifiedMapStats(
	db *storage.DB,
	steamIDs []string,
	byMap map[string][]string,
	hashSet map[string]bool,
	weights map[string]float64,
	normPct func(float64) float64,
	maps map[string]simbo3MapStats,
	stratum string,
) error {
	for mapName, allHashes := range byMap {
		// Filter to hashes in the stratum.
		var hashes []string
		for _, h := range allHashes {
			if hashSet[h] {
				hashes = append(hashes, h)
			}
		}
		if len(hashes) == 0 {
			continue
		}

		outcomes, err := db.MapWinOutcomes(steamIDs, hashes)
		if err != nil {
			return fmt.Errorf("%s/%s: map win outcomes: %w", stratum, mapName, err)
		}
		mapWinPct := weightedMapWinPct(outcomes, weights)
		n := len(outcomes)
		if n < 2 {
			// Too few matches to be meaningful; omit this stratum for this map.
			continue
		}

		sidesByDemo, err := db.RoundSideStatsByDemo(steamIDs, hashes)
		if err != nil {
			return fmt.Errorf("%s/%s: round side stats: %w", stratum, mapName, err)
		}
		ctPct, tPct := weightedSideStats(sidesByDemo, weights)

		ms := maps[mapName]
		switch stratum {
		case "top30":
			ms.MapWinPctVsTop30 = normPct(mapWinPct)
			ms.CTRoundWinPctVsTop30 = normPct(ctPct)
			ms.TRoundWinPctVsTop30 = normPct(tPct)
			ms.Matches3mVsTop30 = n
		case "top20":
			ms.MapWinPctVsTop20 = normPct(mapWinPct)
			ms.CTRoundWinPctVsTop20 = normPct(ctPct)
			ms.TRoundWinPctVsTop20 = normPct(tPct)
			ms.Matches3mVsTop20 = n
		case "top10":
			ms.MapWinPctVsTop10 = normPct(mapWinPct)
			ms.CTRoundWinPctVsTop10 = normPct(ctPct)
			ms.TRoundWinPctVsTop10 = normPct(tPct)
			ms.Matches3mVsTop10 = n
		}
		maps[mapName] = ms
	}
	return nil
}
