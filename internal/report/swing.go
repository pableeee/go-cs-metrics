package report

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"

	"github.com/pable/go-cs-metrics/internal/storage"
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

// PrintSwingByMap renders the per-map breakdown for a single player. With
// bySide set, each map gets CT and T rows after the combined one.
func PrintSwingByMap(w io.Writer, name string, rows []SwingMapRow, minRounds int, bySide bool) {
	desc := fmt.Sprintf("Maps with fewer than %d rounds are omitted.\n", minRounds) +
		"The probability tables stay corpus-wide; only the attribution is per map,\n" +
		"so a map's number says how you performed there, not how that map behaves.\n"
	if bySide {
		desc += "CT and T rows sum back to the map's combined row; a side is not zero-sum\n" +
			"on its own, so compare it against other CT (or T) numbers only.\n"
	}
	printSection(w, fmt.Sprintf("Swing by Map — %s", name), desc+duelSELegend)

	table := swingTable(w)
	if bySide {
		table.Header("MAP", "SIDE", "ROUNDS", "DUELS", "RND_SWING", "RND/RND", "DUEL_SWING", "DUEL/DUEL", "WON", "EXP")
	} else {
		table.Header("MAP", "ROUNDS", "DUELS", "RND_SWING", "RND/RND", "DUEL_SWING", "DUEL/DUEL", "WON", "EXP")
	}
	appendRow := func(mapName, side string, rounds, duels int, rst, rspr, dst float64, ddCell string, won int, exp float64) {
		row := []any{mapName}
		if bySide {
			row = append(row, side)
		}
		row = append(row,
			strconv.Itoa(rounds),
			strconv.Itoa(duels),
			fmt.Sprintf("%+.1f", rst),
			fmt.Sprintf("%+.4f", rspr),
			fmt.Sprintf("%+.1f", dst),
			ddCell,
			strconv.Itoa(won),
			fmt.Sprintf("%.1f", exp),
		)
		table.Append(row...)
	}
	for _, r := range rows {
		p := r.Swing
		appendRow(r.MapName, "ALL", p.Rounds, p.Duels, p.RoundSwingTotal, p.RoundSwingPerRound,
			p.DuelSwingTotal, duelCell(p.DuelSwingPerDuel, p.DuelSwingSE), p.DuelsWon, p.ExpectedWins)
		if bySide {
			for _, sr := range []struct {
				label string
				s     *swing.SideSwing
			}{{"CT", &p.CT}, {"T", &p.T}} {
				appendRow("", sr.label, sr.s.Rounds, sr.s.Duels, sr.s.RoundSwingTotal, sr.s.RoundSwingPerRound,
					sr.s.DuelSwingTotal, duelCell(sr.s.DuelSwingPerDuel, sr.s.DuelSwingSE), sr.s.DuelsWon, sr.s.ExpectedWins)
			}
		}
	}
	table.Render()
}

// PrintSwingByWeapon renders one player's swing sliced by the weapon class
// that resolved each duel.
func PrintSwingByWeapon(w io.Writer, name string, p *swing.PlayerSwing, minDuels int) {
	printSection(w, fmt.Sprintf("Swing by Weapon — %s", name),
		"The weapon is the one that RESOLVED the duel — the killer's. 'pistol' means\n"+
			"duels decided by a pistol, not duels you held one in: on your wins it is your\n"+
			"weapon, on your losses the opponent's.\n"+
			fmt.Sprintf("Classes with fewer than %d duels are omitted. Per-round rates are not\n", minDuels)+
			"defined within a class — a round is not played 'with a weapon'.\n"+duelSELegend)

	type entry struct {
		class string
		s     *swing.SideSwing
	}
	var entries []entry
	for cls, s := range p.Weapons {
		if s.Duels >= minDuels {
			entries = append(entries, entry{cls, s})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].s.Duels > entries[j].s.Duels })

	table := swingTable(w)
	table.Header("WEAPON", "DUELS", "RND_SWING", "DUEL_SWING", "DUEL/DUEL", "WON", "EXP")
	for _, e := range entries {
		table.Append(
			e.class,
			strconv.Itoa(e.s.Duels),
			fmt.Sprintf("%+.1f", e.s.RoundSwingTotal),
			fmt.Sprintf("%+.1f", e.s.DuelSwingTotal),
			duelCell(e.s.DuelSwingPerDuel, e.s.DuelSwingSE),
			strconv.Itoa(e.s.DuelsWon),
			fmt.Sprintf("%.1f", e.s.ExpectedWins),
		)
	}
	table.Render()
}

// PrintSwingFloorCeiling renders the per-demo distribution of both rates for
// the queried players and, with topN > 0, a population leaderboard ranked by
// round-swing floor — the players who beat expectation even on bad days.
func PrintSwingFloorCeiling(w io.Writer, queried, pop []swing.PlayerFloorCeiling, names, tiers map[uint64]string, topN, minDemos int) {
	printSection(w, "Swing Floor / Ceiling",
		fmt.Sprintf("Quantiles of PER-DEMO rates: floor = p25, ceiling = p75. Demos where the\n"+
			"player has fewer than 10 rounds are skipped, and players with fewer than %d\n"+
			"qualifying demos are omitted — a distribution over a handful of demos is noise.\n"+
			"A floor above zero means beating expectation even in the bad games.", minDemos))

	printRows := func(rows []swing.PlayerFloorCeiling, withRank bool) {
		table := swingTable(w)
		header := []any{"PLAYER", "DEMOS", "ROUNDS", "RND_FLOOR", "RND_MED", "RND_CEIL", "DUEL_FLOOR", "DUEL_MED", "DUEL_CEIL"}
		if withRank {
			header = append([]any{"#"}, append(header, "TIER")...)
		}
		table.Header(header...)
		for i, fc := range rows {
			row := []any{
				nameOr(names, fc.SteamID),
				strconv.Itoa(fc.Demos),
				strconv.Itoa(fc.RoundsTotal),
				fmt.Sprintf("%+.4f", fc.RndFloor),
				fmt.Sprintf("%+.4f", fc.RndMedian),
				fmt.Sprintf("%+.4f", fc.RndCeiling),
				fmt.Sprintf("%+.4f", fc.DuelFloor),
				fmt.Sprintf("%+.4f", fc.DuelMedian),
				fmt.Sprintf("%+.4f", fc.DuelCeiling),
			}
			if withRank {
				row = append([]any{strconv.Itoa(i + 1)}, append(row, tiers[fc.SteamID])...)
			}
			table.Append(row...)
		}
		table.Render()
	}

	if len(queried) > 0 {
		printRows(queried, false)
	}
	if topN > 0 && len(pop) > 0 {
		sort.Slice(pop, func(i, j int) bool { return pop[i].RndFloor > pop[j].RndFloor })
		limit := min(topN, len(pop))
		fmt.Fprintf(w, "\nTop %d of %d players by round-swing floor:\n", limit, len(pop))
		printRows(pop[:limit], true)
	}
}

// PrintSwingTeamShape renders how concentrated a queried set's firepower is:
// the median player against the best one. A large gap is the "hard carried"
// shape — one player far above a middle that cannot follow.
func PrintSwingTeamShape(w io.Writer, team string, players []*swing.PlayerSwing, names map[uint64]string) {
	title := "Team Shape"
	if team != "" {
		title += " — " + team
	}
	printSection(w, title,
		"Median vs best DUEL/DUEL across the queried players. The gap is the carry\n"+
			"concentration: a big gap means one player is the team's firepower.\n"+
			"Only meaningful when the queried set is an actual roster.")

	rates := make([]float64, 0, len(players))
	best := players[0]
	for _, p := range players {
		rates = append(rates, p.DuelSwingPerDuel)
		if p.DuelSwingPerDuel > best.DuelSwingPerDuel {
			best = p
		}
	}
	sort.Float64s(rates)
	median := rates[len(rates)/2]
	if len(rates)%2 == 0 {
		median = (rates[len(rates)/2-1] + rates[len(rates)/2]) / 2
	}

	table := swingTable(w)
	table.Header("PLAYERS", "MEDIAN DUEL/DUEL", "BEST", "BEST PLAYER", "GAP")
	table.Append(
		strconv.Itoa(len(players)),
		fmt.Sprintf("%+.4f", median),
		fmt.Sprintf("%+.4f", best.DuelSwingPerDuel),
		nameOr(names, best.SteamID),
		fmt.Sprintf("%+.4f", best.DuelSwingPerDuel-median),
	)
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

// PrintSwingContext renders the Pass 19 round-context aggregates: what the
// rounds GAVE each player, per side, against the population per-side mean.
// This is the denominator axis for the swing numbers — a star's swing is
// bought with resources and freedom a support never gets.
func PrintSwingContext(w io.Writer, ids []uint64, rows map[uint64]map[string]*storage.PlayerContextRow, pop map[string]*storage.PlayerContextRow, names map[uint64]string) {
	printSection(w, "Round Context (resources & positioning)",
		"GOOD_GUN%=share of alive in-round 16 Hz samples with a rifle or sniper in hand\n"+
			"RIFLE%/SNIPER%=the same, split by class. High SNIPER% = the team feeds the AWP.\n"+
			"PACK_DIST=avg distance (m) to the nearest alive teammate. High on T = lurker;\n"+
			"low = pack player. FIRST_CONTACT=avg seconds into the round of first combat\n"+
			"involvement. DEATH=avg seconds into the round of death (survived rounds excluded).\n"+
			"Rounds counts only measured rounds — demos aggregated before the context\n"+
			"backfill are excluded automatically. '-' = no qualifying rounds.")

	fmtSec := func(v float64) string {
		if v < 0 {
			return "-"
		}
		return fmt.Sprintf("%.1f", v)
	}
	pct := func(part, whole int64) string {
		if whole == 0 {
			return "-"
		}
		return fmt.Sprintf("%.1f", 100*float64(part)/float64(whole))
	}

	table := swingTable(w)
	table.Header("PLAYER", "SIDE", "ROUNDS", "GOOD_GUN%", "RIFLE%", "SNIPER%", "PACK_DIST", "FIRST_CONTACT", "DEATH")
	appendRow := func(label string, r *storage.PlayerContextRow) {
		if r == nil {
			return
		}
		table.Append(
			label, r.Side,
			strconv.Itoa(r.Rounds),
			pct(r.GunRifle+r.GunSniper, r.GunSamples),
			pct(r.GunRifle, r.GunSamples),
			pct(r.GunSniper, r.GunSamples),
			fmtSec(r.PackDistAvgM),
			fmtSec(r.FirstContactSec),
			fmtSec(r.DeathSec),
		)
	}
	for _, id := range ids {
		sides := rows[id]
		if sides == nil {
			continue
		}
		name := nameOr(names, id)
		for _, side := range []string{"CT", "T"} {
			appendRow(name, sides[side])
			name = ""
		}
	}
	for _, side := range []string{"CT", "T"} {
		appendRow("— population —", pop[side])
	}
	table.Render()
}
