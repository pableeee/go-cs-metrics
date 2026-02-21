package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
)

var dbPath string

var rootCmd = &cobra.Command{
	Use:   "csmetrics",
	Short: "CS2 demo metrics tool",
	Long:  "Parse CS2 .dem files and compute player/team performance metrics.",
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	defaultDB := filepath.Join(mustUserHome(), ".csmetrics", "metrics.db")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", defaultDB, "path to SQLite database")
	rootCmd.PersistentFlags().BoolVarP(&report.Verbose, "verbose", "v", false, "print metric explanations before each table")

	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(playerCmd)
	rootCmd.AddCommand(shellCmd)
}

func mustUserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
