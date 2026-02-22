package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/charmbracelet/glamour"
	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/storage"
)

const analyzeSystemPrompt = `You are a Counter-Strike 2 performance analyst. You are given structured data
from a demo-parsing tool and a question from the player.

Rules:
- Answer ONLY from the data provided. Never invent or estimate statistics.
- Always cite specific numbers when making a claim.
- If a metric is flagged in "low_confidence", explicitly note the caveat when citing it.
- If the data is insufficient to answer confidently, say so explicitly.
- Be concise and actionable — focus on what the player can actually improve.
- Avoid generic CS2 advice unless it directly explains a pattern in the data.

Metrics glossary:
- ADR: Avg Damage per Round. Typical range 60–90. <60 is low.
- KAST%: % rounds with Kill/Assist/Survival/Trade. Good: >70%.
- K/D: Kills ÷ deaths. 1.0 is break-even.
- TTK (ms): Your first shot to kill, multi-hit kills only. Lower = faster finishing.
- TTD (ms): Enemy's first shot to your death, multi-hit only. Higher = harder to kill.
- One-tap kills: kills where one bullet was enough; shown as % of total kills.
- Sight deviation (°): Crosshair-to-enemy-head angle at first sight. Lower = better pre-aim.
- Correction (°): Aim adjustment from first-sight to first shot fired. Lower = less flicking.
- Counter-strafe %: % of shots fired while nearly stationary. Higher = better shot discipline.
- Opening K/D: first kill/death of the round — high strategic value.
- Effective flashes: blinded enemy died to your team within 1.5s of your flash.
- AWP dry peek: you died to AWP while initiating the peek (not pre-aimed).
- AWP repeek: died to AWP when enemy re-peeked your position.
- 1vN clutch W/A: won/attempted clutch situations when last alive vs N enemies.
- FHHS: first-hit headshot rate — % of winning duels where the first bullet hit the head.
  confidence tags: high=30+ duels, medium=10–29, low=<10 (treat low with caution).
- buy_profile: your avg kills/damage/KAST split by round economy (full/force/half/eco).`

var (
	analyzeModel  string
	analyzeAPIKey string

	analyzePlayerMap   string
	analyzePlayerSince string
	analyzePlayerLast  int
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "AI-powered grounded analysis (requires ANTHROPIC_API_KEY)",
}

var analyzePlayerCmd = &cobra.Command{
	Use:   "player <steamid64> <question>",
	Short: "Analyze a player's aggregate stats with AI",
	Args:  cobra.ExactArgs(2),
	RunE:  runAnalyzePlayer,
}

var analyzeMatchCmd = &cobra.Command{
	Use:   "match <hash-prefix> <question>",
	Short: "Analyze a single match with AI",
	Args:  cobra.ExactArgs(2),
	RunE:  runAnalyzeMatch,
}

func init() {
	analyzeCmd.PersistentFlags().StringVar(&analyzeModel, "model", "claude-haiku-4-5-20251001", "Anthropic model to use")
	analyzeCmd.PersistentFlags().StringVar(&analyzeAPIKey, "api-key", "", "Anthropic API key (falls back to $ANTHROPIC_API_KEY)")

	analyzePlayerCmd.Flags().StringVar(&analyzePlayerMap, "map", "", "filter to a specific map (e.g. nuke, de_nuke)")
	analyzePlayerCmd.Flags().StringVar(&analyzePlayerSince, "since", "", "filter to matches on or after this date (YYYY-MM-DD)")
	analyzePlayerCmd.Flags().IntVar(&analyzePlayerLast, "last", 0, "only use the N most recent matches")

	analyzeCmd.AddCommand(analyzePlayerCmd)
	analyzeCmd.AddCommand(analyzeMatchCmd)
}

func runAnalyzePlayer(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid SteamID64 %q: %w", args[0], err)
	}
	question := args[1]

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	stats, err := db.GetAllPlayerMatchStats(id)
	if err != nil {
		return fmt.Errorf("query stats: %w", err)
	}
	stats = filterStats(stats, analyzePlayerMap, analyzePlayerSince, analyzePlayerLast)
	if len(stats) == 0 {
		return fmt.Errorf("no data found for SteamID64 %d (after filters)", id)
	}

	// Build a set of filtered demo hashes for downstream filtering.
	keep := make(map[string]struct{}, len(stats))
	for _, s := range stats {
		keep[s.DemoHash] = struct{}{}
	}

	agg := buildAggregate(stats)
	mapSideAggs := buildMapSideAggregates(stats)

	// Duel segments — load all, filter to kept hashes, then merge.
	allSegs, err := db.GetAllPlayerDuelSegments(id)
	if err != nil {
		return fmt.Errorf("query duel segments: %w", err)
	}
	var filteredSegs []model.PlayerDuelSegment
	for _, seg := range allSegs {
		if _, ok := keep[seg.DemoHash]; ok {
			filteredSegs = append(filteredSegs, seg)
		}
	}
	mergedSegs := mergeSegments(id, filteredSegs)

	// Weapon stats — load per-demo and aggregate across filtered demos.
	var allWeaponStats []model.PlayerWeaponStats
	for _, s := range stats {
		ws, err := db.GetPlayerWeaponStats(s.DemoHash)
		if err != nil {
			return fmt.Errorf("query weapon stats for %s: %w", s.DemoHash, err)
		}
		for _, w := range ws {
			if w.SteamID == id {
				allWeaponStats = append(allWeaponStats, w)
			}
		}
	}

	// Round stats — load per-demo for buy profile.
	var allRoundStats []model.PlayerRoundStats
	for _, s := range stats {
		rs, err := db.GetPlayerRoundStats(s.DemoHash, id)
		if err != nil {
			return fmt.Errorf("query round stats for %s: %w", s.DemoHash, err)
		}
		allRoundStats = append(allRoundStats, rs...)
	}

	// Aggregate clutch stats across filtered matches.
	clutchByMatch, err := db.GetPlayerClutchStatsByMatch(id)
	if err != nil {
		return fmt.Errorf("query clutch: %w", err)
	}
	var aggClutch model.PlayerClutchMatchStats
	aggClutch.SteamID = id
	for hash, c := range clutchByMatch {
		if _, ok := keep[hash]; !ok {
			continue
		}
		for i := 1; i <= 5; i++ {
			aggClutch.Attempts[i] += c.Attempts[i]
			aggClutch.Wins[i] += c.Wins[i]
		}
	}

	filters := map[string]interface{}{
		"map":   analyzePlayerMap,
		"since": analyzePlayerSince,
		"last":  analyzePlayerLast,
	}
	contextJSON, err := buildPlayerContext(agg, mapSideAggs, &aggClutch, filters, stats, mergedSegs, allWeaponStats, allRoundStats)
	if err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	return callAnthropic(cmd.Context(), analyzeAPIKey, analyzeModel, contextJSON, question)
}

func runAnalyzeMatch(cmd *cobra.Command, args []string) error {
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	demo, err := db.GetDemoByPrefix(args[0])
	if err != nil {
		return fmt.Errorf("find demo: %w", err)
	}
	question := args[1]

	stats, err := db.GetPlayerMatchStats(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("query match stats: %w", err)
	}

	clutch, err := db.GetClutchStatsByDemo(demo.DemoHash)
	if err != nil {
		return fmt.Errorf("query clutch: %w", err)
	}

	contextJSON, err := buildMatchContext(demo, stats, clutch)
	if err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	return callAnthropic(cmd.Context(), analyzeAPIKey, analyzeModel, contextJSON, question)
}

// buildPlayerContext serialises all available player data into compact JSON.
func buildPlayerContext(
	agg model.PlayerAggregate,
	mapSideAggs []model.PlayerMapSideAggregate,
	clutch *model.PlayerClutchMatchStats,
	filters map[string]interface{},
	stats []model.PlayerMatchStats,
	mergedSegs []model.PlayerDuelSegment,
	weaponStats []model.PlayerWeaponStats,
	roundStats []model.PlayerRoundStats,
) (string, error) {
	type mapSideEntry struct {
		Map     string  `json:"map"`
		Side    string  `json:"side"`
		Matches int     `json:"matches"`
		KD      float64 `json:"kd"`
		ADR     float64 `json:"adr"`
		KASTPct float64 `json:"kast_pct"`
	}
	mapSide := make([]mapSideEntry, 0, len(mapSideAggs))
	for _, ms := range mapSideAggs {
		mapSide = append(mapSide, mapSideEntry{
			Map:     ms.MapName,
			Side:    ms.Side,
			Matches: ms.Matches,
			KD:      round2(ms.KDRatio()),
			ADR:     round2(ms.ADR()),
			KASTPct: round2(ms.KASTPct()),
		})
	}

	oneTapPct := 0.0
	if agg.Kills > 0 {
		oneTapPct = round2(float64(agg.OneTapKills) / float64(agg.Kills) * 100)
	}

	aimSection := map[string]interface{}{
		"median_ttk_ms":       round2(agg.AvgTTKMs),
		"median_ttd_ms":       round2(agg.AvgTTDMs),
		"one_tap_kills":       agg.OneTapKills,
		"one_tap_pct":         oneTapPct,
		"median_correction_deg": round2(agg.AvgCorrectionDeg),
		"counter_strafe_pct":  round2(agg.AvgCounterStrafePct),
	}
	if agg.AvgTTKMs == 0 {
		aimSection["median_ttk_ms"] = nil
	}
	if agg.AvgTTDMs == 0 {
		aimSection["median_ttd_ms"] = nil
	}
	if agg.AvgCorrectionDeg == 0 {
		aimSection["median_correction_deg"] = nil
	}

	doc := map[string]interface{}{
		"subject":          "player",
		"player":           agg.Name,
		"matches_analyzed": agg.Matches,
		"filters":          filters,
		"overview": map[string]interface{}{
			"role":     agg.Role,
			"kd":       round2(agg.KDRatio()),
			"hs_pct":   round2(agg.HSPercent()),
			"adr":      round2(agg.ADR()),
			"kast_pct": round2(agg.KASTPct()),
			"kills":    agg.Kills,
			"assists":  agg.Assists,
			"deaths":   agg.Deaths,
			"rounds":   agg.RoundsPlayed,
		},
		"opening": map[string]interface{}{
			"kills":  agg.OpeningKills,
			"deaths": agg.OpeningDeaths,
		},
		"trades": map[string]interface{}{
			"kills":  agg.TradeKills,
			"deaths": agg.TradeDeaths,
		},
		"utility": map[string]interface{}{
			"flash_assists":     agg.FlashAssists,
			"effective_flashes": agg.EffectiveFlashes,
			"utility_damage":    sumUtilityDamage(stats),
			"unused_utility":    sumUnusedUtility(stats),
		},
		"aim": aimSection,
		"awp_deaths": map[string]interface{}{
			"total":    agg.AWPDeaths,
			"dry":      agg.AWPDeathsDry,
			"repeek":   agg.AWPDeathsRePeek,
			"isolated": agg.AWPDeathsIsolated,
		},
		"clutch":      clutchSummary(clutch),
		"map_side":    mapSide,
		"trend":       buildTrendContext(stats),
		"fhhs":        buildFHHSContext(mergedSegs),
		"weapons":     buildWeaponContext(weaponStats),
		"buy_profile":  buildBuyProfile(roundStats),
		"post_plant":   buildPostPlantProfile(roundStats),
		"low_confidence": buildLowConfidence(agg, clutch, mergedSegs),
	}

	b, err := json.Marshal(doc)
	return string(b), err
}

// buildTrendContext produces a chronological per-match summary for trend analysis.
func buildTrendContext(stats []model.PlayerMatchStats) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(stats))
	for _, s := range stats {
		entry := map[string]interface{}{
			"date":     s.MatchDate,
			"map":      strings.TrimPrefix(s.MapName, "de_"),
			"side":     s.Team.String(),
			"kd":       round2(s.KDRatio()),
			"adr":      round2(s.ADR()),
			"kast_pct": round2(s.KASTPct()),
			"kills":    s.Kills,
			"deaths":   s.Deaths,
			"opening_k": s.OpeningKills,
			"opening_d": s.OpeningDeaths,
		}
		if s.MedianTTKMs > 0 {
			entry["ttk_ms"] = round2(s.MedianTTKMs)
		}
		if s.MedianTTDMs > 0 {
			entry["ttd_ms"] = round2(s.MedianTTDMs)
		}
		if s.CounterStrafePercent > 0 {
			entry["cs_pct"] = round2(s.CounterStrafePercent)
		}
		out = append(out, entry)
	}
	return out
}

// buildFHHSContext converts merged duel segments into a context-friendly slice,
// annotating each with a confidence level based on duel count.
func buildFHHSContext(segs []model.PlayerDuelSegment) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(segs))
	for _, seg := range segs {
		fhhsPct := 0.0
		if seg.FirstHitCount > 0 {
			fhhsPct = round2(float64(seg.FirstHitHSCount) / float64(seg.FirstHitCount) * 100)
		}
		confidence := "high"
		if seg.DuelCount < 10 {
			confidence = "low"
		} else if seg.DuelCount < 30 {
			confidence = "medium"
		}
		entry := map[string]interface{}{
			"weapon":     seg.WeaponBucket,
			"distance":   seg.DistanceBin,
			"duels":      seg.DuelCount,
			"fhhs_pct":   fhhsPct,
			"confidence": confidence,
		}
		if seg.MedianSightDeg > 0 {
			entry["sight_deg"] = round2(seg.MedianSightDeg)
		}
		if seg.MedianCorrDeg > 0 {
			entry["correction_deg"] = round2(seg.MedianCorrDeg)
		}
		out = append(out, entry)
	}
	return out
}

// buildWeaponContext aggregates weapon stats across all filtered matches.
func buildWeaponContext(stats []model.PlayerWeaponStats) []map[string]interface{} {
	type accum struct {
		kills, hsKills, assists, deaths, damage, hits int
	}
	m := make(map[string]*accum)
	for _, w := range stats {
		if m[w.Weapon] == nil {
			m[w.Weapon] = &accum{}
		}
		a := m[w.Weapon]
		a.kills += w.Kills
		a.hsKills += w.HeadshotKills
		a.assists += w.Assists
		a.deaths += w.Deaths
		a.damage += w.Damage
		a.hits += w.Hits
	}

	// Sort by kills descending.
	type entry struct {
		weapon string
		a      *accum
	}
	entries := make([]entry, 0, len(m))
	for weapon, a := range m {
		if a.kills > 0 || a.damage > 0 {
			entries = append(entries, entry{weapon, a})
		}
	}
	// Insertion-sort by kills desc (small slice, good enough).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].a.kills > entries[j-1].a.kills; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	out := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		hsPct := 0.0
		if e.a.kills > 0 {
			hsPct = round2(float64(e.a.hsKills) / float64(e.a.kills) * 100)
		}
		avgDmg := 0.0
		if e.a.hits > 0 {
			avgDmg = round2(float64(e.a.damage) / float64(e.a.hits))
		}
		out = append(out, map[string]interface{}{
			"weapon":          e.weapon,
			"kills":           e.a.kills,
			"hs_pct":          hsPct,
			"assists":         e.a.assists,
			"deaths":          e.a.deaths,
			"damage":          e.a.damage,
			"hits":            e.a.hits,
			"avg_dmg_per_hit": avgDmg,
		})
	}
	return out
}

// buildBuyProfile summarises performance by buy type (full/force/half/eco).
func buildBuyProfile(rounds []model.PlayerRoundStats) map[string]interface{} {
	type accum struct {
		count, kills, damage, kastCount int
	}
	m := map[string]*accum{
		"full":  {},
		"force": {},
		"half":  {},
		"eco":   {},
	}
	for _, r := range rounds {
		a := m[r.BuyType]
		if a == nil {
			continue
		}
		a.count++
		a.kills += r.Kills
		a.damage += r.Damage
		if r.KASTEarned {
			a.kastCount++
		}
	}
	out := make(map[string]interface{}, 4)
	for buyType, a := range m {
		if a.count == 0 {
			continue
		}
		out[buyType] = map[string]interface{}{
			"rounds":     a.count,
			"avg_kills":  round2(float64(a.kills) / float64(a.count)),
			"avg_damage": round2(float64(a.damage) / float64(a.count)),
			"kast_pct":   round2(float64(a.kastCount) / float64(a.count) * 100),
		}
	}
	return out
}

// sumUtilityDamage sums UtilityDamage across all filtered matches.
func sumUtilityDamage(stats []model.PlayerMatchStats) int {
	total := 0
	for _, s := range stats {
		total += s.UtilityDamage
	}
	return total
}

// sumUnusedUtility sums UnusedUtility across all filtered matches.
func sumUnusedUtility(stats []model.PlayerMatchStats) int {
	total := 0
	for _, s := range stats {
		total += s.UnusedUtility
	}
	return total
}

// buildPostPlantProfile summarises performance in post-plant vs. non-post-plant rounds.
func buildPostPlantProfile(rounds []model.PlayerRoundStats) map[string]interface{} {
	type accum struct {
		count, kills, damage, kastCount int
	}
	var pp, nonPP accum
	for _, r := range rounds {
		a := &nonPP
		if r.IsPostPlant {
			a = &pp
		}
		a.count++
		a.kills += r.Kills
		a.damage += r.Damage
		if r.KASTEarned {
			a.kastCount++
		}
	}
	summarise := func(a accum) map[string]interface{} {
		if a.count == 0 {
			return nil
		}
		return map[string]interface{}{
			"rounds":     a.count,
			"avg_kills":  round2(float64(a.kills) / float64(a.count)),
			"avg_damage": round2(float64(a.damage) / float64(a.count)),
			"kast_pct":   round2(float64(a.kastCount) / float64(a.count) * 100),
		}
	}
	return map[string]interface{}{
		"post_plant":     summarise(pp),
		"non_post_plant": summarise(nonPP),
	}
}

// buildLowConfidence returns a list of human-readable strings describing metrics
// that have too few samples to be reliably interpreted.
func buildLowConfidence(agg model.PlayerAggregate, clutch *model.PlayerClutchMatchStats, segs []model.PlayerDuelSegment) []string {
	var warnings []string

	if clutch != nil {
		for i := 1; i <= 5; i++ {
			if a := clutch.Attempts[i]; a > 0 && a < 5 {
				warnings = append(warnings, fmt.Sprintf("clutch_1v%d: only %d attempt(s) — win rate unreliable", i, a))
			}
		}
	}

	if agg.AWPDeaths > 0 && agg.AWPDeaths < 10 {
		warnings = append(warnings, fmt.Sprintf("awp_deaths: only %d total — dry/repeek/isolated %% unreliable", agg.AWPDeaths))
	}

	if agg.AvgTTKMs == 0 {
		warnings = append(warnings, "median_ttk_ms: no multi-hit kill data available")
	}
	if agg.AvgTTDMs == 0 {
		warnings = append(warnings, "median_ttd_ms: no multi-hit death data available")
	}
	if agg.AvgCorrectionDeg == 0 {
		warnings = append(warnings, "median_correction_deg: no first-sight duel data available")
	}

	for _, seg := range segs {
		if seg.DuelCount < 10 {
			warnings = append(warnings, fmt.Sprintf("fhhs_%s_%s: only %d duel(s) — treat with caution",
				strings.ToLower(seg.WeaponBucket), strings.ReplaceAll(seg.DistanceBin, " ", "_"), seg.DuelCount))
		}
	}

	return warnings
}

// buildMatchContext serialises a single match into compact JSON.
func buildMatchContext(demo *model.MatchSummary, stats []model.PlayerMatchStats, clutch map[uint64]*model.PlayerClutchMatchStats) (string, error) {
	type playerEntry struct {
		Name     string            `json:"name"`
		Role     string            `json:"role"`
		KD       float64           `json:"kd"`
		ADR      float64           `json:"adr"`
		KASTPct  float64           `json:"kast_pct"`
		Kills    int               `json:"kills"`
		Assists  int               `json:"assists"`
		Deaths   int               `json:"deaths"`
		HSPct    float64           `json:"hs_pct"`
		OpeningK int               `json:"opening_k"`
		OpeningD int               `json:"opening_d"`
		TradeK   int               `json:"trade_k"`
		TradeD   int               `json:"trade_d"`
		Clutch   map[string]string `json:"clutch"`
	}

	players := make([]playerEntry, 0, len(stats))
	for _, s := range stats {
		p := playerEntry{
			Name:     s.Name,
			Role:     s.Role,
			KD:       round2(s.KDRatio()),
			ADR:      round2(s.ADR()),
			KASTPct:  round2(s.KASTPct()),
			Kills:    s.Kills,
			Assists:  s.Assists,
			Deaths:   s.Deaths,
			HSPct:    round2(s.HSPercent()),
			OpeningK: s.OpeningKills,
			OpeningD: s.OpeningDeaths,
			TradeK:   s.TradeKills,
			TradeD:   s.TradeDeaths,
			Clutch:   clutchSummary(clutch[s.SteamID]),
		}
		if p.Role == "" {
			p.Role = "Rifler"
		}
		players = append(players, p)
	}

	score := fmt.Sprintf("%d-%d", demo.CTScore, demo.TScore)
	doc := map[string]interface{}{
		"subject": "match",
		"map":     demo.MapName,
		"date":    demo.MatchDate,
		"score":   score,
		"type":    demo.MatchType,
		"players": players,
	}

	b, err := json.Marshal(doc)
	return string(b), err
}

// clutchSummary builds a map of "1v1"…"1v5" + "total" clutch strings.
// Returns "—" for any count where attempts == 0.
func clutchSummary(c *model.PlayerClutchMatchStats) map[string]string {
	out := make(map[string]string, 6)
	if c == nil {
		for i := 1; i <= 5; i++ {
			out[fmt.Sprintf("1v%d", i)] = "—"
		}
		out["total"] = "—"
		return out
	}
	totalW, totalA := 0, 0
	for i := 1; i <= 5; i++ {
		w, a := c.Wins[i], c.Attempts[i]
		totalW += w
		totalA += a
		out[fmt.Sprintf("1v%d", i)] = clutchStr(w, a)
	}
	out["total"] = clutchStr(totalW, totalA)
	return out
}

// clutchStr formats wins/attempts as "W/A (P%)" or "—".
func clutchStr(wins, attempts int) string {
	if attempts == 0 {
		return "—"
	}
	pct := float64(wins) / float64(attempts) * 100
	return fmt.Sprintf("%d/%d (%.0f%%)", wins, attempts, pct)
}

// round2 rounds a float64 to 2 decimal places.
func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

// callAnthropic streams a response from the Anthropic API and prints it to stdout.
func callAnthropic(ctx context.Context, apiKey, modelID, dataJSON, question string) error {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("no API key: set ANTHROPIC_API_KEY or use --api-key")
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	userMsg := fmt.Sprintf("DATA:\n%s\n\nQUESTION: %s", dataJSON, question)

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(modelID),
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: analyzeSystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userMsg)),
		},
	})

	// Buffer the full response before rendering so glamour can process the
	// complete markdown document (it needs the full text for proper formatting).
	fmt.Fprintln(os.Stdout, "\n─── AI Analysis ──────────────────────────────────────")
	fmt.Fprintln(os.Stdout, "  Waiting for response...")

	var buf strings.Builder
	for stream.Next() {
		evt := stream.Current()
		if evt.Type == "content_block_delta" {
			delta := evt.AsContentBlockDelta()
			if delta.Delta.Type == "text_delta" {
				buf.WriteString(delta.Delta.AsTextDelta().Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "401") || strings.Contains(errStr, "authentication") {
			return fmt.Errorf("API authentication failed — check your API key")
		}
		return fmt.Errorf("streaming error: %w", err)
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		// Fallback to plain text if glamour fails to initialise.
		fmt.Fprintln(os.Stdout, buf.String())
		fmt.Fprintln(os.Stdout, "──────────────────────────────────────────────────────")
		return nil
	}

	rendered, err := renderer.Render(buf.String())
	if err != nil {
		fmt.Fprintln(os.Stdout, buf.String())
	} else {
		fmt.Fprint(os.Stdout, rendered)
	}
	fmt.Fprintln(os.Stdout, "──────────────────────────────────────────────────────")
	return nil
}
