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
		"ENTRY_K", "ENTRY_D", "TRADE_K", "TRADE_D", "FA", "UTIL_DMG",
	)

	for _, s := range stats {
		marker := " "
		if focusSteamID != 0 && s.SteamID == focusSteamID {
			marker = ">"
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
			strconv.Itoa(s.UtilityDamage),
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
