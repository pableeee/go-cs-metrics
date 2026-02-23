# CS2 Toolchain — Pipeline Flow Reference

This document is the authoritative, step-by-step specification of how the three CS2
tools interact. It covers every step from catalog definition to match forecast,
specifying inputs, outputs, file formats, flags, shared state, and caveats.

**Maintainability rule**: if any step's inputs, outputs, flags, or data formats
change — in any of the three repos — this document must be updated in the same commit.

---

## Table of Contents

1. [Architecture overview](#1-architecture-overview)
2. [Shared state](#2-shared-state)
3. [Step 0 — Catalog configuration](#3-step-0--catalog-configuration-eventsyaml)
4. [Step 1 — Demo discovery and download](#4-step-1--demo-discovery-and-download-demoget-sync)
5. [Step 2 — Fix file timestamps](#5-step-2--fix-file-timestamps-demoget-touch-dates)
6. [Step 3 — Demo parsing](#6-step-3--demo-parsing-go-cs-metrics-parse)
7. [Step 4 — Roster file preparation](#7-step-4--roster-file-preparation-manual)
8. [Step 5 — Team export](#8-step-5--team-export-go-cs-metrics-export)
9. [Step 6 — Match simulation](#9-step-6--match-simulation-simbo3-run)
10. [Step 7 — Backtesting and tuning (optional)](#10-step-7--backtesting-and-tuning-optional)
11. [Data format reference](#11-data-format-reference)
12. [Cross-cutting concerns](#12-cross-cutting-concerns)
13. [Partial-workflow quick reference](#13-partial-workflow-quick-reference)

---

## 1. Architecture overview

```
events.yaml                         ← user-maintained event catalog
    │
    ▼
[demoget sync]                      repo: cs-demo-downloader
    │   scrapes HLTV → downloads + extracts .dem files
    │   state: ~/.csmetrics/demoget.db
    ▼
~/demos/pro/<event-slug>/*.dem      ← raw demo files (mtime = extraction date, WRONG)
    │
    ▼  ⚠ REQUIRED before parse
[demoget touch-dates]               repo: cs-demo-downloader
    │   fixes .dem mtimes to actual match dates from RAR filenames
    ▼
~/demos/pro/<event-slug>/*.dem      ← same files, mtime = correct match date
    │
    ▼
[go-cs-metrics parse]               repo: go-cs-metrics
    │   11-pass aggregator → player/round/weapon/duel stats
    │   state: ~/.csmetrics/metrics.db
    ▼
~/.csmetrics/metrics.db             ← all parsed demo metrics
    │
    ├── [roster files]              ← user-maintained JSON (team name + SteamID64s)
    │
    ▼
[go-cs-metrics export]              repo: go-cs-metrics
    │   queries metrics.db for a roster → produces simbo3 team JSON
    ▼
<team>.json                         ← simbo3-compatible team stats file
    │
    ▼
[simbo3 run]                        repo: cs2-pro-match-simulator
    │   Monte Carlo simulation → series outcome forecast
    ▼
match forecast (stdout)
```

---

## 2. Shared state

| Path | Owner | Format | Contents |
|---|---|---|---|
| `~/.csmetrics/demoget.db` | `demoget` | SQLite | Match discovery + download status (matches, demos tables) |
| `~/.csmetrics/metrics.db` | `go-cs-metrics` | SQLite | All parsed demo metrics (demos, player_match_stats, player_round_stats, player_weapon_stats, player_duel_segments) |
| `~/demos/pro/<event-slug>/` | `demoget` → `go-cs-metrics` | Directory of `.dem` files | Extracted demo files; mtime used as match_date by parser |

Both databases default to the paths above. Override with `--db` on any command.

---

## 3. Step 0 — Catalog configuration (`events.yaml`)

**Owner:** user
**Repo:** `cs-demo-downloader/`
**File:** `events.yaml` (in repo root, checked in)

### Input
None — this is the pipeline's root input, authored by hand.

### Format

```yaml
version: 1

defaults:
  output_dir: "~/demos/pro"       # root output directory for all events
  requests_per_second: 0.4        # HLTV scrape rate (1 req / 2.5 s)
  retry_max: 3                    # per-request retry attempts on 5xx

events:
  - id: "iem_cologne_2025"        # unique slug — used as output subdirectory name
    name: "IEM Cologne 2025"      # human-readable label
    tier: 1                       # informational only (not stored in metrics.db)
    organizer: "hltv"             # only "hltv" is supported
    hltv_event_id: 8038           # numeric event ID from hltv.org/events/<id>/...
    map_filter: []                # [] = all maps; ["mirage","inferno"] = subset
    team_filter: []               # [] = all teams; ["Vitality","FaZe"] = subset
```

### Key fields

| Field | Effect |
|---|---|
| `id` | Determines output subdirectory: `<output_dir>/<id>/` |
| `hltv_event_id` | Used to scrape `hltv.org/results?event=<id>` |
| `map_filter` | Case-insensitive substring match on map name in RAR filename |
| `team_filter` | Case-insensitive substring match on team name in match page |

Filters are applied during download — match pages are still visited, but
non-matching archives are not downloaded.

### Output
Consumed exclusively by `demoget sync`. No file is produced.

---

## 4. Step 1 — Demo discovery and download (`demoget sync`)

**Repo:** `cs-demo-downloader/`
**Binary:** `./demoget`

### Inputs

| Input | Format | Source |
|---|---|---|
| `events.yaml` | YAML catalog | Step 0 / user |
| `~/.csmetrics/demoget.db` | SQLite | Auto-created on first run; resumable state |

### Command

```sh
./demoget sync --out ~/demos/pro
# Single event:
./demoget sync --event iem_cologne_2025 --out ~/demos/pro
# Test with one file:
./demoget sync --limit 1 --out ~/demos/pro
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--catalog` | `events.yaml` | Path to the catalog file |
| `--db` | `~/.csmetrics/demoget.db` | Override database path |
| `--out` | value from catalog defaults | Override output root directory |
| `--event <id>` | all events | Restrict to a single event slug |
| `--dry-run` | false | Run phases 1 and 2 (discover + resolve) without downloading |
| `--limit N` | 0 (no limit) | Download at most N demos |
| `--workers N` | 4 | Parallel download concurrency |

### Internal phases

**Phase 1 — Discover**
Scrapes `https://www.hltv.org/results?event=<hltv_event_id>` with pagination.
Each match URL is written to `demoget.db:matches` with `status=pending`.
Already-stored matches are not re-scraped.

**Phase 2 — Resolve**
For each `pending` match, fetches the match page, extracts `a[data-demo-link]` and the match date from `div.date[data-unix]`.
Each resolved demo link is written to `demoget.db:demos` with `status=pending`.
Match pages with no demo link (forfeits, qualifiers) are silently skipped.

**Phase 3 — Download**
For each `pending` demo:
1. Downloads archive to `<event-slug>/<filename>.tmp`
2. Renames to final path on success
3. Detects format by file magic bytes (RAR, gz, bz2, zst)
4. Extracts all `.dem` files from archive
5. Deletes archive
6. Sets `status=done` in `demoget.db`

If a `.dem` file already exists on disk, marked `skipped` without re-download.

### ⚠ Critical side effect

Extraction sets the `.dem` file's mtime to **today** (extraction date), not the
actual match date. **Step 2 must run before Step 3 to correct this.**

### Outputs

| Output | Format | Location |
|---|---|---|
| `.dem` files | CS2 demo binary | `~/demos/pro/<event-slug>/<match>.dem` |
| Download state | SQLite rows | `~/.csmetrics/demoget.db` (matches + demos tables) |

### Progress monitoring

```sh
./demoget summary   # per-event progress table with download sizes
./demoget status    # compact counts by status
./demoget list      # list all done/skipped demos
```

---

## 5. Step 2 — Fix file timestamps (`demoget touch-dates`)

**Repo:** `cs-demo-downloader/`
**Binary:** `./demoget`
**Must run:** after every `demoget sync`, before any `go-cs-metrics parse`

### Why this step is required

`go-cs-metrics parse` reads the `.dem` file's filesystem mtime and stores it as
`match_date` in `metrics.db`. `demoget sync` sets mtime to the extraction
timestamp (today). If you skip this step, every demo receives `match_date = <today>`,
breaking the `--since` filter in `go-cs-metrics export` — you'll silently get zero
qualifying demos or wrong results.

### Input

| Input | Format | Source |
|---|---|---|
| `~/demos/pro/<event-slug>/*.dem` | `.dem` files with wrong mtime | Step 1 |
| `~/.csmetrics/demoget.db` | SQLite | Has correct match dates extracted from RAR filenames during Phase 2 |

### Command

```sh
./demoget touch-dates --out ~/demos/pro
```

This processes all event subdirectories under `--out` in one invocation.

### Process

For each `.dem` file, looks up the match date in `demoget.db` (stored during
Phase 2 from `div.date[data-unix]` on the HLTV match page) and calls `os.Chtimes`
to set the mtime to that date.

### Output

| Output | Format | Location |
|---|---|---|
| Corrected `.dem` files | Same binary files, updated mtime | `~/demos/pro/<event-slug>/*.dem` |

No database rows are changed. The files themselves are not modified — only their
filesystem metadata (mtime) is corrected.

### Recovery: if you forgot to run this before parsing

```sh
# 1. Fix mtimes
./demoget touch-dates --out ~/demos/pro

# 2. Delete affected rows (all tables, ordered by FK constraints)
#    Replace 'YYYY-MM-DD' with the wrong date (the day you ran sync)
sqlite3 ~/.csmetrics/metrics.db "
DELETE FROM player_duel_segments WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_weapon_stats   WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_round_stats    WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM player_match_stats    WHERE demo_hash IN (SELECT hash FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro');
DELETE FROM demos WHERE match_date = 'YYYY-MM-DD' AND tier = 'pro';
"

# 3. Re-parse (now reads correct mtime)
for dir in ~/demos/pro/*/; do
  GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir "$dir" --tier pro --workers 1
done
```

---

## 6. Step 3 — Demo parsing (`go-cs-metrics parse`)

**Repo:** `go-cs-metrics/`
**Binary:** `./go-cs-metrics`

### Inputs

| Input | Format | Source |
|---|---|---|
| `.dem` files | CS2 demo binary | `~/demos/pro/<event-slug>/` — Step 2 output |
| `~/.csmetrics/metrics.db` | SQLite | Auto-created; existing rows used for idempotency |

### Command

```sh
# Parse one event directory (REQUIRED: pass each event dir individually, not the parent)
GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir ~/demos/pro/iem_cologne_2025/ --tier pro --workers 1

# Parse all events
for dir in ~/demos/pro/*/; do
  GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir "$dir" --tier pro --workers 1
done

# Parse explicit files
./go-cs-metrics parse match1.dem match2.dem --tier pro
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--dir <dir>` | — | Parse all `.dem` files directly in `<dir>` (not recursive) |
| `--tier <tier>` | `""` | Tag all demos with this tier string (`pro`, `faceit`, etc.) |
| `--workers N` | NumCPU | Parallel parse workers. **Use 1 for large event dirs** (memory) |
| `--db <path>` | `~/.csmetrics/metrics.db` | Override database path |

### Idempotency

Before parsing, the tool computes a SHA-256 of the first 64 KB of the `.dem` file
(quick-hash) and checks it against `metrics.db:demos`. If a match is found, the file
is **skipped** — re-running `parse` on a directory is safe and essentially free for
already-ingested files.

### Internal pipeline (11 passes)

The aggregator runs 11 sequential passes over the raw event stream from the demo:

| Pass | What it computes |
|---|---|
| 1 | Trade annotation (kill/death within 5 s window) |
| 2 | Opening kills/deaths (first kill after freeze-end) |
| 3 | Per-round stats (buy type, post-plant, clutch, won_round) |
| 4 | Match-level rollup (totals, trade delay medians) |
| 5 | Crosshair placement (from first-sight angles) |
| 6 | Duel engine + FHHS segments (weapon+distance bins) |
| 7 | AWP death classifier (dry/repeek/isolated) |
| 8 | Flash quality window (effective flashes within 1.5 s) |
| 9 | Role classification (AWPer/Entry/Support/Rifler) |
| 10 | TTK/TTD/one-tap kills |
| 11 | Counter-strafe % |

### mtime → match_date

The file's filesystem mtime is read via `os.Stat` and stored as `match_date` in
`demos`. This is why Step 2 (`touch-dates`) is mandatory before Step 3.

### Memory requirements

CS2 demos allocate heavily during parsing. Always set `GOMEMLIMIT` and use
`--workers 1` for bulk directory parses to prevent OOM:

```sh
GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir ~/demos/pro/event/ --tier pro --workers 1
```

`--dir` is **not recursive** — it only finds `.dem` files directly in the given
directory. Pass each event subdirectory individually.

### Outputs — metrics.db schema

All output goes to `~/.csmetrics/metrics.db`. Four tables are populated per demo:

**`demos`**

| Column | Type | Description |
|---|---|---|
| `hash` | TEXT PK | Full SHA-256 of the `.dem` file |
| `map_name` | TEXT | Normalized map name (e.g. `"Mirage"`, not `"de_mirage"`) |
| `match_date` | TEXT | From file mtime (`YYYY-MM-DD`) |
| `match_type` | TEXT | `"MR12"`, `"MR15"`, etc. |
| `tickrate` | REAL | Demo tickrate |
| `ct_score` | INTEGER | Final CT score |
| `t_score` | INTEGER | Final T score |
| `tier` | TEXT | From `--tier` flag |
| `event_id` | TEXT | From sidecar or empty |

**`player_match_stats`** — one row per (demo_hash, steam_id)

Key columns used by export:

| Column | Used by |
|---|---|
| `kills`, `deaths`, `assists` | Rating 2.0 proxy |
| `kast_rounds`, `rounds_played`, `total_damage` | Rating 2.0 proxy |
| `rounds_won` | Map win outcome (anchor player) |
| `opening_kills`, `opening_deaths` | Entry kill/death rates (→ export) |
| `trade_kills`, `trade_deaths` | Trade net rate (→ export) |

**`player_round_stats`** — one row per (demo_hash, steam_id, round_number)

Key columns used by export:

| Column | Used by |
|---|---|
| `team` | CT/T round win rates; post-plant filter |
| `won_round` | CT/T round win rates; eco/force win rates |
| `buy_type` | Eco/force win rates (`'eco'`, `'force'`, `'full'`, `'pistol'`) |
| `is_post_plant` | Post-plant T win rate |

**`player_weapon_stats`**, **`player_duel_segments`** — not used by export; used
by `player`, `show`, `analyze` commands.

---

## 7. Step 4 — Roster file preparation (manual)

**Owner:** user
**No tool involved** — this is human-maintained configuration.

### Purpose

Maps a team identity (name) to its players' SteamID64s. The export command uses
this to filter `player_match_stats` rows to the relevant team.

### Format

```json
{
  "team": "Natus Vincere",
  "players": [
    "76561198034202275",
    "76561197992321696",
    "76561198040577200",
    "76561198121220486",
    "76561198155383140"
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `team` | string | Team name written into the output JSON's `team` field |
| `players` | string[] | SteamID64 strings. Include all active players; the export selects the top 5 by activity automatically |

SteamIDs are stored as TEXT in metrics.db (to avoid int64 overflow). Always use
the decimal SteamID64 string form.

### Finding SteamIDs

```sh
# From the metrics DB (players already parsed):
./go-cs-metrics sql "SELECT DISTINCT steam_id, name FROM player_match_stats ORDER BY name"

# Or look up on hltv.org/player/<id>/... — the numeric part of the URL is the SteamID3;
# convert to SteamID64 by adding 76561197960265728.
```

### Location

Roster files have no fixed location. Convention: keep them alongside the team JSON
outputs, e.g. `~/rosters/navi-roster.json`.

---

## 8. Step 5 — Team export (`go-cs-metrics export`)

**Repo:** `go-cs-metrics/`
**Binary:** `./go-cs-metrics`

### Inputs

| Input | Format | Source |
|---|---|---|
| Roster file | JSON (see Step 4) | User-maintained |
| `~/.csmetrics/metrics.db` | SQLite | Step 3 output |

### Command

```sh
./go-cs-metrics export \
  --roster navi-roster.json \
  --since 90 \
  --quorum 3 \
  --out navi.json
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--roster <path>` | — | Path to roster JSON file |
| `--players <ids>` | — | Comma-separated SteamID64s (alternative to --roster) |
| `--team <name>` | — | Override team name from roster file |
| `--since <days>` | 90 | Look-back window in days from today |
| `--quorum <n>` | 3 | Minimum roster players that must appear in a demo to include it |
| `--out <path>` | stdout | Output file path |
| `--db <path>` | `~/.csmetrics/metrics.db` | Override database path |

### Internal query pipeline

All queries are scoped by the qualifying demo set: demos within `--since` days
where at least `--quorum` roster players appear.

| Query function | DB tables | Produces |
|---|---|---|
| `QualifyingDemos` | `demos`, `player_match_stats` | List of demo hashes + map names + dates in the window |
| `MapWinOutcomes` | `player_match_stats` | Win/loss per demo (anchor = most-active roster player) |
| `RoundSideStats` | `player_round_stats` | CT/T round wins + totals per map |
| `RosterMatchTotals` | `player_match_stats` | Per-player kills/deaths/assists/kast/rounds/damage |
| `MapEntryStats` | `player_match_stats`, `demos` | Per-map opening_kills, opening_deaths, rounds_played |
| `TeamTradeStats` | `player_match_stats` | Total trade_kills, trade_deaths, rounds_played across all maps |
| `BuyTypeWinRates` | `player_round_stats` | Eco wins/total, force wins/total |
| `MapPostPlantTWinRates` | `player_round_stats`, `demos` | Per-map T-side post-plant wins/total |

### Computed fields and their priors/fallbacks

| Output field | Formula | Prior / fallback |
|---|---|---|
| `map_win_pct` | `wins / n_demos` (win = majority rounds won) | 0.0 if no demos on that map |
| `ct_round_win_pct` | `CT_wins / CT_total` | 0.50 if no CT rounds |
| `t_round_win_pct` | `T_wins / T_total` | 0.50 if no T rounds |
| `matches_3m` | Count of qualifying demos per map | 0 |
| `entry_kill_rate` | `opening_kills / rounds_played` per map | 0.0 (omitted from JSON — neutral, no logit adjustment) |
| `entry_death_rate` | `opening_deaths / rounds_played` per map | 0.0 (omitted from JSON) |
| `post_plant_t_win_pct` | `T_plant_wins / T_plant_total` per map | 0.75 if fewer than 5 T post-plant rounds |
| `trade_net_rate` | `(trade_kills − trade_deaths) / rounds_played` | 0.0 if no rounds |
| `eco_win_pct` | `eco_wins / eco_total` | 0.50 if fewer than 10 eco rounds |
| `force_win_pct` | `force_wins / force_total` | 0.50 if fewer than 10 force rounds |
| `players_rating2_3m` | Rating 2.0 proxy for top-5-by-activity players, descending | 1.00 padding for missing slots |
| `rating_floor` | `players_rating2_3m[4]` (5th player = lowest) | 1.00 if padded |

**Rating 2.0 proxy formula:**
```
Rating ≈ 0.0073×KAST% + 0.3591×KPR − 0.5329×DPR + 0.2372×Impact + 0.0032×ADR + 0.1587
Impact  = 2.13×KPR + 0.42×APR − 0.41
```
Not the official HLTV formula; expect ±0.05–0.10 deviation.

### Diagnostic output (stderr)

On success:
```
Querying demos for 5 players since 2025-11-23 (quorum=3)...
Found 34 qualifying demos
  Mirage        18 matches  win=0.67  CT=0.56  T=0.52
  s1mple               18 rounds  KPR=0.92 DPR=0.62 KAST=79%  ADR=91.3  → rating 1.19
Wrote navi.json
```

On failure (no qualifying demos), prints per-player demo counts + a hint:
```
Per-player demo counts (last 90 days, no quorum filter):
  s1mple    3 demo(s)
hint: try --quorum 2
Error: no qualifying demos found in the last 90 days with quorum=3
```

### Output — team JSON

Written to `<team>.json` (or stdout). This file is the direct input to `simbo3 run`.

Full schema:

```json
{
  "team": "Natus Vincere",
  "players_rating2_3m": [1.19, 1.11, 1.08, 1.04, 0.98],
  "maps": {
    "Mirage": {
      "map_win_pct":          0.67,
      "ct_round_win_pct":     0.56,
      "t_round_win_pct":      0.52,
      "matches_3m":           18,
      "entry_kill_rate":      0.14,
      "entry_death_rate":     0.11,
      "post_plant_t_win_pct": 0.78
    }
  },
  "trade_net_rate":  0.02,
  "eco_win_pct":     0.31,
  "force_win_pct":   0.41,
  "rating_floor":    0.98,
  "generated_at":    "2026-02-23T14:00:00Z",
  "window_days":     90,
  "latest_match_date": "2026-02-08",
  "demo_count":      34
}
```

**Provenance fields** (`generated_at`, `window_days`, `latest_match_date`,
`demo_count`) are written for human inspection but ignored by `simbo3` (unknown
fields are discarded by Go's JSON unmarshaller).

**`omitempty` fields**: `entry_kill_rate`, `entry_death_rate`,
`post_plant_t_win_pct`, `trade_net_rate`, `eco_win_pct`, `force_win_pct`,
`rating_floor` are omitted when zero. Simbo3 reads missing/zero values as the
neutral default (no model adjustment).

---

## 9. Step 6 — Match simulation (`simbo3 run`)

**Repo:** `cs2-pro-match-simulator/`
**Binary:** `./bin/simbo3`

### Inputs

| Input | Format | Source |
|---|---|---|
| Team A JSON | Team stats file | Step 5 output |
| Team B JSON | Team stats file | Step 5 output |
| Config JSON (optional) | Coefficient overrides | `simbo3 tune` output or hand-crafted |

### Command

```sh
# Default: BO3 + veto simulation
./bin/simbo3 run --teamA navi.json --teamB faze.json

# Manual maps
./bin/simbo3 run \
  --teamA navi.json --teamB faze.json \
  --format bo3 --mode manual \
  --maps Mirage,Inferno,Nuke \
  --picks A,B,D \
  --start-sides CT,T,rand

# JSON output
./bin/simbo3 run --teamA navi.json --teamB faze.json --output json

# With explain (shows smoothed stats + map matchup probs)
./bin/simbo3 run --teamA navi.json --teamB faze.json --explain

# With tuned coefficients
./bin/simbo3 run --teamA navi.json --teamB faze.json --config tuned.json
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--teamA`, `--teamB` | — (required) | Paths to team JSON files |
| `--format` | `bo3` | Series format: `bo1`, `bo3`, `bo5` |
| `--mode` | `veto` | `veto` (softmax policy) or `manual` (explicit map list) |
| `--maps` | — | Comma-separated map names (manual mode) |
| `--picks` | per-format default | Comma-separated pickers per map: `A`, `B`, `D` |
| `--start-sides` | — | Comma-separated starting sides for Team A: `CT`, `T`, `rand` |
| `--trials` | 50,000 | Monte Carlo trial count |
| `--seed` | 0 (random) | RNG seed for determinism |
| `--config` | — | JSON file with coefficient overrides |
| `--output` | `table` | `table` or `json` |
| `--top-vetos` | 10 | Number of top veto sequences to show (veto mode) |
| `--explain` | false | Print smoothed stats, map probabilities, and veto/manual detail |

### Team JSON validation on load

| Condition | Behaviour |
|---|---|
| Exactly 5 player ratings required | Error — run aborted |
| `matches_3m < 0` on any map | Error — run aborted |
| Rating outside `[0.3, 2.5]` | Warning — continues |
| Map stat outside `[0, 1]` | Warning — continues |
| Map name not in pool | Warning — continues |
| Map missing from pool | Warning — priors applied |

### Model: map win probability

The logit formula (per map per trial):

```
L = α·(S_map_A − S_map_B)           # map win log-odds differential
  + β·(avgRating_A − avgRating_B)   # team rating differential
  + γ·sideTerm                      # side advantage (CT vs T log-odds)
  + δ·(entryKillRate_A − entryKillRate_B)   # entry kill rate differential
  + ε·(tradeNetRate_A − tradeNetRate_B)     # trade net rate differential

p = clamp(sigmoid(L), 0.05, 0.95)
```

Where `sideTerm`:
- A starts CT: `logit(ct_A_smoothed) − logit(t_B_smoothed)`
- A starts T:  `logit(t_A_smoothed) − logit(ct_B_smoothed)`

All raw stats are shrunk toward 0.50 priors before logit conversion:
`smoothed = clamp(w·raw + (1−w)·0.50, 0.05, 0.95)` where `w = n / (n + k)`.

### Default coefficients

| Coefficient | Default | Meaning |
|---|---|---|
| `alpha` | 0.9 | Map win log-odds weight |
| `beta` | 1.2 | Rating delta weight |
| `gamma` | 0.35 | Side log-odds weight |
| `delta` | 0.0 | Entry kill rate weight (inactive until tuned) |
| `epsilon` | 0.0 | Trade net rate weight (inactive until tuned) |
| `k_reliability` | 10 | Shrinkage strength (higher = more conservative) |

### Veto simulation (mode=veto)

Softmax-based probabilistic picks and bans. Utility functions:
- **Pick utility**: `a·logit(p_neutral) + b·comfort`
- **Ban utility**: `a2·logit(1 − p_neutral) + b2·comfort_opponent`

Comfort: `1 − exp(−matches_3m / comfort_c)` — grows with experience on the map.

Side after a pick: opponent deterministically chooses the side that minimizes
the picker's win probability.

### Outputs

To stdout (table or JSON):

| Output | Description |
|---|---|
| Series win probability | `AWinRate ± CI` and `BWinRate ± CI` (95% Wilson) |
| Score distribution | Fraction of trials ending at each score (e.g. `2-0`, `2-1`) |
| Per-map win rate | `A win%` on each map played, with play rate and CI |
| Top veto sequences | Most frequent map selection sequences (veto mode) |

---

## 10. Step 7 — Backtesting and tuning (optional)

**Repo:** `cs2-pro-match-simulator/`
**Binary:** `./bin/simbo3`

### Purpose

Evaluate the model against historical match results and optimise coefficients
to minimise prediction error (log-loss or Brier score).

### Backtest dataset format

A JSON array of `MatchRecord` objects. Each record embeds both teams' full
`TeamStats` inline (not file paths) alongside the actual maps played and outcome.

```json
[
  {
    "match_id": "navi-vs-faze-iem-2025",
    "team_a": { <TeamStats — same schema as Step 5 output> },
    "team_b": { <TeamStats — same schema as Step 5 output> },
    "maps": [
      { "map": "Nuke",    "picker": "A" },
      { "map": "Inferno", "picker": "B", "a_start_ct": true },
      { "map": "Ancient", "picker": "D" }
    ],
    "a_won_series": true
  }
]
```

`TeamStats` here is exactly the schema from Step 5 (the export output),
minus the provenance fields.

### Backtest command

```sh
./bin/simbo3 backtest \
  --dataset playoff_matches.json \
  --trials 10000 \
  --seed 42 \
  --verbose
```

**Output:** log-loss (baseline ln(2)≈0.693), Brier score (baseline 0.25), accuracy,
and a calibration table showing model confidence vs. actual win rate per bin.

### Tune command

```sh
./bin/simbo3 tune \
  --dataset playoff_matches.json \
  --seed 42 \
  --rounds 5 \
  --trials 3000
```

**Algorithm:** coordinate descent. For each of 6 parameters
(`alpha`, `beta`, `gamma`, `k_reliability`, `delta`, `epsilon`), evaluates
25 grid points in parallel, updates if improved, repeats for up to 5 rounds.

**Tunable parameters:**

| Parameter | Search range | Default |
|---|---|---|
| `alpha` | [0.1, 3.0] | 0.9 |
| `beta` | [0.0, 4.0] | 1.2 |
| `gamma` | [0.0, 2.0] | 0.35 |
| `k_reliability` | [1.0, 50.0] | 10.0 |
| `delta` | [0.0, 3.0] | 0.0 |
| `epsilon` | [0.0, 3.0] | 0.0 |

**Output:** best config JSON. Copy to `tuned.json` and use with `--config`:

```sh
./bin/simbo3 run \
  --teamA navi.json --teamB faze.json \
  --config tuned.json
```

### Input for backtest dataset construction

Building the dataset requires generating team JSON files (Step 5) for the state
of each team *at the time of the match* (using `--since` and `QualifyingDemosWindow`
to avoid temporal lookahead). The `go-cs-metrics` `backtest-dataset` command
automates this:

```sh
./go-cs-metrics backtest-dataset \
  --matches matches.yaml \
  --out playoff_matches.json
```

(Roster files are listed in `matches.yaml` alongside match metadata.)

---

## 11. Data format reference

### Roster file (`<team>-roster.json`)

```json
{
  "team": "<string>",
  "players": ["<SteamID64>", ...]
}
```

- SteamIDs: decimal string form of 64-bit ID (e.g. `"76561198034202275"`)
- Include all players you want to consider; top 5 by rounds played are selected

### Team stats file (`<team>.json`) — simbo3 input

```json
{
  "team": "<string>",

  "players_rating2_3m": [<float×5>],

  "maps": {
    "<MapName>": {
      "map_win_pct":          <float [0,1]>,
      "ct_round_win_pct":     <float [0,1]>,
      "t_round_win_pct":      <float [0,1]>,
      "matches_3m":           <int ≥ 0>,
      "entry_kill_rate":      <float, omitempty>,
      "entry_death_rate":     <float, omitempty>,
      "post_plant_t_win_pct": <float, omitempty>
    }
  },

  "trade_net_rate":  <float, omitempty>,
  "eco_win_pct":     <float [0,1], omitempty>,
  "force_win_pct":   <float [0,1], omitempty>,
  "rating_floor":    <float, omitempty>,

  "generated_at":      "<RFC3339>",
  "window_days":       <int>,
  "latest_match_date": "<YYYY-MM-DD>",
  "demo_count":        <int>
}
```

Map names must match the configured pool (case-sensitive). Default pool:
`Mirage`, `Inferno`, `Nuke`, `Ancient`, `Overpass`, `Dust2`, `Train`.

### Coefficient config file (`tuned.json`) — simbo3 override

Any subset of Config fields. Missing fields keep their defaults.

```json
{
  "alpha": 1.1,
  "beta": 1.4,
  "gamma": 0.3,
  "k_reliability": 12.0,
  "delta": 0.8,
  "epsilon": 0.5
}
```

### Backtest dataset (`matches.json`)

```json
[
  {
    "match_id": "<string, optional>",
    "team_a": { <TeamStats without provenance fields> },
    "team_b": { <TeamStats without provenance fields> },
    "maps": [
      {
        "map":        "<MapName>",
        "picker":     "A" | "B" | "D",
        "a_start_ct": <bool, optional>
      }
    ],
    "a_won_series": <bool>
  }
]
```

---

## 12. Cross-cutting concerns

### Idempotency

- **demoget sync**: each phase checks existing DB state; already-processed items are skipped
- **go-cs-metrics parse**: quick-hash (SHA-256 of first 64 KB) checked before full parse; existing demos skipped
- **go-cs-metrics export**: read-only; always safe to re-run

### The mtime contract

`go-cs-metrics parse` reads the `.dem` file's mtime as `match_date`. This is the
**only** source of truth for match date in `metrics.db`. Always run
`demoget touch-dates` after `demoget sync` and before `go-cs-metrics parse`.

### `--dir` is not recursive

`go-cs-metrics parse --dir` only finds `.dem` files **directly** in the given
directory. Pass each event subdirectory individually:

```sh
# Correct:
./go-cs-metrics parse --dir ~/demos/pro/iem_cologne_2025/

# Wrong — finds nothing (no .dem files directly in pro/):
./go-cs-metrics parse --dir ~/demos/pro/
```

### Stale exports

The `latest_match_date` field in the team JSON shows the most recent demo
included. Before simulating a match, verify this is recent:

```sh
jq '.latest_match_date' navi.json faze.json
```

If it's more than a few weeks old, new demos may have been played. Re-download,
re-parse, and re-export before trusting the forecast.

### Quorum tuning

The `--quorum` flag controls how strictly demos are filtered to "team" games.
Higher quorum = more confident the demo represents the team but fewer demos.

| Situation | Recommendation |
|---|---|
| Export returns zero demos | Try `--quorum 2` |
| Team has recent roster change | Try `--quorum 4` to exclude transition demos |
| Sparse DB (few events parsed) | Try `--since 180` |

### Map pool consistency

The map pool in simbo3's config (`DefaultConfig().MapPool`) must match the maps
teams actually play. If a team has stats for a map not in the pool, simbo3
warns but continues. If the competitive map pool changes, update:
1. `cs2-pro-match-simulator/internal/model/model.go` — `DefaultConfig().MapPool`
2. The `input-format.md` doc in that repo

### New metrics: backward compatibility

Fields added to the team JSON after the initial schema (`entry_kill_rate`,
`entry_death_rate`, `post_plant_t_win_pct`, `trade_net_rate`, `eco_win_pct`,
`force_win_pct`, `rating_floor`) all use `omitempty`. Old JSON files without
these fields are still valid; simbo3 reads them as zero (neutral — no model
adjustment). New coefficient defaults (`delta=0`, `epsilon=0`) mean existing
configs also produce identical output.

---

## 13. Partial-workflow quick reference

### Ingest one new event

```sh
cd ~/git/cs-demo-downloader
./demoget sync --event iem_krakow_2026 --out ~/demos/pro
./demoget touch-dates --out ~/demos/pro

cd ~/git/go-cs-metrics
GOMEMLIMIT=4294967296 ./go-cs-metrics parse \
  --dir ~/demos/pro/iem_krakow_2026/ --tier pro --workers 1
```

### Re-export and simulate after new demos

```sh
cd ~/git/go-cs-metrics
./go-cs-metrics export --roster rosters/navi-roster.json --since 90 --quorum 3 --out navi.json
./go-cs-metrics export --roster rosters/vitality-roster.json --since 90 --quorum 3 --out vitality.json

cd ~/git/cs2-pro-match-simulator
./bin/simbo3 run --teamA navi.json --teamB vitality.json --format bo3
```

### Find players in DB

```sh
./go-cs-metrics sql "SELECT DISTINCT steam_id, name FROM player_match_stats ORDER BY name"
```

### Check download progress

```sh
cd ~/git/cs-demo-downloader
./demoget summary
./demoget status
```

### Check what demos are parsed

```sh
cd ~/git/go-cs-metrics
./go-cs-metrics list
./go-cs-metrics summary
```

### Tune coefficients against historical data

```sh
cd ~/git/cs2-pro-match-simulator
./bin/simbo3 tune --dataset playoff_matches.json --seed 42 --rounds 5
# Copy the printed JSON to tuned.json, then:
./bin/simbo3 run --teamA navi.json --teamB vitality.json --config tuned.json
```
