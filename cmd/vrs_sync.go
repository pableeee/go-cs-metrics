package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/vrs"
)

var (
	vrsSyncDBPath string
	vrsSyncYears  string
	vrsSyncForce  bool
)

var vrsSyncCmd = &cobra.Command{
	Use:   "vrs-sync",
	Short: "Sync Valve Regional Standings snapshots from the public GitHub repository",
	Long: `Fetches global VRS standings markdown files from:
  ValveSoftware/counter-strike_regional_standings

Parses each snapshot and stores it in a local SQLite database
(default: ~/.csmetrics/vrs.db). These snapshots are used by the
'export' command to tag demos with opponent VRS rank, enabling
stratified stats (vs_top30, vs_top20) in team JSON output.

Runs incrementally by default: only new dates are fetched.
Use --force to re-fetch all snapshots regardless of DB state.

Example:
  go-cs-metrics vrs-sync
  go-cs-metrics vrs-sync --year 2026
  go-cs-metrics vrs-sync --vrs-db /tmp/vrs.db --force`,
	RunE: runVRSSync,
}

func init() {
	defaultVRSDB := filepath.Join(mustUserHome(), ".csmetrics", "vrs.db")
	vrsSyncCmd.Flags().StringVar(&vrsSyncDBPath, "vrs-db", defaultVRSDB, "path to VRS SQLite database")
	vrsSyncCmd.Flags().StringVar(&vrsSyncYears, "year", "", "comma-separated years to sync, e.g. 2025,2026 (default: 2024,2025,2026)")
	vrsSyncCmd.Flags().BoolVar(&vrsSyncForce, "force", false, "re-fetch all snapshots even if already stored")
}

const (
	vrsRepoOwner = "ValveSoftware"
	vrsRepoName  = "counter-strike_regional_standings"
	vrsUserAgent = "go-cs-metrics/vrs-sync"
	vrsRawBase   = "https://raw.githubusercontent.com/" + vrsRepoOwner + "/" + vrsRepoName + "/refs/heads/main"
	vrsAPIBase   = "https://api.github.com/repos/" + vrsRepoOwner + "/" + vrsRepoName + "/contents/live"
)

func runVRSSync(_ *cobra.Command, _ []string) error {
	store, err := vrs.Open(vrsSyncDBPath)
	if err != nil {
		return fmt.Errorf("open vrs db: %w", err)
	}
	defer store.Close()

	// Build the set of dates already in the DB for incremental sync.
	var existingDates map[string]bool
	if !vrsSyncForce {
		dates, err := store.ExistingDates()
		if err != nil {
			return fmt.Errorf("list existing dates: %w", err)
		}
		existingDates = make(map[string]bool, len(dates))
		for _, d := range dates {
			existingDates[d] = true
		}
		fmt.Fprintf(os.Stderr, "Already stored: %d snapshot(s)\n", len(existingDates))
	}

	years := vrsSyncResolveYears(vrsSyncYears)
	fmt.Fprintf(os.Stderr, "Syncing years: %s\n", strings.Join(years, ", "))

	client := &http.Client{}
	var synced, skipped, failed int
	var minDate, maxDate string

	for _, year := range years {
		files, err := vrsListFiles(client, year)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: listing year %s: %v\n", year, err)
			continue
		}
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "  year %s: no standings files found\n", year)
			continue
		}

		for _, filename := range files {
			date, ok := vrsExtractDate(filename)
			if !ok {
				continue
			}
			if !vrsSyncForce && existingDates[date] {
				skipped++
				continue
			}

			rawURL := fmt.Sprintf("%s/live/%s/%s", vrsRawBase, year, filename)
			content, err := vrsFetchText(client, rawURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: fetch %s: %v\n", rawURL, err)
				failed++
				continue
			}

			snap, err := vrs.ParseGlobalStandings(content)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warn: parse %s: %v\n", filename, err)
				failed++
				continue
			}

			if err := store.InsertSnapshot(snap); err != nil {
				fmt.Fprintf(os.Stderr, "warn: insert %s: %v\n", snap.Date, err)
				failed++
				continue
			}

			synced++
			if minDate == "" || snap.Date < minDate {
				minDate = snap.Date
			}
			if snap.Date > maxDate {
				maxDate = snap.Date
			}
			fmt.Fprintf(os.Stderr, "  synced %s (%d teams)\n", snap.Date, len(snap.Entries))
		}
	}

	fmt.Fprintf(os.Stderr, "\nVRS sync complete: %d synced, %d skipped (already stored), %d failed\n",
		synced, skipped, failed)
	if synced > 0 {
		fmt.Fprintf(os.Stderr, "New date range: %s → %s\n", minDate, maxDate)
	}
	return nil
}

// vrsSyncResolveYears returns the list of years to sync. Defaults to 2024–2026
// when the flag is empty.
func vrsSyncResolveYears(flag string) []string {
	if flag == "" {
		return []string{"2024", "2025", "2026"}
	}
	var out []string
	for _, y := range strings.Split(flag, ",") {
		if y := strings.TrimSpace(y); y != "" {
			out = append(out, y)
		}
	}
	return out
}

// githubDirEntry is the relevant subset of the GitHub contents API response.
type githubDirEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "dir"
}

// vrsListFiles returns filenames for global standings in the live/{year}/ directory.
func vrsListFiles(client *http.Client, year string) ([]string, error) {
	url := fmt.Sprintf("%s/%s", vrsAPIBase, year)
	body, err := vrsFetchText(client, url)
	if err != nil {
		return nil, err
	}

	var entries []githubDirEntry
	if err := json.Unmarshal([]byte(body), &entries); err != nil {
		return nil, fmt.Errorf("parse github dir listing for %s: %w", year, err)
	}

	var files []string
	for _, e := range entries {
		if e.Type == "file" &&
			strings.HasPrefix(e.Name, "standings_global_") &&
			strings.HasSuffix(e.Name, ".md") {
			files = append(files, e.Name)
		}
	}
	return files, nil
}

// vrsExtractDate parses "standings_global_YYYY_MM_DD.md" → "YYYY-MM-DD".
func vrsExtractDate(filename string) (string, bool) {
	base := strings.TrimSuffix(filename, ".md")
	parts := strings.Split(base, "_")
	// Expected: ["standings", "global", "YYYY", "MM", "DD"]
	if len(parts) < 5 {
		return "", false
	}
	y := parts[len(parts)-3]
	m := parts[len(parts)-2]
	d := parts[len(parts)-1]
	if len(y) != 4 || len(m) != 2 || len(d) != 2 {
		return "", false
	}
	return fmt.Sprintf("%s-%s-%s", y, m, d), true
}

// vrsFetchText performs a GET request and returns the response body as a string.
func vrsFetchText(client *http.Client, url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", vrsUserAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
