package cmd

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/faceit"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// fetch command flags.
var (
	// fetchPlayer is the FACEIT nickname or Steam ID64 of the target player.
	fetchPlayer string
	// fetchMap restricts ingestion to demos on this map (e.g. "de_mirage").
	fetchMap string
	// fetchLevel restricts ingestion to matches at this FACEIT skill level (1-10).
	fetchLevel int
	// fetchCount is the number of matches to ingest.
	fetchCount int
	// fetchTier is the tier label stored alongside ingested demos.
	fetchTier string
)

// fetchCmd is the cobra command for downloading and ingesting FACEIT baseline demos.
var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Download and ingest FACEIT baseline demos",
	Long: `Fetches recent matches for a FACEIT player, downloads their demos,
parses them, and stores them with a tier tag for baseline comparisons.

Examples:
  # Your own recent matches tagged as your tier
  csmetrics fetch --player <your-nickname> --count 10 --tier faceit-2

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

// runFetch resolves flags and delegates to doFetch for the actual download/ingest loop.
func runFetch(cmd *cobra.Command, args []string) error {
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

	return doFetch(db, fetchPlayer, fetchMap, fetchLevel, fetchCount, tier)
}

// doFetch is the shared implementation for the fetch command.
func doFetch(db *storage.DB, playerQuery, mapFilter string, level, count int, tier string) error {
	apiKey, err := loadFaceitAPIKey()
	if err != nil {
		return err
	}

	client := faceit.NewClient(apiKey)

	// Resolve player from nickname or Steam ID64.
	var fp *faceit.Player
	if looksLikeSteamID(playerQuery) {
		fp, err = client.GetPlayerBySteamID(playerQuery)
	} else {
		fp, err = client.GetPlayerByNickname(playerQuery)
	}
	if err != nil {
		return fmt.Errorf("lookup player %q: %w", playerQuery, err)
	}
	fmt.Printf("Player: %s  level=%d  ELO=%d  region=%s\n",
		fp.Nickname, fp.Games.CS2.SkillLevel,
		fp.Games.CS2.FaceitELO, fp.Games.CS2.Region)

	// Over-fetch history to leave room for map/level filtering.
	histLimit := count * 5
	if histLimit < 50 {
		histLimit = 50
	}
	history, err := client.GetMatchHistory(fp.PlayerID, histLimit)
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
		if ingested >= count {
			break
		}
		if !strings.EqualFold(item.Status, "FINISHED") {
			continue
		}

		match, err := client.GetMatch(item.MatchID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [skip] %s: %v\n", item.MatchID, err)
			continue
		}

		mapName := match.MapName()
		if mapFilter != "" && mapName != mapFilter {
			continue
		}
		if level > 0 && match.SkillLevel != level {
			continue
		}
		if len(match.DemoURLs) == 0 {
			fmt.Printf("  [skip] %s: no demo URL\n", item.MatchID)
			continue
		}

		matchDate := time.Unix(match.StartedAt, 0).UTC().Format("2006-01-02")
		fmt.Printf("[%d/%d] %s  map=%-15s  level=%d  date=%s\n",
			ingested+1, count, item.MatchID, mapName, match.SkillLevel, matchDate)

		demoURL := match.DemoURLs[0]
		if isKnownBrokenCDN(demoURL) {
			dlKey := loadFaceitDownloadsKey()
			if dlKey == "" {
				fmt.Fprintf(os.Stderr, "  [warn] demo CDN URL won't resolve; set FACEIT_DOWNLOADS_KEY or create ~/.csmetrics/faceit_downloads_key\n")
			} else {
				resolved, rerr := resolveDemoURL(demoURL, dlKey)
				if rerr != nil {
					fmt.Fprintf(os.Stderr, "  [warn] URL resolution failed: %v\n", rerr)
				} else {
					demoURL = resolved
				}
			}
		}

		demPath, err := downloadAndDecompress(demoURL, tmpDir, item.MatchID)
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

		if err := db.InsertDemo(summary, ""); err != nil {
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
		ingested, count, tier)
	return nil
}

// downloadAndDecompress downloads a demo URL (handling gzip or zstd) to dir.
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
	switch {
	case strings.HasSuffix(url, ".bz2"):
		src = bzip2.NewReader(resp.Body)
	case strings.HasSuffix(url, ".zst"):
		dec, err := zstd.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("zstd: %w", err)
		}
		defer dec.Close()
		src = dec
	case strings.HasSuffix(url, ".gz") || resp.Header.Get("Content-Encoding") == "gzip":
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

// isKnownBrokenCDN returns true for FACEIT CDN hostnames that have no DNS record.
func isKnownBrokenCDN(demoURL string) bool {
	return strings.Contains(demoURL, "backblaze.faceit-cdn.net")
}

// resolveDemoURL exchanges a broken FACEIT CDN URL for a signed download URL
// using the official FACEIT Downloads API (https://docs.faceit.com/getting-started/Guides/download-api).
// downloadsKey must be a Downloads API access token (separate from the Data API key).
func resolveDemoURL(brokenURL, downloadsKey string) (string, error) {
	body, err := json.Marshal(map[string]string{"resource_url": brokenURL})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST",
		"https://open.faceit.com/download/v2/demos/download",
		bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+downloadsKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}

	var result struct {
		Payload struct {
			DownloadURL string `json:"download_url"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if result.Payload.DownloadURL == "" {
		return "", fmt.Errorf("empty download_url in response")
	}
	return result.Payload.DownloadURL, nil
}

// loadFaceitDownloadsKey returns the FACEIT Downloads API access token from the
// FACEIT_DOWNLOADS_KEY environment variable or ~/.csmetrics/faceit_downloads_key.
// This is a separate token from the Data API key — apply at https://fce.gg/downloads-api-application.
func loadFaceitDownloadsKey() string {
	if v := os.Getenv("FACEIT_DOWNLOADS_KEY"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".csmetrics", "faceit_downloads_key"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// loadFaceitAPIKey returns the FACEIT Data API key from the FACEIT_API_KEY
// environment variable or ~/.csmetrics/faceit_api_key file.
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

// looksLikeSteamID returns true if s is a numeric string of at least 15 digits,
// consistent with a Steam ID64.
func looksLikeSteamID(s string) bool {
	if len(s) < 15 {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
