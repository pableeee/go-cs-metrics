package cmd

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/faceit"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	fetchPlayer string
	fetchMap    string
	fetchLevel  int
	fetchCount  int
	fetchTier   string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Download and ingest FACEIT baseline demos",
	Long: `Fetches recent matches for a FACEIT player, downloads their demos,
parses them, and stores them with a tier tag for baseline comparisons.

Examples:
  # Your own recent matches tagged as your tier
  csmetrics fetch --player EvilMacri --count 10 --tier faceit-2

  # Level-5 matches on Mirage from a known level-5 player
  csmetrics fetch --player <nickname> --level 5 --map de_mirage --count 10`,
	RunE: runFetch,
}

func init() {
	fetchCmd.Flags().StringVar(&fetchPlayer, "player", "", "FACEIT nickname or Steam ID64 (required)")
	fetchCmd.Flags().StringVar(&fetchMap, "map", "", "only ingest matches on this map (e.g. de_mirage)")
	fetchCmd.Flags().IntVar(&fetchLevel, "level", 0, "only ingest matches at this FACEIT skill level (1–10)")
	fetchCmd.Flags().IntVar(&fetchCount, "count", 10, "number of matches to ingest")
	fetchCmd.Flags().StringVar(&fetchTier, "tier", "", "tier label stored in DB (default: faceit-N if --level set, else 'faceit')")
	_ = fetchCmd.MarkFlagRequired("player")
}

func runFetch(cmd *cobra.Command, args []string) error {
	apiKey, err := loadFaceitAPIKey()
	if err != nil {
		return err
	}

	tier := fetchTier
	if tier == "" {
		if fetchLevel > 0 {
			tier = fmt.Sprintf("faceit-%d", fetchLevel)
		} else {
			tier = "faceit"
		}
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	client := faceit.NewClient(apiKey)

	// Resolve player from nickname or Steam ID64.
	var player *faceit.Player
	if looksLikeSteamID(fetchPlayer) {
		player, err = client.GetPlayerBySteamID(fetchPlayer)
	} else {
		player, err = client.GetPlayerByNickname(fetchPlayer)
	}
	if err != nil {
		return fmt.Errorf("lookup player %q: %w", fetchPlayer, err)
	}
	fmt.Printf("Player: %s  level=%d  ELO=%d  region=%s\n",
		player.Nickname, player.Games.CS2.SkillLevel,
		player.Games.CS2.FaceitELO, player.Games.CS2.Region)

	// Over-fetch history to leave room for map/level filtering.
	histLimit := fetchCount * 5
	if histLimit < 50 {
		histLimit = 50
	}
	history, err := client.GetMatchHistory(player.PlayerID, histLimit)
	if err != nil {
		return fmt.Errorf("match history: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "csmetrics-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ingested := 0
	for _, item := range history {
		if ingested >= fetchCount {
			break
		}
		if item.Status != "FINISHED" {
			continue
		}

		match, err := client.GetMatch(item.MatchID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [skip] %s: %v\n", item.MatchID, err)
			continue
		}

		mapName := match.MapName()
		if fetchMap != "" && mapName != fetchMap {
			continue
		}
		if fetchLevel > 0 && match.SkillLevel != fetchLevel {
			continue
		}
		if len(match.DemoURLs) == 0 {
			fmt.Printf("  [skip] %s: no demo URL\n", item.MatchID)
			continue
		}

		matchDate := time.Unix(match.StartedAt, 0).UTC().Format("2006-01-02")
		fmt.Printf("[%d/%d] %s  map=%-15s  level=%d  date=%s\n",
			ingested+1, fetchCount, item.MatchID, mapName, match.SkillLevel, matchDate)

		demPath, err := downloadAndDecompress(match.DemoURLs[0], tmpDir, item.MatchID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [error] download: %v\n", err)
			continue
		}

		raw, err := parser.ParseDemo(demPath, "FACEIT")
		os.Remove(demPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [error] parse: %v\n", err)
			continue
		}

		exists, err := db.DemoExists(raw.DemoHash)
		if err != nil {
			return err
		}
		if exists {
			fmt.Printf("  already stored\n")
			ingested++
			continue
		}

		matchStats, roundStats, weaponStats, duelSegs, err := aggregator.Aggregate(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [error] aggregate: %v\n", err)
			continue
		}

		ctScore, tScore := computeScore(raw.Rounds)
		summary := model.MatchSummary{
			DemoHash:   raw.DemoHash,
			MapName:    raw.MapName,
			MatchDate:  matchDate,
			MatchType:  "FACEIT",
			Tickrate:   raw.Tickrate,
			CTScore:    ctScore,
			TScore:     tScore,
			Tier:       tier,
			IsBaseline: true,
		}

		if err := db.InsertDemo(summary); err != nil {
			return fmt.Errorf("insert demo: %w", err)
		}
		if err := db.InsertPlayerMatchStats(matchStats); err != nil {
			return fmt.Errorf("insert stats: %w", err)
		}
		if err := db.InsertPlayerRoundStats(roundStats); err != nil {
			return fmt.Errorf("insert round stats: %w", err)
		}
		if err := db.InsertPlayerWeaponStats(weaponStats); err != nil {
			return fmt.Errorf("insert weapon stats: %w", err)
		}
		if err := db.InsertPlayerDuelSegments(duelSegs); err != nil {
			return fmt.Errorf("insert duel segments: %w", err)
		}

		fmt.Printf("  stored: %d players, %d rounds\n", len(matchStats), len(raw.Rounds))
		ingested++
	}

	fmt.Printf("\nDone: %d/%d matches ingested (tier=%q, is_baseline=true)\n",
		ingested, fetchCount, tier)
	return nil
}

// downloadAndDecompress downloads a demo URL (handling optional gzip) to dir.
func downloadAndDecompress(url, dir, matchID string) (string, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	outPath := filepath.Join(dir, matchID+".dem")
	f, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var src io.Reader = resp.Body
	if strings.HasSuffix(url, ".gz") || resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		src = gz
	}

	if _, err := io.Copy(f, src); err != nil {
		os.Remove(outPath)
		return "", fmt.Errorf("write: %w", err)
	}
	return outPath, nil
}

func loadFaceitAPIKey() (string, error) {
	if key := os.Getenv("FACEIT_API_KEY"); key != "" {
		return key, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(home, ".csmetrics", "faceit_api_key"))
	if err != nil {
		return "", fmt.Errorf("FACEIT API key not found: set FACEIT_API_KEY or create ~/.csmetrics/faceit_api_key")
	}
	return strings.TrimSpace(string(data)), nil
}

func looksLikeSteamID(s string) bool {
	if len(s) < 15 {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
