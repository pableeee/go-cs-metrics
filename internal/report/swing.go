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

// duelCell renders duel swing per duel with its sampling standard error, so a
// gap between two players cannot be read without also seeing whether it clears
// the noise. Roughly: a difference under 2x the larger error bar is not a
// difference. See swing.PlayerSwing.DuelVarSum for what the error does and
// does not cover.
func duelCell(perDuel, se float64) string {
	if se == 0 {
		return fmt.Sprintf("%+.4f", perDuel)
	}
	return fmt.Sprintf("%+.4f ±%.4f", perDuel, se)
}

const duelSELegend = "DUEL/DUEL carries ±1 standard error. It is a floor: duels cluster within rounds\n" +
	"and matches rather than being independent draws, and it covers sampling only —\n" +
	"not the features the probability table omits. Treat two players as tied unless\n" +
	"their gap is at least twice the larger error bar."

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
			"mean those duels mattered (RND/RND), and vice versa.\n"+duelSELegend)

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
			duelCell(p.DuelSwingPerDuel, p.DuelSwingSE),
			strconv.Itoa(p.DuelsWon),
			fmt.Sprintf("%.1f", p.ExpectedWins),
		)
	}
	table.Render()
}

// PrintSwingBySide renders both metrics split by the side the player was on,
// two rows per player.
//
// The split is the interesting cut for role analysis: a player can be well
// above expectation defending a site and well below taking space, and the
// combined number averages that away.
func PrintSwingBySide(w io.Writer, players []*swing.PlayerSwing, names map[uint64]string) {
	printSection(w, "Swing by Side",
		"The same two metrics restricted to the side the player was on at the time.\n"+
			"CT and T rows sum back to the overall numbers.\n"+
			"A side is not zero-sum on its own — only the two together are — so compare a CT\n"+
			"number against other CT numbers, never against the same player's T number as if\n"+
			"the scales were shared.\n"+
			"DUEL/DUEL is the axis pair worth plotting: T on one axis, CT on the other, one\n"+
			"point per player. Corners are role archetypes.\n"+duelSELegend)

	table := swingTable(w)
	table.Header("PLAYER", "SIDE", "ROUNDS", "DUELS", "RND_SWING", "RND/RND", "DUEL_SWING", "DUEL/DUEL", "WON", "EXP")
	for _, p := range players {
		name := nameOr(names, p.SteamID)
		for _, r := range []struct {
			label string
			s     *swing.SideSwing
		}{{"CT", &p.CT}, {"T", &p.T}} {
			table.Append(
				name, r.label,
				strconv.Itoa(r.s.Rounds),
				strconv.Itoa(r.s.Duels),
				fmt.Sprintf("%+.1f", r.s.RoundSwingTotal),
				fmt.Sprintf("%+.4f", r.s.RoundSwingPerRound),
				fmt.Sprintf("%+.1f", r.s.DuelSwingTotal),
				duelCell(r.s.DuelSwingPerDuel, r.s.DuelSwingSE),
				strconv.Itoa(r.s.DuelsWon),
				fmt.Sprintf("%.1f", r.s.ExpectedWins),
			)
			name = "" // only label the first row of each pair
		}
	}
	table.Render()
}

// PrintSwingByMap renders the per-map breakdown for a single player.
func PrintSwingByMap(w io.Writer, name string, rows []SwingMapRow, minRounds int) {
	printSection(w, fmt.Sprintf("Swing by Map — %s", name),
		fmt.Sprintf("Maps with fewer than %d rounds are omitted.\n", minRounds)+
			"The probability tables stay corpus-wide; only the attribution is per map,\n"+
			"so a map's number says how you performed there, not how that map behaves.\n"+duelSELegend)

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
			duelCell(p.DuelSwingPerDuel, p.DuelSwingSE),
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
			"'unseen' = the loser never spotted the winner; 'blind' = the winner never spotted the loser.\n"+
			"Advantage alone is NOT monotonic — see the freshness breakdown below for why.")
	table2 := swingTable(w)
	table2.Header("SIGHT ADVANTAGE", "P(WIN)", "N")
	adv := dt.DumpByAdvantage()
	sort.Slice(adv, func(i, j int) bool { return adv[i].P > adv[j].P })
	for _, e := range adv {
		table2.Append(e.Bucket, fmt.Sprintf("%.3f", e.P), strconv.Itoa(e.N))
	}
	table2.Render()

	printSection(w, "Duel Win Probability by advantage x freshness",
		"FRESHNESS=how long both players had been aware of each other when the duel resolved.\n"+
			"A sighting lead decays: it is worth much more when the duel resolves immediately than\n"+
			"once both sides have known for seconds, because by then the opponent picked the moment.\n"+
			"Symmetric buckets ('even', 'neither') sit at exactly 0.500 by construction — mirroring\n"+
			"forces it — so freshness only adds signal where the sighting was actually asymmetric.")
	table3 := swingTable(w)
	table3.Header("SIGHT ADVANTAGE", "FRESHNESS", "P(WIN)", "N")
	af := dt.DumpByAdvantageFreshness()
	sort.Slice(af, func(i, j int) bool {
		if af[i].Bucket != af[j].Bucket {
			return af[i].P > af[j].P
		}
		return af[i].Freshness < af[j].Freshness
	})
	for _, e := range af {
		if e.Bucket == "even" || e.Bucket == "neither" {
			continue // structurally 0.500, no information
		}
		table3.Append(e.Bucket, e.Freshness, fmt.Sprintf("%.3f", e.P), strconv.Itoa(e.N))
	}
	table3.Render()
}

// PrintSwingLeaderboard ranks the reference population by round swing per
// round and shows where the queried players sit inside it.
//
// The population is the point: round swing per round is a fraction of a
// probability point, which is meaningless without knowing the spread.
func PrintSwingLeaderboard(w io.Writer, pop, queried []*swing.PlayerSwing, names, tiers map[uint64]string, topN, minRounds int, bySide bool) {
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
	header := []any{"#", "PLAYER", "TIER", "ROUNDS", "RND/RND", "DUEL/DUEL"}
	if bySide {
		header = append(header, "CT_DUEL/DUEL", "T_DUEL/DUEL")
	}
	table.Header(header...)
	limit := topN
	if limit > len(sorted) {
		limit = len(sorted)
	}
	for i := 0; i < limit; i++ {
		p := sorted[i]
		row := []any{
			strconv.Itoa(i + 1), nameOr(names, p.SteamID), tiers[p.SteamID],
			strconv.Itoa(p.Rounds),
			fmt.Sprintf("%+.4f", p.RoundSwingPerRound),
			duelCell(p.DuelSwingPerDuel, p.DuelSwingSE),
		}
		if bySide {
			row = append(row,
				fmt.Sprintf("%+.4f", p.CT.DuelSwingPerDuel),
				fmt.Sprintf("%+.4f", p.T.DuelSwingPerDuel),
			)
		}
		table.Append(row...)
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
