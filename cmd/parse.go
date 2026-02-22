package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/parser"
	"github.com/pable/go-cs-metrics/internal/report"
	"github.com/pable/go-cs-metrics/internal/storage"
)

// parse command flags.
var (
	// playerSteamID is the optional focus player SteamID64 for highlighted output.
	playerSteamID uint64
	// matchType is the label stored alongside the demo (e.g. "Competitive", "FACEIT").
	matchType string
	// parseTier is the tier label for baseline comparisons (e.g. "faceit-5").
	parseTier string
	// parseBaseline marks the demo as a baseline reference match when true.
	parseBaseline bool
	// parseDir is an optional directory path; all *.dem files inside are parsed.
	parseDir string
	// parseWorkers is the number of parallel parse workers (0 = NumCPU).
	parseWorkers int
)

// parseCmd is the cobra command for parsing a CS2 demo file and storing its metrics.
var parseCmd = &cobra.Command{
	Use:   "parse [<demo.dem>...] [--dir <directory>]",
	Short: "Parse one or more CS2 demo files and store metrics",
	Long: `Parse CS2 demo files and store all metrics in the database.

Single file:
  parse match.dem

Multiple files (shell glob):
  parse /replays/*.dem

Whole directory:
  parse --dir /path/to/replays

When more than one demo is provided, full tables are suppressed and a
brief status line is printed per demo instead. Multiple demos are parsed
and aggregated in parallel (parse+aggregate workers); database writes are
always serialised. Use --workers to control concurrency (default: NumCPU).`,
	Args: cobra.ArbitraryArgs,
	RunE: runParse,
}

func init() {
	parseCmd.Flags().Uint64Var(&playerSteamID, "player", 0, "focus player SteamID64")
	parseCmd.Flags().StringVar(&matchType, "type", "Competitive", "match type label")
	parseCmd.Flags().StringVar(&parseTier, "tier", "", "tier label for baseline comparisons (e.g. faceit-5)")
	parseCmd.Flags().BoolVar(&parseBaseline, "baseline", false, "mark this demo as a baseline reference match")
	parseCmd.Flags().StringVar(&parseDir, "dir", "", "directory containing .dem files to parse in bulk")
	parseCmd.Flags().IntVar(&parseWorkers, "workers", 0, "parallel parse+aggregate workers (0 = NumCPU)")
}

// demoMeta holds the event metadata written by cs-demo-downloader into event.json
// alongside each event's demo files.
type demoMeta struct {
	EventID   string `json:"event_id"`
	EventName string `json:"event_name"`
	Tier      string `json:"tier"`
}

// loadDemoMeta reads event.json from dir. Returns nil without error if the file
// is absent (the normal case for demos not managed by demoget).
func loadDemoMeta(dir string) *demoMeta {
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "event.json"))
	if err != nil {
		return nil
	}
	var m demoMeta
	if err := json.Unmarshal(data, &m); err != nil {
		fmt.Fprintf(os.Stderr, "warn: parse event.json in %s: %v\n", dir, err)
		return nil
	}
	return &m
}

// parseJob carries the input for one parse worker.
type parseJob struct {
	idx  int
	path string
}

// parseResult carries the output of one parse+aggregate cycle.
type parseResult struct {
	idx          int
	path         string
	raw          *model.RawMatch // nil on error
	matchStats   []model.PlayerMatchStats
	roundStats   []model.PlayerRoundStats
	weaponStats  []model.PlayerWeaponStats
	duelSegs     []model.PlayerDuelSegment
	parseElapsed time.Duration
	aggElapsed   time.Duration
	err          error
}

// runDemoWorker consumes parseJobs, calls ParseDemo+Aggregate for each, and
// sends a parseResult to results. It exits when jobs is closed.
func runDemoWorker(jobs <-chan parseJob, results chan<- parseResult, mt string) {
	for job := range jobs {
		res := parseResult{idx: job.idx, path: job.path}

		t0 := time.Now()
		raw, err := parser.ParseDemo(job.path, mt)
		res.parseElapsed = time.Since(t0)
		if err != nil {
			res.err = fmt.Errorf("parse: %w", err)
			results <- res
			continue
		}
		res.raw = raw

		t1 := time.Now()
		ms, rs, ws, ds, err := aggregator.Aggregate(raw)
		res.aggElapsed = time.Since(t1)
		if err != nil {
			res.err = fmt.Errorf("aggregate: %w", err)
			results <- res
			continue
		}
		res.matchStats = ms
		res.roundStats = rs
		res.weaponStats = ws
		res.duelSegs = ds
		results <- res
	}
}

// runParse parses one or more demo files, aggregates metrics, stores them in
// the database, and prints report tables. When more than one demo is provided
// (via args or --dir), full tables are suppressed and a brief status line is
// printed per demo instead. Multiple demos are parsed in parallel via a worker
// pool; all DB writes happen on the calling goroutine to avoid SQLite contention.
func runParse(cmd *cobra.Command, args []string) error {
	// Collect demo paths from positional args and --dir.
	paths := append([]string(nil), args...)
	if parseDir != "" {
		entries, err := os.ReadDir(parseDir)
		if err != nil {
			return fmt.Errorf("read dir: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".dem" {
				paths = append(paths, filepath.Join(parseDir, e.Name()))
			}
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("no demo files specified; provide file args or --dir")
	}

	// Load event metadata from the event.json sidecar written by demoget.
	// --dir is the canonical location; fall back to the directory of the first file.
	metaDir := parseDir
	if metaDir == "" {
		metaDir = filepath.Dir(paths[0])
	}
	meta := loadDemoMeta(metaDir)

	// Effective tier: flag takes precedence, sidecar fills the gap, empty is fine.
	effectiveTier := parseTier
	effectiveEventID := ""
	if meta != nil {
		if effectiveTier == "" {
			effectiveTier = meta.Tier
		}
		effectiveEventID = meta.EventID
		if meta.EventID != "" {
			fmt.Fprintf(os.Stderr, "Event: %s (%s), tier=%q\n",
				meta.EventName, meta.EventID, meta.Tier)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	// Redirect os.Stderr through a pipe for the duration of all parsing.
	// A single filter goroutine silently drops "unknown grenade model N" lines
	// that the demoinfocs-golang library prints directly to os.Stderr for
	// Source 2 grenade entities whose model hash it hasn't indexed yet; all
	// other lines are forwarded to the real stderr unchanged.
	//
	// Using a single pipe shared across all workers is safe: POSIX guarantees
	// that concurrent pipe writes ≤ PIPE_BUF bytes are atomic, and each
	// "unknown grenade model N" line is well under that limit.
	origStderr := os.Stderr
	pr, pw, pipeErr := os.Pipe()
	var stderrDone chan struct{}
	if pipeErr == nil {
		os.Stderr = pw
		stderrDone = make(chan struct{})
		go func() {
			defer close(stderrDone)
			sc := bufio.NewScanner(pr)
			for sc.Scan() {
				line := sc.Text()
				if !strings.HasPrefix(line, "unknown grenade model ") {
					fmt.Fprintln(origStderr, line)
				}
			}
		}()
	}

	// restoreStderr closes the write end of the pipe (signalling EOF to the
	// filter goroutine), restores os.Stderr, and waits for all buffered output
	// to drain. Idempotent via sync.Once.
	var restoreOnce sync.Once
	restoreStderr := func() {
		restoreOnce.Do(func() {
			if pipeErr == nil {
				pw.Close()
				os.Stderr = origStderr
				<-stderrDone
			}
		})
	}
	defer restoreStderr()

	// ── Single-file path ─────────────────────────────────────────────────────
	// Parse sequentially and print full report tables.
	if len(paths) == 1 {
		demoPath := paths[0]
		fmt.Fprintf(os.Stdout, "Parsing %s...\n", demoPath)

		t0 := time.Now()
		raw, err := parser.ParseDemo(demoPath, matchType)
		parseElapsed := time.Since(t0)
		restoreStderr() // no more library stderr output after this point
		if err != nil {
			return fmt.Errorf("parse demo: %w", err)
		}

		exists, err := db.DemoExists(raw.DemoHash)
		if err != nil {
			return fmt.Errorf("check demo: %w", err)
		}
		if exists {
			fmt.Fprintf(os.Stdout, "Demo %s already stored — showing cached results.\n\n", raw.DemoHash[:12])
			return showByHash(db, raw.DemoHash)
		}

		t1 := time.Now()
		matchStats, roundStats, weaponStats, duelSegs, err := aggregator.Aggregate(raw)
		aggElapsed := time.Since(t1)
		if err != nil {
			return fmt.Errorf("aggregate: %w", err)
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
			Tier:       effectiveTier,
			IsBaseline: parseBaseline,
			EventID:    effectiveEventID,
		}

		if err := db.InsertDemo(summary); err != nil {
			return fmt.Errorf("insert demo: %w", err)
		}
		if err := db.InsertPlayerMatchStats(matchStats); err != nil {
			return fmt.Errorf("insert player stats: %w", err)
		}
		if err := db.InsertPlayerRoundStats(roundStats); err != nil {
			return fmt.Errorf("insert round stats: %w", err)
		}
		if err := db.InsertPlayerWeaponStats(weaponStats); err != nil {
			return fmt.Errorf("insert weapon stats: %w", err)
		}
		if err := db.InsertPlayerDuelSegments(duelSegs); err != nil {
			return fmt.Errorf("insert duel segments: %w", err)
		}

		fmt.Fprintf(os.Stdout, "  parse: %s  aggregate: %s  total: %s\n\n",
			parseElapsed.Round(time.Millisecond),
			aggElapsed.Round(time.Millisecond),
			(parseElapsed+aggElapsed).Round(time.Millisecond))

		clutch, err := db.GetClutchStatsByDemo(summary.DemoHash)
		if err != nil {
			return fmt.Errorf("get clutch stats: %w", err)
		}
		report.PrintMatchSummary(os.Stdout, summary)
		report.PrintPlayerRosterTable(os.Stdout, matchStats)
		report.PrintPlayerTable(matchStats, playerSteamID)
		report.PrintDuelTable(os.Stdout, matchStats, playerSteamID)
		report.PrintAWPTable(os.Stdout, matchStats, playerSteamID)
		report.PrintWeaponTable(os.Stdout, weaponStats, matchStats, playerSteamID)
		report.PrintAimTimingTable(os.Stdout, matchStats, playerSteamID)
		report.PrintMatchClutchTable(os.Stdout, matchStats, clutch)
		return nil
	}

	// ── Bulk path: parallel parse+aggregate, serial DB writes ────────────────
	numWorkers := parseWorkers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	if numWorkers > len(paths) {
		numWorkers = len(paths)
	}

	fmt.Fprintf(os.Stdout, "Parsing %d demos with %d worker(s)...\n", len(paths), numWorkers)

	jobs := make(chan parseJob, numWorkers)
	resultsCh := make(chan parseResult, numWorkers)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runDemoWorker(jobs, resultsCh, matchType)
		}()
	}

	// Feed all jobs; close the channel when done so workers exit.
	go func() {
		for i, p := range paths {
			jobs <- parseJob{idx: i, path: p}
		}
		close(jobs)
	}()

	// Close resultsCh once all workers have finished so the writer loop exits.
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var stored, skipped, failed int

	for res := range resultsCh {
		name := filepath.Base(res.path)
		tag := fmt.Sprintf("[%d/%d] %s", res.idx+1, len(paths), name)

		if res.err != nil {
			fmt.Fprintf(origStderr, "  %s  error: %v\n", tag, res.err)
			failed++
			continue
		}

		exists, err := db.DemoExists(res.raw.DemoHash)
		if err != nil {
			return fmt.Errorf("check demo %s: %w", name, err)
		}
		if exists {
			fmt.Fprintf(os.Stdout, "  %s  skipped (already stored)\n", tag)
			skipped++
			continue
		}

		ctScore, tScore := computeScore(res.raw.Rounds)
		summary := model.MatchSummary{
			DemoHash:   res.raw.DemoHash,
			MapName:    res.raw.MapName,
			MatchDate:  res.raw.MatchDate,
			MatchType:  res.raw.MatchType,
			Tickrate:   res.raw.Tickrate,
			CTScore:    ctScore,
			TScore:     tScore,
			Tier:       effectiveTier,
			IsBaseline: parseBaseline,
			EventID:    effectiveEventID,
		}

		if err := db.InsertDemo(summary); err != nil {
			return fmt.Errorf("insert demo: %w", err)
		}
		if err := db.InsertPlayerMatchStats(res.matchStats); err != nil {
			return fmt.Errorf("insert player stats: %w", err)
		}
		if err := db.InsertPlayerRoundStats(res.roundStats); err != nil {
			return fmt.Errorf("insert round stats: %w", err)
		}
		if err := db.InsertPlayerWeaponStats(res.weaponStats); err != nil {
			return fmt.Errorf("insert weapon stats: %w", err)
		}
		if err := db.InsertPlayerDuelSegments(res.duelSegs); err != nil {
			return fmt.Errorf("insert duel segments: %w", err)
		}

		fmt.Fprintf(os.Stdout, "  %s  stored: %s  %s  %d–%d  %d players  %d rounds  (parse %s  agg %s  total %s)\n",
			tag,
			summary.MapName, summary.MatchDate, ctScore, tScore,
			len(res.matchStats), len(res.raw.Rounds),
			res.parseElapsed.Round(time.Millisecond),
			res.aggElapsed.Round(time.Millisecond),
			(res.parseElapsed+res.aggElapsed).Round(time.Millisecond))
		stored++
	}

	restoreStderr()
	fmt.Fprintf(os.Stdout, "\nDone: %d stored, %d skipped, %d failed (total %d)\n",
		stored, skipped, failed, len(paths))
	return nil
}

// computeScore tallies the CT and T round wins from the parsed round data.
func computeScore(rounds []model.RawRound) (ctScore, tScore int) {
	for _, r := range rounds {
		switch r.WinnerTeam {
		case model.TeamCT:
			ctScore++
		case model.TeamT:
			tScore++
		}
	}
	return
}

// showByHash loads a previously stored demo by its full hash and prints all
// report tables. Used when a re-parsed demo is already in the database.
func showByHash(db *storage.DB, hash string) error {
	demo, err := db.GetDemoByPrefix(hash)
	if err != nil || demo == nil {
		return fmt.Errorf("demo not found: %s", hash)
	}
	stats, err := db.GetPlayerMatchStats(hash)
	if err != nil {
		return err
	}
	sideStats, err := db.GetPlayerSideStats(hash)
	if err != nil {
		return err
	}
	weaponStats, err := db.GetPlayerWeaponStats(hash)
	if err != nil {
		return err
	}
	clutch, err := db.GetClutchStatsByDemo(hash)
	if err != nil {
		return fmt.Errorf("get clutch stats: %w", err)
	}
	report.PrintMatchSummary(os.Stdout, *demo)
	report.PrintPlayerRosterTable(os.Stdout, stats)
	report.PrintPlayerTable(stats, playerSteamID)
	report.PrintPlayerSideTable(os.Stdout, sideStats, playerSteamID)
	report.PrintDuelTable(os.Stdout, stats, playerSteamID)
	report.PrintAWPTable(os.Stdout, stats, playerSteamID)
	report.PrintWeaponTable(os.Stdout, weaponStats, stats, playerSteamID)
	report.PrintAimTimingTable(os.Stdout, stats, playerSteamID)
	report.PrintMatchClutchTable(os.Stdout, stats, clutch)
	return nil
}
