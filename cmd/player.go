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

// playerCmd is the cobra command for cross-match aggregate analysis of one or more players.
var playerCmd = &cobra.Command{
	Use:   "player <steamid64> [<steamid64>...]",
	Short: "Cross-match analysis for one or more players",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runPlayer,
}

// runPlayer loads all match data for each given SteamID64, builds cross-match
// aggregates, and prints overview, duel, AWP, map/side, and FHHS tables.
func runPlayer(cmd *cobra.Command, args []string) error {
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	type fhhsEntry struct {
		name  string
		id    uint64
		segs  []model.PlayerDuelSegment
		synth []model.PlayerMatchStats
	}

	var allAggs    []model.PlayerAggregate
	var allMapSide []model.PlayerMapSideAggregate
	var fhhsList   []fhhsEntry

	for _, arg := range args {
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid SteamID64 %q: %w", arg, err)
		}

		stats, err := db.GetAllPlayerMatchStats(id)
		if err != nil {
			return fmt.Errorf("query stats for %d: %w", id, err)
		}
		if len(stats) == 0 {
			fmt.Fprintf(os.Stderr, "No data found for SteamID64 %d\n", id)
			continue
		}

		segs, err := db.GetAllPlayerDuelSegments(id)
		if err != nil {
			return fmt.Errorf("query segments for %d: %w", id, err)
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

		allAggs = append(allAggs, agg)
		allMapSide = append(allMapSide, buildMapSideAggregates(stats)...)
		fhhsList = append(fhhsList, fhhsEntry{
			name: agg.Name,
			id:   id,
			segs: merged,
			synth: []model.PlayerMatchStats{{
				SteamID:        id,
				Name:           agg.Name,
				FirstHitHSRate: overallFHHS,
			}},
		})
	}

	if len(allAggs) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stdout)
	report.PrintPlayerAggregateOverview(os.Stdout, allAggs)
	report.PrintPlayerAggregateDuelTable(os.Stdout, allAggs)
	report.PrintPlayerAggregateAWPTable(os.Stdout, allAggs)
	report.PrintPlayerMapSideTable(os.Stdout, allMapSide)
	report.PrintPlayerAggregateAimTable(os.Stdout, allAggs)
	for _, f := range fhhsList {
		fmt.Fprintln(os.Stdout)
		report.PrintFHHSTable(os.Stdout, f.segs, f.synth, 0)
	}
	return nil
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
		agg.DuelWins += s.DuelWins
		agg.DuelLosses += s.DuelLosses
		agg.AWPDeaths += s.AWPDeaths
		agg.AWPDeathsDry += s.AWPDeathsDry
		agg.AWPDeathsRePeek += s.AWPDeathsRePeek
		agg.AWPDeathsIsolated += s.AWPDeathsIsolated

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
		role := s.Role
		if role == "" {
			role = "Rifler"
		}
		roleCounts[role]++
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
		agg.AvgCounterStrafePercent = csSum / float64(csN)
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
func buildMapSideAggregates(stats []model.PlayerMatchStats) []model.PlayerMapSideAggregate {
	type key struct{ mapName, side string }
	m := make(map[key]*model.PlayerMapSideAggregate)

	for _, s := range stats {
		side := s.Team.String()
		if side != "CT" && side != "T" {
			continue
		}
		mapName := strings.TrimPrefix(s.MapName, "de_")
		k := key{mapName, side}
		if m[k] == nil {
			m[k] = &model.PlayerMapSideAggregate{
				SteamID: s.SteamID,
				Name:    s.Name,
				MapName: mapName,
				Side:    side,
			}
		}
		a := m[k]
		a.Matches++
		a.Kills += s.Kills
		a.Assists += s.Assists
		a.Deaths += s.Deaths
		a.HeadshotKills += s.HeadshotKills
		a.TotalDamage += s.TotalDamage
		a.RoundsPlayed += s.RoundsPlayed
		a.KASTRounds += s.KASTRounds
		a.OpeningKills += s.OpeningKills
		a.OpeningDeaths += s.OpeningDeaths
		a.TradeKills += s.TradeKills
		a.TradeDeaths += s.TradeDeaths
	}

	out := make([]model.PlayerMapSideAggregate, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
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
