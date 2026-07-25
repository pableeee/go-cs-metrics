package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	playerMap    string
	playerSince  string
	playerLast   int
	playerTop    int
	playerTopMin int
	playerRoles  bool
	playerPer    string
	playerSide   string
)

// playerCmd is the cobra command for cross-match aggregate analysis of one or more players.
var playerCmd = &cobra.Command{
	Use:   "player <steamid64> [<steamid64>...]",
	Short: "Cross-match analysis for one or more players",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runPlayer,
}

func init() {
	playerCmd.Flags().StringVar(&playerMap, "map", "", "filter to a specific map (e.g. nuke, de_nuke)")
	playerCmd.Flags().StringVar(&playerSince, "since", "", "filter to matches on or after this date (YYYY-MM-DD)")
	playerCmd.Flags().IntVar(&playerLast, "last", 0, "only use the N most recent matches")
	playerCmd.Flags().IntVar(&playerTop, "top", 0, "also include the top N players by Rating 2.0 proxy from the database")
	playerCmd.Flags().IntVar(&playerTopMin, "top-min", 3, "minimum matches a player must have to appear in the top-N ranking")
	playerCmd.Flags().BoolVar(&playerRoles, "roles", false, "print HLTV-style role decomposition (Firepower/Entry/Trade/Open/Clutch/Snipe/Util)")
	playerCmd.Flags().StringVar(&playerPer, "per", "round", "rate denominator for --roles output: 'round' (default) or '24' (per 24 rounds, ≈ one half)")
	playerCmd.Flags().StringVar(&playerSide, "side", "both", "compute --roles metrics from rounds on this side: 'both' (default), 'ct', or 't'")
}

// runPlayer loads all match data for each given SteamID64, builds cross-match
// aggregates, and prints overview, duel, AWP, map/side, and FHHS tables.
// With --top N, the top N players by Rating 2.0 proxy are appended automatically.
func runPlayer(cmd *cobra.Command, args []string) error {
	// Validate role-view flags early so users see clear errors before the DB opens.
	if playerPer != "round" && playerPer != "24" {
		return fmt.Errorf("--per must be 'round' or '24', got %q", playerPer)
	}
	side := strings.ToLower(playerSide)
	if side != "both" && side != "ct" && side != "t" {
		return fmt.Errorf("--side must be 'both', 'ct', or 't', got %q", playerSide)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	// Build the ordered, deduplicated list of IDs to process.
	// Explicit args come first; --top N appends the highest-rated players not already present.
	allIDs := make([]string, 0, len(args))
	seenIDs := make(map[string]struct{}, len(args))
	for _, arg := range args {
		if _, dup := seenIDs[arg]; !dup {
			allIDs = append(allIDs, arg)
			seenIDs[arg] = struct{}{}
		}
	}
	if playerTop > 0 {
		normMap := strings.TrimPrefix(strings.ToLower(playerMap), "de_")
		topPlayers, err := db.GetTopPlayersByRating(playerTop+len(args), playerTopMin, normMap, playerSince)
		if err != nil {
			return fmt.Errorf("get top players by rating: %w", err)
		}
		var added []string
		for _, tp := range topPlayers {
			if _, dup := seenIDs[tp.SteamID]; dup {
				continue
			}
			if len(added) >= playerTop {
				break
			}
			allIDs = append(allIDs, tp.SteamID)
			seenIDs[tp.SteamID] = struct{}{}
			added = append(added, tp.Name)
		}
		if len(added) > 0 {
			fmt.Fprintf(os.Stdout, "Top-%d by rating added: %s\n", playerTop, strings.Join(added, ", "))
		}
	}

	type fhhsEntry struct {
		name  string
		id    uint64
		segs  []model.PlayerDuelSegment
		synth []model.PlayerMatchStats
	}

	var allAggs    []model.PlayerAggregate
	var allMapSide []model.PlayerMapSideAggregate
	var fhhsList   []fhhsEntry
	var allClutch  []model.PlayerClutchMatchStats
	var allRoles   []model.PlayerRoleStats

	// Cohort ratings for percentile labels. Built lazily and only when --roles
	// is requested. Empty when the cohort is too small (see CohortMinPlayers).
	var cohortRatings []float64
	if playerRoles {
		cohort, err := db.GetCohortAggregates(playerSince, CohortPlayerMinRounds)
		if err != nil {
			return fmt.Errorf("get cohort aggregates: %w", err)
		}
		if len(cohort) >= CohortMinPlayers {
			cohortRatings = buildCohortRatings(cohort)
		}
	}

	for _, arg := range allIDs {
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid SteamID64 %q: %w", arg, err)
		}

		stats, err := db.GetAllPlayerMatchStats(id)
		if err != nil {
			return fmt.Errorf("query stats for %d: %w", id, err)
		}
		stats = filterStats(stats, playerMap, playerSince, playerLast)
		if len(stats) == 0 {
			fmt.Fprintf(os.Stderr, "No data found for SteamID64 %d (after filters)\n", id)
			continue
		}

		segs, err := db.GetAllPlayerDuelSegments(id)
		if err != nil {
			return fmt.Errorf("query segments for %d: %w", id, err)
		}

		// Filter segments to only those matching the filtered demo hashes.
		if playerMap != "" || playerSince != "" || playerLast > 0 {
			keep := make(map[string]struct{}, len(stats))
			for _, s := range stats {
				keep[s.DemoHash] = struct{}{}
			}
			var filteredSegs []model.PlayerDuelSegment
			for _, seg := range segs {
				if _, ok := keep[seg.DemoHash]; ok {
					filteredSegs = append(filteredSegs, seg)
				}
			}
			segs = filteredSegs
		}

		agg := buildAggregate(stats)
		merged := mergeSegments(id, segs)

		// Compute true aggregate FHHS from merged segment counts.
		var totalHits, totalHSHits int
		for _, s := range merged {
			totalHits += s.FirstHitCount
			totalHSHits += s.FirstHitHSCount
		}
		overallFHHS := 0.0
		if totalHits > 0 {
			overallFHHS = float64(totalHSHits) / float64(totalHits) * 100
		}

		// Aggregate clutch stats across filtered matches for this player.
		clutchByMatch, err := db.GetPlayerClutchStatsByMatch(id)
		if err != nil {
			return fmt.Errorf("query clutch for %d: %w", id, err)
		}
		keep := make(map[string]struct{}, len(stats))
		for _, s := range stats {
			keep[s.DemoHash] = struct{}{}
		}
		var aggClutch model.PlayerClutchMatchStats
		aggClutch.SteamID = id
		for hash, c := range clutchByMatch {
			if _, ok := keep[hash]; !ok {
				continue
			}
			for i := 1; i <= 5; i++ {
				aggClutch.Attempts[i] += c.Attempts[i]
				aggClutch.Wins[i] += c.Wins[i]
			}
		}
		allClutch = append(allClutch, aggClutch)

		// Filter out VERY_LOW segments (< 20 first-hit samples) — too noisy to be actionable.
		var cleanSegs []model.PlayerDuelSegment
		for _, seg := range merged {
			if seg.FirstHitCount >= 20 {
				cleanSegs = append(cleanSegs, seg)
			}
		}

		allAggs = append(allAggs, agg)
		allMapSide = append(allMapSide, buildMapSideAggregates(stats)...)
		fhhsList = append(fhhsList, fhhsEntry{
			name: agg.Name,
			id:   id,
			segs: cleanSegs,
			synth: []model.PlayerMatchStats{{
				SteamID:        id,
				Name:           agg.Name,
				FirstHitHSRate: overallFHHS,
			}},
		})

		// ---- Role decomposition (--roles) ----
		if playerRoles {
			rs, err := loadRoleStatsForPlayer(db, id, agg.Name, stats, aggClutch, keep, side)
			if err != nil {
				return fmt.Errorf("role stats for %d: %w", id, err)
			}
			if len(cohortRatings) > 0 {
				rs.Rating2CohortPercentile = percentileOf(cohortRatings, rs.Rating2Combined)
			} else {
				rs.Rating2CohortPercentile = -1
			}
			allRoles = append(allRoles, rs)
		}
	}

	if len(allAggs) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stdout)
	report.PrintPlayerAggregateOverview(os.Stdout, allAggs)
	report.PrintPlayerAggregateDuelTable(os.Stdout, allAggs)
	report.PrintPlayerAggregateAWPTable(os.Stdout, allAggs)
	report.PrintPlayerMapSideTable(os.Stdout, allMapSide)
	report.PrintPlayerMapMechanicsTable(os.Stdout, allMapSide)
	report.PrintPlayerAggregateAimTable(os.Stdout, allAggs)
	report.PrintPlayerAggregateClutchTable(os.Stdout, allAggs, allClutch)
	if playerRoles {
		report.PrintPlayerRoleStats(os.Stdout, allRoles, report.RoleViewOptions{
			Per:  playerPer,
			Side: side,
		})
	}

	// Combine all players' FHHS segments and render a single table.
	var allFHHSSegs []model.PlayerDuelSegment
	var allFHHSSynth []model.PlayerMatchStats
	for _, f := range fhhsList {
		allFHHSSegs = append(allFHHSSegs, f.segs...)
		allFHHSSynth = append(allFHHSSynth, f.synth...)
	}
	if len(allFHHSSegs) > 0 {
		fmt.Fprintln(os.Stdout)
		report.PrintFHHSTable(os.Stdout, allFHHSSegs, allFHHSSynth, 0)
	}
	return nil
}

// filterStats applies --map, --since, and --last filters to a slice of match stats.
// stats must be ordered ascending by date (as returned by GetAllPlayerMatchStats).
func filterStats(stats []model.PlayerMatchStats, mapFilter, since string, last int) []model.PlayerMatchStats {
	mapFilter = strings.TrimPrefix(strings.ToLower(mapFilter), "de_")
	var out []model.PlayerMatchStats
	for _, s := range stats {
		if mapFilter != "" && strings.TrimPrefix(strings.ToLower(s.MapName), "de_") != mapFilter {
			continue
		}
		if since != "" && s.MatchDate < since {
			continue
		}
		out = append(out, s)
	}
	if last > 0 && len(out) > last {
		out = out[len(out)-last:]
	}
	return out
}

// buildAggregate sums integer stats and averages float medians across all matches.
func buildAggregate(stats []model.PlayerMatchStats) model.PlayerAggregate {
	agg := model.PlayerAggregate{
		SteamID: stats[0].SteamID,
		Name:    stats[0].Name,
		Matches: len(stats),
	}
	var expoWinSum, expoLossSum, corrSum, hitsSum float64
	var expoWinN, expoLossN, corrN, hitsN int
	var ttkSum, ttdSum, csSum float64
	var ttkN, ttdN, csN int
	var tradeKillDelaySum, tradeDeathDelaySum float64
	var tradeKillDelayN, tradeDeathDelayN int
	var scanDwellWSum, scanRevWSum, scanYawWSum float64
	roleCounts := make(map[string]int)

	for _, s := range stats {
		agg.Kills += s.Kills
		agg.Assists += s.Assists
		agg.Deaths += s.Deaths
		agg.HeadshotKills += s.HeadshotKills
		agg.TotalDamage += s.TotalDamage
		agg.RoundsPlayed += s.RoundsPlayed
		agg.KASTRounds += s.KASTRounds
		agg.FlashAssists += s.FlashAssists
		agg.EffectiveFlashes += s.EffectiveFlashes
		agg.OpeningKills += s.OpeningKills
		agg.OpeningDeaths += s.OpeningDeaths
		agg.TradeKills += s.TradeKills
		agg.TradeDeaths += s.TradeDeaths
		agg.RoundsWon += s.RoundsWon
		agg.DuelWins += s.DuelWins
		agg.DuelLosses += s.DuelLosses
		agg.AWPDeaths += s.AWPDeaths
		agg.AWPDeathsDry += s.AWPDeathsDry
		agg.AWPDeathsRePeek += s.AWPDeathsRePeek
		agg.AWPDeathsIsolated += s.AWPDeathsIsolated
		agg.OneTapKills += s.OneTapKills

		if s.MedianExposureWinMs > 0 {
			expoWinSum += s.MedianExposureWinMs
			expoWinN++
		}
		if s.MedianExposureLossMs > 0 {
			expoLossSum += s.MedianExposureLossMs
			expoLossN++
		}
		if s.MedianCorrectionDeg > 0 {
			corrSum += s.MedianCorrectionDeg
			corrN++
		}
		if s.MedianHitsToKill > 0 {
			hitsSum += s.MedianHitsToKill
			hitsN++
		}
		if s.MedianTTKMs > 0 {
			ttkSum += s.MedianTTKMs
			ttkN++
		}
		if s.MedianTTDMs > 0 {
			ttdSum += s.MedianTTDMs
			ttdN++
		}
		if s.CounterStrafePercent > 0 {
			csSum += s.CounterStrafePercent
			csN++
		}
		if s.MedianTradeKillDelayMs > 0 {
			tradeKillDelaySum += s.MedianTradeKillDelayMs
			tradeKillDelayN++
		}
		if s.MedianTradeDeathDelayMs > 0 {
			tradeDeathDelaySum += s.MedianTradeDeathDelayMs
			tradeDeathDelayN++
		}
		if s.ScanOOCSeconds > 0 {
			agg.ScanOOCSeconds += s.ScanOOCSeconds
			scanDwellWSum += s.ScanDwellPct * s.ScanOOCSeconds
			scanRevWSum += s.ScanReversalsPerMin * s.ScanOOCSeconds
			scanYawWSum += s.ScanAvgYawDegPerSec * s.ScanOOCSeconds
		}
		role := s.Role
		if role == "" {
			role = "Rifler"
		}
		roleCounts[role]++
	}
	if agg.ScanOOCSeconds > 0 {
		agg.ScanDwellPct = scanDwellWSum / agg.ScanOOCSeconds
		agg.ScanReversalsPerMin = scanRevWSum / agg.ScanOOCSeconds
		agg.ScanAvgYawDegPerSec = scanYawWSum / agg.ScanOOCSeconds
	}

	if expoWinN > 0 {
		agg.AvgExpoWinMs = expoWinSum / float64(expoWinN)
	}
	if expoLossN > 0 {
		agg.AvgExpoLossMs = expoLossSum / float64(expoLossN)
	}
	if corrN > 0 {
		agg.AvgCorrectionDeg = corrSum / float64(corrN)
	}
	if hitsN > 0 {
		agg.AvgHitsToKill = hitsSum / float64(hitsN)
	}
	if ttkN > 0 {
		agg.AvgTTKMs = ttkSum / float64(ttkN)
	}
	if ttdN > 0 {
		agg.AvgTTDMs = ttdSum / float64(ttdN)
	}
	if csN > 0 {
		agg.AvgCounterStrafePct = csSum / float64(csN)
	}
	if tradeKillDelayN > 0 {
		agg.AvgTradeKillDelayMs = tradeKillDelaySum / float64(tradeKillDelayN)
	}
	if tradeDeathDelayN > 0 {
		agg.AvgTradeDeathDelayMs = tradeDeathDelaySum / float64(tradeDeathDelayN)
	}
	// Most common role across matches.
	bestRole, bestCount := "Rifler", 0
	for role, count := range roleCounts {
		if count > bestCount {
			bestRole, bestCount = role, count
		}
	}
	agg.Role = bestRole

	return agg
}

// mergeSegments groups segment rows by (WeaponBucket, DistanceBin), summing counts
// and averaging float medians across demos. Returns a single merged slice.
func mergeSegments(steamID uint64, segs []model.PlayerDuelSegment) []model.PlayerDuelSegment {
	type key struct{ bucket, bin string }
	type accum struct {
		duelCount, firstHitCount, firstHitHSCount int
		corrSum, sightSum, expoSum                float64
		corrN, sightN, expoN                      int
	}
	m := make(map[key]*accum)
	for _, s := range segs {
		k := key{s.WeaponBucket, s.DistanceBin}
		if m[k] == nil {
			m[k] = &accum{}
		}
		a := m[k]
		a.duelCount += s.DuelCount
		a.firstHitCount += s.FirstHitCount
		a.firstHitHSCount += s.FirstHitHSCount
		if s.MedianCorrDeg > 0 {
			a.corrSum += s.MedianCorrDeg
			a.corrN++
		}
		if s.MedianSightDeg > 0 {
			a.sightSum += s.MedianSightDeg
			a.sightN++
		}
		if s.MedianExpoWinMs > 0 {
			a.expoSum += s.MedianExpoWinMs
			a.expoN++
		}
	}

	out := make([]model.PlayerDuelSegment, 0, len(m))
	for k, a := range m {
		seg := model.PlayerDuelSegment{
			SteamID:         steamID,
			WeaponBucket:    k.bucket,
			DistanceBin:     k.bin,
			DuelCount:       a.duelCount,
			FirstHitCount:   a.firstHitCount,
			FirstHitHSCount: a.firstHitHSCount,
		}
		if a.corrN > 0 {
			seg.MedianCorrDeg = a.corrSum / float64(a.corrN)
		}
		if a.sightN > 0 {
			seg.MedianSightDeg = a.sightSum / float64(a.sightN)
		}
		if a.expoN > 0 {
			seg.MedianExpoWinMs = a.expoSum / float64(a.expoN)
		}
		out = append(out, seg)
	}
	return out
}

// buildMapSideAggregates groups match stats by (map, side) and sums integer stats.
// Float medians (TTK, TTD, CS%) are averaged across matches within each group.
func buildMapSideAggregates(stats []model.PlayerMatchStats) []model.PlayerMapSideAggregate {
	type key struct{ mapName, side string }
	type mapAccum struct {
		agg    *model.PlayerMapSideAggregate
		ttkSum float64
		ttkN   int
		ttdSum float64
		ttdN   int
		csSum  float64
		csN    int
	}
	m := make(map[key]*mapAccum)

	for _, s := range stats {
		side := s.Team.String()
		if side != "CT" && side != "T" {
			continue
		}
		mapName := strings.TrimPrefix(s.MapName, "de_")
		k := key{mapName, side}
		if m[k] == nil {
			m[k] = &mapAccum{agg: &model.PlayerMapSideAggregate{
				SteamID: s.SteamID,
				Name:    s.Name,
				MapName: mapName,
				Side:    side,
			}}
		}
		a := m[k]
		a.agg.Matches++
		a.agg.Kills += s.Kills
		a.agg.Assists += s.Assists
		a.agg.Deaths += s.Deaths
		a.agg.HeadshotKills += s.HeadshotKills
		a.agg.TotalDamage += s.TotalDamage
		a.agg.RoundsPlayed += s.RoundsPlayed
		a.agg.KASTRounds += s.KASTRounds
		a.agg.OpeningKills += s.OpeningKills
		a.agg.OpeningDeaths += s.OpeningDeaths
		a.agg.TradeKills += s.TradeKills
		a.agg.TradeDeaths += s.TradeDeaths
		a.agg.OneTapKills += s.OneTapKills
		if s.MedianTTKMs > 0 {
			a.ttkSum += s.MedianTTKMs
			a.ttkN++
		}
		if s.MedianTTDMs > 0 {
			a.ttdSum += s.MedianTTDMs
			a.ttdN++
		}
		if s.CounterStrafePercent > 0 {
			a.csSum += s.CounterStrafePercent
			a.csN++
		}
	}

	out := make([]model.PlayerMapSideAggregate, 0, len(m))
	for _, v := range m {
		if v.ttkN > 0 {
			v.agg.AvgTTKMs = v.ttkSum / float64(v.ttkN)
		}
		if v.ttdN > 0 {
			v.agg.AvgTTDMs = v.ttdSum / float64(v.ttdN)
		}
		if v.csN > 0 {
			v.agg.AvgCounterStrafePct = v.csSum / float64(v.csN)
		}
		out = append(out, *v.agg)
	}
	// Sort by map name ascending, CT before T within each map.
	sort.Slice(out, func(i, j int) bool {
		if out[i].MapName != out[j].MapName {
			return out[i].MapName < out[j].MapName
		}
		return out[i].Side < out[j].Side // "CT" < "T"
	})
	return out
}
