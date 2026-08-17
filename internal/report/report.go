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
	fmt.Fprintf(w, "\n%s\n", color.New(color.Bold).Sprintf("--- %s ---", title))
	if Verbose {
		fmt.Fprintf(w, "%s\n", desc)
	}
}

// colorKD wraps a K/D ratio string in green (≥1.0) or red (<1.0).
func colorKD(kd float64) string {
	s := fmt.Sprintf("%.2f", kd)
	if kd >= 1.0 {
		return color.GreenString(s)
	}
	return color.RedString(s)
}

// colorSide wraps a side string in cyan (CT) or yellow (T).
func colorSide(side string) string {
	switch side {
	case "CT":
		return color.CyanString(side)
	case "T":
		return color.YellowString(side)
	default:
		return side
	}
}

// colorRoundFlag wraps a single round flag in a contextual color:
// green for positive events (kill/trade), red for negative (death),
// yellow for post-plant, magenta for clutch.
func colorRoundFlag(flag string) string {
	switch {
	case flag == "OPEN_K" || flag == "TRADE_K":
		return color.GreenString(flag)
	case flag == "OPEN_D" || flag == "TRADE_D":
		return color.RedString(flag)
	case flag == "POST_PLT":
		return color.YellowString(flag)
	case strings.HasPrefix(flag, "CLUTCH"):
		return color.MagentaString(flag)
	default:
		return flag
	}
}

// formatPercentile renders a cohort percentile as "top X%" / "bot X%" /
// "p50" depending on where it sits. Returns "—" when the percentile is
// negative (cohort too small / no data).
func formatPercentile(p float64) string {
	if p < 0 {
		return "—"
	}
	switch {
	case p >= 95:
		return fmt.Sprintf("top %.0f%%", 100-p)
	case p >= 80:
		return fmt.Sprintf("p%.0f", p)
	case p >= 20:
		return fmt.Sprintf("p%.0f", p)
	default:
		return fmt.Sprintf("bot %.0f%%", p)
	}
}

// formatMinSec renders a duration in seconds as "1m 10s" (HLTV style).
// Negative or zero returns "0s". Fractions are rounded down to whole seconds.
func formatMinSec(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	total := int(seconds)
	m := total / 60
	s := total % 60
	if m == 0 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}

// toAny converts a []string to []any for use with tablewriter.Append.
func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// PrintMatchSummary prints a one-line summary header for the match.
func PrintMatchSummary(w io.Writer, s model.MatchSummary) {
	fmt.Fprintf(w, "\nMap: %s  |  Date: %s  |  Type: %s  |  Score: %s %d – %s %d  |  Hash: %s\n\n",
		s.MapName, s.MatchDate, s.MatchType,
		color.CyanString("CT"), s.CTScore,
		color.YellowString("T"), s.TScore,
		s.DemoHash[:12])
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
		table.Append(colorSide(s.Team.String()), s.Name, strconv.FormatUint(s.SteamID, 10))
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

	withMarker := focusSteamID != 0
	if withMarker {
		table.Header(" ", "NAME", "ROLE", "K", "A", "D", "K/D", "HS%", "ADR", "KAST%",
			"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D", "FA", "EFF_FLASH", "UTIL_DMG", "XHAIR_MED")
	} else {
		table.Header("NAME", "ROLE", "K", "A", "D", "K/D", "HS%", "ADR", "KAST%",
			"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D", "FA", "EFF_FLASH", "UTIL_DMG", "XHAIR_MED")
	}

	for _, s := range stats {
		xhairStr := "—"
		if s.CrosshairEncounters > 0 {
			xhairStr = fmt.Sprintf("%.1f°", s.CrosshairMedianDeg)
		}
		role := s.Role
		if role == "" {
			role = "Rifler"
		}
		row := []string{
			s.Name, role,
			strconv.Itoa(s.Kills), strconv.Itoa(s.Assists), strconv.Itoa(s.Deaths),
			colorKD(s.KDRatio()),
			fmt.Sprintf("%.0f%%", s.HSPercent()),
			fmt.Sprintf("%.1f", s.ADR()),
			fmt.Sprintf("%.0f%%", s.KASTPct()),
			strconv.Itoa(s.OpeningKills), strconv.Itoa(s.OpeningDeaths),
			strconv.Itoa(s.TradeKills), strconv.Itoa(s.TradeDeaths),
			strconv.Itoa(s.FlashAssists), strconv.Itoa(s.EffectiveFlashes),
			strconv.Itoa(s.UtilityDamage), xhairStr,
		}
		if withMarker {
			marker := " "
			if s.SteamID == focusSteamID {
				marker = color.CyanString(">")
			}
			row = append([]string{marker}, row...)
		}
		table.Append(toAny(row)...)
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
	withMarker := focusSteamID != 0
	if withMarker {
		table.Header(" ", "NAME", "SIDE", "K", "A", "D", "K/D", "ADR", "KAST%",
			"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D")
	} else {
		table.Header("NAME", "SIDE", "K", "A", "D", "K/D", "ADR", "KAST%",
			"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D")
	}

	var lastID uint64
	for _, s := range sides {
		name := s.Name
		if s.SteamID == lastID {
			name = `"`
		}
		lastID = s.SteamID
		row := []string{
			name, colorSide(s.Team.String()),
			strconv.Itoa(s.Kills), strconv.Itoa(s.Assists), strconv.Itoa(s.Deaths),
			colorKD(s.KDRatio()),
			fmt.Sprintf("%.1f", s.ADR()),
			fmt.Sprintf("%.0f%%", s.KASTPct()),
			strconv.Itoa(s.OpeningKills), strconv.Itoa(s.OpeningDeaths),
			strconv.Itoa(s.TradeKills), strconv.Itoa(s.TradeDeaths),
		}
		if withMarker {
			marker := " "
			if s.SteamID == focusSteamID {
				marker = color.CyanString(">")
			}
			row = append([]string{marker}, row...)
		}
		table.Append(toAny(row)...)
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

	withMarker := focusSteamID != 0
	if withMarker {
		table.Header(" ", "PLAYER", "W", "L", "WIN%", "EXPO_WIN", "EXPO_LOSS", "HITS/K", "1ST_HS%", "CORRECTION", "<2°%")
	} else {
		table.Header("PLAYER", "W", "L", "WIN%", "EXPO_WIN", "EXPO_LOSS", "HITS/K", "1ST_HS%", "CORRECTION", "<2°%")
	}

	for _, s := range stats {
		winPct := "—"
		if total := s.DuelWins + s.DuelLosses; total > 0 {
			winPct = fmt.Sprintf("%.0f%%", float64(s.DuelWins)/float64(total)*100)
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

		row := []string{s.Name, strconv.Itoa(s.DuelWins), strconv.Itoa(s.DuelLosses), winPct,
			expoWin, expoLoss, hitsK, firstHS, corr, under2}
		if withMarker {
			marker := " "
			if s.SteamID == focusSteamID {
				marker = color.CyanString(">")
			}
			row = append([]string{marker}, row...)
		}
		table.Append(toAny(row)...)
	}
	table.Render()
}

// PrintCrosshairTable prints the crosshair-placement breakdown for a single
// match: how far off target the crosshair sat at first sight, split into its
// yaw (horizontal) and pitch (vertical) components. Skips rendering when no
// player has any first-sight encounters.
//
// The combined median also appears as XHAIR_MED in the Performance Overview;
// this table adds the sample count and the yaw/pitch split, which distinguish
// horizontal pre-aim errors (wrong angle held) from vertical ones (crosshair
// off head level).
func PrintCrosshairTable(w io.Writer, stats []model.PlayerMatchStats, focusSteamID uint64) {
	hasData := false
	for _, s := range stats {
		if s.CrosshairEncounters > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Crosshair Placement",
		"N=first-sight encounters behind the medians (sample size; <20 is noisy)\n"+
			"MED_DEV=median total angular deviation from the enemy at first sight (lower = better pre-aim)\n"+
			"<5°%=share of encounters already within 5° of the enemy — effectively pre-aimed\n"+
			"YAW=median horizontal component (wrong angle held)  PITCH=median vertical component (off head level)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	withMarker := focusSteamID != 0
	if withMarker {
		table.Header(" ", "PLAYER", "N", "MED_DEV", "<5°%", "YAW", "PITCH")
	} else {
		table.Header("PLAYER", "N", "MED_DEV", "<5°%", "YAW", "PITCH")
	}

	for _, s := range stats {
		if s.CrosshairEncounters == 0 {
			continue
		}
		row := []string{
			s.Name,
			strconv.Itoa(s.CrosshairEncounters),
			fmt.Sprintf("%.1f°", s.CrosshairMedianDeg),
			fmt.Sprintf("%.0f%%", s.CrosshairPctUnder5),
			fmt.Sprintf("%.1f°", s.CrosshairMedianYawDeg),
			fmt.Sprintf("%.1f°", s.CrosshairMedianPitchDeg),
		}
		if withMarker {
			marker := " "
			if s.SteamID == focusSteamID {
				marker = color.CyanString(">")
			}
			row = append([]string{marker}, row...)
		}
		table.Append(toAny(row)...)
	}
	table.Render()
}

// PrintPlayerAggregateCrosshairTable is the cross-match version of
// PrintCrosshairTable. Angle stats are encounter-weighted averages of
// per-match medians (see PlayerAggregate).
func PrintPlayerAggregateCrosshairTable(w io.Writer, aggs []model.PlayerAggregate) {
	hasData := false
	for _, a := range aggs {
		if a.CrosshairEncounters > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Crosshair Placement (Aggregate)",
		"N=total first-sight encounters across all matches in the filter\n"+
			"MED_DEV=encounter-weighted median angular deviation at first sight (lower = better pre-aim)\n"+
			"<5°%=share of encounters already within 5° of the enemy — effectively pre-aimed\n"+
			"YAW=horizontal component (wrong angle held)  PITCH=vertical component (off head level)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "N", "MED_DEV", "<5°%", "YAW", "PITCH")
	for _, a := range aggs {
		if a.CrosshairEncounters == 0 {
			continue
		}
		table.Append(
			a.Name,
			strconv.Itoa(a.CrosshairEncounters),
			fmt.Sprintf("%.1f°", a.CrosshairMedianDeg),
			fmt.Sprintf("%.0f%%", a.CrosshairPctUnder5),
			fmt.Sprintf("%.1f°", a.CrosshairMedianYawDeg),
			fmt.Sprintf("%.1f°", a.CrosshairMedianPitchDeg),
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

	withMarker := focusSteamID != 0
	if withMarker {
		table.Header(" ", "PLAYER", "AWP_D", "DRY%", "REPEEK%", "ISOLATED%")
	} else {
		table.Header("PLAYER", "AWP_D", "DRY%", "REPEEK%", "ISOLATED%")
	}

	for _, s := range stats {
		dryPct, repeekPct, isolatedPct := "—", "—", "—"
		if s.AWPDeaths > 0 {
			dryPct = fmt.Sprintf("%.0f%%", float64(s.AWPDeathsDry)/float64(s.AWPDeaths)*100)
			repeekPct = fmt.Sprintf("%.0f%%", float64(s.AWPDeathsRePeek)/float64(s.AWPDeaths)*100)
			isolatedPct = fmt.Sprintf("%.0f%%", float64(s.AWPDeathsIsolated)/float64(s.AWPDeaths)*100)
		}
		row := []string{s.Name, strconv.Itoa(s.AWPDeaths), dryPct, repeekPct, isolatedPct}
		if withMarker {
			marker := " "
			if s.SteamID == focusSteamID {
				marker = color.CyanString(">")
			}
			row = append([]string{marker}, row...)
		}
		table.Append(toAny(row)...)
	}
	table.Render()
}

// PrintPlayerAggregateOverview prints overall performance stats aggregated across all demos.
func PrintPlayerAggregateOverview(w io.Writer, aggs []model.PlayerAggregate) {
	printSection(w, "Performance Overview",
		"RATING=Rating 2.0 proxy (community approx, ±0.05–0.10 vs HLTV)\n"+
			"K=Kills  A=Assists  D=Deaths  K/D=kill-death ratio  HS%=headshot kill %  ADR=avg damage per round\n"+
			"KAST%=rounds with a Kill/Assist/Survival/Trade  FA=flash assists  EFF_FLASH=blinded enemy died to your team within 1.5s\n"+
			"ENTRY_K/RD=opening kills per round  ENTRY_D/RD=opening deaths per round\n"+
			"TRADE_K/RD=trade kills per round  TRADE_D/RD=trade deaths per round")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "MATCHES", "RATING", "K", "A", "D", "K/D", "HS%", "ADR", "KAST%",
		"ENTRY_K/RD", "ENTRY_D/RD", "TRADE_K/RD", "TRADE_D/RD", "FA", "EFF_FLASH")

	fmtRate := func(v, rounds int) string {
		if rounds == 0 {
			return "—"
		}
		return fmt.Sprintf("%.2f", float64(v)/float64(rounds))
	}

	for _, a := range aggs {
		table.Append(
			a.Name,
			strconv.Itoa(a.Matches),
			fmt.Sprintf("%.2f", a.Rating2()),
			strconv.Itoa(a.Kills),
			strconv.Itoa(a.Assists),
			strconv.Itoa(a.Deaths),
			colorKD(a.KDRatio()),
			fmt.Sprintf("%.0f%%", a.HSPercent()),
			fmt.Sprintf("%.1f", a.ADR()),
			fmt.Sprintf("%.0f%%", a.KASTPct()),
			fmtRate(a.OpeningKills, a.RoundsPlayed),
			fmtRate(a.OpeningDeaths, a.RoundsPlayed),
			fmtRate(a.TradeKills, a.RoundsPlayed),
			fmtRate(a.TradeDeaths, a.RoundsPlayed),
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
	table.Header("PLAYER", "W", "L", "WIN%", "AVG_EXPO_WIN", "AVG_EXPO_LOSS", "AVG_HITS/K", "AVG_CORR")

	for _, a := range aggs {
		winPct := "—"
		if total := a.DuelWins + a.DuelLosses; total > 0 {
			winPct = fmt.Sprintf("%.0f%%", float64(a.DuelWins)/float64(total)*100)
		}
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
			winPct,
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
			"ENTRY_K/RD=opening kills per round  ENTRY_D/RD=opening deaths per round\n"+
			"TRADE_K/RD=trade kills per round  TRADE_D/RD=trade deaths per round")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("NAME", "MAP", "SIDE", "M", "K", "D", "K/D", "HS%", "ADR", "KAST%",
		"ENTRY_K/RD", "ENTRY_D/RD", "TRADE_K/RD", "TRADE_D/RD")

	fmtRate := func(v, rounds int) string {
		if rounds == 0 {
			return "—"
		}
		return fmt.Sprintf("%.2f", float64(v)/float64(rounds))
	}

	for _, a := range aggs {
		table.Append(
			a.Name,
			a.MapName,
			colorSide(a.Side),
			strconv.Itoa(a.Matches),
			strconv.Itoa(a.Kills),
			strconv.Itoa(a.Deaths),
			colorKD(a.KDRatio()),
			fmt.Sprintf("%.0f%%", a.HSPercent()),
			fmt.Sprintf("%.1f", a.ADR()),
			fmt.Sprintf("%.0f%%", a.KASTPct()),
			fmtRate(a.OpeningKills, a.RoundsPlayed),
			fmtRate(a.OpeningDeaths, a.RoundsPlayed),
			fmtRate(a.TradeKills, a.RoundsPlayed),
			fmtRate(a.TradeDeaths, a.RoundsPlayed),
		)
	}
	table.Render()
}

// PrintPlayerMapMechanicsTable prints per-map aim timing and mechanics stats
// (TTK, TTD, one-tap%, counter-strafe%) split by side. Skipped if no data.
func PrintPlayerMapMechanicsTable(w io.Writer, aggs []model.PlayerMapSideAggregate) {
	hasData := false
	for _, a := range aggs {
		if a.AvgTTKMs > 0 || a.AvgTTDMs > 0 || a.OneTapKills > 0 || a.AvgCounterStrafePct > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Mechanics by Map & Side",
		"AVG_TTK=avg ms from first shot to kill  AVG_TTD=avg ms from enemy's first shot to your death (higher=good)\n"+
			"1TAP%=one-tap kills as % of total kills  AVG_CS%=avg counter-strafe % (shots at horiz speed ≤ 34 u/s)")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("NAME", "MAP", "SIDE", "AVG_TTK", "AVG_TTD", "1TAP%", "AVG_CS%")

	fmtMs := func(ms float64) string {
		if ms <= 0 {
			return "—"
		}
		return fmt.Sprintf("%.0fms", ms)
	}
	fmtPct := func(v, total int) string {
		if total == 0 {
			return "—"
		}
		return fmt.Sprintf("%.0f%%", float64(v)/float64(total)*100)
	}

	for _, a := range aggs {
		csStr := "—"
		if a.AvgCounterStrafePct > 0 {
			csStr = fmt.Sprintf("%.0f%%", a.AvgCounterStrafePct)
		}
		table.Append(
			a.Name,
			a.MapName,
			colorSide(a.Side),
			fmtMs(a.AvgTTKMs),
			fmtMs(a.AvgTTDMs),
			fmtPct(a.OneTapKills, a.Kills),
			csStr,
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
	// Build name and overall-FHHS lookup.
	nameByID := make(map[uint64]string, len(players))
	overallFHHS := make(map[uint64]float64, len(players))
	for _, p := range players {
		nameByID[p.SteamID] = p.Name
		overallFHHS[p.SteamID] = p.FirstHitHSRate
	}

	// Filter segments before printing anything.
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

	printSection(w, "First-Hit Headshot Rate (FHHS)",
		"FHHS%=% of won duels where first shot hit the head (higher = better aim transfer on first contact)\n"+
			"N(hits)=sample count  FLAG=OK(≥50)/LOW(20–49) reliability  95% CI=Wilson confidence interval\n"+
			"MED_CORR=median pre-shot crosshair correction in degrees  *=weakest stable high-sample bin\n"+
			"MED_SIGHT=median first-sight angular deviation in this bin  EXPO_WIN=median ms from enemy visible\n"+
			"to your kill in this bin (the per-bin version of Duel Intelligence's EXPO_WIN — shows *where* you're slow)\n"+
			"SHOT_DLY=median ms from the enemy becoming visible to your FIRST SHOT. Read it against MED_CORR:\n"+
			"  a big correction with a short delay is reaction under surprise; a big correction with a long\n"+
			"  delay means you had time and still travelled too far — habit, not reflex.\n"+
			"NOTE: every column here comes from duels you WON — the duel engine only records segments on the\n"+
			"  killer's side, so lost duels are absent by construction.\n"+
			"VERY_LOW entries (<20 hits) are excluded — not enough data to be actionable")

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
	table.Header(" ", "PLAYER", "WEAPON", "DISTANCE", "N(hits)", "FHHS%", "95% CI",
		"MED_CORR", "MED_SIGHT", "SHOT_DLY", "EXPO_WIN", "FLAG")

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
		sightStr := "—"
		if s.MedianSightDeg > 0 {
			sightStr = fmt.Sprintf("%.1f°", s.MedianSightDeg)
		}
		expoStr := "—"
		if s.MedianExpoWinMs > 0 {
			expoStr = fmt.Sprintf("%.0fms", s.MedianExpoWinMs)
		}
		delayStr := "—"
		if s.MedianShotDelayMs > 0 {
			delayStr = fmt.Sprintf("%.0fms", s.MedianShotDelayMs)
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
			sightStr,
			delayStr,
			expoStr,
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
			"CS%=% of shots fired while horizontal speed ≤ 34 u/s (counter-strafed)\n"+
			"TRADE_K_MS=median ms from a teammate's death to your trade kill (lower = faster refrag)\n"+
			"TRADE_D_MS=median ms from your kill to your own death when traded back\n"+
			"DWELL%=out-of-combat time with crosshair settled (<25°/s); low = panic swiping\n"+
			"REV/MIN=out-of-combat yaw direction reversals (both legs ≥60°/s) per minute\n"+
			"YAW°/S=mean out-of-combat yaw speed; pairs with DWELL% to separate slow scanning from swiping")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	withMarker := focusSteamID != 0
	if withMarker {
		table.Header(" ", "PLAYER", "MEDIAN_TTK", "MEDIAN_TTD", "ONE_TAP%", "CS%",
			"TRADE_K_MS", "TRADE_D_MS", "DWELL%", "REV/MIN", "YAW°/S")
	} else {
		table.Header("PLAYER", "MEDIAN_TTK", "MEDIAN_TTD", "ONE_TAP%", "CS%",
			"TRADE_K_MS", "TRADE_D_MS", "DWELL%", "REV/MIN", "YAW°/S")
	}

	for _, s := range stats {
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
		tradeKStr := "—"
		if s.MedianTradeKillDelayMs > 0 {
			tradeKStr = fmt.Sprintf("%.0fms", s.MedianTradeKillDelayMs)
		}
		tradeDStr := "—"
		if s.MedianTradeDeathDelayMs > 0 {
			tradeDStr = fmt.Sprintf("%.0fms", s.MedianTradeDeathDelayMs)
		}
		dwellStr, revStr, yawStr := "—", "—", "—"
		if s.ScanOOCSeconds > 0 {
			dwellStr = fmt.Sprintf("%.0f%%", s.ScanDwellPct)
			revStr = fmt.Sprintf("%.1f", s.ScanReversalsPerMin)
			yawStr = fmt.Sprintf("%.0f", s.ScanAvgYawDegPerSec)
		}
		row := []string{s.Name, ttkStr, ttdStr, oneTapStr, csStr,
			tradeKStr, tradeDStr, dwellStr, revStr, yawStr}
		if withMarker {
			marker := " "
			if s.SteamID == focusSteamID {
				marker = color.CyanString(">")
			}
			row = append([]string{marker}, row...)
		}
		table.Append(toAny(row)...)
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
			colorKD(s.KDRatio()),
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
			"ONE_TAP%=% of kills that were one-taps  CS%=% of shots fired while counter-strafed (speed ≤ 34 u/s)\n"+
			"TRADE_K_MS=median ms from a teammate's death to your trade kill\n"+
			"DWELL%=out-of-combat time with crosshair settled (<25°/s)  REV/MIN=yaw reversals per minute\n"+
			"YAW°/S=mean out-of-combat yaw speed")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("DATE", "MAP", "RD", "MEDIAN_TTK", "MEDIAN_TTD", "ONE_TAP%", "CS%",
		"TRADE_K_MS", "DWELL%", "REV/MIN", "YAW°/S")

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
		tradeKStr := "—"
		if s.MedianTradeKillDelayMs > 0 {
			tradeKStr = fmt.Sprintf("%.0fms", s.MedianTradeKillDelayMs)
		}
		dwellStr, revStr, yawStr := "—", "—", "—"
		if s.ScanOOCSeconds > 0 {
			dwellStr = fmt.Sprintf("%.0f%%", s.ScanDwellPct)
			revStr = fmt.Sprintf("%.1f", s.ScanReversalsPerMin)
			yawStr = fmt.Sprintf("%.0f", s.ScanAvgYawDegPerSec)
		}
		table.Append(
			s.MatchDate,
			mapDisplay,
			strconv.Itoa(s.RoundsPlayed),
			ttkStr,
			ttdStr,
			oneTapStr,
			csStr,
			tradeKStr,
			dwellStr,
			revStr,
			yawStr,
		)
	}
	table.Render()
}

// clutchCell formats a single 1vN cell as "W/A (P%)" with color based on win rate.
// Returns "—" when attempts is zero.
func clutchCell(wins, attempts int) string {
	if attempts == 0 {
		return "—"
	}
	pct := float64(wins) / float64(attempts) * 100
	s := fmt.Sprintf("%d/%d (%.0f%%)", wins, attempts, pct)
	switch {
	case wins == attempts:
		return color.GreenString(s)
	case wins > 0:
		return color.YellowString(s)
	default:
		return color.RedString(s)
	}
}

// PrintMatchClutchTable prints per-player clutch W/A counts for a single match.
// Players with no clutch situations are shown as "—". Skips rendering if no player
// had a clutch situation in the match.
func PrintMatchClutchTable(w io.Writer, stats []model.PlayerMatchStats, clutch map[uint64]*model.PlayerClutchMatchStats) {
	hasData := false
	for _, s := range stats {
		if c := clutch[s.SteamID]; c != nil && c.TotalAttempts() > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Clutch",
		"Clutch situations this match. W/A (%) = wins/attempts per enemy count.\n"+
			"Green = all won, yellow = partial, red = none won.")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "1v1", "1v2", "1v3", "1v4", "1v5", "TOTAL")

	for _, s := range stats {
		c := clutch[s.SteamID]
		if c == nil {
			c = &model.PlayerClutchMatchStats{}
		}
		cells := make([]string, 5)
		for i := 1; i <= 5; i++ {
			cells[i-1] = clutchCell(c.Wins[i], c.Attempts[i])
		}
		table.Append(s.Name,
			cells[0], cells[1], cells[2], cells[3], cells[4],
			clutchCell(c.TotalWins(), c.TotalAttempts()),
		)
	}
	table.Render()
}

// PrintPlayerAggregateClutchTable prints clutch W/A counts aggregated across all demos
// for each player, broken down by enemy count (1v1–1v5). Matched by SteamID.
func PrintPlayerAggregateClutchTable(w io.Writer, aggs []model.PlayerAggregate, clutch []model.PlayerClutchMatchStats) {
	// Build SteamID → clutch lookup.
	byID := make(map[uint64]*model.PlayerClutchMatchStats, len(clutch))
	for i := range clutch {
		byID[clutch[i].SteamID] = &clutch[i]
	}
	// Only render if at least one player has clutch data.
	hasData := false
	for _, a := range aggs {
		if c := byID[a.SteamID]; c != nil && c.TotalAttempts() > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Clutch (Aggregate)",
		"Clutch situations aggregated across all matches. W/A = wins/attempts per enemy count.\n"+
			"Green = all won, yellow = partial, red = none won.")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "1v1", "1v2", "1v3", "1v4", "1v5", "TOTAL")

	for _, a := range aggs {
		c := byID[a.SteamID]
		if c == nil {
			c = &model.PlayerClutchMatchStats{}
		}
		cells := make([]string, 5)
		for i := 1; i <= 5; i++ {
			cells[i-1] = clutchCell(c.Wins[i], c.Attempts[i])
		}
		table.Append(a.Name,
			cells[0], cells[1], cells[2], cells[3], cells[4],
			clutchCell(c.TotalWins(), c.TotalAttempts()),
		)
	}
	table.Render()
}

// PrintClutchTrendTable prints a chronological per-match clutch breakdown for a player.
// Each row shows W/A (wins/attempts) per enemy count (1v1–1v5) for matches that had
// at least one clutch situation. Skips matches with no clutch data.
func PrintClutchTrendTable(w io.Writer, stats []model.PlayerMatchStats, clutchMap map[string]*model.PlayerClutchMatchStats) {
	hasData := false
	for _, s := range stats {
		if c := clutchMap[s.DemoHash]; c != nil && c.TotalAttempts() > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}
	printSection(w, "Clutch Trend",
		"Per-match clutch situations in chronological order. W/A = wins/attempts per enemy count.\n"+
			"Green = all won, yellow = partial, red = none won. TOTAL includes win rate %.")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("DATE", "MAP", "1v1", "1v2", "1v3", "1v4", "1v5", "TOTAL")

	for _, s := range stats {
		c := clutchMap[s.DemoHash]
		if c == nil || c.TotalAttempts() == 0 {
			continue
		}
		mapDisplay := strings.TrimPrefix(s.MapName, "de_")
		cells := make([]string, 5)
		for i := 1; i <= 5; i++ {
			cells[i-1] = clutchCell(c.Wins[i], c.Attempts[i])
		}
		table.Append(s.MatchDate, mapDisplay,
			cells[0], cells[1], cells[2], cells[3], cells[4],
			clutchCell(c.TotalWins(), c.TotalAttempts()),
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
			"KAST=✓ if earned KAST that round  FLAGS=OPEN_K/OPEN_D/TRADE_K/TRADE_D/POST_PLT/CLUTCH_1vN\n"+
			"DWELL%=out-of-combat time with crosshair settled (<25°/s); low = panic swiping  REV=yaw reversals\n"+
			"YAW°/S=mean out-of-combat yaw speed  — shown when the round had <5 s of qualifying out-of-combat time")
	// No UNUSED (unused_utility) column: the csraw2 bridge never populates
	// RawRound.PlayerEndState.GrenadeCount (bridge.go), so the column is 0 for
	// every demo ingested through convert/replay — i.e. the whole corpus. It
	// would render a permanent zero. See docs/unrendered-metrics.md.
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("RD", "SIDE", "BUY", "K", "A", "DMG", "KAST", "DWELL%", "REV", "YAW°/S", "FLAGS")

	buyCount := make(map[string]int)
	for _, s := range stats {
		buyType := s.BuyType
		if buyType == "" {
			buyType = "eco"
		}
		buyCount[buyType]++

		kastStr := " "
		if s.KASTEarned {
			kastStr = color.GreenString("✓")
		}

		var flags []string
		if s.IsOpeningKill {
			flags = append(flags, colorRoundFlag("OPEN_K"))
		}
		if s.IsOpeningDeath {
			flags = append(flags, colorRoundFlag("OPEN_D"))
		}
		if s.IsTradeKill {
			flags = append(flags, colorRoundFlag("TRADE_K"))
		}
		if s.IsTradeDeath {
			flags = append(flags, colorRoundFlag("TRADE_D"))
		}
		if s.IsPostPlant {
			flags = append(flags, colorRoundFlag("POST_PLT"))
		}
		if s.IsInClutch {
			flags = append(flags, colorRoundFlag(fmt.Sprintf("CLUTCH_1v%d", s.ClutchEnemyCount)))
		}
		flagStr := strings.Join(flags, ",")

		dwellStr, revStr, yawStr := "—", "—", "—"
		if s.ScanOOCSeconds >= 5 {
			dwellStr = fmt.Sprintf("%.0f%%", s.ScanDwellPct)
			revStr = strconv.Itoa(s.ScanReversals)
			yawStr = fmt.Sprintf("%.0f", s.ScanAvgYawDegPerSec)
		}

		table.Append(
			strconv.Itoa(s.RoundNumber),
			colorSide(s.Team.String()),
			buyType,
			strconv.Itoa(s.Kills),
			strconv.Itoa(s.Assists),
			strconv.Itoa(s.Damage),
			kastStr,
			dwellStr,
			revStr,
			yawStr,
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
			"AVG_TTK=avg ms from your first shot fired to kill (you as attacker, multi-hit kills only)\n"+
			"AVG_TTD=avg ms from enemy's first shot to your death (you as victim); higher than TTK is good\n"+
			"ONE_TAP%=one-tap kills as % of total kills across all matches\n"+
			"AVG_CS%=average per-match counter-strafe % (shots at horizontal speed ≤ 34 u/s)\n"+
			"TRADE_K_MS=avg ms from a teammate's death to your trade kill (lower = faster refrag)\n"+
			"TRADE_D_MS=avg ms from your kill to your own death when traded back\n"+
			"DWELL%=out-of-combat time with crosshair settled (<25°/s), time-weighted; low = panic swiping\n"+
			"REV/MIN=out-of-combat yaw direction reversals (both legs ≥60°/s) per minute, time-weighted\n"+
			"YAW°/S=mean out-of-combat yaw speed, time-weighted; pairs with DWELL% to separate slow scanning from swiping")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "ROLE", "AVG_TTK", "AVG_TTD", "ONE_TAP%", "AVG_CS%",
		"TRADE_K_MS", "TRADE_D_MS", "DWELL%", "REV/MIN", "YAW°/S")

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
		tradeKStr := "—"
		if a.AvgTradeKillDelayMs > 0 {
			tradeKStr = fmt.Sprintf("%.0fms", a.AvgTradeKillDelayMs)
		}
		tradeDStr := "—"
		if a.AvgTradeDeathDelayMs > 0 {
			tradeDStr = fmt.Sprintf("%.0fms", a.AvgTradeDeathDelayMs)
		}
		dwellStr, revStr, yawStr := "—", "—", "—"
		if a.ScanOOCSeconds > 0 {
			dwellStr = fmt.Sprintf("%.0f%%", a.ScanDwellPct)
			revStr = fmt.Sprintf("%.1f", a.ScanReversalsPerMin)
			yawStr = fmt.Sprintf("%.0f", a.ScanAvgYawDegPerSec)
		}
		table.Append(a.Name, role, ttkStr, ttdStr, oneTapStr, csStr,
			tradeKStr, tradeDStr, dwellStr, revStr, yawStr)
	}
	table.Render()
}

// PrintWeaponTable prints a per-weapon breakdown table.
// If focusSteamID is non-zero, only rows for that player are shown.
func PrintWeaponTable(w io.Writer, stats []model.PlayerWeaponStats, players []model.PlayerMatchStats, focusSteamID uint64) {
	printSection(w, "Weapon Breakdown",
		"K=kills with this weapon  HS%=headshot kill %  A=assists  D=deaths  DAMAGE=total damage dealt\n"+
			"HITS=total hits landed  DMG/HIT=average damage per hit  SHOTS=weapon-fire events\n"+
			"ACC%=raw hits per shot  ACC_VIS%=hits per shot with an enemy visible (excludes blind\n"+
			"  fire — smoke spam, prefire, wallbangs — and is the comparable aim number)\n"+
			"\"-\" means the weapon was never fired (grenade damage or deaths-only rows)")
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
	table.Header("PLAYER", "WEAPON", "K", "HS%", "A", "D", "DAMAGE", "HITS", "DMG/HIT", "SHOTS", "ACC%", "ACC_VIS%")

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
			shotCell(s.ShotsFired, strconv.Itoa(s.ShotsFired)),
			shotCell(s.ShotsFired, fmt.Sprintf("%.1f%%", s.Accuracy())),
			shotCell(s.ShotsVisible, fmt.Sprintf("%.1f%%", s.AccuracyVisible())),
		)
	}
	table.Render()
}

// shotCell renders "-" when the denominator is zero, so a weapon that was
// never fired (grenade damage, deaths-only rows) is visibly distinct from
// one fired with 0% accuracy.
func shotCell(n int, s string) string {
	if n == 0 {
		return "-"
	}
	return s
}

// weaponAccuracyMinShots is the sample floor for the aggregate accuracy
// table. Below it the per-weapon rates swing too much to act on.
const weaponAccuracyMinShots = 50

// PrintPlayerWeaponAccuracyTable renders cross-demo shot accounting per
// weapon: how many shots were fired, how many were blind, and what the hit
// and head-hit rates look like once blind fire is excluded.
//
// stats may span several players; rows are grouped per player and ordered by
// shots fired. nameByID supplies display names.
func PrintPlayerWeaponAccuracyTable(w io.Writer, stats []model.PlayerWeaponStats, nameByID map[uint64]string) {
	var rows []model.PlayerWeaponStats
	for _, s := range stats {
		if s.ShotsFired >= weaponAccuracyMinShots {
			rows = append(rows, s)
		}
	}
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SteamID != rows[j].SteamID {
			return rows[i].SteamID < rows[j].SteamID
		}
		return rows[i].ShotsFired > rows[j].ShotsFired
	})

	printSection(w, "Weapon Accuracy (Aggregate)",
		"SHOTS=weapon-fire events across all matches in the filter\n"+
			"BLIND%=shots taken with no enemy in your spotted mask — smoke spam, prefire,\n"+
			"  wallbangs, suppressing fire. Higher tiers deliberately do far more of this,\n"+
			"  so raw ACC% is NOT comparable between players of different levels.\n"+
			"ACC%=raw hits per shot  ACC_VIS%=hits per shot with an enemy visible (the comparable number)\n"+
			"HS/HIT=head-hitbox hits per hit (counts head hits that did NOT kill, unlike HS% on kills)\n"+
			"HS/HIT_V=same, restricted to shots with an enemy visible. NOTE: the spotted mask is set\n"+
			"  after an enemy is acquired, so a duel's opening shot usually falls in the blind bucket\n"+
			"  and the follow-up spray in the visible one — read HS/HIT_V as head rate once the duel\n"+
			"  is running (spray discipline), not as first-shot precision.\n"+
			"DMG/HIT=average damage per hit  SHOT/K=shots fired per kill\n"+
			fmt.Sprintf("Weapons with fewer than %d shots are omitted.", weaponAccuracyMinShots))

	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("PLAYER", "WEAPON", "SHOTS", "BLIND%", "ACC%", "ACC_VIS%", "HS/HIT", "HS/HIT_V", "DMG/HIT", "SHOT/K")

	for i := range rows {
		s := &rows[i]
		name := nameByID[s.SteamID]
		if name == "" {
			name = strconv.FormatUint(s.SteamID, 10)
		}
		shotsPerKill := "-"
		if s.Kills > 0 {
			shotsPerKill = fmt.Sprintf("%.1f", float64(s.ShotsFired)/float64(s.Kills))
		}
		table.Append(
			name,
			s.Weapon,
			strconv.Itoa(s.ShotsFired),
			fmt.Sprintf("%.1f%%", s.BlindShotPct()),
			fmt.Sprintf("%.1f%%", s.Accuracy()),
			shotCell(s.ShotsVisible, fmt.Sprintf("%.1f%%", s.AccuracyVisible())),
			fmt.Sprintf("%.1f%%", s.HeadHitPct()),
			shotCell(s.HitsVisible, fmt.Sprintf("%.1f%%", s.HeadHitPctVisible())),
			fmt.Sprintf("%.1f", s.AvgDamagePerHit()),
			shotsPerKill,
		)
	}
	table.Render()
}

// RoleViewOptions configures the `--roles` rendering. Per controls the rate
// denominator ("round" → values as printed per round; "24" → multiplied by 24
// for HLTV-style per-half display). Side narrows the role decomposition to
// CT-only / T-only rounds when "ct" or "t"; "both" shows the full data.
type RoleViewOptions struct {
	Per  string
	Side string
}

// rate scales a per-round value by 24 when Per == "24" and otherwise returns
// it unchanged. Used to flip every per-round metric in the role tables.
func (o RoleViewOptions) rate(v float64) float64 {
	if o.Per == "24" {
		return v * 24.0
	}
	return v
}

// rateLabel renders the appropriate suffix for rate column headers.
func (o RoleViewOptions) rateLabel() string {
	if o.Per == "24" {
		return "/24"
	}
	return "/RD"
}

// sideSuffix returns " (CT)" / " (T)" for side-restricted views, blank otherwise.
// Lets section titles surface what's being computed without the user having
// to look at the flags.
func (o RoleViewOptions) sideSuffix() string {
	switch o.Side {
	case "ct":
		return " (CT side)"
	case "t":
		return " (T side)"
	}
	return ""
}

// PrintPlayerRoleStats renders the HLTV-style role decomposition: a top-rail
// summary followed by one table per role (Firepower / Entrying / Trading /
// Opening / Clutching / Sniping / Utility). Players are rows, metrics are
// columns. Metrics sourced from event tables (Sniping rounds-with-kill,
// Utility kills, flashes thrown, opponent-flash seconds) print "—" when
// the corresponding source has no rows for the player (see PlayerRoleStats
// coverage flags).
//
// When opts.Side is "ct" or "t", the headline (RATING, ROUNDS, KAST%, K/D/DMG
// rates) and every per-round-derived metric reflect only that side's rounds;
// metrics from non-side-tagged match-level columns (trade kills, saved /
// saved-by, assisted kills, sniper kills, utility, HLTV flash assists, time
// alive) remain combined across both sides. The Role Overview RATING_CT /
// RATING_T columns also collapse to a single RATING in side-restricted views.
func PrintPlayerRoleStats(w io.Writer, roles []model.PlayerRoleStats, opts RoleViewOptions) {
	if len(roles) == 0 {
		return
	}
	rateU := opts.rateLabel()
	sideS := opts.sideSuffix()
	sampleS := sampleSuffix(roles)
	// Show the RANK column when at least one player has a valid percentile.
	showRank := false
	for _, r := range roles {
		if r.Rating2CohortPercentile >= 0 {
			showRank = true
			break
		}
	}

	// ---- Headline (§1 top rail) ----
	if opts.Side == "both" {
		printSection(w, "Role Overview"+sideS+sampleS,
			"RATING=Rating 2.0 (combined / CT / T)  KAST%=K/A/S/T rounds  KPR/DPR=kills/deaths per round\n"+
				"ADR=avg damage per round  MK%=rounds with 2+ kills  ROUNDS=total rounds in filter")
	} else {
		printSection(w, "Role Overview"+sideS+sampleS,
			"RATING=Rating 2.0 over the selected side only  KAST%=K/A/S/T rounds  K"+rateU+"/D"+rateU+"=kills/deaths\n"+
				"DMG"+rateU+"=avg damage  MK%=rounds with 2+ kills  ROUNDS=total side-rounds in filter")
	}
	t1 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	headers := []string{"PLAYER", "MAPS", "ROUNDS", "RATING"}
	if opts.Side == "both" {
		headers = append(headers, "RATING_CT", "RATING_T")
	}
	if showRank {
		headers = append(headers, "RANK")
	}
	headers = append(headers, "KAST%", "K"+rateU, "D"+rateU, "DMG"+rateU, "MK%")
	t1.Header(toAny(headers)...)
	for _, r := range roles {
		row := []string{
			r.Name,
			strconv.Itoa(r.Matches),
			strconv.Itoa(r.RoundsPlayed),
			fmt.Sprintf("%.2f", r.Rating2Combined),
		}
		if opts.Side == "both" {
			row = append(row,
				fmt.Sprintf("%.2f", r.Rating2CT),
				fmt.Sprintf("%.2f", r.Rating2T),
			)
		}
		if showRank {
			row = append(row, formatPercentile(r.Rating2CohortPercentile))
		}
		row = append(row,
			fmt.Sprintf("%.0f%%", r.KASTPct),
			fmt.Sprintf("%.2f", opts.rate(r.KPR)),
			fmt.Sprintf("%.2f", opts.rate(r.DPR)),
			fmt.Sprintf("%.1f", opts.rate(r.ADR)),
			fmt.Sprintf("%.1f%%", r.MultiKillPct),
		)
		t1.Append(toAny(row)...)
	}
	t1.Render()

	// ---- §2.1 Firepower ----
	printSection(w, "Firepower"+sideS+sampleS,
		"RD_WITH_K%=rounds with at least one kill  K/RWIN=kills in won rounds / rounds won\n"+
			"DMG/RWIN=damage in won rounds / rounds won  PISTOL_R=Rating 2.0 over pistol rounds only (R1/R13)")
	t2 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t2.Header("PLAYER", "RD_WITH_K%", "MK%", "K/RWIN", "DMG/RWIN", "PISTOL_R")
	for _, r := range roles {
		t2.Append(
			r.Name,
			fmt.Sprintf("%.1f%%", r.RoundsWithKillPct),
			fmt.Sprintf("%.1f%%", r.MultiKillPct),
			fmt.Sprintf("%.2f", r.KillsPerRoundWin),
			fmt.Sprintf("%.1f", r.DamagePerRoundWin),
			fmt.Sprintf("%.2f", r.PistolRoundRating),
		)
	}
	t2.Render()

	// ---- §2.2 Entrying ----
	printSection(w, "Entrying"+sideS+sampleS,
		"TRADED_D"+rateU+"=deaths traded by a teammate within 5s "+rateUnitWord(opts)+"  TRADED_D%=share of deaths traded\n"+
			"OPEN_D_TRADED%=share of opening deaths that were traded by a teammate within 5s\n"+
			"ASSISTS"+rateU+"=assists "+rateUnitWord(opts)+"  SUPPORT%=rounds with assist/survive/traded-death but no kill\n"+
			"SAVED_BY"+rateU+"=times "+rateUnitWord(opts)+" a teammate killed your last attacker within 1s of damage")
	t3 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t3.Header("PLAYER", "TRADED_D"+rateU, "TRADED_D%", "OPEN_D_TRADED%", "ASSISTS"+rateU, "SUPPORT%", "SAVED_BY"+rateU)
	for _, r := range roles {
		t3.Append(
			r.Name,
			fmt.Sprintf("%.2f", opts.rate(r.TradedDeathsPerRound)),
			fmt.Sprintf("%.1f%%", r.TradedDeathsPct),
			fmt.Sprintf("%.1f%%", r.OpeningDeathTradedPct),
			fmt.Sprintf("%.2f", opts.rate(r.AssistsPerRound)),
			fmt.Sprintf("%.1f%%", r.SupportRoundsPct),
			fmt.Sprintf("%.2f", opts.rate(r.SavedByTeammatePerRound)),
		)
	}
	t3.Render()

	// ---- §2.3 Trading ----
	printSection(w, "Trading"+sideS+sampleS,
		"TRADE_K"+rateU+"=kills within 5s of a teammate's death "+rateUnitWord(opts)+"  TRADE_K%=share of kills that are trades\n"+
			"DMG/KILL=total damage divided by total kills; <100 ⇒ kill-stealing tendency\n"+
			"SAVED"+rateU+"=times "+rateUnitWord(opts)+" you killed an opponent attacking a teammate within 1s\n"+
			"ASSISTED_K%=share of kills on opponents already damaged by a teammate this round")
	t4 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t4.Header("PLAYER", "TRADE_K"+rateU, "TRADE_K%", "DMG/KILL", "SAVED"+rateU, "ASSISTED_K%")
	for _, r := range roles {
		t4.Append(
			r.Name,
			fmt.Sprintf("%.2f", opts.rate(r.TradeKillsPerRound)),
			fmt.Sprintf("%.1f%%", r.TradeKillsPct),
			fmt.Sprintf("%.0f", r.DamagePerKill),
			fmt.Sprintf("%.2f", opts.rate(r.SavedTeammatePerRound)),
			fmt.Sprintf("%.1f%%", r.AssistedKillsPct),
		)
	}
	t4.Render()

	// ---- §2.4 Opening ----
	printSection(w, "Opening"+sideS+sampleS,
		"OPEN_K"+rateU+"=opening kills "+rateUnitWord(opts)+"  OPEN_D"+rateU+"=opening deaths "+rateUnitWord(opts)+"\n"+
			"ATTEMPTS%=% of rounds in the opening duel  SUCCESS%=opening duels won\n"+
			"WIN_AFTER_OPEN%=round wins when this player got the opener (5v4 follow-through)")
	t5 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t5.Header("PLAYER", "OPEN_K"+rateU, "OPEN_D"+rateU, "ATTEMPTS%", "SUCCESS%", "WIN_AFTER_OPEN%")
	for _, r := range roles {
		t5.Append(
			r.Name,
			fmt.Sprintf("%.2f", opts.rate(r.OpeningKPR)),
			fmt.Sprintf("%.2f", opts.rate(r.OpeningDPR)),
			fmt.Sprintf("%.1f%%", r.OpeningAttemptsPct),
			fmt.Sprintf("%.1f%%", r.OpeningSuccessPct),
			fmt.Sprintf("%.1f%%", r.WinAfterOpenPct),
		)
	}
	t5.Render()

	// ---- §2.5 Clutching ----
	printSection(w, "Clutching"+sideS+sampleS,
		"CLUTCH_PTS"+rateU+"=weighted clutch wins "+rateUnitWord(opts)+" (1v1=1, 1v2=2, 1v3=4, 1v4=8, 1v5=16)\n"+
			"1v1=attempts/wins  1v1_WIN%=cohort avg is ~60% (2v1 trades inflate the baseline)\n"+
			"SAVES/LOSS%=% of round losses where the player survived\n"+
			"TIME_ALIVE"+rateU+"=avg action-time alive "+rateUnitWord(opts)+" (HLTV cohort 50–90 s/round)\n"+
			"LAST_ALIVE_SVR%=% of rounds where the player was at any point the sole survivor on the server")
	t6 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t6.Header("PLAYER", "CLUTCH_PTS"+rateU, "1v1", "1v1_WIN%", "SAVES/LOSS%", "TIME_ALIVE"+rateU, "LAST_ALIVE_SVR%")
	for _, r := range roles {
		oneVOne := fmt.Sprintf("%d/%d", r.OneVOneWins, r.OneVOneAttempts)
		winPct := "—"
		if r.OneVOneAttempts > 0 {
			winPct = fmt.Sprintf("%.1f%%", r.OneVOneWinPct)
		}
		t6.Append(
			r.Name,
			fmt.Sprintf("%.3f", opts.rate(r.ClutchPointsPerRound)),
			oneVOne,
			winPct,
			fmt.Sprintf("%.1f%%", r.SavesPerLossPct),
			formatMinSec(opts.rate(r.TimeAlivePerRoundSec)),
			fmt.Sprintf("%.1f%%", r.LastAliveServerPct),
		)
	}
	t6.Render()

	// ---- §2.6 Sniping ----
	printSection(w, "Sniping"+sideS+sampleS,
		"AWP+SSG only. SNIPER_K"+rateU+"=sniper kills "+rateUnitWord(opts)+"  SNIPER_K%=share of all kills\n"+
			"RD_W/SNIPE%=% of rounds with at least one sniper kill  SNIPE_MK%=% of rounds with 2+\n"+
			"SNIPER_OPEN"+rateU+"=sniper opening kills "+rateUnitWord(opts)+"\n"+
			"Note: per-round metrics come from player_death_events (sparse for older demos; run replay --force to backfill)")
	t7 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t7.Header("PLAYER", "SNIPER_K"+rateU, "SNIPER_K%", "RD_W/SNIPE%", "SNIPE_MK%", "SNIPER_OPEN"+rateU)
	for _, r := range roles {
		rdSnipe := "—"
		snipeMK := "—"
		snipeOpen := "—"
		if r.HasSniperData {
			rdSnipe = fmt.Sprintf("%.1f%%", r.RoundsWithSniperKillPct)
			snipeMK = fmt.Sprintf("%.1f%%", r.SniperMultiKillRoundPct)
			snipeOpen = fmt.Sprintf("%.3f", opts.rate(r.SniperOpeningKillsPerRound))
		}
		t7.Append(
			r.Name,
			fmt.Sprintf("%.2f", opts.rate(r.SniperKillsPerRound)),
			fmt.Sprintf("%.1f%%", r.SniperKillsPct),
			rdSnipe,
			snipeMK,
			snipeOpen,
		)
	}
	t7.Render()

	// ---- §2.7 Utility ----
	printSection(w, "Utility"+sideS+sampleS,
		"UTIL_DMG"+rateU+"=HE+molotov damage "+rateUnitWord(opts)+"  UTIL_K/100R=HE+molotov+incendiary kills per 100 rounds\n"+
			"FLASH"+rateU+"=flashbangs thrown "+rateUnitWord(opts)+"  OPP_FLASH_S"+rateU+"=opponent blind seconds produced "+rateUnitWord(opts)+"\n"+
			"TEAM_FLASH_S"+rateU+"=teammate blind seconds caused "+rateUnitWord(opts)+" (flash discipline; compare against OPP_FLASH_S)\n"+
			"FLASH_A"+rateU+"=HLTV-style flash assists "+rateUnitWord(opts)+" (killer dealt ≥25 dmg to victim during blind window)\n"+
			"Note: UTIL_K, FLASH, OPP_FLASH_S, TEAM_FLASH_S come from event tables (sparse for older demos; run replay --force to backfill)")
	t8 := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	t8.Header("PLAYER", "UTIL_DMG"+rateU, "UTIL_K/100R", "FLASH"+rateU,
		"OPP_FLASH_S"+rateU, "TEAM_FLASH_S"+rateU, "FLASH_A"+rateU)
	for _, r := range roles {
		utilK := "—"
		if r.HasUtilityData {
			utilK = fmt.Sprintf("%.2f", r.UtilityKillsPer100R) // already per 100 rounds — unaffected by --per
		}
		flashRd := "—"
		if r.HasFlashThrowData {
			flashRd = fmt.Sprintf("%.2f", opts.rate(r.FlashesThrownPerRound))
		}
		oppFlash := "—"
		teamFlash := "—"
		if r.HasFlashTimeData {
			oppFlash = fmt.Sprintf("%.2f", opts.rate(r.OppFlashSecPerRound))
			teamFlash = fmt.Sprintf("%.2f", opts.rate(r.TeamFlashSecPerRound))
		}
		t8.Append(
			r.Name,
			fmt.Sprintf("%.2f", opts.rate(r.UtilityDamagePerRound)),
			utilK,
			flashRd,
			oppFlash,
			teamFlash,
			fmt.Sprintf("%.2f", opts.rate(r.HltvFlashAssistsPerRound)),
		)
	}
	t8.Render()
}

// rateUnitWord renders the textual unit used in section descriptions. Pairs
// with rateLabel() — "/RD" → "per round", "/24" → "per 24 rounds".
func rateUnitWord(o RoleViewOptions) string {
	if o.Per == "24" {
		return "per 24 rounds"
	}
	return "per round"
}

// sampleSuffix renders " — N maps · M rounds" for single-player views so the
// reader sees the sample size at a glance. Multi-player views skip this since
// the Role Overview header already lists MAPS/ROUNDS per row.
func sampleSuffix(roles []model.PlayerRoleStats) string {
	if len(roles) != 1 {
		return ""
	}
	r := roles[0]
	return fmt.Sprintf(" — %d maps · %d rounds", r.Matches, r.RoundsPlayed)
}
