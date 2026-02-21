package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	cPrompt  = color.New(color.FgCyan, color.Bold)
	cMuted   = color.New(color.Faint)
	cError   = color.New(color.FgRed, color.Bold)
	cWarn    = color.New(color.FgYellow)
	cHeader  = color.New(color.FgCyan, color.Bold)
	cCmd     = color.New(color.FgYellow, color.Bold)
	cGreeting = color.New(color.Bold)
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start an interactive REPL session",
	Long:  "Open a persistent session against the database. Type 'help' for available commands.",
	Args:  cobra.NoArgs,
	RunE:  runShell,
}

func runShell(_ *cobra.Command, _ []string) error {
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	cGreeting.Println("csmetrics shell")
	cMuted.Println("type 'help' or 'exit'")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		cPrompt.Print("csmetrics")
		cMuted.Print("> ")
		if !scanner.Scan() {
			fmt.Println()
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		tokens := strings.Fields(line)
		cmd, args := tokens[0], tokens[1:]

		switch cmd {
		case "exit", "quit":
			return nil
		case "help":
			shellHelp()
		case "list":
			shellList(db)
		case "show":
			if len(args) == 0 {
				cError.Fprintln(os.Stderr, "usage: show <hash-prefix> [--player <steamid64>]")
				continue
			}
			prefix := args[0]
			var playerID uint64
			for i := 1; i+1 < len(args); i++ {
				if args[i] == "--player" {
					playerID, _ = strconv.ParseUint(args[i+1], 10, 64)
				}
			}
			shellShow(db, prefix, playerID)
		case "player":
			if len(args) == 0 {
				cError.Fprintln(os.Stderr, "usage: player <steamid64> [<steamid64>...]")
				continue
			}
			shellPlayer(db, args)
		default:
			cWarn.Fprintf(os.Stderr, "unknown command %q — type 'help'\n", cmd)
		}
	}
	return nil
}

func shellHelp() {
	fmt.Println()
	type entry struct{ cmd, desc string }
	rows := []entry{
		{"list", "list all stored demos"},
		{"show <hash-prefix>", "show a match's stats"},
		{"show <hash-prefix> --player <id>", "same, highlighting one player"},
		{"player <steamid64> [...]", "cross-match analysis for one or more players"},
		{"help", "show this message"},
		{"exit / quit", "close the session"},
	}
	for _, r := range rows {
		fmt.Print("  ")
		cCmd.Printf("%-38s", r.cmd)
		fmt.Println(r.desc)
	}
	fmt.Println()
}

func shellList(db *storage.DB) {
	demos, err := db.ListDemos()
	if err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if len(demos) == 0 {
		cMuted.Println("No demos stored yet.")
		return
	}
	cHeader.Fprintf(os.Stdout, "%-14s  %-12s  %-10s  %-12s  %6s  %s\n",
		"HASH", "MAP", "DATE", "TYPE", "SCORE", "TICK")
	cMuted.Fprintf(os.Stdout, "%-14s  %-12s  %-10s  %-12s  %6s  %s\n",
		"──────────────", "────────────", "──────────", "────────────", "──────", "────")
	for _, d := range demos {
		score := fmt.Sprintf("%d-%d", d.CTScore, d.TScore)
		fmt.Fprintf(os.Stdout, "%-14s  %-12s  %-10s  %-12s  %6s  %.0f\n",
			d.DemoHash[:12], d.MapName, d.MatchDate, d.MatchType, score, d.Tickrate)
	}
}

func shellShow(db *storage.DB, prefix string, playerID uint64) {
	demo, err := db.GetDemoByPrefix(prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if demo == nil {
		fmt.Fprintf(os.Stderr, "no demo found with prefix %q\n", prefix)
		return
	}
	stats, err := db.GetPlayerMatchStats(demo.DemoHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	sideStats, err := db.GetPlayerSideStats(demo.DemoHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	weaponStats, err := db.GetPlayerWeaponStats(demo.DemoHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	duelSegs, err := db.GetPlayerDuelSegments(demo.DemoHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	report.PrintMatchSummary(os.Stdout, *demo)
	report.PrintPlayerTable(stats, playerID)
	report.PrintPlayerSideTable(os.Stdout, sideStats, playerID)
	report.PrintDuelTable(os.Stdout, stats, playerID)
	report.PrintAWPTable(os.Stdout, stats, playerID)
	report.PrintFHHSTable(os.Stdout, duelSegs, stats, playerID)
	report.PrintWeaponTable(os.Stdout, weaponStats, stats, playerID)
}

func shellPlayer(db *storage.DB, args []string) {
	type fhhsEntry struct {
		name  string
		id    uint64
		segs  []model.PlayerDuelSegment
		synth []model.PlayerMatchStats
	}

	var allAggs    []model.PlayerAggregate
	var allMapSide []model.PlayerMapSideAggregate
	var fhhsList   []fhhsEntry

	for _, arg := range args {
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid SteamID64 %q: %v\n", arg, err)
			continue
		}
		stats, err := db.GetAllPlayerMatchStats(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		if len(stats) == 0 {
			fmt.Fprintf(os.Stderr, "no data for SteamID64 %d\n", id)
			continue
		}
		segs, err := db.GetAllPlayerDuelSegments(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}

		agg := buildAggregate(stats)
		merged := mergeSegments(id, segs)

		var totalHits, totalHSHits int
		for _, s := range merged {
			totalHits += s.FirstHitCount
			totalHSHits += s.FirstHitHSCount
		}
		overallFHHS := 0.0
		if totalHits > 0 {
			overallFHHS = float64(totalHSHits) / float64(totalHits) * 100
		}

		allAggs = append(allAggs, agg)
		allMapSide = append(allMapSide, buildMapSideAggregates(stats)...)
		fhhsList = append(fhhsList, fhhsEntry{
			name: agg.Name,
			id:   id,
			segs: merged,
			synth: []model.PlayerMatchStats{{
				SteamID:        id,
				Name:           agg.Name,
				FirstHitHSRate: overallFHHS,
			}},
		})
	}

	if len(allAggs) == 0 {
		return
	}

	fmt.Fprintln(os.Stdout)
	report.PrintPlayerAggregateOverview(os.Stdout, allAggs)
	report.PrintPlayerAggregateDuelTable(os.Stdout, allAggs)
	report.PrintPlayerAggregateAWPTable(os.Stdout, allAggs)
	report.PrintPlayerMapSideTable(os.Stdout, allMapSide)
	for _, f := range fhhsList {
		fmt.Fprintf(os.Stdout, "\n")
		cHeader.Fprintf(os.Stdout, "--- FHHS: %s ---\n", f.name)
		report.PrintFHHSTable(os.Stdout, f.segs, f.synth, 0)
	}
}
