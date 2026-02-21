// Package cmd implements the CLI commands for csmetrics, including demo parsing,
// listing, showing, FACEIT fetching, cross-match player analysis, and an
// interactive shell.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/report"
)

// dbPath is the file path to the SQLite database, set via the --db flag.
var dbPath string

// silent suppresses verbose metric explanations when true, set via the --silent flag.
var silent bool

// rootCmd is the top-level cobra command for the csmetrics CLI.
var rootCmd = &cobra.Command{
	Use:   "csmetrics",
	Short: "CS2 demo metrics tool",
	Long:  "Parse CS2 .dem files and compute player/team performance metrics.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		report.Verbose = !silent
	},
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
	rootCmd.PersistentFlags().BoolVarP(&silent, "silent", "s", false, "hide metric explanations before each table")

	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(playerCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(roundsCmd)
}

// mustUserHome returns the current user's home directory, falling back to "."
// if it cannot be determined.
func mustUserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
