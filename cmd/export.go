package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	exportTeam    string
	exportPlayers string
	exportRoster  string
	exportSince   int
	exportQuorum  int
	exportOut     string
)

// rosterFile is the schema for --roster JSON files.
type rosterFile struct {
	Team    string   `json:"team"`
	Players []string `json:"players"`
}

// simbo3TeamStats is the top-level JSON schema expected by cs2-pro-match-simulator.
type simbo3TeamStats struct {
	Team              string                    `json:"team"`
	PlayersRating2_3m []float64                 `json:"players_rating2_3m"`
	Maps              map[string]simbo3MapStats `json:"maps"`
}

// simbo3MapStats is the per-map block within the simbo3 team JSON.
type simbo3MapStats struct {
	MapWinPct     float64 `json:"map_win_pct"`
	CTRoundWinPct float64 `json:"ct_round_win_pct"`
	TRoundWinPct  float64 `json:"t_round_win_pct"`
	Matches3m     int     `json:"matches_3m"`
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export team stats as a simbo3-compatible JSON file",
	Long: `Queries the metrics database for a team roster and produces a JSON file
in the format expected by cs2-pro-match-simulator (simbo3).

Specify the roster via --players (comma-separated SteamID64s) or
--roster (path to a JSON file). If both are provided, --players takes precedence.
If --team is set alongside --roster, it overrides the name from the roster file.

Player ratings are estimated using the community approximation of HLTV Rating 2.0:
  Rating ≈ 0.0073*KAST% + 0.3591*KPR - 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
  Impact  = 2.13*KPR + 0.42*APR - 0.41

Example:
  csmetrics export --team "NaVi" --players "76561198034202275,76561197992321696,..." --out navi.json
  csmetrics export --roster navi.json --out navi-simbo3.json`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportTeam, "team", "", "team name for the output JSON")
	exportCmd.Flags().StringVar(&exportPlayers, "players", "", "comma-separated SteamID64s")
	exportCmd.Flags().StringVar(&exportRoster, "roster", "", `roster JSON file: {"team":"...","players":["...",...]}`)
	exportCmd.Flags().IntVar(&exportSince, "since", 90, "look-back window in days")
	exportCmd.Flags().IntVar(&exportQuorum, "quorum", 3, "min roster players per demo to include it")
	exportCmd.Flags().StringVar(&exportOut, "out", "", "output file path (default: stdout)")
}

func runExport(_ *cobra.Command, _ []string) error {
	teamName, steamIDs, err := resolveRoster()
	if err != nil {
		return err
	}
	if len(steamIDs) == 0 {
		return fmt.Errorf("no players specified: use --players or --roster")
	}
	if teamName == "" {
		return fmt.Errorf("no team name specified: use --team or include it in the roster file")
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	since := time.Now().AddDate(0, 0, -exportSince)
	fmt.Fprintf(os.Stderr, "Querying demos for %d players since %s (quorum=%d)...\n",
		len(steamIDs), since.Format("2006-01-02"), exportQuorum)

	demos, err := db.QualifyingDemos(steamIDs, since, exportQuorum)
	if err != nil {
		return fmt.Errorf("query qualifying demos: %w", err)
	}
	if len(demos) == 0 {
		return fmt.Errorf("no qualifying demos found in the last %d days with quorum=%d", exportSince, exportQuorum)
	}
	fmt.Fprintf(os.Stderr, "Found %d qualifying demos\n", len(demos))

	// Group demo hashes by map name and collect all hashes for the rating query.
	// Normalize map names from CS2 format (de_mirage) to simbo3 format (Mirage).
	byMap := make(map[string][]string)
	allHashes := make([]string, 0, len(demos))
	for _, d := range demos {
		name := normalizeMapName(d.MapName)
		byMap[name] = append(byMap[name], d.Hash)
		allHashes = append(allHashes, d.Hash)
	}

	// Compute per-map stats.
	maps := make(map[string]simbo3MapStats, len(byMap))
	for mapName, hashes := range byMap {
		outcomes, err := db.MapWinOutcomes(steamIDs, hashes)
		if err != nil {
			return fmt.Errorf("map win outcomes for %s: %w", mapName, err)
		}

		var winSum float64
		for _, o := range outcomes {
			if o.RoundsPlayed == 0 {
				continue
			}
			switch {
			case o.RoundsWon*2 > o.RoundsPlayed:
				winSum += 1.0
			case o.RoundsWon*2 == o.RoundsPlayed:
				winSum += 0.5 // draw
			}
		}
		n := len(outcomes)
		var mapWinPct float64
		if n > 0 {
			mapWinPct = winSum / float64(n)
		}

		sides, err := db.RoundSideStats(steamIDs, hashes)
		if err != nil {
			return fmt.Errorf("round side stats for %s: %w", mapName, err)
		}
		ctPct, tPct := 0.50, 0.50
		if sides.CTTotal > 0 {
			ctPct = float64(sides.CTWins) / float64(sides.CTTotal)
		} else {
			fmt.Fprintf(os.Stderr, "warn: no CT rounds found for %s — using prior 0.50\n", mapName)
		}
		if sides.TTotal > 0 {
			tPct = float64(sides.TWins) / float64(sides.TTotal)
		} else {
			fmt.Fprintf(os.Stderr, "warn: no T rounds found for %s — using prior 0.50\n", mapName)
		}

		maps[mapName] = simbo3MapStats{
			MapWinPct:     roundTo2dp(mapWinPct),
			CTRoundWinPct: roundTo2dp(ctPct),
			TRoundWinPct:  roundTo2dp(tPct),
			Matches3m:     n,
		}
		fmt.Fprintf(os.Stderr, "  %-12s  %2d matches  win=%.2f  CT=%.2f  T=%.2f\n",
			mapName, n, mapWinPct, ctPct, tPct)
	}

	// Compute HLTV Rating 2.0 proxies for the top 5 players by activity.
	totals, err := db.RosterMatchTotals(steamIDs, allHashes)
	if err != nil {
		return fmt.Errorf("roster match totals: %w", err)
	}
	ratings := buildRatings(totals)

	out := simbo3TeamStats{
		Team:              teamName,
		PlayersRating2_3m: ratings,
		Maps:              maps,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}

	if exportOut == "" {
		fmt.Println(string(data))
		return nil
	}
	if err := os.WriteFile(exportOut, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write %s: %w", exportOut, err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", exportOut)
	return nil
}

// resolveRoster returns the team name and SteamID list from flags.
// --players takes precedence over --roster; --team always overrides the roster file name.
func resolveRoster() (teamName string, steamIDs []string, err error) {
	if exportPlayers != "" {
		for _, raw := range strings.Split(exportPlayers, ",") {
			if id := strings.TrimSpace(raw); id != "" {
				steamIDs = append(steamIDs, id)
			}
		}
		return exportTeam, steamIDs, nil
	}
	if exportRoster != "" {
		data, readErr := os.ReadFile(exportRoster)
		if readErr != nil {
			return "", nil, fmt.Errorf("read roster file: %w", readErr)
		}
		var rf rosterFile
		if jsonErr := json.Unmarshal(data, &rf); jsonErr != nil {
			return "", nil, fmt.Errorf("parse roster file: %w", jsonErr)
		}
		name := rf.Team
		if exportTeam != "" {
			name = exportTeam
		}
		return name, rf.Players, nil
	}
	return exportTeam, nil, nil
}

// buildRatings computes the HLTV Rating 2.0 community proxy for up to 5 players.
// totals must be sorted by rounds_played DESC (guaranteed by RosterMatchTotals).
// Slots with no matching player are padded with 1.00 (neutral prior).
func buildRatings(totals []storage.PlayerTotals) []float64 {
	top := totals
	if len(top) > 5 {
		top = top[:5]
	}

	ratings := make([]float64, 5)
	for i := range ratings {
		ratings[i] = 1.00
	}

	for i, p := range top {
		if p.RoundsPlayed == 0 {
			continue
		}
		kpr := float64(p.Kills) / float64(p.RoundsPlayed)
		dpr := float64(p.Deaths) / float64(p.RoundsPlayed)
		apr := float64(p.Assists) / float64(p.RoundsPlayed)
		kast := 100.0 * float64(p.KastRounds) / float64(p.RoundsPlayed)
		adr := float64(p.TotalDamage) / float64(p.RoundsPlayed)
		impact := 2.13*kpr + 0.42*apr - 0.41
		r := 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impact + 0.0032*adr + 0.1587
		ratings[i] = roundTo2dp(r)
		fmt.Fprintf(os.Stderr, "  %-20s  %3d rounds  KPR=%.2f DPR=%.2f KAST=%.0f%% ADR=%.1f  → rating %.2f\n",
			p.Name, p.RoundsPlayed, kpr, dpr, kast, adr, r)
	}

	if len(top) < 5 {
		fmt.Fprintf(os.Stderr, "warn: only %d player(s) found, padding remaining %d slot(s) with 1.00\n",
			len(top), 5-len(top))
	}

	// Highest-rated player first, matching HLTV's typical display order.
	sort.Slice(ratings, func(i, j int) bool { return ratings[i] > ratings[j] })
	return ratings
}

func roundTo2dp(v float64) float64 {
	return math.Round(v*100) / 100
}

// normalizeMapName converts CS2 map identifiers to the title-case names used by
// simbo3 (e.g. "de_mirage" → "Mirage", "de_dust2" → "Dust2").
func normalizeMapName(name string) string {
	name = strings.TrimPrefix(name, "de_")
	if len(name) == 0 {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
