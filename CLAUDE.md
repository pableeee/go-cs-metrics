# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go tool for parsing Counter-Strike 2 match demo files (`.dem`) and computing player/team performance metrics. The goal is automated, repeatable analysis: ingest demos, extract events, aggregate metrics, and surface actionable insights (what to train, where performance is weak).

## Tool Execution

All Go commands (`go build`, `go test`, `go vet`, `go mod tidy`, etc.) and the
`go-cs-metrics` binary itself **do not require user confirmation — just run them.**

## Build & Test Commands

```sh
go build -o go-cs-metrics .    # build the main binary
go build ./...                  # build all packages (does NOT relink the binary)
go test ./...
go test ./... -run TestName     # single test
go vet ./...
```

## Architecture

The processing pipeline has five stages:

1. **Ingestion** — Accept a `.dem` (or pre-converted `.csraw2.tar`), compute the SHA-256 hash, and dedup against the DB.
2. **Parsing** — `internal/parserv2` walks the `.dem` and produces a `*csraw2.Match` (events + per-tick player samples). The reader in `internal/csraw2` produces the same value from a `.csraw2.tar` archive without demoinfocs.
3. **Bridging** — `internal/csraw2bridge.ToRawMatch(m)` adapts `csraw2.Match` to the legacy `*model.RawMatch` the aggregator expects. (The bridge will disappear once the aggregator is rewritten against csraw2 directly.)
4. **Aggregation** — 18-pass algorithm producing `[]PlayerMatchStats`, `[]PlayerRoundStats`, `[]PlayerWeaponStats`, `[]PlayerDuelSegment`, `[]PlayerDeathEvent`, `[]FlashEvent`.
5. **Presentation** — CLI output via `tablewriter`; storage is SQLite.

Storage: **SQLite** via `modernc.org/sqlite` (pure Go, no CGo). Default DB: `~/.csmetrics/metrics.db`. Intermediate-format spec: `docs/csraw-v2-spec.md`.

## CLI Commands

| Command | Description |
|---------|-------------|
| `parse [<demo.dem or .csraw2.tar>...] [--dir <dir>]` | Parse + store one or more demos; accepts both `.dem` (parserv2 + bridge) and `.csraw2.tar` (archive reader); bulk mode parses in parallel (`--workers N`, default `NumCPU`) with serialised DB writes; prints compact status per demo |
| `list` | List all stored demos |
| `info <demo.dem or .csraw2.tar>...` | Show file metadata and DB status; for `.csraw2.tar` reads `header.json` directly (no parquet decode); for `.dem` uses quick 64 KB hash |
| `show <hash-prefix>` | Re-display a stored demo's tables |
| `player <steamid64>...` | Cross-match aggregate report for one or more players (`--map`, `--since`, `--last` filters); `--top N` appends the top N players by Rating 2.0 proxy for comparison |
| `rounds <hash-prefix> <steamid64>` | Per-round drill-down with buy type, flags (POST_PLT, CLUTCH_1vN); `--clutch`, `--post-plant`, `--side`, `--buy` filters |
| `trend <steamid64>` | Chronological per-match performance trend (KPR/ADR/KAST% + TTK/TTD/CS%) |
| `deaths <steamid64>` | Death drill-down from `player_death_events`: totals plus breakdowns by round phase, engagement distance, weapon, and map, each with HS_TAKEN%/FLASHED%/TRADED%/OPENING%/AVG_DIST (`--map`, `--since`, `--before`, `--phase`, `--top-weapons`) |
| `swing <steamid64>...` | Round swing and duel swing (win-probability added) from empirical probability tables counted over the corpus; `--by-map`, `--by-side` (CT/T split of both metrics), `--tier`, `--since`, `--before`, `--show-tables` to audit the tables, `--top N` for the reference distribution + percentile rank |
| `sql <query>` | Run an arbitrary SQL query against the metrics database; prints results as a table |
| `drop [--force]` | Delete the metrics database file; requires `--force` to actually delete |
| `analyze player <steamid64> <question>` | AI-powered grounded analysis of a player's aggregate stats (requires `ANTHROPIC_API_KEY`) |
| `analyze match <hash-prefix> <question>` | AI-powered grounded analysis of a single match (requires `ANTHROPIC_API_KEY`) |
| `convert --dir <dir> --tier <tier>` | Convert `.dem` files to `.csraw2.tar` intermediate format (one demoinfocs pass via `parserv2`; no DB write); `--out-dir` writes output elsewhere; `--workers N`, `--force` |
| `replay --dir <dir>` | Ingest `.csraw2.tar` files into the DB without demoinfocs; fast, low RAM, supports many workers |
| `export` | Export team stats as a JSON file compatible with Monte Carlo match simulators (`--team`, `--players`, `--roster`, `--since`, `--quorum`, `--out`) |
| `summary` | High-level database overview: match count, date range, map breakdown, top players, match type distribution |
| `query --dir <dir> <expr>` | Find rounds matching a CEL expression across `.csraw2.tar` files (no DB needed); walks `--dir` recursively; `--csv` for full column output; `--html <file>` for interactive 2D radar replay viewer (self-contained HTML, `--limit N` caps clip count, `--radar-dir` for map PNGs). Variables cover util counts, HE damage, flash kills, alive counts, buy types, bomb events, and entry side — both T and CT explicitly. |

All commands share `--db` to point at an alternate database and `--silent` / `-s` to suppress column legends (verbose output is on by default).

## Preferred Parsing Method: `.csraw2.tar` via `convert` + `replay`

**Always prefer the `.csraw2.tar` pipeline over direct `.dem` parsing for any batch work.**

```sh
# Step 1 — convert .dem → .csraw2.tar (one-time, sequential, GOMEMLIMIT required)
GOMEMLIMIT=4294967296 ./go-cs-metrics convert --dir ~/demos/pro/<event>/ --tier pro --workers 1

# Step 2 — ingest into DB (fast, many workers, no GOMEMLIMIT needed)
./go-cs-metrics replay --dir ~/demos/pro/<event>/
# or equivalently:
./go-cs-metrics parse --dir ~/demos/pro/<event>/ --workers 32
```

### Why

| | `.dem` via `parse` | `.csraw2.tar` via `replay`/`parse` |
|---|---|---|
| Workers | 1 (forced — OOM with >1) | 32+ (no demoinfocs pressure) |
| RAM peak | 4–29 GB per demo | a few MB (parquet decode only) |
| GOMEMLIMIT required | yes | no |
| Re-ingest after schema change | re-parse .dem (slow) | `replay` from .csraw2.tar (fast) |
| 2D viewer support | yes (via `parserv2` → bridge) | yes (samples → frames) |

`.dem` parsing in `cmd/parse` goes through `parserv2` (the same code `convert` uses) and the `csraw2bridge`, so both pipelines produce identical aggregator inputs.

### Accuracy

Validated on the 45-demo personal corpus: every metric matches the
`.dem`-direct baseline byte-for-byte. The two duel-engine-sensitive
position/angle inputs (`weapon_fires.pos_*` / `damages.victim_pos_*` and
`weapon_fires.{yaw,pitch}_deg` / `first_sights.observer_{yaw,pitch}_deg`)
are stored as `float32` to preserve duel-distance and pre-shot-correction
bucket boundaries; everything else is `int16`-quantised.

### `.csraw2.tar` archive

Converted files live in `~/demos/converted-pro/<event>/` (same structure as
`~/demos/pro/`). Schema and per-stream layout are documented in
`docs/csraw-v2-spec.md`.

The `parse` command accepts `.csraw2.tar` directly (single file or `--dir`),
making it a drop-in for `.dem` in all workflows.

## Data Model

Core types (all in `internal/model/model.go`):

- **`PlayerMatchStats`** — aggregated metrics per player per demo (35+ columns)
- **`PlayerRoundStats`** — per-round breakdown for drill-down
- **`PlayerWeaponStats`** — per-weapon kill/damage/shot breakdown (incl. accuracy and the aimed/blind split)
- **`PlayerDuelSegment`** — FHHS counts per (round, weapon_bucket, distance_bin) per demo
- **`PlayerAggregate`** — cross-demo sums/averages used by the `player` command

## Aggregator: 18 Passes

1. Trade annotation (backward + forward scan within 5 s window); captures trade kill/death delay in ticks for timing metrics
2. Opening kills (first kill after `FreezeEndTick`)
3. Per-round per-player stats (buy type, post-plant flag, clutch detection, `won_round` flag; **team is resolved per round** — end state → that round's kills → its damages → match-level fallback, because sides swap at halftime)
4. Match-level rollup (includes `rounds_won`, `median_trade_kill_delay_ms`, `median_trade_death_delay_ms`)
5. Crosshair placement (from `RawFirstSight` / `m_bSpottedByMask`)
6. Duel engine + FHHS segments (exposure time, pre-shot correction, sight→first-shot delay, weapon+distance bins, **per round** — `player_duel_segments.round_number` lets FHHS be sliced by side / buy type / round phase via `player_round_stats`)
7. AWP death classifier (dry/repeek/isolated)
8. Flash quality window (effective flashes within 1.5 s)
9. Role classification (AWPer/Entry/Support/Rifler)
10. TTK/TTD/one-tap kills (first shot fired → kill, 3 s rolling window)
11. Counter-strafe % (shots fired at horizontal speed ≤ 34 u/s, via `e.Shooter.Velocity()` captured at WeaponFire time)
12. Death events (per-kill rows with position, weapon, distance, victim yaw, tactical context, plus duel context — both parties' first-sight ticks and the plant state — in `player_death_events`. Each row is one duel seen from both sides, so unlike `player_duel_segments` this table covers **lost** duels too, which is what makes duel swing possible)
13. Flash events (per-PlayerFlashed rows with blind angle, duration — `flash_events` table)
14. Save & Assist annotation (HLTV-compatible 1 s save window + assisted-kill flag; populates `saved_by_teammate`, `saved_teammate`, `assisted_kills` on `player_match_stats`)
15. HLTV-style flash assists (25 dmg threshold during blind window; populates `hltv_flash_assists` on `player_match_stats` — distinct from the in-game `flash_assists` field which uses the kill-feed ~40 dmg rule)
16. Liveness — per-player action time alive and sole-survivor moments (populates `alive_seconds_total` and `last_alive_server_rounds` on `player_match_stats`; alive time anchored at `FreezeEndTick`)
17. Scan volatility — out-of-combat crosshair discipline / "panic swiping" from 16 Hz view samples (dwell% below 25 °/s, yaw reversals with both legs ≥ 60 °/s, avg yaw speed; excludes enemy-visible samples and ±2 s around own combat events; populates `scan_*` columns on `player_match_stats` and `player_round_stats`)
18. Shot accounting — per-weapon `shots_fired` plus the aimed/blind split (each weapon fire and each enemy hit tagged with whether an enemy was in the player's spotted mask at that tick, via the nearest `RawViewSample`); populates `shots_fired`, `shots_visible`, `hits_visible`, `head_hits`, `head_hits_visible` on `player_weapon_stats`

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
GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir /path/to/event/ --workers 1
```

- `GOMEMLIMIT=4294967296` (4 GB) tells the GC to scavenge and return pages to the OS aggressively. It does **not** hard-cap RSS at 4 GB — it caps the managed heap, so RSS can still exceed 4 GB for pathological demos (observed peak: ~19–24 GB during a large batch). What it prevents is RSS ballooning to ~29 GB and triggering the WSL OOM killer.
- `--workers 1` ensures demos are parsed sequentially. After each demo, `debug.FreeOSMemory()` is called explicitly (in the sequential code path) and all references to the parsed `RawMatch` are nil'd before the next parse begins.
- **Do not use `--workers > 1`** for large event directories — N concurrent parses multiply the GC pressure and reliably OOM.

### Why `debug.FreeOSMemory()` is needed

Go's background scavenger returns pages to the OS gradually (over seconds). Between sequential demo parses the scavenger may not have finished before the next parse's allocations begin, causing RSS to accumulate across demos. `FreeOSMemory()` performs a full GC cycle and immediately returns all freed pages to the OS, resetting RSS before the next parse.

### Anomalous demos

Some demos (~577 MB) allocate far more than their file size suggests — likely due to high event density (many rounds, extensive utility usage). These demos work fine with `GOMEMLIMIT` set; without it they OOM even as a single-file parse.

### Quick-hash pre-check

`parse --dir` does a cheap existence check before the expensive full parse:

- `.dem` inputs — SHA-256 of the first 64 KB (`parser.QuickHash`).
- `.csraw2.tar` inputs — read just `header.json` via `csraw2.ReadHeader` and use its embedded `quick_hash`.

Either way the check costs milliseconds, so re-running `parse --dir` after an interrupted batch is essentially free for already-ingested demos.

## Key Implementation Notes

- **SteamID64 stored as TEXT** — avoids signed integer overflow for IDs above `2^63`.
- **`INSERT OR REPLACE`** everywhere — full idempotency; re-parsing the same demo hash is safe.
- **Wilson CI** used for FHHS proportions (stable for small samples unlike Wald).
- **Distance** computed as `||attackerPos − victimPos|| * 0.01905` (Hammer units → meters).
- **`player` command aggregation**: integers summed directly; float medians averaged across matches (approximate); FHHS rate recomputed from raw segment count totals (accurate).
- **Schema migrations**: new columns are added automatically at startup via `ALTER TABLE ... ADD COLUMN ... DEFAULT` statements (duplicate-column errors silently ignored). Existing rows default to `0`/`''`. A full DB rebuild is only required if a column type or a table structure changes (not just additions).
- **Parse skips already-stored demos**: `parse --dir` skips any demo whose hash is already in the `demos` table. Passing the same directory again after a schema migration will NOT backfill new columns for old rows — see below.
- **`match_date` comes from file mtime**: the parser reads the `.dem` file's filesystem modification time, not anything inside the demo. Always fix file mtimes to actual match dates before the first parse, otherwise every demo gets `match_date = today` and `--since` filtering in `export` breaks silently.
- **`--dir` is not recursive**: only finds `.dem` and `.csraw2.tar` files directly in the given directory. Pass each event subdirectory individually, not the parent.

## Recovering from a Schema Migration (New Columns on Old Demos)

When a new column is added to `player_match_stats` or `player_round_stats`, existing rows get the column's `DEFAULT` value (usually `0`). Demos are not re-aggregated automatically because `parse` / `replay` skip files whose hash is already stored.

**Which columns can be backfilled with SQL vs. which require a full re-aggregation:**

| Scenario | Fix |
|---|---|
| New column in `player_match_stats` is derivable from `player_round_stats` | SQL `UPDATE` backfill (fast, no re-aggregation) |
| New column requires re-running the aggregator (e.g. counter-strafe, TTK, duel engine) | `replay --dir <event>/ --force` against `.csraw2.tar` archives — fast, no demoinfocs |
| Aggregator needs a field the converter never wrote (csraw2 schema bump) | Full re-convert from `.dem` + replay |

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

`match_date` is sourced from the `.dem` file's mtime at convert/parse time and is baked into `header.match_date` of the `.csraw2.tar`. If mtimes weren't fixed before that step, every demo gets `match_date = <convert date>` and `export --since` filtering breaks silently. There is no SQL-only fix — the correct date can only be obtained by re-converting from a `.dem` with the right mtime, then replaying.

```sh
# 1. Fix file mtimes on the .dem files
./demoget touch-dates --out /path/to/demos

# 2. Delete affected demos from DB (all tables, respecting foreign keys)
#    Adjust the WHERE clause to target the specific wrong date(s)
sqlite3 ~/.csmetrics/metrics.db "
DELETE FROM player_duel_segments WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_weapon_stats   WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_round_stats    WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_match_stats    WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro';
"

# 3. Re-convert (header now picks up the fixed mtime) + replay
for dir in /path/to/demos/*/; do
  GOMEMLIMIT=4294967296 ./go-cs-metrics convert --dir "$dir" --tier pro --force --workers 1
  ./go-cs-metrics replay --dir "$dir"
done
```

Verify dates look right afterward:
```sh
sqlite3 ~/.csmetrics/metrics.db "SELECT MIN(match_date), MAX(match_date), COUNT(*) FROM demos WHERE tier='pro';"
```

### Full re-aggregation (when SQL backfill isn't possible)

`parse` / `replay` won't re-aggregate demos already in the DB. To force a full rebuild:

```sh
./go-cs-metrics drop --force
# Then replay every event from its .csraw2.tar archive (fast, no demoinfocs):
for dir in /path/to/demos/*/; do
  ./go-cs-metrics replay --dir "$dir"
done
```

If the csraw2 schema itself bumped and the archives lack a needed field, re-convert from `.dem` first (see the recovery flow above).

Note: `--dir` does a flat search — pass each event subdirectory individually, not the parent directory.

## Documentation Rule

**Every change — bug fix, feature, refactor, or behavioural tweak — must be reflected in ALL relevant docs files before the work is considered done.** This includes `README.md`, `docs/architecture.md`, and any other file under `docs/` that covers the modified area. When adding or changing a command, flag, metric, output table, or pipeline behaviour, update those files as part of the same change. Do not commit code changes without the corresponding doc updates.

## Export: Rating 2.0 Proxy

The `export` command computes a community approximation of HLTV Rating 2.0:

```
Rating ≈ 0.0073*KAST% + 0.3591*KPR − 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
Impact  = 2.13*KPR + 0.42*APR − 0.41
```

Key files:
- `internal/storage/export_queries.go` — `QualifyingDemos`, `MapWinOutcomes`, `RoundSideStats`, `RosterMatchTotals`, `PopulationMeanRating`
- `cmd/export.go` — roster resolution, per-map stat aggregation, Rating 2.0 proxy, JSON output

Top 5 players by `rounds_played` are selected; extras padded with 1.00. **Not official HLTV math** — expect ±0.05–0.10 deviation.

### Sample-size shrinkage (`--rating-shrink-rounds`, `--rating-prior`)

`players_rating2_3m` is **not** sample-size-corrected by default, so a team with very
few demos can get a wildly inflated rating (observed: a 2-demo team produced a 2.24
player rating, out-rating well-sampled tier-1 rosters). This corrupts simbo3 rankings
for any field containing thin-data teams.

Optional empirical-Bayes shrinkage fixes this:

```
r' = priorMean + (r − priorMean) · rounds / (rounds + shrinkRounds)
```

- `--rating-shrink-rounds K` (default `0` = **off**, preserving legacy behavior) — the
  weighted-round count at which a player gets half its own rating and half the prior.
  `200` (~9 maps) pulls thin teams hard toward the mean while leaving well-sampled
  teams (hundreds of rounds) essentially untouched.
- `--rating-prior P` (default `-1` = **auto**) — the mean to shrink toward. Auto uses
  `PopulationMeanRating` over the same `[since, before)` window (players with ≥100
  rounds), so the prior tracks the actual rating scale (currently ≈1.24 — inflated,
  see KAST note below) rather than assuming 1.0. Applies to the global rating and all
  VRS strata (`players_rating_vs_top30/20/10`). `backtest-dataset` always uses raw
  (un-shrunk) ratings.

> **Note — the DB-wide KAST bug is FIXED (verified 2026-08-03).** It previously
> pinned ~95% of `player_match_stats` rows at `kast_rounds == rounds_played`
> (KAST 100%), inflating every rating by ~+0.2 and lifting the population mean
> to ~1.24. After the corpus re-aggregation only 4.4% of rows sit at 100% (a
> plausible rate for genuinely perfect-KAST matches), mean KAST is 73.2%, and
> the population mean rating is **1.029** — i.e. the rating scale is now
> correctly centred on ~1.0. The auto prior (`--rating-prior -1`) tracks this
> empirically, so it needed no change; a hardcoded 1.0 prior would now also be
> defensible.

## Key Validation Rules

- Total kills must match scoreboard kills.
- ADR should roughly align with known sources for the same match.
- Unit-test trade logic thoroughly — the time-window heuristics are the most error-prone part.
