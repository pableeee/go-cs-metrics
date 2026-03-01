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
	replayWorkers int
	replayForce   bool
)

var replayCmd = &cobra.Command{
	Use:   "replay --dir <event_dir>",
	Short: "Re-aggregate .csdem.gz files into the database",
	Long: `Re-aggregate .csdem.gz intermediate files into the metrics database.

Unlike 'parse', replay reads the compact .csdem.gz format instead of full .dem files,
making it much faster and requiring far less memory. Multiple workers are viable.

Useful for:
  - Full DB rebuilds after metric changes (drop + replay all events)
  - Initial ingest after 'convert' has produced .csdem.gz files

Example:
  go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/
  go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/ --force   # re-aggregate all`,
	RunE: runReplay,
}

func init() {
	replayCmd.Flags().StringVar(&replayDir, "dir", "", "directory containing .csdem.gz files (required)")
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
	uf          *model.UnifiedMatchFile
	matchStats  []model.PlayerMatchStats
	roundStats  []model.PlayerRoundStats
	weaponStats []model.PlayerWeaponStats
	duelSegs    []model.PlayerDuelSegment
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
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".csdem.gz") {
			paths = append(paths, filepath.Join(replayDir, e.Name()))
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .csdem.gz files found in %s", replayDir)
	}

	// Load event metadata from event.json sidecar (same as parse).
	meta := loadDemoMeta(replayDir)
	effectiveEventID := ""
	if meta != nil && meta.EventID != "" {
		effectiveEventID = meta.EventID
		fmt.Fprintf(os.Stderr, "Event: %s (%s), tier=%q\n",
			meta.EventName, meta.EventID, meta.Tier)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	db, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	// Suppress "unknown grenade model N" from demoinfocs (carried through in
	// the raw data; should be silent, but guard anyway).
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

		m := res.uf.Match
		exists, err := db.DemoExists(m.DemoHash)
		if err != nil {
			return fmt.Errorf("check demo %s: %w", name, err)
		}
		if exists && !replayForce {
			fmt.Fprintf(os.Stdout, "  %s  skipped (already in DB)\n", tag)
			skipped++
			return nil
		}

		// Effective tier: from file, overridden by event.json if present.
		effectiveTier := res.uf.Tier
		if meta != nil && meta.Tier != "" {
			effectiveTier = meta.Tier
		}

		ctScore, tScore := replayComputeScore(m.Rounds)
		summary := model.MatchSummary{
			DemoHash:  m.DemoHash,
			MapName:   m.MapName,
			MatchDate: m.MatchDate,
			MatchType: m.MatchType,
			Tickrate:  m.Tickrate,
			CTScore:   ctScore,
			TScore:    tScore,
			Tier:      effectiveTier,
			EventID:   effectiveEventID,
		}

		if err := db.InsertDemo(summary, ""); err != nil {
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

		ctScore2, tScore2 := ctScore, tScore
		fmt.Fprintf(os.Stdout, "  %s  stored: %s  %s  %d–%d  %d players  %d rounds  (%s)\n",
			tag,
			summary.MapName, summary.MatchDate, ctScore2, tScore2,
			len(res.matchStats), len(m.Rounds),
			res.elapsed.Round(time.Millisecond))
		stored++
		return nil
	}

	doReplay := func(job replayJob) replayResult {
		res := replayResult{idx: job.idx, path: job.path}
		t0 := time.Now()

		uf, err := model.LoadUnifiedMatchFile(job.path)
		if err != nil {
			res.err = fmt.Errorf("load: %w", err)
			return res
		}
		res.uf = uf

		raw := uf.Match.ToRawMatch()
		ms, rs, ws, ds, err := aggregator.Aggregate(raw)
		if err != nil {
			res.err = fmt.Errorf("aggregate: %w", err)
			return res
		}
		res.matchStats = ms
		res.roundStats = rs
		res.weaponStats = ws
		res.duelSegs = ds
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
			res.uf = nil
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

// replayComputeScore tallies CT and T round wins from unified rounds.
func replayComputeScore(rounds []model.UnifiedRound) (ctScore, tScore int) {
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
