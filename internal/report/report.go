package report

import (
	"fmt"
	"io"
	"os"
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
