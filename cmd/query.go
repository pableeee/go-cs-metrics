package cmd

import (
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/roundquery"
)

var (
	queryDir string
	queryCSV bool
)

var queryCmd = &cobra.Command{
	Use:   "query --dir <dir> <expression>",
	Short: "Find rounds matching a CEL expression",
	Long: `Scan .csdem.gz files and return every round that satisfies a CEL expression.

Walks --dir recursively. The expression is evaluated against a flat set of
per-round variables — no SQL or schema knowledge required.

VARIABLES

  Identity
    map          string   e.g. "de_inferno"
    round_num    int      1-based round number
    winner       string   "T" | "CT"
    entry_side   string   "T" | "CT" | ""

  Buy types (per side)
    type_t / type_ct   string   "pistol" | "eco" | "force" | "full"
    equip_t / equip_ct int      total equipment value in USD

  Utility thrown (count per side)
    smokes_t / smokes_ct
    flashes_t / flashes_ct
    molotovs_t / molotovs_ct
    hes_t / hes_ct

  HE grenade damage dealt (HP total per side)
    he_damage_t / he_damage_ct

  Flash-assisted kills (victim was blind) per killer side
    flash_kills_t / flash_kills_ct

  Alive player counts
    alive_start_t / alive_start_ct    at round start (after freeze)
    alive_plant_t / alive_plant_ct    at bomb plant tick; 0 if no plant

  Bomb events
    planted  bool
    defused  bool
    exploded bool

EXAMPLES

  # T-side executes: heavy utility + bomb planted
  go-cs-metrics query --dir ~/demos/converted-pro/ \
    'planted && smokes_t + molotovs_t + flashes_t >= 4'

  # CT retakes, especially low-man
  go-cs-metrics query --dir ~/demos/converted-pro/ \
    'planted && winner == "CT" && alive_plant_ct <= 2'

  # Flash-assisted kills (T entries or CT aggression)
  go-cs-metrics query --dir ~/demos/converted-pro/ 'flash_kills_t >= 1'

  # Coordinated CT HE aggression (e.g. Inferno B)
  go-cs-metrics query --dir ~/demos/converted-pro/iem_cologne_2025/ \
    'map == "de_inferno" && hes_ct >= 2 && he_damage_ct >= 80'

  # Force buy wins
  go-cs-metrics query --dir ~/demos/converted-pro/ \
    'type_t == "force" && winner == "T"'`,
	Args: cobra.ExactArgs(1),
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().StringVar(&queryDir, "dir", "", "root directory to scan for .csdem.gz files (required)")
	queryCmd.Flags().BoolVar(&queryCSV, "csv", false, "output CSV instead of text table")
	_ = queryCmd.MarkFlagRequired("dir")
}

func runQuery(cmd *cobra.Command, args []string) error {
	expr := args[0]

	env, err := roundquery.NewEnv()
	if err != nil {
		return fmt.Errorf("init CEL: %w", err)
	}
	prg, err := roundquery.Compile(env, expr)
	if err != nil {
		return err // already has descriptive prefix
	}

	// Collect .csdem.gz paths recursively.
	var paths []string
	err = filepath.WalkDir(queryDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".csdem.gz") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", queryDir, err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .csdem.gz files found under %s", queryDir)
	}

	fmt.Fprintf(os.Stderr, "Scanning %d files...\n", len(paths))

	var hits []roundquery.RoundRecord
	var scanErrors int

	for _, path := range paths {
		uf, err := model.LoadUnifiedMatchFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", filepath.Base(path), err)
			scanErrors++
			continue
		}

		matchFile := strings.TrimSuffix(filepath.Base(path), ".csdem.gz")
		records := roundquery.BuildRecords(uf.Match, matchFile, uf.Match.MatchDate)

		for _, r := range records {
			ok, err := roundquery.Matches(prg, r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  eval error round %d in %s: %v\n", r.RoundNum, matchFile, err)
				continue
			}
			if ok {
				hits = append(hits, r)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Done. %d rounds matched across %d files (%d errors).\n\n",
		len(hits), len(paths), scanErrors)

	if len(hits) == 0 {
		return nil
	}

	if queryCSV {
		printQueryCSV(hits)
	} else {
		printQueryTable(hits)
	}
	return nil
}

func printQueryTable(records []roundquery.RoundRecord) {
	const matchW = 38
	fmt.Printf("%-10s  %-12s  %-*s  %3s  %-2s  %-6s  %-6s  %4s %4s %4s %4s  %4s %4s %4s %4s  %3s %3s %3s %3s  %5s  %-5s\n",
		"DATE", "MAP", matchW, "MATCH",
		"RND", "W", "TYPE_T", "TY_CT",
		"SMK_T", "FLA_T", "MOL_T", "HE_T",
		"SMK_CT", "FLA_CT", "MOL_CT", "HE_CT",
		"FK_T", "FK_CT", "HED_T", "HED_CT",
		"PLANT", "ENTRY")
	for _, r := range records {
		name := r.MatchFile
		if len(name) > matchW {
			name = name[:matchW-3] + "..."
		}
		plant := "no"
		if r.Planted {
			plant = "yes"
		}
		fmt.Printf("%-10s  %-12s  %-*s  %3d  %-2s  %-6s  %-6s  %4d %4d %4d %4d  %4d %4d %4d %4d  %3d %3d %3d %3d  %5s  %-5s\n",
			r.Date, r.Map, matchW, name,
			r.RoundNum, r.Winner, r.TypeT, r.TypeCT,
			r.SmokesT, r.FlashesT, r.MolotovsT, r.HEsT,
			r.SmokesCT, r.FlashesCT, r.MolotovsCT, r.HEsCT,
			r.FlashKillsT, r.FlashKillsCT, r.HEDamageT, r.HEDamageCT,
			plant, r.EntrySide)
	}
}

func printQueryCSV(records []roundquery.RoundRecord) {
	w := csv.NewWriter(os.Stdout)
	_ = w.Write([]string{
		"date", "map", "match", "round_num", "winner",
		"type_t", "type_ct", "equip_t", "equip_ct",
		"smokes_t", "smokes_ct", "flashes_t", "flashes_ct",
		"molotovs_t", "molotovs_ct", "hes_t", "hes_ct",
		"he_damage_t", "he_damage_ct",
		"flash_kills_t", "flash_kills_ct",
		"alive_start_t", "alive_start_ct",
		"alive_plant_t", "alive_plant_ct",
		"planted", "defused", "exploded", "entry_side",
	})
	for _, r := range records {
		_ = w.Write([]string{
			r.Date, r.Map, r.MatchFile,
			fmt.Sprint(r.RoundNum), r.Winner,
			r.TypeT, r.TypeCT,
			fmt.Sprint(r.EquipT), fmt.Sprint(r.EquipCT),
			fmt.Sprint(r.SmokesT), fmt.Sprint(r.SmokesCT),
			fmt.Sprint(r.FlashesT), fmt.Sprint(r.FlashesCT),
			fmt.Sprint(r.MolotovsT), fmt.Sprint(r.MolotovsCT),
			fmt.Sprint(r.HEsT), fmt.Sprint(r.HEsCT),
			fmt.Sprint(r.HEDamageT), fmt.Sprint(r.HEDamageCT),
			fmt.Sprint(r.FlashKillsT), fmt.Sprint(r.FlashKillsCT),
			fmt.Sprint(r.AliveStartT), fmt.Sprint(r.AliveStartCT),
			fmt.Sprint(r.AlivePlantT), fmt.Sprint(r.AlivePlantCT),
			fmt.Sprint(r.Planted), fmt.Sprint(r.Defused), fmt.Sprint(r.Exploded),
			r.EntrySide,
		})
	}
	w.Flush()
}
