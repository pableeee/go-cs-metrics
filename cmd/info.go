package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// infoCmd prints basic info about a demo file and whether it is stored in the DB.
var infoCmd = &cobra.Command{
	Use:   "info <demo.dem> [<demo.dem>...]",
	Short: "Show basic info about a demo file and its DB status",
	Long: `Show file metadata, quick hash, and database status for one or more demo files.

No full parse is performed — only the first 64 KB is read to compute the quick
hash. If the demo is already stored, its recorded map, date, score, tier, and
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
	return nil
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
