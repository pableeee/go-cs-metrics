package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/steam"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// fetch-mm command flags.
var (
	mmSteamID   string
	mmAuthCode  string
	mmShareCode string
	mmCount     int
	mmMap       string
	mmTier      string
)

// fetchMMCmd downloads and ingests Valve Matchmaking / Premier demos via the
// Steam share code API.
var fetchMMCmd = &cobra.Command{
	Use:   "fetch-mm",
	Short: "Download and ingest Valve MM / Premier demos via Steam share codes",
	Long: `Chains through CS2 match sharing codes to download and ingest your recent
Valve Matchmaking or Premier demos.

Credentials can be provided as flags or environment variables:
  --steam-id    / STEAM_ID      Steam ID64 (e.g. 76561198012345678)
  --auth-code   / STEAM_AUTH_CODE  Game auth code from Steam Settings → Account → Game Details
                                   Format: AAAA-BBBBB-CCCC
  --steam-key   / STEAM_API_KEY    Steam Web API key from https://steamcommunity.com/dev
  --share-code  / STEAM_SHARE_CODE Starting share code (CSGO-XXXXX-XXXXX-XXXXX-XXXXX)

On the first run, provide --share-code with your most recently known match code.
The tool saves the last processed code to ~/.csmetrics/mm_last_code so subsequent
runs can pick up where they left off without needing --share-code again.

How to get your starting share code:
  • In CS2: Watch → Your Matches → right-click any match → Copy Share Code
  • Or from Refrag / csgostats.gg / Leetify match detail pages

Examples:
  # First run — provide your starting share code
  csmetrics fetch-mm --steam-id 76561198012345678 --share-code CSGO-XXXXX-XXXXX-XXXXX-XXXXX --count 10

  # Subsequent runs — pick up automatically from last processed match
  csmetrics fetch-mm --steam-id 76561198012345678 --count 10

  # Filter to a specific map
  csmetrics fetch-mm --steam-id 76561198012345678 --map de_mirage --count 5`,
	RunE: runFetchMM,
}

func init() {
	fetchMMCmd.Flags().StringVar(&mmSteamID, "steam-id", "", "Steam ID64 (or STEAM_ID env)")
	fetchMMCmd.Flags().StringVar(&mmAuthCode, "auth-code", "", "Game auth code from Steam settings (or STEAM_AUTH_CODE env)")
	fetchMMCmd.Flags().StringVar(&mmShareCode, "share-code", "", "starting CSGO share code (or STEAM_SHARE_CODE env); omit to resume from last run")
	fetchMMCmd.Flags().IntVar(&mmCount, "count", 10, "number of matches to ingest")
	fetchMMCmd.Flags().StringVar(&mmMap, "map", "", "only ingest matches on this map (e.g. de_mirage)")
	fetchMMCmd.Flags().StringVar(&mmTier, "tier", "mm", "tier label stored in DB")
	_ = fetchMMCmd.MarkFlagRequired("steam-id")
}

func runFetchMM(cmd *cobra.Command, args []string) error {
	// Resolve credentials from flags → env vars.
	authCode := firstNonEmpty(mmAuthCode, os.Getenv("STEAM_AUTH_CODE"))
	if authCode == "" {
		return fmt.Errorf("auth code required: use --auth-code or STEAM_AUTH_CODE env\n" +
			"  Generate one at Steam Settings → Account → Game Details")
	}

	steamAPIKey, err := loadSteamAPIKey()
	if err != nil {
		return err
	}

	// Resolve starting share code: flag → env → persisted last code.
	startCode := firstNonEmpty(mmShareCode, os.Getenv("STEAM_SHARE_CODE"))
	if startCode == "" {
		startCode, err = loadMMLastCode()
		if err != nil {
			return fmt.Errorf("no starting share code: provide --share-code or STEAM_SHARE_CODE, " +
				"or re-run after a previous fetch-mm that persisted a code")
		}
		fmt.Printf("Resuming from last known code: %s\n", startCode)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	return doFetchMM(db, mmSteamID, authCode, steamAPIKey, startCode, mmMap, mmCount, mmTier)
}

func doFetchMM(db *storage.DB, steamID, authCode, apiKey, startCode, mapFilter string, count int, tier string) error {
	client := steam.NewClient(apiKey)

	tmpDir, err := os.MkdirTemp("", "csmetrics-mm-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ingested := 0
	currentCode := startCode

	fmt.Printf("Fetching up to %d match(es) from share code chain…\n", count)

	for ingested < count {
		nextCode, err := client.NextShareCode(steamID, authCode, currentCode)
		if err != nil {
			return fmt.Errorf("share code chain: %w", err)
		}
		if nextCode == "" {
			fmt.Println("No more matches available in chain.")
			break
		}

		currentCode = nextCode

		sc, err := steam.Decode(currentCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [skip] decode %s: %v\n", currentCode, err)
			continue
		}

		// Lower 32 bits of matchID encode the Unix timestamp of the match.
		matchTS := time.Unix(int64(sc.MatchID&0xFFFFFFFF), 0).UTC()
		matchDate := matchTS.Format("2006-01-02")

		fmt.Printf("[%d/%d] code=%s  matchID=%d  date=%s\n",
			ingested+1, count, currentCode, sc.MatchID, matchDate)

		if time.Since(matchTS) > 32*24*time.Hour {
			fmt.Fprintf(os.Stderr, "  [warn] match is older than 32 days — demo has likely expired\n")
		}

		fmt.Printf("  resolving replay server…")
		replayURL, err := steam.ResolveReplayURL(sc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  [skip] %v\n", err)
			// Still advance the code even if demo expired.
			_ = saveMMLastCode(currentCode)
			continue
		}
		fmt.Println(" ok")

		demPath, err := downloadAndDecompress(replayURL, tmpDir, fmt.Sprintf("%d", sc.MatchID))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [error] download: %v\n", err)
			continue
		}

		raw, err := parser.ParseDemo(demPath, "MM")
		os.Remove(demPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [error] parse: %v\n", err)
			continue
		}

		if mapFilter != "" && !strings.EqualFold(raw.MapName, mapFilter) {
			fmt.Printf("  [skip] map=%s (want %s)\n", raw.MapName, mapFilter)
			_ = saveMMLastCode(currentCode)
			continue
		}

		exists, err := db.DemoExists(raw.DemoHash)
		if err != nil {
			return err
		}
		if exists {
			fmt.Printf("  already stored (map=%s)\n", raw.MapName)
			_ = saveMMLastCode(currentCode)
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
			DemoHash:  raw.DemoHash,
			MapName:   raw.MapName,
			MatchDate: matchDate,
			MatchType: "MM",
			Tickrate:  raw.Tickrate,
			CTScore:   ctScore,
			TScore:    tScore,
			Tier:      tier,
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

		fmt.Printf("  stored: map=%s  players=%d  rounds=%d\n",
			raw.MapName, len(matchStats), len(raw.Rounds))
		_ = saveMMLastCode(currentCode)
		ingested++

		// Brief pause to stay within Steam API rate limits.
		time.Sleep(1 * time.Second)
	}

	fmt.Printf("\nDone: %d/%d matches ingested (tier=%q)\n", ingested, count, tier)
	return nil
}

// loadSteamAPIKey returns the Steam Web API key from STEAM_API_KEY env or
// ~/.csmetrics/steam_api_key file.
func loadSteamAPIKey() (string, error) {
	if key := os.Getenv("STEAM_API_KEY"); key != "" {
		return key, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(home, ".csmetrics", "steam_api_key"))
	if err != nil {
		return "", fmt.Errorf("Steam Web API key not found: set STEAM_API_KEY or create ~/.csmetrics/steam_api_key\n" +
			"  Get a key at https://steamcommunity.com/dev/apikey")
	}
	return strings.TrimSpace(string(data)), nil
}

// lastCodePath returns the path where the last processed share code is persisted.
func lastCodePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".csmetrics", "mm_last_code"), nil
}

func loadMMLastCode() (string, error) {
	p, err := lastCodePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	code := strings.TrimSpace(string(data))
	if code == "" {
		return "", fmt.Errorf("empty")
	}
	return code, nil
}

func saveMMLastCode(code string) error {
	p, err := lastCodePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(code+"\n"), 0600)
}

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
