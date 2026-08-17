package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"

	"github.com/pable/go-cs-metrics/internal/storage"
)

// phaseOrder puts round phases in chronological order rather than the
// count-descending order the query returns, so the table reads as a timeline.
var phaseOrder = map[string]int{
	"pistol":     0,
	"early":      1,
	"mid":        2,
	"late":       3,
	"post_plant": 4,
}

// distBinOrder orders the distance buckets near-to-far.
var distBinOrder = map[string]int{
	"0-5m":   0,
	"5-10m":  1,
	"10-15m": 2,
	"15-20m": 3,
	"20-30m": 4,
	"30m+":   5,
}

// PrintDeathTotals renders the headline summary for the `deaths` command.
// Pass roundsPlayed <= 0 when a rounds denominator doesn't apply (e.g. the
// view is restricted to one round phase); ROUNDS and DPR then render as "—"
// rather than a misleading zero.
func PrintDeathTotals(w io.Writer, t storage.DeathTotals, roundsPlayed int, playerName string) {
	title := fmt.Sprintf("Death Profile — %s", playerName)
	if t.Demos > 0 {
		title += fmt.Sprintf(" — %d maps · %d deaths · %s → %s",
			t.Demos, t.Deaths, t.FirstDate, t.LastDate)
	}
	printSection(w, title,
		"DPR=deaths per round played  HS_TAKEN%=share of your deaths from a headshot\n"+
			"FLASHED%=you were blind when you died  TRADED%=a teammate killed your killer within 5s\n"+
			"OPENING%=share of your deaths that were the round's first death\n"+
			"AVG_DIST=mean distance in meters between you and your killer")
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header("DEATHS", "ROUNDS", "DPR", "HS_TAKEN%", "FLASHED%", "TRADED%", "OPENING%", "AVG_DIST")
	rounds, dpr := "—", "—"
	if roundsPlayed > 0 {
		rounds = fmt.Sprintf("%d", roundsPlayed)
		dpr = fmt.Sprintf("%.2f", float64(t.Deaths)/float64(roundsPlayed))
	}
	table.Append(
		fmt.Sprintf("%d", t.Deaths),
		rounds,
		dpr,
		pctOf(t.Headshots, t.Deaths),
		pctOf(t.Flashed, t.Deaths),
		pctOf(t.Traded, t.Deaths),
		pctOf(t.OpeningDeal, t.Deaths),
		fmt.Sprintf("%.1fm", t.AvgDistM),
	)
	table.Render()
}

// PrintDeathBreakdown renders one grouped death table (by phase, weapon, map,
// or distance). total is the overall death count used for the SHARE% column;
// pass 0 to hide that column.
func PrintDeathBreakdown(w io.Writer, title, keyHeader, desc string, rows []storage.DeathBreakdown, total int) {
	if len(rows) == 0 {
		return
	}
	printSection(w, title, desc)
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
	table.Header(keyHeader, "DEATHS", "SHARE%", "HS_TAKEN%", "FLASHED%", "TRADED%", "OPENING%", "AVG_DIST")
	for _, r := range rows {
		share := "—"
		if total > 0 {
			share = fmt.Sprintf("%.1f%%", 100*float64(r.Deaths)/float64(total))
		}
		table.Append(
			r.Key,
			fmt.Sprintf("%d", r.Deaths),
			share,
			pctOf(r.Headshots, r.Deaths),
			pctOf(r.Flashed, r.Deaths),
			pctOf(r.Traded, r.Deaths),
			pctOf(r.OpeningDeal, r.Deaths),
			fmt.Sprintf("%.1fm", r.AvgDistM),
		)
	}
	table.Render()
}

// SortDeathsByPhase reorders phase rows chronologically. Unknown phases sort
// last, preserving their relative order.
func SortDeathsByPhase(rows []storage.DeathBreakdown) {
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oki := phaseOrder[rows[i].Key]
		oj, okj := phaseOrder[rows[j].Key]
		if !oki {
			oi = len(phaseOrder)
		}
		if !okj {
			oj = len(phaseOrder)
		}
		return oi < oj
	})
}

// SortDeathsByDistance reorders distance-bin rows near-to-far.
func SortDeathsByDistance(rows []storage.DeathBreakdown) {
	sort.SliceStable(rows, func(i, j int) bool {
		oi, oki := distBinOrder[rows[i].Key]
		oj, okj := distBinOrder[rows[j].Key]
		if !oki {
			oi = len(distBinOrder)
		}
		if !okj {
			oj = len(distBinOrder)
		}
		return oi < oj
	})
}

// pctOf formats n/d as a percentage, or "—" when the denominator is zero.
func pctOf(n, d int) string {
	if d == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(d))
}
