package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var dropForce bool

// dropCmd deletes the metrics database file.
var dropCmd = &cobra.Command{
	Use:   "drop",
	Short: "Delete the metrics database",
	Long:  "Permanently delete the SQLite metrics database. All stored demo data will be lost. Re-parse your demos afterwards to rebuild.",
	Args:  cobra.NoArgs,
	RunE:  runDrop,
}

func init() {
	dropCmd.Flags().BoolVarP(&dropForce, "force", "f", false, "skip confirmation prompt")
}

func runDrop(cmd *cobra.Command, args []string) error {
	if !dropForce {
		fmt.Fprintf(os.Stderr, "This will permanently delete: %s\n", dbPath)
		fmt.Fprintf(os.Stderr, "Re-run with --force to confirm.\n")
		return nil
	}
	if err := os.Remove(dbPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stdout, "Database does not exist, nothing to drop.")
			return nil
		}
		return fmt.Errorf("remove database: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Deleted: %s\n", dbPath)
	return nil
}
