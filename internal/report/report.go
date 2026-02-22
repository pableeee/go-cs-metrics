// Package report formats and prints player, match, and aggregate statistics
// as terminal tables using tablewriter.
package report

import (
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/pable/go-cs-metrics/internal/model"
)

// Verbose controls whether metric explanations are printed before each table.
// Set this to true when the -v flag is passed.
var Verbose = true

// printSection prints a bold section title and, when Verbose is true, a one-line
// explanation of the columns that follow.
func printSection(w io.Writer, title, desc string) {
	fmt.Fprintf(w, "\n--- %s ---\n", title)
	if Verbose {
		fmt.Fprintf(w, "%s\n", desc)
	}
}

// PrintMatchSummary prints a one-line summary header for the match.
func PrintMatchSummary(w io.Writer, s model.MatchSummary) {
	fmt.Fprintf(w, "\nMap: %s  |  Date: %s  |  Type: %s  |  Score: CT %d – T %d  |  Hash: %s\n\n",
		s.MapName, s.MatchDate, s.MatchType, s.CTScore, s.TScore, s.DemoHash[:12])
}

// PrintPlayerRosterTable prints a compact name → SteamID64 listing so the user
// can identify which ID to pass to commands like "rounds <hash> <steamid>".
func PrintPlayerRosterTable(w io.Writer, stats []model.PlayerMatchStats) {
	fmt.Fprintf(w, "Players (use SteamID with: rounds <hash-prefix> <steamid>)\n")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignLeft},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignLeft},
		},
	}))
	table.Header("TEAM", "NAME", "STEAM_ID")
	for _, s := range stats {
		table.Append(s.Team.String(), s.Name, strconv.FormatUint(s.SteamID, 10))
	}
	table.Render()
	fmt.Fprintln(w)
}

// PrintPlayerTable prints the player stats table to stdout.
// If focusSteamID is non-zero, that player's row is marked with ">".
func PrintPlayerTable(stats []model.PlayerMatchStats, focusSteamID uint64) {
	PrintPlayerTableTo(os.Stdout, stats, focusSteamID)
}

// PrintPlayerTableTo writes the table to the provided writer.
func PrintPlayerTableTo(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	printSection(w, "Performance Overview",
		"K=Kills  A=Assists  D=Deaths  K/D=kill-death ratio  HS%=headshot kill %  ADR=avg damage per round\n"+
			"KAST%=rounds with a Kill/Assist/Survival/Trade  ROLE=heuristic role (AWPer/Entry/Support/Rifler)\n"+
			"ENTRY_K/D=first kill/death of the round  TRADE_K/D=kill traded within 5s\n"+
			"FA=flash assists  EFF_FLASH=blinded enemy died to your team within 1.5s\n"+
			"UTIL_DMG=HE/molotov damage  XHAIR_MED=median crosshair deviation at first sight (lower = better pre-aim)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignCenter},
		},
	}))

	table.Header(
		" ", "NAME", "ROLE", "TEAM", "K", "A", "D", "K/D", "HS%", "ADR", "KAST%",
		"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D", "FA", "EFF_FLASH", "UTIL_DMG", "XHAIR_MED",
	)

	for _, s := range stats {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = color.CyanString(">")
		}
		xhairStr := "—"
		if s.CrosshairEncounters > 0 {
			xhairStr = fmt.Sprintf("%.1f°", s.CrosshairMedianDeg)
		}
		role := s.Role
		if role == "" {
			role = "Rifler"
		}
		table.Append(
			marker,
			s.Name,
			role,
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

// PrintPlayerSideTable prints per-side (CT/T) basic stats for all players in a match.
// Rows are ordered by player (same order as PrintPlayerTable) with CT before T per player.
// If focusSteamID is non-zero, that player's rows are marked with ">".
func PrintPlayerSideTable(w io.Writer, sides []model.PlayerSideStats, focusSteamID uint64) {
	if len(sides) == 0 {
		return
	}
	printSection(w, "Per-Side Breakdown",
		"Stats split by CT and T halves for each player in this match.\n"+
			"K/A/D and ADR derived from round-level data. KAST/ENTRY/TRADE as per Performance Overview.")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header(" ", "NAME", "SIDE", "K", "A", "D", "K/D", "ADR", "KAST%",
		"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D")

	var lastID uint64
	for _, s := range sides {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = color.CyanString(">")
		}
		name := s.Name
		if s.SteamID == lastID {
			name = `"`
		}
		lastID = s.SteamID
		table.Append(
			marker,
			name,
			s.Team.String(),
			strconv.Itoa(s.Kills),
			strconv.Itoa(s.Assists),
			strconv.Itoa(s.Deaths),
			fmt.Sprintf("%.2f", s.KDRatio()),
			fmt.Sprintf("%.1f", s.ADR()),
			fmt.Sprintf("%.0f%%", s.KASTPct()),
			strconv.Itoa(s.OpeningKills),
			strconv.Itoa(s.OpeningDeaths),
			strconv.Itoa(s.TradeKills),
			strconv.Itoa(s.TradeDeaths),
		)
	}
	table.Render()
}

// PrintDuelTable prints the duel intelligence table.
// Columns: PLAYER | W | L | EXPO_WIN | EXPO_LOSS | HITS/K | 1ST_HS% | CORRECTION | <2°%
func PrintDuelTable(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	printSection(w, "Duel Intelligence",
		"W/L=duel wins and losses  EXPO_WIN=median ms from enemy visible to your kill (lower = faster)\n"+
			"EXPO_LOSS=same for duels lost  HITS/K=median bullets to kill  1ST_HS%=% of won duels where first shot hit the head\n"+
			"CORRECTION=degrees of crosshair adjustment before first shot (<2° ≈ pre-aimed)  <2°%=share of duels with correction under 2°")
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
			marker = color.CyanString(">")
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
	printSection(w, "AWP Deaths",
		"AWP_D=total deaths to AWP  DRY%=victim had no flash in last 3s (fully avoidable peek)\n"+
			"REPEEK%=victim had a kill earlier that round (punished for aggressive re-peek)\n"+
			"ISOLATED%=no teammates within 512 units at kill tick (taken without support)")
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
			marker = color.CyanString(">")
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
	printSection(w, "Performance Overview",
		"K=Kills  A=Assists  D=Deaths  K/D=kill-death ratio  HS%=headshot kill %  ADR=avg damage per round\n"+
			"KAST%=rounds with a Kill/Assist/Survival/Trade  ENTRY_K/D=first kill/death of the round\n"+
			"TRADE_K/D=kill traded within 5s  FA=flash assists  EFF_FLASH=blinded enemy died to your team within 1.5s")
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
	printSection(w, "Duel Intelligence",
		"W/L=duel wins and losses (summed)  AVG_EXPO_WIN=avg of per-match median ms from enemy visible to your kill\n"+
			"AVG_EXPO_LOSS=same for duels lost  AVG_HITS/K=avg of per-match median bullets to kill\n"+
			"AVG_CORR=avg of per-match median pre-shot crosshair correction in degrees")
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
	printSection(w, "AWP Deaths",
		"AWP_D=total deaths to AWP  DRY%=victim had no flash in last 3s (fully avoidable peek)\n"+
			"REPEEK%=victim had a kill earlier that round (punished for aggressive re-peek)\n"+
			"ISOLATED%=no teammates within 512 units at kill tick (taken without support)")
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

// PrintPlayerMapSideTable prints per-map CT/T split stats aggregated across all demos.
func PrintPlayerMapSideTable(w io.Writer, aggs []model.PlayerMapSideAggregate) {
	if len(aggs) == 0 {
		return
	}
	printSection(w, "Performance by Map & Side",
		"Stats split by map and side (CT/T). M=matches on that combination.\n"+
			"All other columns match the Performance Overview definitions.")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("NAME", "MAP", "SIDE", "M", "K", "D", "K/D", "HS%", "ADR", "KAST%",
		"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D")

	for _, a := range aggs {
		table.Append(
			a.Name,
			a.MapName,
			a.Side,
			strconv.Itoa(a.Matches),
			strconv.Itoa(a.Kills),
			strconv.Itoa(a.Deaths),
			fmt.Sprintf("%.2f", a.KDRatio()),
			fmt.Sprintf("%.0f%%", a.HSPercent()),
			fmt.Sprintf("%.1f", a.ADR()),
			fmt.Sprintf("%.0f%%", a.KASTPct()),
			strconv.Itoa(a.OpeningKills),
			strconv.Itoa(a.OpeningDeaths),
			strconv.Itoa(a.TradeKills),
			strconv.Itoa(a.TradeDeaths),
		)
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

// sampleFlag returns a reliability label ("OK", "LOW", or "VERY_LOW") based on
// the number of samples n.
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

// colorFlag wraps a sample-flag string in a terminal color: cyan for OK,
// yellow for LOW, and dim red for VERY_LOW.
func colorFlag(flag string) string {
	switch flag {
	case "OK":
		return color.CyanString(flag)
	case "LOW":
		return color.YellowString(flag)
	default:
		return color.New(color.FgRed, color.Faint).Sprint(flag)
	}
}

// isRifleBucket reports whether b is a rifle weapon bucket (AK, M4, Galil,
// FAMAS, or ScopedRifle).
func isRifleBucket(b string) bool {
	return b == "AK" || b == "M4" || b == "Galil" || b == "FAMAS" || b == "ScopedRifle"
}

// isMidRangeBin reports whether b represents a mid-range engagement distance
// (10-15m, 15-20m, or 20-30m).
func isMidRangeBin(b string) bool {
	return b == "10-15m" || b == "15-20m" || b == "20-30m"
}

// PrintFHHSTable prints the First-Hit Headshot Rate segmented by weapon + distance.
// Priority bins (high sample, low FHHS relative to overall, mid-range rifle) are marked with "*".
// If focusSteamID is non-zero, only rows for that player are shown.
func PrintFHHSTable(w io.Writer, segs []model.PlayerDuelSegment, players []model.PlayerMatchStats, focusSteamID uint64) {
	printSection(w, "First-Hit Headshot Rate (FHHS)",
		"FHHS%=% of won duels where first shot hit the head (higher = better aim transfer on first contact)\n"+
			"N(hits)=sample count  FLAG=OK(≥50)/LOW(≥20)/VERY_LOW(<20) reliability  95% CI=Wilson confidence interval\n"+
			"MED_CORR=median pre-shot crosshair correction in degrees  *=weakest stable high-sample bin")
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
			marker = color.YellowString("*")
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
			colorFlag(flag),
		)
	}
	table.Render()

	if len(priorityLines) > 0 {
		fmt.Fprintln(w, "\nPriority bins:")
		for _, line := range priorityLines {
			fmt.Fprintf(w, "  %s %s\n", color.YellowString("*"), line)
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

// PrintAimTimingTable prints the TTK, TTD, and Counter-Strafe % table.
// If focusSteamID is non-zero, that player's row is highlighted with ">".
// Rows where all three values are zero are shown as "—".
func PrintAimTimingTable(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	// Only show if at least one player has data.
	hasData := false
	for _, s := range stats {
		if s.MedianTTKMs > 0 || s.MedianTTDMs > 0 || s.OneTapKills > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Aim Timing & Movement",
		"MEDIAN_TTK=median ms from first shot fired → kill, multi-hit kills only (lower = faster finisher)\n"+
			"MEDIAN_TTD=median ms from enemy's first shot → your death, multi-hit only (lower = died faster)\n"+
			"ONE_TAP%=% of kills where the first shot fired in a 3s window was the killing shot\n"+
			"CS%=% of shots fired while horizontal speed ≤ 34 u/s (counter-strafed)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header(" ", "PLAYER", "MEDIAN_TTK", "MEDIAN_TTD", "ONE_TAP%", "CS%")

	for _, s := range stats {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = color.CyanString(">")
		}
		ttkStr := "—"
		if s.MedianTTKMs > 0 {
			ttkStr = fmt.Sprintf("%.0fms", s.MedianTTKMs)
		}
		ttdStr := "—"
		if s.MedianTTDMs > 0 {
			ttdStr = fmt.Sprintf("%.0fms", s.MedianTTDMs)
		}
		oneTapStr := "—"
		if s.Kills > 0 {
			oneTapStr = fmt.Sprintf("%.0f%%", float64(s.OneTapKills)/float64(s.Kills)*100)
		}
		csStr := "—"
		if s.CounterStrafePercent > 0 {
			csStr = fmt.Sprintf("%.0f%%", s.CounterStrafePercent)
		}
		table.Append(marker, s.Name, ttkStr, ttdStr, oneTapStr, csStr)
	}
	table.Render()
}

// PrintTrendTable prints a chronological per-match performance table for a player.
func PrintTrendTable(w io.Writer, stats []model.PlayerMatchStats) {
	printSection(w, "Performance Trend",
		"Per-match stats in chronological order.\n"+
			"DATE=match date  MAP=map  RD=rounds played  KPR=kills/round  ADR=avg damage/round  KAST=KAST%")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("DATE", "MAP", "RD", "K", "A", "D", "K/D", "KPR", "ADR", "KAST%")

	for _, s := range stats {
		mapDisplay := strings.TrimPrefix(s.MapName, "de_")
		kpr := "—"
		if s.RoundsPlayed > 0 {
			kpr = fmt.Sprintf("%.2f", float64(s.Kills)/float64(s.RoundsPlayed))
		}
		table.Append(
			s.MatchDate,
			mapDisplay,
			strconv.Itoa(s.RoundsPlayed),
			strconv.Itoa(s.Kills),
			strconv.Itoa(s.Assists),
			strconv.Itoa(s.Deaths),
			fmt.Sprintf("%.2f", s.KDRatio()),
			kpr,
			fmt.Sprintf("%.1f", s.ADR()),
			fmt.Sprintf("%.0f%%", s.KASTPct()),
		)
	}
	table.Render()
}

// PrintAimTrendTable prints a chronological per-match aim timing table for a player.
// It is only rendered if at least one match has TTK, TTD, or one-tap data.
func PrintAimTrendTable(w io.Writer, stats []model.PlayerMatchStats) {
	hasData := false
	for _, s := range stats {
		if s.MedianTTKMs > 0 || s.MedianTTDMs > 0 || s.OneTapKills > 0 || s.CounterStrafePercent > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Aim Timing Trend",
		"Per-match aim timing in chronological order.\n"+
			"MEDIAN_TTK/TTD=ms from first shot fired to kill/death (multi-hit only)\n"+
			"ONE_TAP%=% of kills that were one-taps  CS%=% of shots fired while counter-strafed (speed ≤ 34 u/s)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("DATE", "MAP", "RD", "MEDIAN_TTK", "MEDIAN_TTD", "ONE_TAP%", "CS%")

	for _, s := range stats {
		mapDisplay := strings.TrimPrefix(s.MapName, "de_")
		ttkStr := "—"
		if s.MedianTTKMs > 0 {
			ttkStr = fmt.Sprintf("%.0fms", s.MedianTTKMs)
		}
		ttdStr := "—"
		if s.MedianTTDMs > 0 {
			ttdStr = fmt.Sprintf("%.0fms", s.MedianTTDMs)
		}
		oneTapStr := "—"
		if s.Kills > 0 {
			oneTapStr = fmt.Sprintf("%.0f%%", float64(s.OneTapKills)/float64(s.Kills)*100)
		}
		csStr := "—"
		if s.CounterStrafePercent > 0 {
			csStr = fmt.Sprintf("%.0f%%", s.CounterStrafePercent)
		}
		table.Append(
			s.MatchDate,
			mapDisplay,
			strconv.Itoa(s.RoundsPlayed),
			ttkStr,
			ttdStr,
			oneTapStr,
			csStr,
		)
	}
	table.Render()
}

// PrintRoundDetailTable prints a per-round drill-down table for a single player in a match.
func PrintRoundDetailTable(w io.Writer, stats []model.PlayerRoundStats, playerName, mapName string) {
	if len(stats) == 0 {
		return
	}
	printSection(w, fmt.Sprintf("%s — %s — %d rounds", playerName, mapName, len(stats)),
		"SIDE=CT or T  BUY=buy type (full/force/half/eco)  K/A/DMG=kills/assists/damage\n"+
			"KAST=✓ if earned KAST that round  FLAGS=OPEN_K/OPEN_D/TRADE_K/TRADE_D/POST_PLT/CLUTCH_1vN")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("RD", "SIDE", "BUY", "K", "A", "DMG", "KAST", "FLAGS")

	buyCount := make(map[string]int)
	for _, s := range stats {
		buyType := s.BuyType
		if buyType == "" {
			buyType = "eco"
		}
		buyCount[buyType]++

		kastStr := " "
		if s.KASTEarned {
			kastStr = "✓"
		}

		var flags []string
		if s.IsOpeningKill {
			flags = append(flags, "OPEN_K")
		}
		if s.IsOpeningDeath {
			flags = append(flags, "OPEN_D")
		}
		if s.IsTradeKill {
			flags = append(flags, "TRADE_K")
		}
		if s.IsTradeDeath {
			flags = append(flags, "TRADE_D")
		}
		if s.IsPostPlant {
			flags = append(flags, "POST_PLT")
		}
		if s.IsInClutch {
			flags = append(flags, fmt.Sprintf("CLUTCH_1v%d", s.ClutchEnemyCount))
		}
		flagStr := strings.Join(flags, ",")

		table.Append(
			strconv.Itoa(s.RoundNumber),
			s.Team.String(),
			buyType,
			strconv.Itoa(s.Kills),
			strconv.Itoa(s.Assists),
			strconv.Itoa(s.Damage),
			kastStr,
			flagStr,
		)
	}
	table.Render()

	// Buy profile summary.
	total := len(stats)
	fmt.Fprintf(w, "\nBuy Profile: ")
	for _, bt := range []string{"full", "force", "half", "eco"} {
		n := buyCount[bt]
		fmt.Fprintf(w, "%s=%d (%.0f%%)  ", bt, n, float64(n)/float64(total)*100)
	}
	fmt.Fprintln(w)
}

// PrintPlayerAggregateAimTable prints TTK/TTD/one-tap stats aggregated across all demos.
func PrintPlayerAggregateAimTable(w io.Writer, aggs []model.PlayerAggregate) {
	hasData := false
	for _, a := range aggs {
		if a.AvgTTKMs > 0 || a.AvgTTDMs > 0 || a.OneTapKills > 0 || a.Role != "" {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Aim Timing & Movement (Aggregate)",
		"ROLE=most common heuristic role across matches\n"+
			"AVG_TTK/AVG_TTD=average of per-match median ms from first shot fired, multi-hit kills only\n"+
			"ONE_TAP%=one-tap kills as % of total kills across all matches\n"+
			"AVG_CS%=average per-match counter-strafe % (shots at horizontal speed ≤ 34 u/s)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "ROLE", "AVG_TTK", "AVG_TTD", "ONE_TAP%", "AVG_CS%")

	for _, a := range aggs {
		role := a.Role
		if role == "" {
			role = "Rifler"
		}
		ttkStr := "—"
		if a.AvgTTKMs > 0 {
			ttkStr = fmt.Sprintf("%.0fms", a.AvgTTKMs)
		}
		ttdStr := "—"
		if a.AvgTTDMs > 0 {
			ttdStr = fmt.Sprintf("%.0fms", a.AvgTTDMs)
		}
		oneTapStr := "—"
		if a.Kills > 0 {
			oneTapStr = fmt.Sprintf("%.0f%%", float64(a.OneTapKills)/float64(a.Kills)*100)
		}
		csStr := "—"
		if a.AvgCounterStrafePct > 0 {
			csStr = fmt.Sprintf("%.0f%%", a.AvgCounterStrafePct)
		}
		table.Append(a.Name, role, ttkStr, ttdStr, oneTapStr, csStr)
	}
	table.Render()
}

// PrintWeaponTable prints a per-weapon breakdown table.
// If focusSteamID is non-zero, only rows for that player are shown.
func PrintWeaponTable(w io.Writer, stats []model.PlayerWeaponStats, players []model.PlayerMatchStats, focusSteamID uint64) {
	printSection(w, "Weapon Breakdown",
		"K=kills with this weapon  HS%=headshot kill %  A=assists  D=deaths  DAMAGE=total damage dealt\n"+
			"HITS=total hits landed  DMG/HIT=average damage per hit")
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
