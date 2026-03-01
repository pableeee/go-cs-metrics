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

	"github.com/pable/go-cs-metrics/internal/converter"
)

var (
	convertDir     string
	convertTier    string
	convertWorkers int
	convertForce   bool
)

var convertCmd = &cobra.Command{
	Use:   "convert --dir <event_dir> --tier <tier>",
	Short: "Convert .dem files to .csdem.gz intermediate format",
	Long: `Parse CS2 demo files and write them as .csdem.gz files alongside the originals.

The .csdem.gz format is ~150–500× smaller than a raw .dem and contains all data
needed by both go-cs-metrics (replay) and cs-demo-viewer (2D replay).

After converting, the original .dem files may be deleted to reclaim disk space.
Use 'replay' to re-ingest from .csdem.gz files without needing the originals.

Example:
  GOMEMLIMIT=4294967296 go-cs-metrics convert --dir ~/demos/pro/iem_katowice_2025/ --tier pro`,
	RunE: runConvert,
}

func init() {
	convertCmd.Flags().StringVar(&convertDir, "dir", "", "directory containing .dem files (required)")
	convertCmd.Flags().StringVar(&convertTier, "tier", "", "tier label, e.g. pro (required)")
	convertCmd.Flags().IntVar(&convertWorkers, "workers", 1, "parallel conversion workers (default 1; see GOMEMLIMIT note)")
	convertCmd.Flags().BoolVar(&convertForce, "force", false, "overwrite existing .csdem.gz files")
	convertCmd.MarkFlagRequired("dir")
	convertCmd.MarkFlagRequired("tier")
}

// convertJob carries one file to be converted.
type convertJob struct {
	idx  int
	path string
}

// convertResult carries the outcome of one conversion.
type convertResult struct {
	idx     int
	path    string
	elapsed time.Duration
	err     error
	skipped bool
}

func runConvert(cmd *cobra.Command, args []string) error {
	entries, err := os.ReadDir(convertDir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".dem" {
			paths = append(paths, filepath.Join(convertDir, e.Name()))
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("no .dem files found in %s", convertDir)
	}

	// Suppress "unknown grenade model N" lines from demoinfocs stderr.
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

	fmt.Fprintf(os.Stdout, "Converting %d demos with %d worker(s)...\n", len(paths), convertWorkers)

	var converted, skipped, failed int

	writeResult := func(res convertResult) {
		name := filepath.Base(res.path)
		tag := fmt.Sprintf("[%d/%d] %s", res.idx+1, len(paths), name)
		switch {
		case res.err != nil:
			fmt.Fprintf(origStderr, "  %s  error: %v\n", tag, res.err)
			failed++
		case res.skipped:
			fmt.Fprintf(os.Stdout, "  %s  skipped (already exists)\n", tag)
			skipped++
		default:
			fmt.Fprintf(os.Stdout, "  %s  converted  (%s)\n", tag, res.elapsed.Round(time.Millisecond))
			converted++
		}
	}

	doConvert := func(job convertJob) convertResult {
		res := convertResult{idx: job.idx, path: job.path}
		outPath := replaceExtConvert(job.path, ".csdem.gz")
		if !convertForce {
			if _, err := os.Stat(outPath); err == nil {
				res.skipped = true
				return res
			}
		}
		t0 := time.Now()
		uf, err := converter.ConvertDemo(job.path, "Competitive")
		if err != nil {
			res.err = fmt.Errorf("convert: %w", err)
			return res
		}
		uf.Tier = convertTier
		if err := uf.Save(outPath); err != nil {
			res.err = fmt.Errorf("save: %w", err)
			return res
		}
		res.elapsed = time.Since(t0)
		return res
	}

	numWorkers := convertWorkers
	if numWorkers <= 0 {
		numWorkers = 1
	}

	if numWorkers == 1 {
		for i, path := range paths {
			res := doConvert(convertJob{idx: i, path: path})
			writeResult(res)
			// Free OS memory between demos to prevent RSS accumulation.
			debug.FreeOSMemory()
		}
	} else {
		jobs := make(chan convertJob, numWorkers)
		resultsCh := make(chan convertResult, numWorkers)
		var wg sync.WaitGroup
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					resultsCh <- doConvert(job)
				}
			}()
		}
		go func() {
			for i, path := range paths {
				jobs <- convertJob{idx: i, path: path}
			}
			close(jobs)
		}()
		go func() {
			wg.Wait()
			close(resultsCh)
		}()
		for res := range resultsCh {
			writeResult(res)
		}
	}

	restoreStderr()
	fmt.Fprintf(os.Stdout, "\nDone: %d converted, %d skipped, %d failed (total %d)\n",
		converted, skipped, failed, len(paths))
	return nil
}

// replaceExtConvert replaces the file extension in path with ext.
func replaceExtConvert(path, ext string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i] + ext
	}
	return path + ext
}

