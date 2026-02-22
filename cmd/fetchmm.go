package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/steam"
)

// fetch-mm command flags.
var (
	mmSteamID   string
	mmAuthCode  string
	mmShareCode string
	mmCount     int
)

// fetchMMCmd walks the CS2 match sharing code chain for a player and prints
// each match's share code plus the expected demo filename, saving the last
// seen code so subsequent runs can resume automatically.
//
// Automatic demo download is not supported: Valve's replay servers require
// a signed URL that can only be obtained from the Steam Game Coordinator,
// which in turn requires an active Steam client session. Use the share codes
// printed here to download demos via Refrag, Leetify, CS2's in-game Watch
// menu, or cs-demo-manager, then ingest them with: csmetrics parse --dir <dir>
var fetchMMCmd = &cobra.Command{
	Use:   "fetch-mm",
	Short: "Walk CS2 MM/Premier share code chain and print match info",
	Long: `Walks your CS2 match sharing code chain and prints each match's share
code and expected demo filename.

IMPORTANT: Automatic demo download is not possible without the Steam Game
Coordinator. Valve's replay servers now require a signed token that only
the GC can provide. To ingest your MM/Premier demos:

  1. Run fetch-mm to see your recent matches and save your place in the chain
  2. Download the demos via one of:
       - CS2 Watch menu → Your Matches → Download Demo
       - Refrag (refrag.gg) — exports .dem files
       - cs-demo-manager (github.com/akiver/cs-demo-manager)
  3. Ingest with: csmetrics parse --dir <folder-with-demos>

Credentials can be provided as flags or environment variables:
  --steam-id    / STEAM_ID         Steam ID64 (e.g. 76561198012345678)
  --auth-code   / STEAM_AUTH_CODE  Game auth code — Steam Settings → Account → Game Details
                                   Format: AAAA-BBBBB-CCCC
  --steam-key   / STEAM_API_KEY    Steam Web API key (https://steamcommunity.com/dev/apikey)
  --share-code  / STEAM_SHARE_CODE Starting CSGO-XXXXX share code (required on first run)

The last processed code is saved to ~/.csmetrics/mm_last_code so subsequent
runs resume automatically without needing --share-code.

Examples:
  # First run — anchor at any known share code
  csmetrics fetch-mm --steam-id 76561198012345678 --share-code CSGO-XXXXX-XXXXX-XXXXX-XXXXX

  # Subsequent runs — auto-resumes from last seen code
  csmetrics fetch-mm --steam-id 76561198012345678`,
	RunE: runFetchMM,
}

func init() {
	fetchMMCmd.Flags().StringVar(&mmSteamID, "steam-id", "", "Steam ID64 (or STEAM_ID env)")
	fetchMMCmd.Flags().StringVar(&mmAuthCode, "auth-code", "", "game auth code from Steam settings (or STEAM_AUTH_CODE env)")
	fetchMMCmd.Flags().StringVar(&mmShareCode, "share-code", "", "starting CSGO share code (or STEAM_SHARE_CODE env); omit to resume from last run")
	fetchMMCmd.Flags().IntVar(&mmCount, "count", 20, "max share codes to walk")
	_ = fetchMMCmd.MarkFlagRequired("steam-id")
}

func runFetchMM(cmd *cobra.Command, args []string) error {
	authCode := firstNonEmpty(mmAuthCode, os.Getenv("STEAM_AUTH_CODE"))
	if authCode == "" {
		return fmt.Errorf("auth code required: use --auth-code or STEAM_AUTH_CODE env\n" +
			"  Generate one at Steam Settings → Account → Game Details")
	}

	steamAPIKey, err := loadSteamAPIKey()
	if err != nil {
		return err
	}

	startCode := firstNonEmpty(mmShareCode, os.Getenv("STEAM_SHARE_CODE"))
	if startCode == "" {
		startCode, err = loadMMLastCode()
		if err != nil {
			return fmt.Errorf("no starting share code: provide --share-code or STEAM_SHARE_CODE\n" +
				"  Get one from CS2 Watch menu → right-click a match → Copy Share Code")
		}
		fmt.Printf("Resuming from: %s\n", startCode)
	}

	return doFetchMM(mmSteamID, authCode, steamAPIKey, startCode, mmCount)
}

func doFetchMM(steamID, authCode, apiKey, startCode string, count int) error {
	client := steam.NewClient(apiKey)

	fmt.Printf("Walking share code chain (up to %d)…\n\n", count)
	fmt.Printf("  %-42s  %s\n", "Share Code", "Demo filename")
	fmt.Printf("  %-42s  %s\n", strings.Repeat("-", 42), strings.Repeat("-", 50))

	currentCode := startCode
	found := 0

	for found < count {
		nextCode, err := client.NextShareCode(steamID, authCode, currentCode)
		if err != nil {
			// Rate-limit: print what we have so far and stop cleanly.
			if strings.Contains(err.Error(), "rate limited") {
				fmt.Printf("\n  [warn] %v\n", err)
				break
			}
			return fmt.Errorf("share code chain: %w", err)
		}
		if nextCode == "" {
			fmt.Printf("\n(chain exhausted after %d match(es))\n", found)
			break
		}

		currentCode = nextCode

		sc, err := steam.Decode(currentCode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [skip] decode %s: %v\n", currentCode, err)
			continue
		}

		fmt.Printf("  %-42s  %s\n", currentCode, steam.DemoFilename(sc))
		_ = saveMMLastCode(currentCode)
		found++
	}

	if found > 0 {
		fmt.Printf("\nLast code saved to ~/.csmetrics/mm_last_code — next run resumes from here.\n")
		fmt.Printf("\nTo ingest demos:\n")
		fmt.Printf("  1. Download .dem files via CS2 Watch menu, Refrag, or cs-demo-manager\n")
		fmt.Printf("  2. csmetrics parse --dir <folder>\n")
	}

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
