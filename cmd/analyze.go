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
	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/storage"
)

const analyzeSystemPrompt = `You are a Counter-Strike 2 performance analyst. You are given structured data
from a demo-parsing tool and a question from the player.

Rules:
- Answer ONLY from the data provided. Never invent or estimate statistics.
- Always cite specific numbers when making a claim.
- If the data is insufficient to answer confidently, say so explicitly.
- Be concise and actionable — focus on what the player can actually improve.
- Avoid generic CS2 advice unless it directly explains a pattern in the data.

Metrics glossary:
- ADR: Avg Damage per Round. Typical range 60–90. <60 is low.
- KAST%: % rounds with Kill/Assist/Survival/Trade. Good: >70%.
- K/D: Kills ÷ deaths. 1.0 is break-even.
- TTK (ms): Your first shot to kill. Lower = faster finishing.
- TTD (ms): Enemy's first shot to your death. Higher = harder to kill.
- One-tap kills: kills in a single hit (one bullet to kill).
- Crosshair placement (°): Median deviation at first sight of enemy. Lower = better pre-aim.
- Correction (°): Aim adjustment needed before first hit. Lower = less flicking.
- Counter-strafe %: % of kills taken while nearly stationary. Higher = better shot discipline.
- Opening K/D: first kill/death of the round — high strategic value.
- Effective flashes: blinded enemy died to your team within 1.5s of your flash.
- AWP dry peek: you died to AWP while initiating the peek (not pre-aimed).
- AWP repeek: died to AWP when enemy re-peeked your position.
- 1vN clutch W/A: won/attempted clutch situations when last alive vs N enemies.`

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

	agg := buildAggregate(stats)
	mapSideAggs := buildMapSideAggregates(stats)

	// Aggregate clutch stats across filtered matches.
	clutchByMatch, err := db.GetPlayerClutchStatsByMatch(id)
	if err != nil {
		return fmt.Errorf("query clutch: %w", err)
	}
	keep := make(map[string]struct{}, len(stats))
	for _, s := range stats {
		keep[s.DemoHash] = struct{}{}
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
	contextJSON, err := buildPlayerContext(agg, mapSideAggs, &aggClutch, filters)
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

// buildPlayerContext serialises aggregated player data into compact JSON.
func buildPlayerContext(agg model.PlayerAggregate, mapSideAggs []model.PlayerMapSideAggregate, clutch *model.PlayerClutchMatchStats, filters map[string]interface{}) (string, error) {
	type mapSideEntry struct {
		Map      string  `json:"map"`
		Side     string  `json:"side"`
		Matches  int     `json:"matches"`
		KD       float64 `json:"kd"`
		ADR      float64 `json:"adr"`
		KASTPct  float64 `json:"kast_pct"`
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
		"flash": map[string]interface{}{
			"assists":   agg.FlashAssists,
			"effective": agg.EffectiveFlashes,
		},
		"aim": map[string]interface{}{
			"median_ttk_ms":        round2(agg.AvgTTKMs),
			"median_ttd_ms":        round2(agg.AvgTTDMs),
			"one_tap_kills":        agg.OneTapKills,
			"crosshair_median_deg": round2(agg.AvgCorrectionDeg),
			"median_correction_deg": round2(agg.AvgCorrectionDeg),
			"counter_strafe_pct":   round2(agg.AvgCounterStrafePct),
		},
		"awp_deaths": map[string]interface{}{
			"total":    agg.AWPDeaths,
			"dry":      agg.AWPDeathsDry,
			"repeek":   agg.AWPDeathsRePeek,
			"isolated": agg.AWPDeathsIsolated,
		},
		"clutch":   clutchSummary(clutch),
		"map_side": mapSide,
	}

	b, err := json.Marshal(doc)
	return string(b), err
}

// buildMatchContext serialises a single match into compact JSON.
func buildMatchContext(demo *model.MatchSummary, stats []model.PlayerMatchStats, clutch map[uint64]*model.PlayerClutchMatchStats) (string, error) {
	type playerEntry struct {
		Name      string  `json:"name"`
		Role      string  `json:"role"`
		KD        float64 `json:"kd"`
		ADR       float64 `json:"adr"`
		KASTPct   float64 `json:"kast_pct"`
		Kills     int     `json:"kills"`
		Assists   int     `json:"assists"`
		Deaths    int     `json:"deaths"`
		HSPct     float64 `json:"hs_pct"`
		OpeningK  int     `json:"opening_k"`
		OpeningD  int     `json:"opening_d"`
		TradeK    int     `json:"trade_k"`
		TradeD    int     `json:"trade_d"`
		Clutch    map[string]string `json:"clutch"`
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
	// Use integer arithmetic to avoid floating-point drift.
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

	fmt.Fprintln(os.Stdout, "\n─── AI Analysis ─────────────────────────────────────")

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

	for stream.Next() {
		evt := stream.Current()
		if evt.Type == "content_block_delta" {
			delta := evt.AsContentBlockDelta()
			if delta.Delta.Type == "text_delta" {
				fmt.Fprint(os.Stdout, delta.Delta.AsTextDelta().Text)
			}
		}
	}
	fmt.Fprintln(os.Stdout, "\n─────────────────────────────────────────────────────")

	if err := stream.Err(); err != nil {
		// Provide a cleaner error message for common API errors.
		errStr := err.Error()
		if strings.Contains(errStr, "401") || strings.Contains(errStr, "authentication") {
			return fmt.Errorf("API authentication failed — check your API key")
		}
		return fmt.Errorf("streaming error: %w", err)
	}
	return nil
}
