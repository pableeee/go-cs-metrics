package report

import (
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/pable/go-cs-metrics/internal/model"
)

// PrintMatchSummary prints a one-line summary header for the match.
func PrintMatchSummary(w io.Writer, s model.MatchSummary) {
	fmt.Fprintf(w, "\nMap: %s  |  Date: %s  |  Type: %s  |  Score: CT %d – T %d  |  Hash: %s\n\n",
		s.MapName, s.MatchDate, s.MatchType, s.CTScore, s.TScore, s.DemoHash[:12])
}

// PrintPlayerTable prints the player stats table to stdout.
// If focusSteamID is non-zero, that player's row is marked with ">".
func PrintPlayerTable(stats []model.PlayerMatchStats, focusSteamID uint64) {
	PrintPlayerTableTo(os.Stdout, stats, focusSteamID)
}

// PrintPlayerTableTo writes the table to the provided writer.
func PrintPlayerTableTo(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignCenter},
		},
	}))

	table.Header(
		" ", "NAME", "TEAM", "K", "A", "D", "K/D", "HS%", "ADR", "KAST%",
		"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D", "FA", "EFF_FLASH", "UTIL_DMG", "XHAIR_MED",
	)

	for _, s := range stats {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = ">"
		}
		xhairStr := "—"
		if s.CrosshairEncounters > 0 {
			xhairStr = fmt.Sprintf("%.1f°", s.CrosshairMedianDeg)
		}
		table.Append(
			marker,
			s.Name,
			s.Team.String(),
			strconv.Itoa(s.Kills),
			strconv.Itoa(s.Assists),
			strconv.Itoa(s.Deaths),
			fmt.Sprintf("%.2f", s.KDRatio()),
			fmt.Sprintf("%.0f%%", s.HSPercent()),
			fmt.Sprintf("%.1f", s.ADR()),
			fmt.Sprintf("%.0f%%", s.KASTPct()),
			strconv.Itoa(s.OpeningKills),
			strconv.Itoa(s.OpeningDeaths),
			strconv.Itoa(s.TradeKills),
			strconv.Itoa(s.TradeDeaths),
			strconv.Itoa(s.FlashAssists),
			strconv.Itoa(s.EffectiveFlashes),
			strconv.Itoa(s.UtilityDamage),
			xhairStr,
		)
	}
	table.Render()
}

// PrintDuelTable prints the duel intelligence table.
// Columns: PLAYER | W | L | EXPO_WIN | EXPO_LOSS | HITS/K | 1ST_HS% | CORRECTION | <2°%
func PrintDuelTable(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignCenter},
		},
	}))

	table.Header(" ", "PLAYER", "W", "L", "EXPO_WIN", "EXPO_LOSS", "HITS/K", "1ST_HS%", "CORRECTION", "<2°%")

	for _, s := range stats {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = ">"
		}

		expoWin := "—"
		if s.DuelWins > 0 {
			expoWin = fmt.Sprintf("%.0fms", s.MedianExposureWinMs)
		}
		expoLoss := "—"
		if s.DuelLosses > 0 {
			expoLoss = fmt.Sprintf("%.0fms", s.MedianExposureLossMs)
		}
		hitsK := "—"
		if s.MedianHitsToKill > 0 {
			hitsK = fmt.Sprintf("%.1f", s.MedianHitsToKill)
		}
		firstHS := "—"
		if s.DuelWins > 0 {
			firstHS = fmt.Sprintf("%.0f%%", s.FirstHitHSRate)
		}
		corr := "—"
		if s.MedianCorrectionDeg > 0 {
			corr = fmt.Sprintf("%.1f°", s.MedianCorrectionDeg)
		}
		under2 := "—"
		if s.PctCorrectionUnder2Deg > 0 || s.MedianCorrectionDeg >= 0 && s.DuelWins > 0 {
			under2 = fmt.Sprintf("%.0f%%", s.PctCorrectionUnder2Deg)
		}

		table.Append(
			marker,
			s.Name,
			strconv.Itoa(s.DuelWins),
			strconv.Itoa(s.DuelLosses),
			expoWin,
			expoLoss,
			hitsK,
			firstHS,
			corr,
			under2,
		)
	}
	table.Render()
}

// PrintAWPTable prints the AWP death classification table.
// Columns: PLAYER | AWP_D | DRY% | REPEEK% | ISOLATED%
func PrintAWPTable(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignCenter},
		},
	}))

	table.Header(" ", "PLAYER", "AWP_D", "DRY%", "REPEEK%", "ISOLATED%")

	for _, s := range stats {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = ">"
		}

		dryPct := "—"
		repeekPct := "—"
		isolatedPct := "—"
		if s.AWPDeaths > 0 {
			dryPct = fmt.Sprintf("%.0f%%", float64(s.AWPDeathsDry)/float64(s.AWPDeaths)*100)
			repeekPct = fmt.Sprintf("%.0f%%", float64(s.AWPDeathsRePeek)/float64(s.AWPDeaths)*100)
			isolatedPct = fmt.Sprintf("%.0f%%", float64(s.AWPDeathsIsolated)/float64(s.AWPDeaths)*100)
		}

		table.Append(
			marker,
			s.Name,
			strconv.Itoa(s.AWPDeaths),
			dryPct,
			repeekPct,
			isolatedPct,
		)
	}
	table.Render()
}

// PrintPlayerAggregateOverview prints overall performance stats aggregated across all demos.
func PrintPlayerAggregateOverview(w io.Writer, aggs []model.PlayerAggregate) {
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "MATCHES", "K", "A", "D", "K/D", "HS%", "ADR", "KAST%",
		"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D", "FA", "EFF_FLASH")

	for _, a := range aggs {
		table.Append(
			a.Name,
			strconv.Itoa(a.Matches),
			strconv.Itoa(a.Kills),
			strconv.Itoa(a.Assists),
			strconv.Itoa(a.Deaths),
			fmt.Sprintf("%.2f", a.KDRatio()),
			fmt.Sprintf("%.0f%%", a.HSPercent()),
			fmt.Sprintf("%.1f", a.ADR()),
			fmt.Sprintf("%.0f%%", a.KASTPct()),
			strconv.Itoa(a.OpeningKills),
			strconv.Itoa(a.OpeningDeaths),
			strconv.Itoa(a.TradeKills),
			strconv.Itoa(a.TradeDeaths),
			strconv.Itoa(a.FlashAssists),
			strconv.Itoa(a.EffectiveFlashes),
		)
	}
	table.Render()
}

// PrintPlayerAggregateDuelTable prints duel engine stats aggregated across all demos.
func PrintPlayerAggregateDuelTable(w io.Writer, aggs []model.PlayerAggregate) {
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "W", "L", "AVG_EXPO_WIN", "AVG_EXPO_LOSS", "AVG_HITS/K", "AVG_CORR")

	for _, a := range aggs {
		expoWin := "—"
		if a.AvgExpoWinMs > 0 {
			expoWin = fmt.Sprintf("%.0fms", a.AvgExpoWinMs)
		}
		expoLoss := "—"
		if a.AvgExpoLossMs > 0 {
			expoLoss = fmt.Sprintf("%.0fms", a.AvgExpoLossMs)
		}
		hitsK := "—"
		if a.AvgHitsToKill > 0 {
			hitsK = fmt.Sprintf("%.1f", a.AvgHitsToKill)
		}
		corr := "—"
		if a.AvgCorrectionDeg > 0 {
			corr = fmt.Sprintf("%.1f°", a.AvgCorrectionDeg)
		}
		table.Append(
			a.Name,
			strconv.Itoa(a.DuelWins),
			strconv.Itoa(a.DuelLosses),
			expoWin,
			expoLoss,
			hitsK,
			corr,
		)
	}
	table.Render()
}

// PrintPlayerAggregateAWPTable prints AWP death classification aggregated across all demos.
func PrintPlayerAggregateAWPTable(w io.Writer, aggs []model.PlayerAggregate) {
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "AWP_D", "DRY%", "REPEEK%", "ISOLATED%")

	for _, a := range aggs {
		dryPct, repeekPct, isolatedPct := "—", "—", "—"
		if a.AWPDeaths > 0 {
			dryPct = fmt.Sprintf("%.0f%%", float64(a.AWPDeathsDry)/float64(a.AWPDeaths)*100)
			repeekPct = fmt.Sprintf("%.0f%%", float64(a.AWPDeathsRePeek)/float64(a.AWPDeaths)*100)
			isolatedPct = fmt.Sprintf("%.0f%%", float64(a.AWPDeathsIsolated)/float64(a.AWPDeaths)*100)
		}
		table.Append(a.Name, strconv.Itoa(a.AWPDeaths), dryPct, repeekPct, isolatedPct)
	}
	table.Render()
}

// binOrder returns a sort key for distance bin strings (ascending distance).
func binOrder(bin string) int {
	switch bin {
	case "0-5m":
		return 0
	case "5-10m":
		return 1
	case "10-15m":
		return 2
	case "15-20m":
		return 3
	case "20-30m":
		return 4
	case "30m+":
		return 5
	default:
		return 6
	}
}

// bucketOrder returns a sort key for weapon bucket strings.
func bucketOrder(bucket string) int {
	switch bucket {
	case "AK":
		return 0
	case "M4":
		return 1
	case "Galil":
		return 2
	case "FAMAS":
		return 3
	case "ScopedRifle":
		return 4
	case "AWP":
		return 5
	case "Scout":
		return 6
	case "Deagle":
		return 7
	case "Pistol":
		return 8
	default:
		return 9
	}
}

func sampleFlag(n int) string {
	switch {
	case n >= 50:
		return "OK"
	case n >= 20:
		return "LOW"
	default:
		return "VERY_LOW"
	}
}

func isRifleBucket(b string) bool {
	return b == "AK" || b == "M4" || b == "Galil" || b == "FAMAS" || b == "ScopedRifle"
}

func isMidRangeBin(b string) bool {
	return b == "10-15m" || b == "15-20m" || b == "20-30m"
}

// PrintFHHSTable prints the First-Hit Headshot Rate segmented by weapon + distance.
// Priority bins (high sample, low FHHS relative to overall, mid-range rifle) are marked with "*".
// If focusSteamID is non-zero, only rows for that player are shown.
func PrintFHHSTable(w io.Writer, segs []model.PlayerDuelSegment, players []model.PlayerMatchStats, focusSteamID uint64) {
	// Build name and overall-FHHS lookup.
	nameByID := make(map[uint64]string, len(players))
	overallFHHS := make(map[uint64]float64, len(players))
	for _, p := range players {
		nameByID[p.SteamID] = p.Name
		overallFHHS[p.SteamID] = p.FirstHitHSRate
	}

	// Filter segments.
	var relevant []model.PlayerDuelSegment
	for _, s := range segs {
		if focusSteamID != 0 && s.SteamID != focusSteamID {
			continue
		}
		relevant = append(relevant, s)
	}
	if len(relevant) == 0 {
		return
	}

	// Sort: by player SteamID, then weapon bucket, then distance bin.
	sort.Slice(relevant, func(i, j int) bool {
		a, b := relevant[i], relevant[j]
		if a.SteamID != b.SteamID {
			return a.SteamID < b.SteamID
		}
		oa, ob := bucketOrder(a.WeaponBucket), bucketOrder(b.WeaponBucket)
		if oa != ob {
			return oa < ob
		}
		return binOrder(a.DistanceBin) < binOrder(b.DistanceBin)
	})

	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignCenter},
		},
	}))
	table.Header(" ", "PLAYER", "WEAPON", "DISTANCE", "N(hits)", "FHHS%", "95% CI", "MED_CORR", "FLAG")

	var priorityLines []string

	for _, s := range relevant {
		fhhs := 0.0
		if s.FirstHitCount > 0 {
			fhhs = float64(s.FirstHitHSCount) / float64(s.FirstHitCount) * 100
		}

		fhhsStr := "—"
		ciStr := "—"
		if s.FirstHitCount > 0 {
			fhhsStr = fmt.Sprintf("%.0f%%", fhhs)
			lo, hi := wilsonCI(s.FirstHitHSCount, s.FirstHitCount)
			ciStr = fmt.Sprintf("%.0f–%.0f%%", lo*100, hi*100)
		}

		corrStr := "—"
		if s.MedianCorrDeg > 0 {
			corrStr = fmt.Sprintf("%.1f°", s.MedianCorrDeg)
		}

		flag := sampleFlag(s.FirstHitCount)
		overall := overallFHHS[s.SteamID]
		isPriority := s.FirstHitCount >= 50 &&
			fhhs < overall-6.0 &&
			isRifleBucket(s.WeaponBucket) &&
			isMidRangeBin(s.DistanceBin)

		marker := " "
		if isPriority {
			marker = "*"
			name := nameByID[s.SteamID]
			priorityLines = append(priorityLines,
				fmt.Sprintf("%s %s@%s is your weakest stable bin: %.0f%% FHHS (N=%d).",
					name, s.WeaponBucket, s.DistanceBin, fhhs, s.FirstHitCount))
		}

		name := nameByID[s.SteamID]
		if name == "" {
			name = strconv.FormatUint(s.SteamID, 10)
		}

		table.Append(
			marker,
			name,
			s.WeaponBucket,
			s.DistanceBin,
			strconv.Itoa(s.FirstHitCount),
			fhhsStr,
			ciStr,
			corrStr,
			flag,
		)
	}
	table.Render()

	if len(priorityLines) > 0 {
		fmt.Fprintln(w, "\nPriority bins:")
		for _, line := range priorityLines {
			fmt.Fprintf(w, "  * %s\n", line)
		}
		fmt.Fprintln(w)
	}
}

// wilsonCI computes the 95% Wilson score confidence interval for a proportion.
// Returns (lo, hi) as fractions in [0, 1].
func wilsonCI(hits, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	z := 1.96
	p := float64(hits) / float64(n)
	nf := float64(n)
	denom := 1 + z*z/nf
	center := (p + z*z/(2*nf)) / denom
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / denom
	return math.Max(0, center-half), math.Min(1, center+half)
}

// PrintWeaponTable prints a per-weapon breakdown table.
// If focusSteamID is non-zero, only rows for that player are shown.
func PrintWeaponTable(w io.Writer, stats []model.PlayerWeaponStats, players []model.PlayerMatchStats, focusSteamID uint64) {
	// Build name lookup.
	nameByID := make(map[uint64]string, len(players))
	for _, p := range players {
		nameByID[p.SteamID] = p.Name
	}

	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignCenter},
		},
	}))
	table.Header("PLAYER", "WEAPON", "K", "HS%", "A", "D", "DAMAGE", "HITS", "DMG/HIT")

	for i := range stats {
		s := &stats[i]
		if focusSteamID != 0 && s.SteamID != focusSteamID {
			continue
		}
		name := nameByID[s.SteamID]
		if name == "" {
			name = strconv.FormatUint(s.SteamID, 10)
		}
		table.Append(
			name,
			s.Weapon,
			strconv.Itoa(s.Kills),
			fmt.Sprintf("%.0f%%", s.HSPercent()),
			strconv.Itoa(s.Assists),
			strconv.Itoa(s.Deaths),
			strconv.Itoa(s.Damage),
			strconv.Itoa(s.Hits),
			fmt.Sprintf("%.1f", s.AvgDamagePerHit()),
		)
	}
	table.Render()
}
