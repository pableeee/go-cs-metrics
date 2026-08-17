package report

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"

	"github.com/pable/go-cs-metrics/internal/swing"
)

// SwingMapRow pairs a map name with that map's swing result.
type SwingMapRow struct {
	MapName string
	Swing   *swing.PlayerSwing
}

func swingTable(w io.Writer) *tablewriter.Table {
	return tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row:    tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignRight}},
		Header: tw.CellConfig{Alignment: tw.CellAlignment{Global: tw.AlignCenter}},
	}))
}

// PrintSwingOverview renders both swing metrics side by side.
func PrintSwingOverview(w io.Writer, players []*swing.PlayerSwing, names map[uint64]string) {
	printSection(w, "Swing (Win Probability Added)",
		"ROUNDS/DUELS=sample the numbers rest on\n"+
			"RND_SWING=summed change in your team's round-win probability across your kills and deaths\n"+
			"RND/RND=the same per round played — the headline number. 0 = exactly average impact.\n"+
			"DUEL_SWING=summed (won − expected) over your duels, from P(win | first-sight advantage, range, weapon)\n"+
			"DUEL/DUEL=the same per duel. Positive = you win duels you were not favoured to win.\n"+
			"WON vs EXP=duels actually won against the number expected given the situations you were in.\n"+
			"The two are independent: winning duels you should lose (DUEL/DUEL > 0) does not\n"+
			"mean those duels mattered (RND/RND), and vice versa.")

	table := swingTable(w)
	table.Header("PLAYER", "ROUNDS", "DUELS", "RND_SWING", "RND/RND", "DUEL_SWING", "DUEL/DUEL", "WON", "EXP")
	for _, p := range players {
		name := names[p.SteamID]
		if name == "" {
			name = strconv.FormatUint(p.SteamID, 10)
		}
		table.Append(
			name,
			strconv.Itoa(p.Rounds),
			strconv.Itoa(p.Duels),
			fmt.Sprintf("%+.1f", p.RoundSwingTotal),
			fmt.Sprintf("%+.4f", p.RoundSwingPerRound),
			fmt.Sprintf("%+.1f", p.DuelSwingTotal),
			fmt.Sprintf("%+.4f", p.DuelSwingPerDuel),
			strconv.Itoa(p.DuelsWon),
			fmt.Sprintf("%.1f", p.ExpectedWins),
		)
	}
	table.Render()
}

// PrintSwingByMap renders the per-map breakdown for a single player.
func PrintSwingByMap(w io.Writer, name string, rows []SwingMapRow, minRounds int) {
	printSection(w, fmt.Sprintf("Swing by Map — %s", name),
		fmt.Sprintf("Maps with fewer than %d rounds are omitted.\n", minRounds)+
			"The probability tables stay corpus-wide; only the attribution is per map,\n"+
			"so a map's number says how you performed there, not how that map behaves.")

	table := swingTable(w)
	table.Header("MAP", "ROUNDS", "DUELS", "RND_SWING", "RND/RND", "DUEL_SWING", "DUEL/DUEL", "WON", "EXP")
	for _, r := range rows {
		p := r.Swing
		table.Append(
			r.MapName,
			strconv.Itoa(p.Rounds),
			strconv.Itoa(p.Duels),
			fmt.Sprintf("%+.1f", p.RoundSwingTotal),
			fmt.Sprintf("%+.4f", p.RoundSwingPerRound),
			fmt.Sprintf("%+.1f", p.DuelSwingTotal),
			fmt.Sprintf("%+.4f", p.DuelSwingPerDuel),
			strconv.Itoa(p.DuelsWon),
			fmt.Sprintf("%.1f", p.ExpectedWins),
		)
	}
	table.Render()
}

// PrintSwingTables dumps the empirical probability tables so the numbers can
// be audited by eye — the main advantage of counting over fitting.
func PrintSwingTables(w io.Writer, rt *swing.RoundTable, dt *swing.DuelTable) {
	printSection(w, "Round Win Probability (empirical)",
		"P(CT wins | alive counts, bomb planted), counted over every state rounds passed through.\n"+
			"Cells below the sample floor are omitted; the lookup backs off to a coarser key for those.")
	table := swingTable(w)
	table.Header("CT ALIVE", "T ALIVE", "PLANTED", "P(CT WINS)", "N")
	for _, e := range rt.Dump() {
		planted := "no"
		if e.Planted {
			planted = "yes"
		}
		table.Append(
			strconv.Itoa(e.AliveCT), strconv.Itoa(e.AliveT), planted,
			fmt.Sprintf("%.3f", e.P), strconv.Itoa(e.N),
		)
	}
	table.Render()

	printSection(w, "Duel Win Probability (empirical)",
		"P(win the duel | first-sight advantage), counted with both sides of every duel entered.\n"+
			"'unseen' = the loser never spotted the winner; 'blind' = the winner never spotted the loser.")
	table2 := swingTable(w)
	table2.Header("SIGHT ADVANTAGE", "P(WIN)", "N")
	adv := dt.DumpByAdvantage()
	sort.Slice(adv, func(i, j int) bool { return adv[i].P > adv[j].P })
	for _, e := range adv {
		table2.Append(e.Bucket, fmt.Sprintf("%.3f", e.P), strconv.Itoa(e.N))
	}
	table2.Render()
}

// PrintSwingLeaderboard ranks the reference population by round swing per
// round and shows where the queried players sit inside it.
//
// The population is the point: round swing per round is a fraction of a
// probability point, which is meaningless without knowing the spread.
func PrintSwingLeaderboard(w io.Writer, pop, queried []*swing.PlayerSwing, names, tiers map[uint64]string, topN, minRounds int) {
	if len(pop) == 0 {
		return
	}
	sorted := make([]*swing.PlayerSwing, len(pop))
	copy(sorted, pop)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].RoundSwingPerRound > sorted[j].RoundSwingPerRound
	})

	vals := make([]float64, len(sorted))
	for i, p := range sorted {
		vals[i] = p.RoundSwingPerRound
	}

	printSection(w, "Round Swing — reference distribution",
		fmt.Sprintf("Population: %d players with at least %d rounds.\n", len(sorted), minRounds)+
			"RND/RND is win probability added per round played. It is NOT HLTV's published round\n"+
			"swing: that one also credits damage, flash assists and trading, so the scales differ.\n"+
			"Use this distribution, not an outside number, to read your own value.")

	pct := func(q float64) float64 {
		idx := int(q * float64(len(vals)-1))
		return vals[idx]
	}
	dist := swingTable(w)
	dist.Header("PERCENTILE", "RND/RND")
	for _, e := range []struct {
		label string
		q     float64
	}{
		{"max", 0}, {"p95", 0.05}, {"p75", 0.25}, {"median", 0.5},
		{"p25", 0.75}, {"p05", 0.95}, {"min", 1},
	} {
		dist.Append(e.label, fmt.Sprintf("%+.4f", pct(e.q)))
	}
	dist.Render()

	table := swingTable(w)
	table.Header("#", "PLAYER", "TIER", "ROUNDS", "RND/RND", "DUEL/DUEL")
	limit := topN
	if limit > len(sorted) {
		limit = len(sorted)
	}
	for i := 0; i < limit; i++ {
		p := sorted[i]
		table.Append(
			strconv.Itoa(i+1), nameOr(names, p.SteamID), tiers[p.SteamID],
			strconv.Itoa(p.Rounds),
			fmt.Sprintf("%+.4f", p.RoundSwingPerRound),
			fmt.Sprintf("%+.4f", p.DuelSwingPerDuel),
		)
	}
	table.Render()

	// Where each queried player lands in that population.
	for _, q := range queried {
		rank, in := 0, false
		for i, p := range sorted {
			if p.SteamID == q.SteamID {
				rank, in = i+1, true
				break
			}
		}
		better := 0
		for _, v := range vals {
			if v < q.RoundSwingPerRound {
				better++
			}
		}
		percentile := 100 * float64(better) / float64(len(vals))
		name := nameOr(names, q.SteamID)
		if in {
			fmt.Fprintf(w, "\n%s: %+.4f RND/RND — rank %d of %d (%.0fth percentile).\n",
				name, q.RoundSwingPerRound, rank, len(sorted), percentile)
		} else {
			fmt.Fprintf(w, "\n%s: %+.4f RND/RND — below the %d-round floor, so not ranked; "+
				"would sit at the %.0fth percentile of the population.\n",
				name, q.RoundSwingPerRound, minRounds, percentile)
		}
	}
}

func nameOr(names map[uint64]string, id uint64) string {
	if n := names[id]; n != "" {
		return n
	}
	return strconv.FormatUint(id, 10)
}
