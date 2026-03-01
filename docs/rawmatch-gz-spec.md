# Spec: RawMatch Intermediate Format (`.rawmatch.gz`)

## Motivation

Demo files are 200–750 MB each; the current archive is ~526 GB. When the
aggregator changes (new metrics, model updates, pass logic fixes), all affected
demos must be re-downloaded and re-parsed through the full demoinfocs frame-walk
(~minutes per demo, ~29 GB RAM peaks). The parser already produces a clean
intermediate value — `*model.RawMatch` — before handing off to the 11-pass
aggregator. Serializing that struct to disk enables the `.dem` file to be deleted
while still supporting full re-aggregation later.

Expected size reduction: **300–1000× per demo** (e.g. 300 MB `.dem` → ~500 KB `.rawmatch.gz`).

---

## Format

**Wrapper struct** (`internal/model/rawmatch_file.go`):

```go
type RawMatchFile struct {
    Version  int       `json:"version"`   // = 1; bump on breaking schema changes
    Tier     string    `json:"tier"`      // "pro", "faceit-5", etc. — from --tier at parse time
    RawMatch *RawMatch `json:"raw_match"`
}
const RawMatchFileVersion = 1
```

Serialized as **gzip-compressed JSON** (`encoding/json` + `compress/gzip`, standard
library only — zero new dependencies).

**File naming:** same directory as the `.dem`, same base name, `.rawmatch.gz` extension.

```
~/demos/pro/iem_katowice_2025/liquid-vs-spirit-m1-nuke.dem
~/demos/pro/iem_katowice_2025/liquid-vs-spirit-m1-nuke.rawmatch.gz
```

The `Tier` field is baked in at parse time so `replay` requires no `--tier` flag.
`RawMatch.MatchDate` (set from file mtime during parsing) is preserved in the
serialized file, so re-aggregation uses the original match date regardless of
when or where replay runs.

---

## New User Workflow

```sh
# Initial parse (unchanged behaviour, plus saves .rawmatch.gz)
go-cs-metrics parse --dir ~/demos/pro/iem_katowice_2025/ --tier pro --save-raw

# Free disk space whenever convenient (manual step, user decides)
rm ~/demos/pro/iem_katowice_2025/*.dem

# After metrics/model change: re-aggregate from .rawmatch.gz, no .dem needed
go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/

# Or re-aggregate all events at once
for dir in ~/demos/pro/*/; do
  go-cs-metrics replay --dir "$dir"
done
```

---

## Implementation

### Files to Create / Modify

| File | Action | Status |
|------|--------|--------|
| `internal/model/rawmatch_file.go` | **Create** — `RawMatchFile` struct + Save/Load | ✅ done |
| `cmd/parse.go` | **Modify** — add `--save-raw` flag and call sites | ✅ done |
| `cmd/replay.go` | **Create** — `replay` command | ⬜ todo |
| `cmd/root.go` | **Modify** — register `replay` | ⬜ todo |
| `docs/cs2-pipeline-flow.md` | **Modify** — document new format + commands | ⬜ todo |

---

### 1. `internal/model/rawmatch_file.go` ✅

- `RawMatchFile` struct (version + tier + `*RawMatch`)
- `func (r *RawMatchFile) Save(path string) error`
  - `json.Encode` → `gzip.NewWriter` → write atomically (temp file + rename)
- `func LoadRawMatchFile(path string) (*RawMatchFile, error)`
  - open → `gzip.NewReader` → `json.Decode`
  - validates `Version == RawMatchFileVersion` and `RawMatch != nil`
- No new imports beyond standard library

### 2. `cmd/parse.go` ✅

- New bool flag `--save-raw` (default false), stored in `parseSaveRaw`
- Helper `saveRawMatchFile(demPath, tier string, raw *model.RawMatch)`:
  - derives output path by replacing `.dem` with `.rawmatch.gz`
  - builds `RawMatchFile{Version: 1, Tier: tier, RawMatch: raw}` and calls `.Save()`
  - logs to stderr on error (non-fatal; DB write proceeds regardless)
- Call site in **single-file path**: after aggregate, before `db.InsertDemo`
- Call site in **`writeDemoResult`** (bulk path): after `DemoExists` returns false,
  before `db.InsertDemo` — so skipped demos don't get a raw file written

### 3. `cmd/replay.go` ⬜

```
go-cs-metrics replay --dir <event_dir> [--workers N] [--force]
```

Behaviour:
- Glob `*.rawmatch.gz` in `--dir` (non-recursive, same as `parse --dir`)
- For each file (parallel load+aggregate, sequential DB write — mirrors `parse`):
  1. `LoadRawMatchFile(path)` → `*RawMatchFile`
  2. Check `db.DemoExists(raw.DemoHash)` — skip unless `--force`
  3. `aggregator.Aggregate(rawMatchFile.RawMatch)` → 4 stat slices
  4. Read `event.json` sidecar from directory for `event_id` (same as `parse.go`)
  5. `db.InsertDemo(summary, "")` + all four `InsertPlayer*` calls (INSERT OR REPLACE)
- Progress: `[N/M] replayed <filename>` (same style as `parse`)
- `--workers` default 1 (aggregator is CPU-heavy; same default as `parse`)
- `--force` re-aggregates even if hash already in DB

Key implementation notes:
- `MatchSummary` fields from raw match + tier from `RawMatchFile.Tier` + eventID from `event.json`
- `CTScore`/`TScore` via existing `computeScore(raw.Rounds)` helper (already in parse.go)
- Sequential path should `debug.FreeOSMemory()` between files (same reason as parse)
- No quickHash available for raw files — skip the quick-hash pre-check; use full `DemoExists`

```go
// Skeleton
func newReplayCmd() *cobra.Command {
    var replayDir string
    var replayWorkers int
    var replayForce bool

    cmd := &cobra.Command{
        Use:   "replay --dir <event_dir>",
        Short: "Re-aggregate .rawmatch.gz files into the database without the original .dem files",
        Long:  `...`,
        RunE: func(cmd *cobra.Command, args []string) error {
            return runReplay(replayDir, replayWorkers, replayForce)
        },
    }
    cmd.Flags().StringVar(&replayDir, "dir", "", "directory containing .rawmatch.gz files (required)")
    cmd.Flags().IntVar(&replayWorkers, "workers", 1, "parallel load+aggregate workers")
    cmd.Flags().BoolVar(&replayForce, "force", false, "re-aggregate even if demo hash already in DB")
    cmd.MarkFlagRequired("dir")
    return cmd
}
```

### 4. `cmd/root.go` ⬜

Add inside `init()`:
```go
rootCmd.AddCommand(newReplayCmd())
```

### 5. `docs/cs2-pipeline-flow.md` ⬜

Add a new **Step 3b — Save Raw / Replay** section between Step 3 (parse) and Step 4 (export):

- Document `.rawmatch.gz` format (fields, version, gzip+JSON)
- Document `parse --save-raw` flag
- Document `replay` command with flags table
- Note: skip-by-default behaviour; `--force` for full refresh after model change
- Update Shared State table to include `~/demos/pro/<event-slug>/*.rawmatch.gz`
- Update Step 3 flags table to include `--save-raw`

---

## Verification

```sh
cd ~/git/cs/go-cs-metrics

# Build
go build ./...

# Parse one event with --save-raw
./go-cs-metrics parse --dir ~/demos/pro/iem_katowice_2025/ --tier pro --save-raw --workers 1
ls -lh ~/demos/pro/iem_katowice_2025/*.rawmatch.gz   # expect ~300–700 KB each

# Inspect one file (should be valid JSON)
gunzip -c ~/demos/pro/iem_katowice_2025/<some>.rawmatch.gz | python3 -m json.tool | head -40

# Drop DB and re-aggregate from raw files only (no .dem needed)
./go-cs-metrics drop --force
./go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/
./go-cs-metrics list    # confirm demos re-appear

# Verify stats match original parse (spot-check a player)
./go-cs-metrics player <steam_id> --since 90

# Confirm --force re-runs on already-stored demos
./go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/ --force

# Run tests
go test ./...
```
