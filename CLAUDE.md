# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go tool for parsing Counter-Strike 2 match demo files (`.dem`) and computing player/team performance metrics. The goal is automated, repeatable analysis: ingest demos, extract events, aggregate metrics, and surface actionable insights (what to train, where performance is weak).

## Build & Test Commands

```sh
go build -o go-cs-metrics .    # build the main binary
go build ./...                  # build all packages (does NOT relink the binary)
go test ./...
go test ./... -run TestName     # single test
go vet ./...
```

## Architecture

The processing pipeline has four stages:

1. **Ingestion** — Accept a `.dem` file, compute its hash, and store it.
2. **Parsing** — Convert the demo into structured, tick-based events (`RawMatch`).
3. **Aggregation** — 11-pass algorithm producing `[]PlayerMatchStats`, `[]PlayerRoundStats`, `[]PlayerWeaponStats`, `[]PlayerDuelSegment`.
4. **Presentation** — CLI output via `tablewriter`; storage is SQLite.

Storage: **SQLite** via `modernc.org/sqlite` (pure Go, no CGo). Default DB: `~/.csmetrics/metrics.db`.

## CLI Commands

| Command | Description |
|---------|-------------|
| `parse [<demo.dem>...] [--dir <dir>]` | Parse + store one or more demos; bulk mode parses in parallel (`--workers N`, default `NumCPU`) with serialised DB writes; prints compact status per demo |
| `list` | List all stored demos |
| `show <hash-prefix>` | Re-display a stored demo's tables |
| `fetch` | *(disabled — not registered as a CLI command; non-functional due to platform auth changes; see `docs/demo-download-automation.md`)* |
| `player <steamid64>...` | Cross-match aggregate report for one or more players (`--map`, `--since`, `--last` filters); `--top N` appends the top N players by Rating 2.0 proxy for comparison |
| `rounds <hash-prefix> <steamid64>` | Per-round drill-down with buy type, flags (POST_PLT, CLUTCH_1vN); `--clutch`, `--post-plant`, `--side`, `--buy` filters |
| `trend <steamid64>` | Chronological per-match performance trend (KPR/ADR/KAST% + TTK/TTD/CS%) |
| `sql <query>` | Run an arbitrary SQL query against the metrics database; prints results as a table |
| `drop [--force]` | Delete the metrics database file; requires `--force` to actually delete |
| `analyze player <steamid64> <question>` | AI-powered grounded analysis of a player's aggregate stats (requires `ANTHROPIC_API_KEY`) |
| `analyze match <hash-prefix> <question>` | AI-powered grounded analysis of a single match (requires `ANTHROPIC_API_KEY`) |
| `export` | Export team stats as a simbo3-compatible JSON file (`--team`, `--players`, `--roster`, `--since`, `--quorum`, `--out`); see Integration section |
| `summary` | High-level database overview: match count, date range, map breakdown, top players, match type distribution |

All commands share `--db` to point at an alternate database and `--silent` / `-s` to suppress column legends (verbose output is on by default).

## Data Model

Core types (all in `internal/model/model.go`):

- **`PlayerMatchStats`** — aggregated metrics per player per demo (35+ columns)
- **`PlayerRoundStats`** — per-round breakdown for drill-down
- **`PlayerWeaponStats`** — per-weapon kill/damage breakdown
- **`PlayerDuelSegment`** — FHHS counts per (weapon_bucket, distance_bin) per demo
- **`PlayerAggregate`** — cross-demo sums/averages used by the `player` command

## Aggregator: 11 Passes

1. Trade annotation (backward + forward scan within 5 s window); captures trade kill/death delay in ticks for timing metrics
2. Opening kills (first kill after `FreezeEndTick`)
3. Per-round per-player stats (buy type, post-plant flag, clutch detection, `won_round` flag)
4. Match-level rollup (includes `rounds_won`, `median_trade_kill_delay_ms`, `median_trade_death_delay_ms`)
5. Crosshair placement (from `RawFirstSight` / `m_bSpottedByMask`)
6. Duel engine + FHHS segments (exposure time, pre-shot correction, weapon+distance bins)
7. AWP death classifier (dry/repeek/isolated)
8. Flash quality window (effective flashes within 1.5 s)
9. Role classification (AWPer/Entry/Support/Rifler)
10. TTK/TTD/one-tap kills (first shot fired → kill, 3 s rolling window)
11. Counter-strafe % (shots fired at horizontal speed ≤ 34 u/s, via `e.Shooter.Velocity()` captured at WeaponFire time)

## Memory Behaviour of the Parser

The demoinfocs library allocates heavily during parsing — each demo creates a large volume of short-lived objects. Memory characteristics measured on WSL2:

| Demo | File size | Peak RSS (default GC) | Peak RSS (GOGC=off) | Notes |
|---|---|---|---|---|
| `g2-vs-mouz-m1-overpass-p3.dem` | 1.1 GB | ~72 MB | ~10 GB | typical large demo |
| `furia-vs-vitality-m2-inferno.dem` | 577 MB | ~29 GB (OOM) | n/a | pathological demo — many events |

**Key insight**: peak RSS is dominated by GC pressure, not file size. Each demo causes ~10 GB of cumulative allocation; the GC normally keeps live heap small, but Go does not return freed pages to the OS promptly. With multiple workers or back-to-back sequential parses, freed-but-not-returned pages accumulate and RSS grows until the process is OOM-killed.

### Required workaround: `GOMEMLIMIT`

Always set `GOMEMLIMIT` when parsing a directory of demos:

```sh
GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir ~/demos/pro/iem_krakow_2026/ --workers 1
```

- `GOMEMLIMIT=4294967296` (4 GB) tells the GC to scavenge and return pages to the OS aggressively. It does **not** hard-cap RSS at 4 GB — it caps the managed heap, so RSS can still exceed 4 GB for pathological demos (observed peak: ~19–24 GB during krakow batch). What it prevents is RSS ballooning to ~29 GB and triggering the WSL OOM killer.
- `--workers 1` ensures demos are parsed sequentially. After each demo, `debug.FreeOSMemory()` is called explicitly (in the sequential code path) and all references to the parsed `RawMatch` are nil'd before the next parse begins.
- **Do not use `--workers > 1`** for large event directories — N concurrent parses multiply the GC pressure and reliably OOM.

### Why `debug.FreeOSMemory()` is needed

Go's background scavenger returns pages to the OS gradually (over seconds). Between sequential demo parses the scavenger may not have finished before the next parse's allocations begin, causing RSS to accumulate across demos. `FreeOSMemory()` performs a full GC cycle and immediately returns all freed pages to the OS, resetting RSS before the next parse.

### Anomalous demos

Some demos (~577 MB) allocate far more than their file size suggests — likely due to high event density (many rounds, extensive utility usage). These demos work fine with `GOMEMLIMIT` set; without it they OOM even as a single-file parse.

### Quick-hash pre-check

The `parse --dir` command computes a SHA-256 of the first 64 KB of each file and checks it against the DB before doing the expensive full parse. Already-stored demos are skipped in milliseconds. This makes re-running `parse --dir` after an interrupted batch essentially free for the already-ingested demos.

## Key Implementation Notes

- **SteamID64 stored as TEXT** — avoids signed integer overflow for IDs above `2^63`.
- **`INSERT OR REPLACE`** everywhere — full idempotency; re-parsing the same demo hash is safe.
- **Wilson CI** used for FHHS proportions (stable for small samples unlike Wald).
- **Distance** computed as `||attackerPos − victimPos|| * 0.01905` (Hammer units → meters).
- **`player` command aggregation**: integers summed directly; float medians averaged across matches (approximate); FHHS rate recomputed from raw segment count totals (accurate).
- **Schema migrations**: new columns are added automatically at startup via `ALTER TABLE ... ADD COLUMN ... DEFAULT` statements (duplicate-column errors silently ignored). Existing rows default to `0`/`''`. A full DB rebuild is only required if a column type or a table structure changes (not just additions).
- **Parse skips already-stored demos**: `parse --dir` skips any demo whose hash is already in the `demos` table. Passing the same directory again after a schema migration will NOT backfill new columns for old rows — see below.
- **`match_date` comes from file mtime**: the parser reads the `.dem` file's filesystem modification time, not anything inside the demo. `demoget sync` sets mtime to the extraction date (today). Always run `demoget touch-dates --out <dir>` after downloading and before the first parse, otherwise every demo gets `match_date = today` and `--since` filtering in `export` breaks silently.
- **`--dir` is not recursive**: only finds `.dem` files directly in the given directory. Pass each event subdirectory individually (`--dir ~/demos/pro/iem_cologne_2025/`), not the parent.

## Recovering from a Schema Migration (New Columns on Old Demos)

When a new column is added to `player_match_stats` or `player_round_stats`, existing rows get the column's `DEFAULT` value (usually `0`). Demos are not re-parsed automatically because the parser skips files whose hash is already stored.

**Which columns can be backfilled with SQL vs. which require a full re-parse:**

| Scenario | Fix |
|---|---|
| New column in `player_match_stats` is derivable from `player_round_stats` | SQL `UPDATE` backfill (fast, no re-parse) |
| New column requires re-running the aggregator (e.g. counter-strafe, TTK, duel engine) | Full re-parse — drop DB and re-parse all demos |

### Example: backfilling `rounds_won`

`rounds_won` in `player_match_stats` is just `SUM(won_round)` from `player_round_stats`. If `player_round_stats.won_round` is correctly populated but `player_match_stats.rounds_won` is all zeros (e.g. after the column was added mid-dataset), run:

```sh
sqlite3 ~/.csmetrics/metrics.db "
UPDATE player_match_stats
SET rounds_won = (
  SELECT COALESCE(SUM(won_round), 0)
  FROM player_round_stats prs
  WHERE prs.demo_hash = player_match_stats.demo_hash
    AND prs.steam_id  = player_match_stats.steam_id
)
WHERE rounds_won = 0;
"
```

Verify with:
```sh
sqlite3 ~/.csmetrics/metrics.db "
SELECT
  SUM(CASE WHEN rounds_won = 0 THEN 1 ELSE 0 END) AS still_zero,
  SUM(CASE WHEN rounds_won > 0 THEN 1 ELSE 0 END) AS populated,
  ROUND(AVG(CAST(rounds_won AS REAL) / NULLIF(rounds_played,0)), 3) AS avg_win_rate
FROM player_match_stats;
"
-- avg_win_rate should be ~0.50; still_zero should be small (legitimate 0-round-win demos only)
```

### Recovering from wrong match_dates (forgot touch-dates)

If `demoget touch-dates` was not run before parsing, all affected demos will have `match_date = <parse date>` instead of the actual match date. This silently breaks `export --since` filtering. There is no SQL-only fix — the correct date can only be obtained by re-parsing the file after fixing its mtime.

```sh
# 1. Fix file mtimes
cd ~/git/cs-demo-downloader
./demoget touch-dates --out ~/demos/pro

# 2. Delete affected demos from DB (all tables, respecting foreign keys)
#    Adjust the WHERE clause to target the specific wrong date(s)
sqlite3 ~/.csmetrics/metrics.db "
DELETE FROM player_duel_segments WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_weapon_stats   WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_round_stats    WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_match_stats    WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro';
"

# 3. Re-parse (now reads correct mtime from fixed files)
for dir in ~/demos/pro/*/; do
  ./go-cs-metrics parse --dir "$dir" --tier pro
done
```

Verify dates look right afterward:
```sh
sqlite3 ~/.csmetrics/metrics.db "SELECT MIN(match_date), MAX(match_date), COUNT(*) FROM demos WHERE tier='pro';"
```

### Full re-parse (when SQL backfill isn't possible)

The parser won't re-parse demos already in the DB. To force a full rebuild:

```sh
./go-cs-metrics drop --force
# Then re-parse all events:
for dir in ~/demos/pro/*/; do
  ./go-cs-metrics parse --dir "$dir" --tier pro
done
```

Note: `--dir` does a flat search for `.dem` files — pass each event subdirectory individually, not the parent `pro/` directory.

## Known Issues / Improvement Backlog

See `docs/prediction-analysis.md` for a full analysis of model accuracy vs actual results across three tier-1 events (Budapest Major 2025, IEM Krakow 2026, PGL Cluj-Napoca 2026).

Priority issues identified:
1. ~~**simbo3 map pool is stale**~~ — fixed: pool updated to Mirage/Inferno/Nuke/Ancient/Overpass/Dust2/Train.
2. **PARIVISION stand-in skew** — `zweih` (SteamID `76561198210626739`) is a current PARIVISION player but was a stand-in for other matches in 2025 (38 demos vs ~9 for the rest of the lineup), inflating his contribution to the export.
3. **Single-event DB** — only IEM Krakow 2026 demos stored; predictions for Budapest Major use lookahead data. Add ESL Pro League S22 and IEM Chengdu 2025 demos.
4. **MOUZ systematically overrated** — strong group-stage map stats don't translate to playoff performance.

## Documentation Rule

**Every change — bug fix, feature, refactor, or behavioural tweak — must be reflected in ALL relevant docs files before the work is considered done.** This includes `README.md`, `docs/architecture.md`, and any other file under `docs/` that covers the modified area. When adding or changing a command, flag, metric, output table, or pipeline behaviour, update those files as part of the same change. Do not commit code changes without the corresponding doc updates.

### Cross-repo pipeline flow doc

The workspace-level file **`docs/cs2-pipeline-flow.md`** (in this repo) is the authoritative
specification for the full three-tool pipeline. **Update it in the same commit** whenever
you change anything that affects the pipeline flow from this repo, including:

- Any field added to or removed from `simbo3MapStats` or `simbo3TeamStats` in `cmd/export.go`
- Any new query function added to `internal/storage/export_queries.go` that feeds into export
- Any prior or fallback value used when computing export fields
- Any flag added, removed, or changed on the `export` command
- The `--dir` non-recursive behaviour, the mtime/match_date contract, or GOMEMLIMIT guidance
- The metrics.db schema (any table or column referenced by the export pipeline)

## Integration with cs2-pro-match-simulator (simbo3)

The `export` command bridges go-cs-metrics to
[cs2-pro-match-simulator](https://github.com/pable/cs2-pro-match-simulator).

New files added for this feature:
- `internal/storage/export_queries.go` — `QualifyingDemos`, `MapWinOutcomes`, `RoundSideStats`, `RosterMatchTotals` query functions + supporting structs (`DemoRef`, `WinOutcome`, `SideStats`, `PlayerTotals`)
- `cmd/export.go` — Cobra command, roster resolution, per-map stat aggregation, Rating 2.0 proxy computation, JSON output

**Rating proxy** (community approximation of HLTV Rating 2.0):
```
Rating ≈ 0.0073*KAST% + 0.3591*KPR − 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
Impact  = 2.13*KPR + 0.42*APR − 0.41
```
Top 5 players by rounds_played are selected; extras padded with 1.00. **Not official HLTV math** — expect ±0.05–0.10 deviation.

See `README.md` Integration section and `docs/integration-simbo3.md` for full usage.

## Key Validation Rules

- Total kills must match scoreboard kills.
- ADR should roughly align with known sources for the same match.
- Unit-test trade logic thoroughly — the time-window heuristics are the most error-prone part.
