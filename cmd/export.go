package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	exportTeam     string
	exportPlayers  string
	exportRoster   string
	exportSince    int
	exportQuorum   int
	exportOut      string
	exportHalfLife float64
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
  csmetrics export --team "NaVi" --players "76561198034202275,76561197992321696,..." --out navi.json
  csmetrics export --roster navi.json --out navi-simbo3.json`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportTeam, "team", "", "team name for the output JSON")
	exportCmd.Flags().StringVar(&exportPlayers, "players", "", "comma-separated SteamID64s")
	exportCmd.Flags().StringVar(&exportRoster, "roster", "", `roster JSON file: {"team":"...","players":["...",...]}`)
	exportCmd.Flags().IntVar(&exportSince, "since", 90, "look-back window in days")
	exportCmd.Flags().IntVar(&exportQuorum, "quorum", 3, "min roster players per demo to include it")
	exportCmd.Flags().StringVar(&exportOut, "out", "", "output file path (default: stdout)")
	exportCmd.Flags().Float64Var(&exportHalfLife, "half-life", 35,
		"temporal decay half-life in days (0 = uniform weights)")
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

	since := time.Now().AddDate(0, 0, -exportSince)
	fmt.Fprintf(os.Stderr, "Querying demos for %d players since %s (quorum=%d)...\n",
		len(steamIDs), since.Format("2006-01-02"), exportQuorum)

	demos, err := db.QualifyingDemos(steamIDs, since, exportQuorum)
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

	weights := demoWeights(demos, time.Now(), exportHalfLife)

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
			MapWinPct:     roundTo2dp(mapWinPct),
			CTRoundWinPct: roundTo2dp(ctPct),
			TRoundWinPct:  roundTo2dp(tPct),
			Matches3m:     n,
		}
		fmt.Fprintf(os.Stderr, "  %-12s  %2d matches  win=%.2f  CT=%.2f  T=%.2f\n",
			mapName, n, mapWinPct, ctPct, tPct)
	}

	// Compute HLTV Rating 2.0 proxies for the top 5 players by activity.
	byDemo, err := db.RosterMatchTotalsByDemo(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("roster match totals: %w", err)
	}
	ratings := buildWeightedRatings(byDemo, weights)

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

// buildWeightedRatings groups PlayerDemoTotals by player, accumulates
// weighted stat sums, computes KPR/DPR/APR/KAST/ADR from weighted totals.
// Returns a 5-element slice sorted descending, padded with 1.00.
func buildWeightedRatings(byDemo []storage.PlayerDemoTotals, weights map[string]float64) []float64 {
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
		ratings[i] = roundTo2dp(r)
		fmt.Fprintf(os.Stderr, "  %-20s  wRounds=%.1f  KPR=%.2f DPR=%.2f KAST=%.0f%% ADR=%.1f  → rating %.2f\n",
			p.name, p.rounds, kpr, dpr, kast, adr, r)
	}

	if len(top) < 5 {
		fmt.Fprintf(os.Stderr, "warn: only %d player(s) found, padding remaining %d slot(s) with 1.00\n",
			len(top), 5-len(top))
	}

	sort.Slice(ratings, func(i, j int) bool { return ratings[i] > ratings[j] })
	return ratings
}


func roundTo2dp(v float64) float64 {
	return math.Round(v*100) / 100
}
