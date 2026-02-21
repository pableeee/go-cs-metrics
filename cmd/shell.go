package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var errInterrupt = errors.New("interrupt")

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

	fd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(fd)

	var history []string
	var scanner *bufio.Scanner
	if !isTTY {
		scanner = bufio.NewScanner(os.Stdin)
	}

	for {
		var line string
		if isTTY {
			line, err = readLine(history)
			if errors.Is(err, io.EOF) {
				fmt.Println()
				break
			}
			if err != nil { // Ctrl+C: redraw prompt and continue
				continue
			}
		} else {
			cPrompt.Print("csmetrics")
			cMuted.Print("> ")
			if !scanner.Scan() {
				fmt.Println()
				break
			}
			line = strings.TrimSpace(scanner.Text())
		}

		if line == "" {
			continue
		}

		if isTTY && (len(history) == 0 || history[len(history)-1] != line) {
			history = append(history, line)
		}

		tokens := strings.Fields(line)
		cmd, args := tokens[0], tokens[1:]

		switch cmd {
		case "exit", "quit":
			return nil
		case "help":
			shellHelp()
		case "parse":
			shellParse(db, args)
		case "list":
			shellList(db)
		case "show":
			if len(args) == 0 {
				cError.Fprintln(os.Stderr, "usage: show <hash-prefix> [--player <steamid64>]")
				continue
			}
			pos, flags := shellFlags(args)
			prefix := pos[0]
			var playerID uint64
			if v, ok := flags["player"]; ok {
				playerID, _ = strconv.ParseUint(v, 10, 64)
			}
			shellShow(db, prefix, playerID)
		case "fetch":
			shellFetch(db, args)
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

// readLine prints the prompt and reads one line in raw terminal mode,
// supporting up/down arrow history navigation within the current session.
// Returns ("", io.EOF) on Ctrl+D or closed input, ("", errInterrupt) on Ctrl+C.
func readLine(hist []string) (string, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	var buf []byte
	histIdx := len(hist) // start past the end — the "new line" position
	var savedLine string  // line saved before navigating into history

	redraw := func() {
		os.Stdout.WriteString("\r\x1b[K") // carriage-return + erase to EOL
		cPrompt.Fprint(os.Stdout, "csmetrics")
		cMuted.Fprint(os.Stdout, "> ")
		os.Stdout.Write(buf)
	}
	redraw()

	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			os.Stdout.WriteString("\r\n")
			return "", io.EOF
		}
		switch b[0] {
		case 3: // Ctrl+C
			os.Stdout.WriteString("\r\n")
			return "", errInterrupt
		case 4: // Ctrl+D — EOF only on empty line (bash behaviour)
			if len(buf) == 0 {
				os.Stdout.WriteString("\r\n")
				return "", io.EOF
			}
		case 13, 10: // Enter (CR or LF)
			line := strings.TrimSpace(string(buf))
			os.Stdout.WriteString("\r\n")
			return line, nil
		case 127, 8: // Backspace / DEL
			if len(buf) > 0 {
				_, size := utf8.DecodeLastRune(buf)
				buf = buf[:len(buf)-size]
				redraw()
			}
		case 27: // ESC — read the rest of the CSI sequence
			seq := make([]byte, 2)
			if _, err := os.Stdin.Read(seq[:1]); err != nil || seq[0] != '[' {
				continue
			}
			if _, err := os.Stdin.Read(seq[1:]); err != nil {
				continue
			}
			switch seq[1] {
			case 'A': // Up arrow
				if histIdx == len(hist) {
					savedLine = string(buf)
				}
				if histIdx > 0 {
					histIdx--
					buf = []byte(hist[histIdx])
					redraw()
				}
			case 'B': // Down arrow
				if histIdx < len(hist) {
					histIdx++
					if histIdx == len(hist) {
						buf = []byte(savedLine)
					} else {
						buf = []byte(hist[histIdx])
					}
					redraw()
				}
			}
		default:
			if b[0] >= 32 { // printable ASCII
				buf = append(buf, b[0])
				redraw()
			}
		}
	}
}

func shellHelp() {
	fmt.Println()
	type entry struct{ cmd, desc string }
	rows := []entry{
		{"parse <demo.dem> [--player <id>] [--type <t>] [--tier <tag>] [--baseline]", "parse + store a demo"},
		{"list", "list all stored demos"},
		{"show <hash-prefix> [--player <id>]", "re-display a stored match"},
		{"fetch --player <name|id> [--map <m>] [--level <n>] [--count <n>] [--tier <t>]", "download FACEIT demos"},
		{"player <steamid64> [...]", "cross-match aggregate report"},
		{"help", "show this message"},
		{"exit / quit", "close the session"},
	}
	for _, r := range rows {
		fmt.Print("  ")
		cCmd.Print(r.cmd)
		fmt.Printf("  —  %s\n", r.desc)
	}
	fmt.Println()
}

// shellFlags splits args into positional arguments and --key value flag pairs.
// Names listed in boolFlags are treated as value-less boolean flags
// (e.g. --baseline sets flags["baseline"] = "true").
func shellFlags(args []string, boolFlags ...string) (positional []string, flags map[string]string) {
	flags = make(map[string]string)
	bools := make(map[string]bool, len(boolFlags))
	for _, b := range boolFlags {
		bools[b] = true
	}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := args[i][2:]
			if bools[key] {
				flags[key] = "true"
			} else if i+1 < len(args) {
				i++
				flags[key] = args[i]
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return
}

func shellParse(db *storage.DB, args []string) {
	pos, flags := shellFlags(args, "baseline")
	if len(pos) == 0 {
		cError.Fprintln(os.Stderr, "usage: parse <demo.dem> [--player <id>] [--type <type>] [--tier <tag>] [--baseline]")
		return
	}
	demoPath := pos[0]

	var playerID uint64
	if v, ok := flags["player"]; ok {
		playerID, _ = strconv.ParseUint(v, 10, 64)
	}
	mType := "Competitive"
	if v, ok := flags["type"]; ok {
		mType = v
	}
	tier := flags["tier"]
	baseline := flags["baseline"] == "true"

	fmt.Fprintf(os.Stdout, "Parsing %s...\n", demoPath)
	raw, err := parser.ParseDemo(demoPath, mType)
	if err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}

	exists, err := db.DemoExists(raw.DemoHash)
	if err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if exists {
		fmt.Fprintf(os.Stdout, "Demo %s already stored — showing cached results.\n\n", raw.DemoHash[:12])
		shellShow(db, raw.DemoHash[:12], playerID)
		return
	}

	matchStats, roundStats, weaponStats, duelSegs, err := aggregator.Aggregate(raw)
	if err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}

	ctScore, tScore := computeScore(raw.Rounds)
	summary := model.MatchSummary{
		DemoHash:   raw.DemoHash,
		MapName:    raw.MapName,
		MatchDate:  raw.MatchDate,
		MatchType:  raw.MatchType,
		Tickrate:   raw.Tickrate,
		CTScore:    ctScore,
		TScore:     tScore,
		Tier:       tier,
		IsBaseline: baseline,
	}

	if err := db.InsertDemo(summary); err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if err := db.InsertPlayerMatchStats(matchStats); err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if err := db.InsertPlayerRoundStats(roundStats); err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if err := db.InsertPlayerWeaponStats(weaponStats); err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	if err := db.InsertPlayerDuelSegments(duelSegs); err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}

	report.PrintMatchSummary(os.Stdout, summary)
	report.PrintPlayerTable(matchStats, playerID)
	report.PrintDuelTable(os.Stdout, matchStats, playerID)
	report.PrintAWPTable(os.Stdout, matchStats, playerID)
	report.PrintFHHSTable(os.Stdout, duelSegs, matchStats, playerID)
	report.PrintWeaponTable(os.Stdout, weaponStats, matchStats, playerID)
}

func shellFetch(db *storage.DB, args []string) {
	_, flags := shellFlags(args)
	playerQuery := flags["player"]
	if playerQuery == "" {
		cError.Fprintln(os.Stderr, "usage: fetch --player <nickname|id> [--map <map>] [--level <n>] [--count <n>] [--tier <tag>]")
		return
	}
	mapFilter := flags["map"]
	level := 0
	if v, ok := flags["level"]; ok {
		level, _ = strconv.Atoi(v)
	}
	count := 10
	if v, ok := flags["count"]; ok {
		count, _ = strconv.Atoi(v)
	}
	tier := flags["tier"]
	if tier == "" {
		if level > 0 {
			tier = fmt.Sprintf("faceit-%d", level)
		} else {
			tier = "faceit"
		}
	}
	if err := doFetch(db, playerQuery, mapFilter, level, count, tier); err != nil {
		cError.Fprintf(os.Stderr, "error: %v\n", err)
	}
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
