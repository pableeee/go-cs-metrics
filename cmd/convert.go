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

	"github.com/pable/go-cs-metrics/internal/csraw2"
	"github.com/pable/go-cs-metrics/internal/parserv2"
)

var (
	convertDir     string
	convertOutDir  string
	convertTier    string
	convertWorkers int
	convertForce   bool
)

var convertCmd = &cobra.Command{
	Use:   "convert --dir <event_dir> --tier <tier>",
	Short: "Convert .dem files to .csraw2.tar intermediate format",
	Long: `Parse CS2 demo files and write them as .csraw2.tar files alongside the originals.

The .csraw2.tar format is a tar archive of header.json + per-stream parquet
files (see docs/csraw-v2-spec.md). It captures every gameplay-relevant field
the parser sees, so re-aggregation never needs to re-read the .dem.

After converting, the original .dem files may be deleted to reclaim disk space.
Use 'replay' to re-ingest from .csraw2.tar files without needing the originals.

Example:
  GOMEMLIMIT=4294967296 go-cs-metrics convert --dir ~/demos/pro/iem_katowice_2025/ --tier pro
  GOMEMLIMIT=4294967296 go-cs-metrics convert --dir ~/demos/pro/iem_katowice_2025/ --tier pro --out-dir ~/demos/converted-pro/iem_katowice_2025/`,
	RunE: runConvert,
}

func init() {
	convertCmd.Flags().StringVar(&convertDir, "dir", "", "directory containing .dem files (required)")
	convertCmd.Flags().StringVar(&convertOutDir, "out-dir", "", "output directory for .csraw2.tar files (default: same as --dir)")
	convertCmd.Flags().StringVar(&convertTier, "tier", "", "tier label, e.g. pro (required)")
	convertCmd.Flags().IntVar(&convertWorkers, "workers", 1, "parallel conversion workers (default 1; see GOMEMLIMIT note)")
	convertCmd.Flags().BoolVar(&convertForce, "force", false, "overwrite existing .csraw2.tar files")
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

	outDir := convertOutDir
	if outDir == "" {
		outDir = convertDir
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
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
		base := filepath.Base(replaceExtConvert(job.path, ".csraw2.tar"))
		outPath := filepath.Join(outDir, base)
		if !convertForce {
			if _, err := os.Stat(outPath); err == nil {
				res.skipped = true
				return res
			}
		}
		t0 := time.Now()
		m, err := parserv2.ParseDemoV2(job.path, convertTier, "competitive")
		if err != nil {
			res.err = fmt.Errorf("parse: %w", err)
			return res
		}
		// Stage to a tmp file and rename so partial archives never appear.
		tmpPath := outPath + ".tmp"
		f, err := os.Create(tmpPath)
		if err != nil {
			res.err = fmt.Errorf("create tmp: %w", err)
			return res
		}
		if err := csraw2.Write(f, m); err != nil {
			f.Close()
			os.Remove(tmpPath)
			res.err = fmt.Errorf("write archive: %w", err)
			return res
		}
		if err := f.Close(); err != nil {
			os.Remove(tmpPath)
			res.err = fmt.Errorf("close tmp: %w", err)
			return res
		}
		if err := os.Rename(tmpPath, outPath); err != nil {
			os.Remove(tmpPath)
			res.err = fmt.Errorf("rename: %w", err)
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

	// Exit non-zero when nothing was produced but work was attempted. A total
	// failure usually signals a systematic problem (e.g. a parser/demo-format
	// incompatibility), and callers must not treat it as success — scripts that
	// delete source .dem files on a zero-exit would otherwise lose data.
	if converted == 0 && skipped == 0 && failed > 0 {
		return fmt.Errorf("convert produced no archives: %d/%d demos failed", failed, len(paths))
	}
	return nil
}

// replaceExtConvert replaces the file extension in path with ext.
func replaceExtConvert(path, ext string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i] + ext
	}
	return path + ext
}
