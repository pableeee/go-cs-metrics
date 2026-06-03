# CS2 Toolchain Pipeline Flow

Authoritative step-by-step specification of the full demo-to-forecast pipeline.
Every input, output, file format, flag, and caveat is documented here.

---

## Table of Contents

1. [Overview](#overview)
2. [Step 0 — VRS Sync](#step-0--vrs-sync)
3. [Step 1 — Demo Download (demoget sync)](#step-1--demo-download)
4. [Step 2 — Fix MTimes (demoget touch-dates)](#step-2--fix-mtimes)
5. [Step 3 — Demo Parse (go-cs-metrics parse)](#step-3--demo-parse)
6. [Step 3a — Demo Convert (go-cs-metrics convert)](#step-3a--demo-convert)
7. [Step 3b — DB Replay (go-cs-metrics replay)](#step-3b--db-replay)
8. [Step 4 — Team Export (go-cs-metrics export)](#step-4--team-export)
9. [Step 5 — Match Simulation (simbo3 run)](#step-5--match-simulation)
10. [JSON Schemas](#json-schemas)
11. [Shared State](#shared-state)
12. [Caveats](#caveats)

---

## Overview

```
[optional] vrs-sync
    │  fetches VRS snapshot markdown from GitHub → ~/.csmetrics/vrs.db
    ▼
events.yaml  (HLTV event IDs + filters)
    │
    ▼
demoget sync --out ~/demos/pro
    │  scrapes HLTV results → resolves demo links → downloads + extracts .dem
    │  state: ~/.csmetrics/demoget.db
    ▼
demoget touch-dates --out ~/demos/pro
    │  fixes .dem mtimes to actual match dates encoded in RAR filenames
    ▼
go-cs-metrics parse --dir ~/demos/pro/<event-slug>/ --tier pro   ← direct path
    │  11-pass aggregator → player/round/weapon/duel stats
    │  storage: ~/.csmetrics/metrics.db
    │
    │   ── OR (recommended for long-term storage) ──
    │
go-cs-metrics convert --dir ~/demos/pro/<event-slug>/ --tier pro
    │  single demoinfocs pass (parserv2) → .csraw2.tar alongside each .dem
    │  tar archive: header.json + per-stream parquet (kills, damages, weapon_fires,
    │  flashes, first_sights, grenades, bombs, item_pickups/drops, equip_changes,
    │  chat_commands, player_samples, projectile_samples). See docs/csraw-v2-spec.md.
    ▼
[optional] rm ~/demos/pro/<event-slug>/*.dem   ← reclaim disk space
    │
    ▼
go-cs-metrics replay --dir ~/demos/pro/<event-slug>/
    │  reads .csraw2.tar → csraw2bridge → 11-pass aggregator → metrics.db
    │  fast, low RAM; supports --workers > 1
    ▼
go-cs-metrics export --roster <team>-roster.json --since 90 --quorum 3 --out <team>.json
    │  map win%, CT/T round win%, Rating 2.0 proxy per player
    │  optional: VRS-stratified stats (vs_top30, vs_top20) when --vrs-db present
    ▼
simbo3 run --teamA a.json --teamB b.json [--format bo3] [--mode veto]
    │  Monte Carlo simulation → series win probabilities + score distribution
    │  uses VRS-stratified stats when vrs_global_rank fields are present
    ▼
match forecast
```

---

## Step 0 — VRS Sync

**Binary:** `go-cs-metrics`
**Command:** `go-cs-metrics vrs-sync`
**When to run:** Once, then periodically (monthly VRS updates).

### Inputs

| Source | Description |
|--------|-------------|
| GitHub API | `api.github.com/repos/ValveSoftware/counter-strike_regional_standings/contents/live/{year}` |
| Raw GitHub | `raw.githubusercontent.com/ValveSoftware/counter-strike_regional_standings/refs/heads/main/live/{year}/standings_global_YYYY_MM_DD.md` |

### Outputs

| Path | Description |
|------|-------------|
| `~/.csmetrics/vrs.db` | SQLite database of VRS snapshots |

### Schema (`vrs.db`)

```sql
CREATE TABLE vrs_snapshots (
    snapshot_date TEXT NOT NULL,   -- "YYYY-MM-DD"
    vrs_rank      INTEGER NOT NULL,
    team_name     TEXT NOT NULL,
    vrs_points    REAL NOT NULL,
    roster        TEXT NOT NULL,   -- JSON array: ["ZywOo","apEX","mezii","ropz","flameZ"]
    PRIMARY KEY (snapshot_date, team_name)
);
CREATE INDEX idx_vrs_date ON vrs_snapshots(snapshot_date);
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--vrs-db` | `~/.csmetrics/vrs.db` | Path to VRS SQLite database |
| `--year` | `2024,2025,2026` | Comma-separated years to fetch |
| `--force` | false | Re-fetch all dates, ignoring existing |

### Incremental Sync

By default, only snapshot dates not yet in the database are fetched. The GitHub
API directory listing is used to enumerate available files; the raw content URL
is fetched for each new file without authentication (public repo).

### Markdown Format Parsed

Files are named `standings_global_YYYY_MM_DD.md` and contain a table like:

```markdown
### Standings as of 2026_02_02

| Rank | Points | Team | Roster | Details |
|------|--------|------|--------|---------|
| 1  | 1951 | FURIA    | FalleN, KSCERATO, molodoy, YEKINDAR, yuurih | [details](...) |
| 2  | 1843 | G2       | NiKo, huNter-, jks, nexa, ropz               | [details](...) |
```

The parser extracts: rank (col 1), points (col 2), team name (col 3), roster as
comma-split player names (col 4, markdown links unwrapped).

---

## Step 1 — Demo Download

**Binary:** `demoget`
**Command:** `demoget sync --out ~/demos/pro`

### Inputs

| File | Description |
|------|-------------|
| `events.yaml` | List of HLTV event IDs and optional filters |

### Outputs

| Path | Description |
|------|-------------|
| `~/demos/pro/<event-slug>/*.dem` | Extracted CS2 demo files (mtime = extraction time, NOT match date) |
| `~/.csmetrics/demoget.db` | Download state: match discovery, URL resolution, completion status |

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--event` | (all) | Sync only this event ID |
| `--workers` | 4 | Parallel demo downloads (use 1 if HLTV throttles/403s) |
| `--limit` | 0 | Download at most N demos (0 = no limit) |
| `--reresolve` | false | Force re-resolution of all matches with no demo yet (overrides the time-bounded auto-retry) |
| `--dry-run` | false | Print planned work without downloading |

### Caveats

- `demoget sync` sets `.dem` file mtimes to the extraction timestamp (today).
- **Always run `demoget touch-dates` before `parse` to fix mtimes.**
- Corrupt RARs at HLTV's end (`corrupt decode header`, `decoder expected more data`) will not recover on retry.
- **Demos published after a sync:** HLTV often uploads a match's demo hours-to-days
  after it's played. A match resolved with no demo yet is marked `no_demo` (not
  `resolved`) and **auto-retried on later syncs for 14 days** (by `matches.first_seen`).
  After that window it's assumed permanently demo-less; use `demoget sync --reresolve`
  to force a re-check of all demo-less matches (e.g. for a finished older event whose
  demos appeared late).
- `--dir` mode only finds demos in the specific event subdirectory, not recursively.

---

## Step 2 — Fix MTimes

**Binary:** `demoget`
**Command:** `demoget touch-dates --out ~/demos/pro`

### What it does

Reads the actual match date from each RAR filename (HLTV encodes it there) and
sets the `.dem` file's mtime accordingly. `go-cs-metrics parse` uses file mtime
as `match_date` — incorrect mtimes produce wrong `match_date` values in the DB,
silently breaking `--since` filtering in `export`.

### When to run

After every `demoget sync`, before the first `parse` of newly downloaded demos.

---

## Step 3 — Demo Parse

**Binary:** `go-cs-metrics`
**Command:** `go-cs-metrics parse --dir ~/demos/pro/<event-slug>/ --tier pro`

### Inputs

| Source | Description |
|--------|-------------|
| `*.dem` files | CS2 demo files in the given directory (flat, non-recursive) |
| file mtime | Used as `match_date` — must be fixed by `touch-dates` first |

### Outputs

Written to `~/.csmetrics/metrics.db`:

| Table | Description |
|-------|-------------|
| `demos` | One row per demo: hash, map_name, match_date, event_id, tier |
| `player_match_stats` | Per-player per-demo aggregated stats (35+ columns) |
| `player_round_stats` | Per-round breakdown for each player |
| `player_weapon_stats` | Per-weapon kill/damage breakdown |
| `player_duel_segments` | FHHS duel segments (weapon+distance bins) |
| `grenade_events` | One row per grenade throw-to-land (smoke/flash/he/molotov/decoy) with throw + land positions; `match_date` and `map_name` denormalized for fast meta queries |
| `player_death_events` | One row per kill with victim/killer positions, victim yaw, distance, `was_flashed`, `was_traded`, `is_opening_death`, `round_phase`; `match_date` and `map_name` denormalized for heatmaps and meta tracking |

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | required | Directory containing `.dem` files (non-recursive) |
| `--tier` | `""` | Event tier tag stored in `demos.tier` (e.g. `pro`) |
| `--workers` | NumCPU | Parallel parse workers (use 1 for large batches to avoid OOM) |

### Memory

Set `GOMEMLIMIT=4294967296` for large batches:

```sh
GOMEMLIMIT=4294967296 ./go-cs-metrics parse --dir ~/demos/pro/iem_cologne_2025/ --tier pro --workers 1
```

---

## Step 3a — Demo Convert

**Binary:** `go-cs-metrics`
**Command:** `go-cs-metrics convert --dir ~/demos/pro/<event-slug>/ --tier pro`

Performs a single demoinfocs parse pass (via `internal/parserv2`) and writes
a `.csraw2.tar` archive alongside each `.dem`. The archive captures every
gameplay-relevant field the parser sees, so re-aggregation never needs to
revisit the `.dem`. Original `.dem` files can be deleted after conversion
to reclaim disk space — **but only once the `.csraw2.tar` actually exists**;
never delete a `.dem` just because `convert` exited. To make that safe,
`convert` exits **non-zero** when nothing was produced (`0 converted` with
`>0 failed`), which signals a systematic failure (e.g. a parser/demo-format
mismatch) rather than success. Partial batches still exit 0, so per-file
cleanup must check for each archive individually.

> **demoinfocs v5:** parsing uses the unreleased `demoinfocs-golang/v5`.
> CS2's April 2026 demo-format change panics v4.5.1 (`unable to find existing
> entity`); the fix is only on v5. Player velocity is derived from the
> inter-frame position delta (v5 dropped `Player.Velocity()`), and the map
> name is captured from the `CSVCMsg_ServerInfo` net message (v5 dropped the
> public `Parser.Header()`).

### Inputs

| Source | Description |
|--------|-------------|
| `*.dem` files | CS2 demo files in the given directory (flat, non-recursive) |
| file mtime | Used as `match_date` — must be fixed by `touch-dates` first |

### Outputs

| Path | Description |
|------|-------------|
| `*.csraw2.tar` | v2 intermediate archive alongside each `.dem` |

### File format (`.csraw2.tar`)

Uncompressed tar archive of `header.json` + one parquet file per event stream.
Per-stream parquet is zstd-compressed internally; the tar is for filesystem
ergonomics, not extra compression. Streams: `kills`, `damages`,
`weapon_fires`, `flashes`, `first_sights`, `grenade_throws`,
`grenade_detonations`, `bomb_actions`, `item_pickups`, `item_drops`,
`equip_changes`, `chat_commands`, `player_samples`, `projectile_samples`.
Full schema: `docs/csraw-v2-spec.md`.

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | required | Directory containing `.dem` files (non-recursive) |
| `--out-dir` | same as `--dir` | Destination directory for `.csraw2.tar` files |
| `--tier` | required | Tier label baked into `header.tier` (e.g. `pro`) |
| `--workers` | 1 | Parallel workers (same GOMEMLIMIT/RAM caveats as `parse`) |
| `--force` | false | Overwrite existing `.csraw2.tar` files |

### Memory

Same constraints as `parse` — the demoinfocs parse pass has identical memory pressure:

```sh
GOMEMLIMIT=4294967296 ./go-cs-metrics convert --dir ~/demos/pro/iem_cologne_2025/ --tier pro --workers 1
```

---

## Step 3b — DB Replay

**Binary:** `go-cs-metrics`
**Command:** `go-cs-metrics replay --dir ~/demos/pro/<event-slug>/`

Reads `.csraw2.tar` files produced by `convert`, runs them through the
`csraw2bridge` adapter to produce a `model.RawMatch`, and feeds that through
the 11-pass aggregator into `metrics.db`. Much faster and lower memory than
`parse` because no demoinfocs work is needed — only parquet decode and
aggregation.

Use `replay` for:
- **Initial ingest** after `convert` has produced `.csraw2.tar` files
- **Full DB rebuild** after metric changes: `drop --force` then `replay` all events

### Inputs

| Source | Description |
|--------|-------------|
| `*.csraw2.tar` files | v2 intermediate archives in the given directory |
| `event.json` sidecar | EventID and tier override (same as `parse`) |

### Outputs

Same as `parse` — writes to `metrics.db` tables: `demos`, `player_match_stats`,
`player_round_stats`, `player_weapon_stats`, `player_duel_segments`,
`grenade_events`, `player_death_events`.

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | required | Directory containing `.csraw2.tar` files (non-recursive) |
| `--tier` | from header | Override the tier label embedded in `header.tier` |
| `--workers` | 1 | Parallel load+aggregate workers (safe to increase — low RAM) |
| `--force` | false | Re-aggregate even if demo hash already in DB |

---

## Step 4 — Team Export

**Binary:** `go-cs-metrics`
**Command:** `go-cs-metrics export --roster <team>-roster.json --since 90 --quorum 3 --out <team>.json`

### Inputs

| Source | Description |
|--------|-------------|
| `--roster` | JSON file: `{"team": "...", "players": ["steamid64", ...]}` |
| `metrics.db` | Parsed demo data |
| `vrs.db` (optional) | VRS snapshots for opponent quality stratification |

### Outputs

`<team>.json` — simbo3-compatible team stats JSON. See [JSON Schemas](#json-schemas).

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--roster` | required | Roster JSON file (mutually exclusive with `--players`) |
| `--players` | — | Comma-separated SteamID64s (overrides `--roster`) |
| `--team` | from roster | Team name override |
| `--since` | 90 | Look-back window in days |
| `--before` | today | Exclude demos on or after this date (useful for backtesting) |
| `--quorum` | 3 | Minimum roster players per demo to include it |
| `--half-life` | 35 | Temporal decay half-life in days (0 = uniform weights) |
| `--out` | stdout | Output file path |
| `--vrs-db` | `~/.csmetrics/vrs.db` | VRS database for stratified stats (skipped if absent) |

### Export Algorithm

1. **Qualifying demos**: demos where ≥ `quorum` roster players appear within `--since` days.
2. **Temporal weights**: `w(demo) = exp(-ln(2) / halfLife * days_before_refDate)`.
3. **Tier factor**: shrinks win-rate deviations toward 0.50 for non-top-tier event demos.
4. **Per-map stats**: `MapWinOutcomes` + `RoundSideStatsByDemo` → weighted win%/CT%/T%.
5. **Player ratings**: HLTV Rating 2.0 proxy per player, weighted by rounds played.
6. **Opponent norm**: weighted average opponent rating → scales team ratings toward baseline.
7. **VRS stratification** (if `--vrs-db` present):
   - Each demo's opponent is matched to a VRS team via player name lookup
   - Demos partitioned: all → vs_top30 → vs_top20 → vs_top10
   - Per-stratum map stats and ratings computed separately
   - Own team's VRS rank matched from roster player names

### Rating Formula

```
Rating ≈ 0.0073*KAST% + 0.3591*KPR - 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
Impact  = 2.13*KPR + 0.42*APR - 0.41
```

Community approximation of HLTV Rating 2.0. Expect ±0.05–0.10 vs. official HLTV.

### VRS Opponent Matching

- For each qualifying demo, opponent player names are looked up in the VRS snapshot
  dated on or before the match date.
- A match requires ≥ 3/5 player names (case-insensitive). Score of 2 is accepted
  if uniquely the best-matching team (absorbs stand-ins / accent variants).
- Unmatched demos are excluded from stratified stats but included in all-demos stats.
- `stderr` reports: `X/N demos matched (top30: Y, top20: Z, top10: W, unmatched: Z)`.

---

## Step 5 — Match Simulation

**Binary:** `simbo3`
**Command:** `simbo3 run --teamA a.json --teamB b.json [--format bo3]`

### Inputs

| File | Description |
|------|-------------|
| `--teamA` / `--teamB` | Team stats JSON files |
| `--config` (optional) | Coefficient override JSON |

### VRS-Stratified Stat Selection

When both team JSONs contain `vrs_global_rank` fields:

- For Team A's stats when facing Team B: selects the best available stratum
  based on Team B's `vrs_global_rank`:
  - B rank ≤ 10 and `vs_top10` stats present → use `vs_top10`
  - B rank ≤ 20 and `vs_top20` stats present → use `vs_top20`
  - B rank ≤ 30 and `vs_top30` stats present → use `vs_top30`
  - Otherwise → use all-demos baseline
- Same logic applies symmetrically for Team B facing Team A.
- Applies to map win%, CT/T win%, and player ratings.
- Falls back to the next broader stratum if the current one has `matches_3m < 2`.

### Key flags

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | bo3 | Series format: `bo1`, `bo3`, `bo5` |
| `--mode` | veto | Map selection: `veto` or `manual` |
| `--trials` | 50000 | Monte Carlo trial count |
| `--seed` | random | RNG seed for reproducibility |
| `--config` | defaults | Coefficient override file |
| `--explain` | false | Show intermediate stats and analytical breakdown |

---

## JSON Schemas

### Team Stats JSON (`simbo3TeamStats`)

```jsonc
{
  "team": "G2",
  "players_rating2_3m": [1.18, 1.14, 1.10, 1.05, 1.02],
  "maps": {
    "Mirage": {
      // All-demos baseline (always present)
      "map_win_pct": 0.62,
      "ct_round_win_pct": 0.53,
      "t_round_win_pct": 0.51,
      "matches_3m": 21,
      "entry_kill_rate": 0.31,
      "entry_death_rate": 0.19,
      "post_plant_t_win_pct": 0.74,

      // vs_top30 stratum (omitempty — present only when matches_3m_vs_top30 >= 2)
      "map_win_pct_vs_top30": 0.58,
      "ct_round_win_pct_vs_top30": 0.51,
      "t_round_win_pct_vs_top30": 0.49,
      "matches_3m_vs_top30": 9,

      // vs_top20 stratum (omitempty — present only when matches_3m_vs_top20 >= 2)
      "map_win_pct_vs_top20": 0.55,
      "ct_round_win_pct_vs_top20": 0.50,
      "t_round_win_pct_vs_top20": 0.48,
      "matches_3m_vs_top20": 4,

      // vs_top10 stratum (omitempty — present only when matches_3m_vs_top10 >= 2)
      "map_win_pct_vs_top10": 0.50,
      "ct_round_win_pct_vs_top10": 0.48,
      "t_round_win_pct_vs_top10": 0.46,
      "matches_3m_vs_top10": 2
    }
  },
  "generated_at": "2026-02-27T12:00:00Z",
  "window_days": 90,
  "latest_match_date": "2026-02-25",
  "demo_count": 36,
  "trade_net_rate": 0.02,
  "eco_win_pct": 0.21,
  "force_win_pct": 0.38,
  "rating_floor": 1.02,

  // VRS-stratified ratings (omitempty)
  "players_rating_vs_top30": [1.12, 1.08, 1.04, 0.99, 0.97],
  "players_rating_vs_top20": [1.09, 1.05, 1.01, 0.96, 0.94],
  "players_rating_vs_top10": [1.05, 1.01, 0.98, 0.93, 0.91],
  "demo_count_vs_top30": 9,
  "demo_count_vs_top20": 4,
  "demo_count_vs_top10": 2,

  // Own VRS rank (omitempty)
  "vrs_global_rank": 2,
  "vrs_snapshot_date": "2026-02-02"
}
```

### Roster File

```json
{
  "team": "G2 Esports",
  "players": ["76561198034202275", "76561198049926188", "76561198070377309", "76561198080748388", "76561197983956651"]
}
```

---

## Shared State

| Path | Owner | Contents |
|------|-------|----------|
| `~/.csmetrics/demoget.db` | demoget | Match discovery + download status |
| `~/.csmetrics/metrics.db` | go-cs-metrics | Parsed demo metrics |
| `~/.csmetrics/vrs.db` | go-cs-metrics vrs-sync | VRS global standing snapshots |
| `~/demos/pro/<event-slug>/` | demoget → go-cs-metrics | Extracted `.dem` files |
| `~/demos/pro/<event-slug>/*.csraw2.tar` | go-cs-metrics convert | v2 intermediate archives (replaces `.dem` for long-term storage) |

---

## Caveats

### Rating 2.0 Approximation
Community formula, expect ±0.05–0.10 vs. official HLTV.

### Small Map Samples
Maps with `matches_3m < 5` default heavily toward 0.50 prior (shrinkage k=10).
Stratified strata with fewer than 2 qualifying matches are omitted entirely.

### top10 Stratum Activation
The `vs_top10` stratum only activates when a team has faced VRS top-10 opponents
in their qualifying window. For online/regional events (EPL group stage, ESL
Challenger), this stratum will typically be **inactive for all teams** — the field
is ranks ~15–60 and contains no top-10 opponents. The stratum engages at IEM,
BLAST, PGL, and other events where top-10 teams regularly meet each other.

### Stale Exports
Check `latest_match_date` in the team JSON before simulating.

### Quorum Tuning
If `export` finds no qualifying demos, try `--quorum 2` or `--since 180`.

### touch-dates Required
`demoget sync` sets mtime = today. `go-cs-metrics` uses mtime as `match_date`.
Skip `touch-dates` → every demo gets `match_date = extraction_date`, breaking
`--since` filtering in `export`. Run `touch-dates` after every sync.

### `--dir` Non-Recursive
`parse --dir` and `convert --dir` only find `.dem`/`.csraw2.tar` files directly in
the given directory. Always pass the event subdirectory
(`~/demos/pro/iem_cologne_2025/`), not the parent.

### VRS Player Name Matching
- Names are matched case-insensitively against VRS roster strings.
- Threshold of 3/5 absorbs 2 stand-ins or accent-stripped name variants per team.
- `LatestSnapshotBefore(matchDate)` is used — up to ~30 days of roster drift
  for monthly snapshots.
- Unmatched demos are excluded from stratified buckets but included in all-demos stats.
- EPL group-stage invitees (e.g. Gaimin Gladiators, SemperFi) often have
  **zero demos in the metrics DB** — they do not appear at top-tier LAN events.
  Export will fail even with `--quorum 1`. Use a prior-only JSON for these teams:
  `{"team":"Name","players_rating2_3m":[1.0,1.0,1.0,1.0,1.0],"maps":{}}`.
- These teams also typically have zero VRS presence if no snapshot covers their
  relevant time window.

### No-Data Teams
Teams without any parsed demos produce a JSON with `players_rating2_3m: [1.0, ...]`
and an empty `maps: {}`. The simulator will use all-prior stats for every map.

### Prior-Only Teams
If a team cannot be matched to VRS (zero DB presence), hand-craft a JSON with:
```json
{
  "team": "Unknown Team",
  "players_rating2_3m": [1.0, 1.0, 1.0, 1.0, 1.0],
  "maps": {}
}
```
Treat simulation output as pure noise.
