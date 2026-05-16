package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/csraw2"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// infoCmd prints basic info about a demo file and whether it is stored in the DB.
var infoCmd = &cobra.Command{
	Use:   "info <demo.dem> [<demo.dem>...]",
	Short: "Show basic info about a demo file and its DB status",
	Long: `Show file metadata and database status for one or more demo files.

For .dem files: only the first 64 KB is read to compute the quick hash.
For .csraw2.tar files: header.json is read directly (no parquet decode).

If the demo is already stored, its recorded map, date, score, tier, and
event are printed.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runInfo,
}

func init() {
	rootCmd.AddCommand(infoCmd)
}

func runInfo(cmd *cobra.Command, args []string) error {
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	for i, path := range args {
		if i > 0 {
			fmt.Println()
		}
		if err := printDemoInfo(db, path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		}
	}
	return nil
}

func printDemoInfo(db *storage.DB, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	fmt.Printf("File:   %s\n", path)
	fmt.Printf("Size:   %s\n", humanBytes(fi.Size()))
	fmt.Printf("Mtime:  %s\n", fi.ModTime().Format("2006-01-02 15:04:05"))

	if isCSRaw2(path) {
		return printCSRaw2Info(db, path)
	}

	qh, err := parser.QuickHash(path)
	if err != nil {
		fmt.Printf("Hash:   (error: %v)\n", err)
		fmt.Printf("Status: UNKNOWN\n")
		return nil
	}
	fmt.Printf("Hash:   %s  (quick, 64 KB)\n", qh[:16])

	found, fullHash, dbErr := db.DemoExistsByQuickHash(qh)
	if dbErr != nil {
		fmt.Printf("Status: DB ERROR: %v\n", dbErr)
		return nil
	}
	if !found {
		fmt.Printf("Status: NOT IN DB\n")
		return nil
	}

	demo, err := db.GetDemoByPrefix(fullHash)
	if err != nil || demo == nil {
		fmt.Printf("Status: IN DB  hash=%s  (no metadata)\n", fullHash[:12])
		return nil
	}

	printStoredDemo(db, demo)
	return nil
}

// printCSRaw2Info prints info about a .csraw2.tar file and its DB status.
// It reads only header.json from the archive — no parquet decode needed.
func printCSRaw2Info(db *storage.DB, path string) error {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("Format: csraw2.tar (open error: %v)\n", err)
		fmt.Printf("Status: UNKNOWN\n")
		return nil
	}
	defer f.Close()

	h, err := csraw2.ReadHeader(f)
	if err != nil {
		fmt.Printf("Format: csraw2.tar (header error: %v)\n", err)
		fmt.Printf("Status: UNKNOWN\n")
		return nil
	}

	demoHash := strings.TrimPrefix(h.DemoHash, "sha256:")
	quickHash := strings.TrimPrefix(h.QuickHash, "sha256:")

	fmt.Printf("Format: csraw2.tar (schema %s)\n", h.CSRawSchemaVersion)
	fmt.Printf("Hash:   %s  (embedded SHA-256)\n", demoHash[:16])
	if quickHash != "" {
		fmt.Printf("Quick:  %s  (first 64 KB)\n", quickHash[:16])
	}
	fmt.Printf("Map:    %s\n", h.Map)
	fmt.Printf("Date:   %s\n", h.MatchDate)
	if h.Tier != "" {
		fmt.Printf("Tier:   %s\n", h.Tier)
	}
	fmt.Printf("Players:%d\n", len(h.Players))

	exists, dbErr := db.DemoExists(demoHash)
	if dbErr != nil {
		fmt.Printf("Status: DB ERROR: %v\n", dbErr)
		return nil
	}
	if !exists {
		fmt.Printf("Status: NOT IN DB\n")
		return nil
	}

	demo, err := db.GetDemoByPrefix(demoHash)
	if err != nil || demo == nil {
		fmt.Printf("Status: IN DB  hash=%s  (no metadata)\n", demoHash[:12])
		return nil
	}

	printStoredDemo(db, demo)
	return nil
}

// printStoredDemo prints the stored metadata row plus the roster table.
func printStoredDemo(db *storage.DB, demo *model.MatchSummary) {
	fmt.Printf("Status: IN DB\n")
	fmt.Printf("  Hash:  %s\n", demo.DemoHash[:16])
	fmt.Printf("  Map:   %s\n", demo.MapName)
	fmt.Printf("  Date:  %s\n", demo.MatchDate)
	fmt.Printf("  Score: CT %d — T %d\n", demo.CTScore, demo.TScore)
	if demo.Tier != "" {
		fmt.Printf("  Tier:  %s\n", demo.Tier)
	}
	if demo.EventID != "" {
		fmt.Printf("  Event: %s\n", demo.EventID)
	}
	fmt.Printf("  Tick:  %.0f\n", demo.Tickrate)

	players, err := db.GetPlayerMatchStats(demo.DemoHash)
	if err == nil && len(players) > 0 {
		fmt.Println()
		printInfoRoster(players)
	}
}

// printInfoRoster prints a compact two-sided roster table (CT then T).
func printInfoRoster(players []model.PlayerMatchStats) {
	printSide := func(side model.Team, label string) {
		fmt.Printf("  %-22s  %3s  %3s  %5s  %s\n", label, "K", "D", "ADR", "STEAM ID")
		fmt.Printf("  %-22s  %3s  %3s  %5s  %s\n", "----------------------", "---", "---", "-----", "------------------")
		for _, p := range players {
			if p.Team != side {
				continue
			}
			adr := 0.0
			if p.RoundsPlayed > 0 {
				adr = float64(p.TotalDamage) / float64(p.RoundsPlayed)
			}
			name := p.Name
			if len(name) > 22 {
				name = name[:19] + "..."
			}
			fmt.Printf("  %-22s  %3d  %3d  %5.1f  %d\n", name, p.Kills, p.Deaths, adr, p.SteamID)
		}
	}
	printSide(model.TeamCT, "CT")
	fmt.Println()
	printSide(model.TeamT, "T")
}

// humanBytes formats a byte count as a human-readable string (e.g. "1.1 GB").
func humanBytes(n int64) string {
	const (
		KB = 1 << 10
		MB = 1 << 20
		GB = 1 << 30
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/GB)
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/MB)
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/KB)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
