package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/pable/go-cs-metrics/internal/aggregator"
	"github.com/pable/go-cs-metrics/internal/model"
	"github.com/pable/go-cs-metrics/internal/storage"
)

var (
	replayDir     string
	replayTier    string
	replayWorkers int
	replayForce   bool
)

var replayCmd = &cobra.Command{
	Use:   "replay --dir <event_dir>",
	Short: "Re-aggregate .csraw2.tar files into the database",
	Long: `Re-aggregate .csraw2.tar intermediate files into the metrics database.

Unlike 'parse', replay only reads the compact .csraw2.tar format instead of
full .dem files, so no demoinfocs work is needed and many workers are viable.

Useful for:
  - Full DB rebuilds after metric changes (drop + replay all events)
  - Initial ingest after 'convert' has produced .csraw2.tar files

Example:
  go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/
  go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/ --force   # re-aggregate all`,
	RunE: runReplay,
}

func init() {
	replayCmd.Flags().StringVar(&replayDir, "dir", "", "directory containing .csraw2.tar files (required)")
	replayCmd.Flags().StringVar(&replayTier, "tier", "", "tier label override (defaults to value in archive header)")
	replayCmd.Flags().IntVar(&replayWorkers, "workers", 1, "parallel load+aggregate workers")
	replayCmd.Flags().BoolVar(&replayForce, "force", false, "re-aggregate even if demo hash already in DB")
	replayCmd.MarkFlagRequired("dir")
}

// replayJob carries one file to be replayed.
type replayJob struct {
	idx  int
	path string
}

// replayResult carries the outcome of one load+aggregate cycle.
type replayResult struct {
	idx         int
	path        string
	raw         *model.RawMatch
	quickHash   string
	matchStats  []model.PlayerMatchStats
	roundStats  []model.PlayerRoundStats
	weaponStats []model.PlayerWeaponStats
	duelSegs    []model.PlayerDuelSegment
	deathEvents []model.PlayerDeathEvent
	flashEvents []model.FlashEvent
	elapsed     time.Duration
	err         error
}

func runReplay(cmd *cobra.Command, args []string) error {
	entries, err := os.ReadDir(replayDir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".csraw2.tar") {
			paths = append(paths, filepath.Join(replayDir, e.Name()))
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .csraw2.tar files found in %s", replayDir)
	}

	// Load event metadata from event.json sidecar (same as parse).
	meta := loadDemoMeta(replayDir)
	effectiveTier := replayTier
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

	// Replay reads archives only — no demoinfocs is involved, so the parser
	// stderr filter is unnecessary. Still wire a minimal pipe for parity with
	// other commands so any unexpected library noise gets filtered.
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

	fmt.Fprintf(os.Stdout, "Replaying %d files with %d worker(s)...\n", len(paths), replayWorkers)

	var stored, skipped, failed int

	writeReplayResult := func(res replayResult) error {
		name := filepath.Base(res.path)
		tag := fmt.Sprintf("[%d/%d] %s", res.idx+1, len(paths), name)

		if res.err != nil {
			fmt.Fprintf(origStderr, "  %s  error: %v\n", tag, res.err)
			failed++
			return nil
		}

		raw := res.raw
		exists, err := db.DemoExists(raw.DemoHash)
		if err != nil {
			return fmt.Errorf("check demo %s: %w", name, err)
		}
		if exists && !replayForce {
			fmt.Fprintf(os.Stdout, "  %s  skipped (already in DB)\n", tag)
			skipped++
			return nil
		}

		ctScore, tScore := computeScore(raw.Rounds)
		// Tier precedence: --tier flag / event.json sidecar, then the
		// archive header (matches the --tier flag's documented default).
		demoTier := effectiveTier
		if demoTier == "" {
			demoTier = raw.Tier
		}
		summary := model.MatchSummary{
			DemoHash:  raw.DemoHash,
			MapName:   raw.MapName,
			MatchDate: raw.MatchDate,
			MatchType: raw.MatchType,
			Tickrate:  raw.Tickrate,
			CTScore:   ctScore,
			TScore:    tScore,
			Tier:      demoTier,
			EventID:   effectiveEventID,
		}

		if err := db.InsertDemo(summary, res.quickHash); err != nil {
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
		if err := db.InsertGrenadeEvents(raw.DemoHash, raw.MatchDate, raw.MapName, raw.Grenades); err != nil {
			return fmt.Errorf("insert grenade events: %w", err)
		}
		if err := db.InsertPlayerDeathEvents(raw.DemoHash, res.deathEvents); err != nil {
			return fmt.Errorf("insert death events: %w", err)
		}
		if err := db.InsertFlashEvents(raw.DemoHash, res.flashEvents); err != nil {
			return fmt.Errorf("insert flash events: %w", err)
		}

		fmt.Fprintf(os.Stdout, "  %s  stored: %s  %s  %d–%d  %d players  %d rounds  (%s)\n",
			tag,
			summary.MapName, summary.MatchDate, ctScore, tScore,
			len(res.matchStats), len(raw.Rounds),
			res.elapsed.Round(time.Millisecond))
		stored++
		return nil
	}

	doReplay := func(job replayJob) replayResult {
		res := replayResult{idx: job.idx, path: job.path}
		t0 := time.Now()

		raw, qh, err := loadAndBridge(job.path, "Competitive", effectiveTier)
		if err != nil {
			res.err = fmt.Errorf("load: %w", err)
			return res
		}
		res.raw = raw
		res.quickHash = qh

		ms, rs, ws, ds, des, fes, err := aggregator.Aggregate(raw)
		if err != nil {
			res.err = fmt.Errorf("aggregate: %w", err)
			return res
		}
		res.matchStats = ms
		res.roundStats = rs
		res.weaponStats = ws
		res.duelSegs = ds
		res.deathEvents = des
		res.flashEvents = fes
		res.elapsed = time.Since(t0)
		return res
	}

	numWorkers := replayWorkers
	if numWorkers <= 0 {
		numWorkers = 1
	}

	if numWorkers == 1 {
		for i, path := range paths {
			res := doReplay(replayJob{idx: i, path: path})
			if err := writeReplayResult(res); err != nil {
				return err
			}
			// Release references and return heap pages before next file.
			res.raw = nil
			res.matchStats = nil
			res.roundStats = nil
			res.weaponStats = nil
			res.duelSegs = nil
			debug.FreeOSMemory()
		}
	} else {
		jobs := make(chan replayJob, numWorkers)
		resultsCh := make(chan replayResult, numWorkers)
		var wg sync.WaitGroup
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					resultsCh <- doReplay(job)
				}
			}()
		}
		go func() {
			for i, path := range paths {
				jobs <- replayJob{idx: i, path: path}
			}
			close(jobs)
		}()
		go func() {
			wg.Wait()
			close(resultsCh)
		}()
		for res := range resultsCh {
			if err := writeReplayResult(res); err != nil {
				return err
			}
		}
	}

	restoreStderr()
	fmt.Fprintf(os.Stdout, "\nDone: %d stored, %d skipped, %d failed (total %d)\n",
		stored, skipped, failed, len(paths))
	return nil
}
