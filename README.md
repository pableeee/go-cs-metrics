# go-cs-metrics

A command-line tool for parsing Counter-Strike 2 match demos (`.dem`) and computing detailed player performance metrics. Designed for repeatable, automated analysis: ingest demos, extract tick-level events, aggregate metrics, and surface actionable insights — what to train, where performance is weak, and how you compare against players at different skill levels.

---

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [parse](#parse)
  - [list](#list)
  - [show](#show)
  - [player](#player)
  - [rounds](#rounds)
  - [trend](#trend)
  - [sql](#sql)
  - [drop](#drop)
  - [analyze](#analyze)
  - [export](#export)
  - [summary](#summary)
- [Integration with simbo3](#integration-with-simbo3)
- [Metric Definitions](#metric-definitions)
  - [General](#general)
  - [Entry Frags](#entry-frags)
  - [Trades](#trades)
  - [Utility](#utility)
  - [Crosshair Placement](#crosshair-placement)
  - [Duel Engine](#duel-engine)
  - [AWP Death Classifier](#awp-death-classifier)
  - [Flash Quality](#flash-quality)
  - [Weapon Breakdown](#weapon-breakdown)
- [Baseline Comparisons](#baseline-comparisons)
  - [Tier Tags](#tier-tags)
- [Database](#database)
- [Architecture](#architecture)
- [Development](#development)
- [Testing](#testing)
- [Known Limitations & Roadmap](#known-limitations--roadmap)

---

## Features

- **Full demo parsing** — tick-level event extraction using [`demoinfocs-golang`](https://github.com/markus-wa/demoinfocs-golang): kills, damage, flashes, weapon fires, spotted-flag transitions.
- **Rich metric suite** — K/D/A, ADR, KAST, HS%, entry frags, trade kills/deaths, utility damage, unused utility, flash assists, flash quality, crosshair placement, duel engine (exposure time, hits-to-kill, pre-shot correction), AWP death classification.
- **Role detection** — per-match heuristic label (AWPer / Entry / Support / Rifler) computed from kill distribution and opening/utility stats; shown in the player table.
- **Buy type** — eco/half/force/full classification per player per round, derived from equipment value at freeze-end; used in drill-down tables.
- **Aim timing** — Median TTK (ms from first shot fired to kill), Median TTD (ms from enemy's first shot to your death), and one-tap kill percentage.
- **Trade timing** — Median milliseconds between a trade kill and the kill being traded, and between a trade death and the teammate's retaliatory kill.
- **Round W/L tracking** — `won_round` flag per player per round; aggregated as win rate in the `player` and `analyze` commands; broken down by economy tier (eco/force/half/full) and post-plant context.
- **FHHS breakdown** — first-hit headshot rate segmented by weapon bucket and distance bin, with Wilson 95% CI and automatic priority bin detection.
- **Cross-match player analysis** — `player` command aggregates stats across all stored demos for one or more SteamID64s, producing a full overview + duel + AWP + FHHS + aim timing report per player.
- **Per-round drill-down** — `rounds` command shows per-round side, buy type, K/A/damage, KAST, and tactical flags for one player in one match, with a buy profile summary.
- **Per-weapon breakdown** — kills, HS%, assists, deaths, damage, hits, damage-per-hit per weapon per player.
- **Idempotent ingestion** — demos are SHA-256 hashed; re-parsing the same file is a no-op.
- **SQLite storage** — portable single-file database at `~/.csmetrics/metrics.db`; no server required.
- **Focus mode** — any output command accepts `--player <SteamID64>` to highlight your row and filter weapon tables to your stats only.

---

## Prerequisites

- **Go 1.24+**
- A CS2 `.dem` file (download manually from Refrag, cs-demo-manager, or CS2's Watch menu)

---

## Installation

```sh
# Clone the repo
git clone https://github.com/pable/go-cs-metrics
cd go-cs-metrics

# Build binary into repo root
make build

# Or install to ~/go/bin so it's in your PATH
make install
```

The binary is named `go-cs-metrics` (or `csmetrics` if you install via `go install`).

---

## Quick Start

```sh
# 1. Parse a demo and store its metrics
./go-cs-metrics parse /path/to/match.dem

# 2. Highlight your stats (replace with your Steam ID64)
./go-cs-metrics parse /path/to/match.dem --player 76561198XXXXXXXXX

# 3. List all stored demos
./go-cs-metrics list

# 4. Re-inspect a stored match by its hash prefix
./go-cs-metrics show a3f9c2 --player 76561198XXXXXXXXX

# 5. Cross-match analysis for a player (all stored demos)
./go-cs-metrics player 76561198XXXXXXXXX

# 6. Compare two players side-by-side
./go-cs-metrics player 76561198XXXXXXXXX 76561198012345678
```

---

## Commands

All commands share two global flags:

| Flag | Description |
|------|-------------|
| `--db <path>` | Path to SQLite database (default: `~/.csmetrics/metrics.db`) |
| `-s` / `--silent` | Hide metric explanations printed before each table (verbose output is shown by default) |

```sh
./go-cs-metrics --db /custom/path/metrics.db <command>
./go-cs-metrics -s player 76561198XXXXXXXXX
```

---

### parse

Parse one or more `.dem` files, aggregate all metrics, and store the results. If a demo was already parsed (same SHA-256 hash), the cached results are shown (single mode) or the file is skipped (bulk mode).

```
./go-cs-metrics parse [<demo.dem>...] [--dir <directory>] [flags]
```

**Bulk mode** — triggered when more than one demo is provided (via multiple args or `--dir`). Full tables are suppressed; a compact status line is printed per demo instead, followed by a stored/skipped/failed summary. Parse and aggregate elapsed times are included in the status line.

**Timing** — after each successfully processed demo, elapsed times for the parse and aggregate stages (and their total) are printed. In single mode this appears as a line before the tables; in bulk mode it is appended to the per-demo status line.

| Flag | Default | Description |
|------|---------|-------------|
| `--player` | `0` | SteamID64 of the player to highlight in output tables |
| `--type` | `Competitive` | Match type label stored in the database (e.g. `FACEIT`, `Scrim`) |
| `--tier` | `""` | Tier label for baseline comparisons (e.g. `faceit-5`, `premier-10k`); auto-detected from an `event.json` sidecar in the demo directory if present |
| `--baseline` | `false` | Mark this demo as a baseline reference match |
| `--dir` | `""` | Directory containing `.dem` files to parse in bulk (all `*.dem` files inside) |

**Output tables:**

1. **Match summary** — map, date, type, score, hash prefix
2. **Player stats** — K/A/D, K/D, HS%, ADR, KAST%, role, entry kills/deaths, trade kills/deaths, flash assists, effective flashes, utility damage, crosshair median angle
3. **Per-side breakdown** — K/A/D, K/D, ADR, KAST%, entry/trade counts split by CT and T halves
4. **Duel engine** — duel wins/losses, median exposure time on wins and losses, median hits-to-kill, first-bullet HS rate, pre-shot correction angle and % under 2°
5. **AWP death classifier** — total AWP deaths, % dry-peek, % re-peek, % isolated
6. **Weapon breakdown** — per-weapon kills, HS%, assists, deaths, damage, hits, damage-per-hit (filtered to `--player` if specified)
7. **Aim timing** — median TTK, median TTD, one-tap%, counter-strafe%

> **Note:** FHHS (first-hit headshot rate by weapon × distance) is only shown in the `player` command where cross-match sample sizes are large enough to be meaningful.

**Examples:**

```sh
# Single demo with focus player
./go-cs-metrics parse match.dem --player 76561198XXXXXXXXX --type Competitive

# Bulk: parse entire CS2 replays folder
./go-cs-metrics parse --dir '/path/to/csgo/replays' --player 76561198XXXXXXXXX

# Bulk: shell glob (multiple positional args)
./go-cs-metrics parse /replays/match730_*.dem
```

```
Parsing match.dem...
  parse: 4.2s  aggregate: 312ms  total: 4.512s

Map: de_mirage  |  Date: 2026-02-20  |  Type: Competitive  |  Score: CT 13 – T 7  |  Hash: a3f9c2d81b40

 ┌─────────────────────────────────────────────────────────────────────────────────────────────────────┐
 │                                          PLAYER STATS                                               │
 ├────┬────────────┬──────┬───┬───┬───┬──────┬─────┬───────┬───────┬─────────┬─────────┬─────────┬───┤
 │    │ NAME       │ TEAM │ K │ A │ D │  K/D │ HS% │   ADR │ KAST% │ ENTRY_K │ ENTRY_D │ TRADE_K │...│
 ├────┼────────────┼──────┼───┼───┼───┼──────┼─────┼───────┼───────┼─────────┼─────────┼─────────┼───┤
 │ > │ YourName   │ CT   │ 18│  4│ 12│ 1.50 │ 44% │ 87.3  │  73%  │       3 │       2 │       4 │...│
 ...
```

Bulk mode status line (includes timing):

```
[1/3] match1.dem
  stored: de_mirage  2024-11-01  13–5  10 players  18 rounds  (parse 4.2s  agg 312ms  total 4.512s)
```

---

### list

List all demos stored in the database, ordered by match date (newest first).

```
./go-cs-metrics list
```

**Output columns:** hash prefix, map, date, type, CT–T score, tickrate.

Map names are stored in normalized title-case form (e.g. `Mirage`, not `de_mirage`).

```
HASH            MAP       DATE        TYPE          SCORE   TICK
──────────────  ────────  ──────────  ────────────  ──────  ────
a3f9c2d81b40    Mirage    2026-02-20  Competitive   13-7    128
b7e1a4f03c22    Inferno   2026-02-18  FACEIT        16-14   64
...
```

---

### show

Display the full stats for a previously stored match by its hash prefix (at least 6 characters, enough to be unambiguous).

```
./go-cs-metrics show <hash-prefix> [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--player` | `0` | SteamID64 to highlight and filter weapon tables |

**Example:**

```sh
./go-cs-metrics show a3f9c2 --player 76561198XXXXXXXXX
```

Outputs the same four tables as `parse`.

---

### player

Aggregate all stored demo data for one or more SteamID64s and print a full cross-match performance report. Each player gets a sequential report with four tables.

```
./go-cs-metrics player <steamid64> [<steamid64>...] [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--map <name>` | `""` | Only include matches on this map (e.g. `nuke`, `de_nuke`; prefix stripped, case-insensitive) |
| `--since <date>` | `""` | Only include matches on or after this date (`YYYY-MM-DD`) |
| `--last <N>` | `0` | Only use the N most recent matches (applied after map/since filters) |
| `--top <N>` | `0` | Automatically append the top N players from the database by Rating 2.0 proxy; useful for comparing yourself against the strongest players in your demo set |
| `--top-min <N>` | `3` | Minimum number of qualifying demos a player must have to be considered for `--top` ranking |

**Output tables per player:**

1. **Overview** — matches played, K/A/D, K/D, HS%, ADR, KAST%, entry kills/deaths, trade kills/deaths, flash assists, effective flashes
2. **Duel profile** — duel wins/losses, average exposure time (win and loss), average hits-to-kill, average pre-shot correction
3. **AWP breakdown** — total AWP deaths with dry-peek %, re-peek %, and isolated %
4. **Map & side split** — K/D, HS%, ADR, KAST%, entry/trade counts broken down by map and side (CT/T)
5. **Aim timing** — role, average TTK, average TTD, one-tap%, average counter-strafe%
6. **FHHS table** — first-hit headshot rate by weapon bucket × distance bin, Wilson 95% CI, sample quality flags, priority bins marked with `*`

**Examples:**

```sh
./go-cs-metrics player 76561198XXXXXXXXX
./go-cs-metrics player 76561198XXXXXXXXX --map nuke
./go-cs-metrics player 76561198XXXXXXXXX --since 2026-01-01 --last 10

# Compare yourself against the top 5 players in your demo set
./go-cs-metrics player 76561198XXXXXXXXX --top 5

# Same but restricted to nuke and at least 5 qualifying demos per player
./go-cs-metrics player 76561198XXXXXXXXX --map nuke --top 5 --top-min 5
```

When `--top N` is used, the highest-rated players not already in the request are resolved from the database (same `--map`/`--since` filters applied; `--last` does not affect ranking), and a note is printed before the tables:

```
Top-5 by rating added: s1mple, NiKo, ZywOo, device, sh1ro
```

The **Rating 2.0 proxy** used for ranking (community approximation, not official HLTV math, expect ±0.05–0.10 deviation):

```
Impact = 2.13×KPR + 0.42×APR − 0.41
Rating ≈ 0.0073×KAST% + 0.3591×KPR − 0.5329×DPR + 0.2372×Impact + 0.0032×ADR + 0.1587
```

```
=== YourName (76561198XXXXXXXXX) — 38 matches ===

 PLAYER     | MATCHES | K   | A   | D   | K/D  | HS%  | ADR   | KAST% | ...
 YourName   |      38 | 760 | 220 | 580 | 1.31 | 40%  | 110.0 |  75%  | ...

 PLAYER     | W   | L   | AVG_EXPO_WIN | AVG_EXPO_LOSS | AVG_HITS/K | AVG_CORR
 YourName   | 620 | 550 |       800 ms |        400 ms |        2.4 |     2.5°

...

 BUCKET | DIST    | N  | FHHS  | 95% CI        | FLAG
 AK   * | 10-15m  | 79 | 14.3% | [7.7%, 25.0%] | OK
```

Integer stats (kills, duels, etc.) are **summed** across matches. Float medians (exposure, correction) are **averaged** per match. FHHS is computed from raw count totals for accuracy.

---

### rounds

Per-round drill-down table for one player in one match. Shows side, buy type, kills/assists/damage, KAST, and tactical flags per round, plus a buy profile summary line.

```
./go-cs-metrics rounds <hash-prefix> <steamid64> [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--clutch` | `false` | Only show clutch rounds (`CLUTCH_1vN`) |
| `--post-plant` | `false` | Only show post-plant rounds (`POST_PLT`) |
| `--side <CT\|T>` | `""` | Filter by side |
| `--buy <type>` | `""` | Filter by buy type: `eco`, `half`, `force`, `full` |

**Example:**

```sh
# All rounds
./go-cs-metrics rounds a3f9c2 76561198XXXXXXXXX

# Only clutch rounds
./go-cs-metrics rounds a3f9c2 76561198XXXXXXXXX --clutch

# Eco rounds on CT side
./go-cs-metrics rounds a3f9c2 76561198XXXXXXXXX --buy eco --side CT
```

```
=== PlayerName — Mirage — 25 rounds ===

 RD | SIDE | BUY   | K | A | DMG | KAST | FLAGS
  1 | CT   | full  | 2 | 0 | 150 | ✓    | OPEN_K
  2 | CT   | full  | 0 | 1 |  45 | ✓    |
  3 | CT   | eco   | 0 | 0 |   0 |      |
 ...

Buy Profile: full=14 (56%)  force=5 (20%)  half=3 (12%)  eco=3 (12%)
```

FLAGS: `OPEN_K` = opening kill, `OPEN_D` = opening death, `TRADE_K` = trade kill, `TRADE_D` = trade death, `POST_PLT` = bomb was planted this round, `CLUTCH_1vN` = player was last alive on their team facing N enemies.

> **Note:** New columns are added automatically at startup. Re-parse demos after an update to populate newly added metrics with correct values.

---

### trend

Chronological per-match performance trend for a single player. Shows two tables in ascending match-date order.

```
./go-cs-metrics trend <steamid64>
```

**Table 1 — Performance Trend:** DATE, MAP, RD (rounds), K, A, D, K/D, KPR (kills per round), ADR, KAST%

**Table 2 — Aim Timing Trend** (only shown if TTK/TTD data exists): DATE, MAP, RD, MEDIAN_TTK, MEDIAN_TTD, ONE_TAP%, CS%

**Example:**

```sh
./go-cs-metrics trend 76561198XXXXXXXXX
```

```
--- Performance Trend ---
 DATE        | MAP     | RD | K  | A | D  | K/D  | KPR  | ADR   | KAST%
 2026-01-10  | Mirage  | 24 | 18 | 5 | 14 | 1.29 | 0.75 |  82.3 |  71%
 2026-01-15  | Inferno | 26 | 22 | 3 | 11 | 2.00 | 0.85 |  97.1 |  77%
 ...
```

---

### drop

Permanently delete the metrics database file. All stored demo data is lost; re-parse your demos to rebuild.

```
./go-cs-metrics drop [--force]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--force` / `-f` | `false` | Skip the confirmation prompt and delete immediately |

Without `--force`, the command prints the database path and exits safely. Add `--force` to actually delete:

```sh
./go-cs-metrics drop --force
# Deleted: /home/user/.csmetrics/metrics.db
```

> Use this before re-parsing when a schema change requires a full rebuild.

---

### analyze

AI-powered grounded analysis. Serialises the tool's structured metrics into compact JSON and calls the Anthropic API with a natural-language question. The model can only reference data that was provided — hallucinated statistics are minimised by design. Opt-in: requires an Anthropic API key.

```
./go-cs-metrics analyze player <steamid64> [--map <map>] [--since <date>] [--last <N>] <question>
./go-cs-metrics analyze match  <hash-prefix> <question>
```

| Flag | Default | Description |
|------|---------|-------------|
| `--model` | `claude-haiku-4-5-20251001` | Anthropic model to use |
| `--api-key` | `""` | Anthropic API key (falls back to `$ANTHROPIC_API_KEY`) |
| `--map` *(player only)* | `""` | Filter to a specific map |
| `--since` *(player only)* | `""` | Filter to matches on or after this date (`YYYY-MM-DD`) |
| `--last` *(player only)* | `0` | Only use the N most recent matches |

**Setup:** set `ANTHROPIC_API_KEY` in your environment, or pass `--api-key sk-ant-...`.

**Examples:**

```sh
export ANTHROPIC_API_KEY=sk-ant-...

# Player analysis
./go-cs-metrics analyze player 76561198XXXXXXXXX "what's my biggest weakness?"
./go-cs-metrics analyze player 76561198XXXXXXXXX --map nuke "how do I perform on nuke CT vs T?"
./go-cs-metrics analyze player 76561198XXXXXXXXX --last 5 "has my aim improved recently?"

# Match analysis
./go-cs-metrics analyze match a3f9c2 "why did we lose this match?"
```

The response is rendered as formatted markdown in the terminal (via `glamour`) and clearly labelled as AI interpretation.

**Data sent to the model (`analyze player`):**

| Section | Contents |
|---------|----------|
| `overview` | role, K/D, HS%, ADR, KAST%, kills, assists, deaths, rounds, rounds_won, win_rate |
| `opening` / `trades` | kills/deaths; trade timing median ms |
| `utility` | flash assists, effective flashes, utility damage, unused utility |
| `aim` | median TTK, median TTD, one-tap%, correction°, counter-strafe% |
| `awp_deaths` | total, dry-peek %, re-peek %, isolated % |
| `clutch` | 1v1–1v5 wins/attempts/% |
| `map_side` | per-map CT/T K/D, ADR, KAST% |
| `trend` | chronological per-match stats including rounds_won |
| `fhhs` | per-weapon × distance FHHS with confidence tags |
| `fhhs_by_map` | same, grouped by map |
| `aim_by_map` | per-map TTK, TTD, correction°, CS%, one-tap% |
| `weapons` | per-weapon kills, HS%, damage, avg damage/hit |
| `buy_profile` | avg kills/damage/KAST%/win_rate by eco tier |
| `post_plant` | avg kills/damage/KAST%/win_rate in vs. outside post-plant |
| `low_confidence` | list of metrics with insufficient sample sizes |

---

### sql

Run an arbitrary SQL query against the metrics database and print the results as a formatted table. Useful for ad-hoc analysis and queries that go beyond the built-in commands.

```
./go-cs-metrics sql "<query>"
```

The query is passed as a single argument (quote it in the shell if it contains spaces). Results are printed with right-aligned numeric columns and a row count footer.

**Schema overview** (also shown in `./go-cs-metrics sql --help`):

| Table | Key columns |
|-------|-------------|
| `demos` | `hash`, `map_name`, `match_date`, `match_type`, `ct_score`, `t_score`, `tier`, `is_baseline`, `event_id` |
| `player_match_stats` | `demo_hash`, `steam_id` (TEXT), `name`, `kills`, `assists`, `deaths`, `total_damage`, `rounds_played`, `kast_rounds`, `role`, `median_ttk_ms`, `median_ttd_ms`, … |
| `player_round_stats` | `demo_hash`, `steam_id` (TEXT), `round_number`, `team`, `kills`, `damage`, `buy_type`, `is_post_plant`, `is_in_clutch`, `clutch_enemy_count`, … |
| `player_weapon_stats` | `demo_hash`, `steam_id` (TEXT), `weapon`, `kills`, `headshot_kills`, `damage`, `hits` |
| `player_duel_segments` | `demo_hash`, `steam_id` (TEXT), `weapon_bucket`, `distance_bin`, `duel_count`, `first_hit_count`, `first_hit_hs_count`, … |

> **Note:** `steam_id` is stored as TEXT. Use single quotes in WHERE clauses: `WHERE steam_id = '76561198031906602'`

**Examples:**

```sh
# Recent matches with scores
./go-cs-metrics sql "SELECT map_name, match_date, ct_score, t_score FROM demos ORDER BY match_date DESC LIMIT 5"

# Your ADR per map
./go-cs-metrics sql "
  SELECT d.map_name, ROUND(AVG(CAST(p.total_damage AS REAL)/p.rounds_played),1) AS adr
  FROM player_match_stats p JOIN demos d ON d.hash = p.demo_hash
  WHERE p.steam_id = '76561198XXXXXXXXX'
  GROUP BY d.map_name ORDER BY adr DESC"

# Clutch rounds for a player
./go-cs-metrics sql "
  SELECT round_number, clutch_enemy_count, kills, damage
  FROM player_round_stats
  WHERE steam_id = '76561198XXXXXXXXX' AND is_in_clutch = 1
  ORDER BY demo_hash, round_number"
```

---

### export

Export team aggregate stats as a JSON file in the format expected by
[cs2-pro-match-simulator (simbo3)](https://github.com/pable/cs2-pro-match-simulator).
Queries the database for a roster of SteamID64s and computes map win rates, CT/T round
win rates, match counts, and HLTV Rating 2.0 proxy values — all derived from the parsed
demo data.

```
./go-cs-metrics export [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--team <name>` | `""` | Team name written into the output JSON (required) |
| `--players <ids>` | `""` | Comma-separated SteamID64s (takes precedence over `--roster`) |
| `--roster <file>` | `""` | JSON file `{"team":"...","players":["...",...]}` |
| `--since <days>` | `90` | Look-back window in days |
| `--quorum <n>` | `3` | Minimum roster players that must appear in a demo for it to be included |
| `--out <file>` | `""` | Output path; defaults to stdout |

A demo is included if at least `--quorum` players from the roster appear in
`player_match_stats` for that demo within the `--since` window.

**Rating proxy formula** (community approximation of HLTV Rating 2.0):

```
Rating ≈ 0.0073*KAST% + 0.3591*KPR − 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
Impact  = 2.13*KPR + 0.42*APR − 0.41
```

The top 5 players by rounds played are selected. Fewer than 5 are padded with `1.00`.

**Output JSON structure:**

```json
{
  "team": "NaVi",
  "players_rating2_3m": [1.19, 1.12, 1.08, 1.03, 0.97],
  "maps": {
    "Mirage": { "map_win_pct": 0.67, "ct_round_win_pct": 0.56, "t_round_win_pct": 0.52, "matches_3m": 18 }
  },
  "generated_at": "2026-02-22T10:00:00Z",
  "window_days": 90,
  "latest_match_date": "2026-02-20",
  "demo_count": 34
}
```

`generated_at` and `window_days` record when and over what period the file was produced. `latest_match_date` is the most recent match in the qualifying sample — useful for detecting stale exports. `demo_count` is the total number of qualifying demos used.

> **Note:** `players_rating2_3m` and `matches_3m` use HLTV's conventional `_3m` naming regardless of `--since`. The actual window is captured in `window_days`. A warning is printed to stderr when `--since` is not 90.

**Example — inline roster:**

```sh
./go-cs-metrics export \
  --team "NaVi" \
  --players "76561198034202275,76561197992321696,76561198040577200,76561198121220486,76561198155383140" \
  --since 90 \
  --quorum 3 \
  --out navi.json
```

**Example — roster file:**

```sh
# navi-roster.json
# {"team": "Natus Vincere", "players": ["76561198034202275", ...]}

./go-cs-metrics export --roster navi-roster.json --out navi.json
```

See [Integration with simbo3](#integration-with-simbo3) for the full workflow.

---

### summary

Display a high-level overview of the entire database — useful for a quick health-check of what has been ingested.

```
./go-cs-metrics summary
```

No flags. Output sections:

1. **Overview** — total demos stored, date range, unique maps, unique players seen, total rounds across all demos.
2. **Maps** — per-map match count, CT wins, T wins, and CT win percentage.
3. **Most Active Players** — top 10 players by matches played, with their averaged K/D, ADR, and KAST%.
4. **Match Types** — breakdown by match type label (only shown when more than one type is present).

```
=== Database Summary ===

  Demos stored  : 42
  Date range    : 2025-08-01 → 2026-02-20
  Unique maps   : 7
  Players seen  : 34
  Total rounds  : 891

--- Maps ---

 MAP      | MATCHES | CT WINS | T WINS | CT WIN%
 Mirage  |      12 |       8 |      4 |    67%
 Inferno |       8 |       5 |      3 |    63%
 ...

--- Most Active Players ---

 NAME       | STEAM ID            | MATCHES | AVG K/D | AVG ADR | AVG KAST%
 PlayerOne  | 76561198XXXXXXXXX   |      12 |    1.35 |    82.1 |       72%
 ...
```

---

## Integration with simbo3

`go-cs-metrics export` bridges this tool to
[cs2-pro-match-simulator](https://github.com/pable/cs2-pro-match-simulator), which
forecasts CS2 BO3 match outcomes via Monte Carlo simulation.

**Full workflow:**

```sh
# 1. Parse demos for both teams (repeat for every match you have)
./go-cs-metrics parse /path/to/navi_vs_faze.dem

# 2. Export a simbo3-compatible JSON for each team
./go-cs-metrics export \
  --roster navi-roster.json \
  --since 90 --quorum 3 \
  --out navi.json

./go-cs-metrics export \
  --roster faze-roster.json \
  --since 90 --quorum 3 \
  --out faze.json

# 3. Run the simulator
cd ~/git/cs2-pro-match-simulator
go run ./cmd/simbo3/ run --teamA navi.json --teamB faze.json
```

**Roster file format:**

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

**Diagnostic output** (goes to stderr, JSON to stdout or `--out`):

```
Querying demos for 5 players since 2025-11-23 (quorum=3)...
Found 34 qualifying demos
  Mirage        18 matches  win=0.67  CT=0.56  T=0.52
  Inferno       14 matches  win=0.71  CT=0.58  T=0.54
  s1mple              18 rounds  KPR=0.92 DPR=0.62 KAST=79%  ADR=91.3  → rating 1.19
  ...
Wrote navi.json
```

**Checking export freshness:**

The output JSON includes `latest_match_date` and `demo_count` so you can quickly verify the data covers a recent period before running a simulation:

```sh
jq '{latest: .latest_match_date, demos: .demo_count, window: .window_days}' navi.json
# { "latest": "2026-02-20", "demos": 34, "window": 90 }
```

**Key caveats:**
- Rating 2.0 is a *proxy* — the official HLTV formula is proprietary. Expect ±0.05–0.10 deviation.
- The tool has no concept of team names; you must supply rosters as SteamID lists.
- Lower `--quorum` if few demos exist (but watch for noisy stats with small samples).
- Check `latest_match_date` in the JSON before simulating — a stale export will produce predictions based on outdated form.

---

## Metric Definitions

### General

| Metric | Definition |
|--------|------------|
| **K / A / D** | Kills, assists, deaths from kill events. Self-damage excluded. |
| **K/D Ratio** | `kills / deaths`. Infinity displayed as kill count if deaths = 0. |
| **HS%** | `headshot_kills / kills × 100`. Headshots to the body don't count. |
| **ADR** | `total_damage / rounds_played`. Damage is capped at victim's health (overkill not counted). |
| **KAST%** | Percentage of rounds where the player got a **K**ill, **A**ssist, **S**urvived, or was **T**raded (teammate killed the enemy who killed them within the trade window). |

---

### Entry Frags

The **opening kill** is the first kill of a round that occurs after freeze-time ends.

| Metric | Definition |
|--------|------------|
| **Entry Kills** | Rounds where the player got the opening kill. |
| **Entry Deaths** | Rounds where the player was the first to die. |

A player can appear in both columns (e.g., got a kill, then immediately died) only if their kill and death both came before any other kill — in practice this tracks the first kill only, so each round contributes at most one entry kill and one entry death across the whole team.

---

### Trades

A **trade** is detected within a 5-second window (configurable via `TicksPerSecond × 5`).

| Metric | Definition |
|--------|------------|
| **Trade Kills** | Rounds where the player killed an enemy who had just killed a teammate within the trade window. |
| **Trade Deaths** | Rounds where the player died and a teammate subsequently killed the player's killer within the trade window. |

**Algorithm:**
- For each kill K in a round (sorted by tick ascending):
  - Look backward within the window: if a prior kill J had `J.Killer == K.Victim` and `J.VictimTeam == K.KillerTeam`, then K is a trade kill.
  - Look forward within the window: if a subsequent kill J has `J.Victim == K.Killer` and `J.KillerTeam == K.VictimTeam`, then K is a trade death.

---

### Utility

| Metric | Definition |
|--------|------------|
| **Flash Assists (FA)** | Rounds where the player's flash blinded an enemy who was subsequently killed by a teammate (detected via `AssistedFlash` flag on the kill event). |
| **Utility Damage** | Total health damage dealt by HE grenades, molotovs, and incendiary grenades. |
| **Unused Utility** | Count of non-flash grenades (HE, molotov, incendiary, smoke, decoy) remaining in inventory at round end. High values indicate unexploited utility budget. |

---

### Crosshair Placement

Measured at the moment an enemy is **first spotted** each round (server-side `m_bSpottedByMask` transition). The angular deviation between the observer's crosshair direction and the enemy's head position is computed in 3D using the Source 2 forward-vector convention.

| Metric | Definition |
|--------|------------|
| **XHAIR_MED** | Median total angular deviation (degrees) across all first-sight encounters in the match. Lower = better pre-aim. |
| **% under 5°** | Percentage of encounters where the deviation was under 5°. |
| **Pitch / Yaw split** | Median deviations separated into vertical (pitch) and horizontal (yaw) components, useful for diagnosing whether placement errors are height-related or angle-related. |

> **Note:** The crosshair placement formula uses server-side visibility flags and manually computed eye heights due to a Source 2 limitation where `PositionEyes()` panics. Values should be treated as directional proxies, not absolute ground truth, until validated against a known demo.

---

### Duel Engine

Tracks the lifecycle of every kill: from the moment the killer first spotted the victim (`m_bSpottedByMask`) to the kill tick.

| Metric | Definition |
|--------|------------|
| **Duel Wins (W)** | Kills where the killer had prior sight of the victim before the kill tick. |
| **Duel Losses (L)** | Deaths (all deaths count as losses, regardless of whether the victim had sight of the killer). |
| **Median Exposure Win (ms)** | Median time between first sight and kill, across all duel wins. Shorter = faster reaction / better pre-aim. |
| **Median Exposure Loss (ms)** | Median time between the victim's first sight of the killer and the kill tick. 0 ms = victim never spotted the killer (peeked from behind / off-angle). |
| **Median Hits-to-Kill** | Median number of bullet hits required to complete a kill. Lower = better damage output per duel. |
| **First-Bullet HS Rate** | Percentage of duel wins where the first bullet hit was to the head. Measures crosshair placement at the moment of engagement. |
| **Pre-Shot Correction** | Angle (degrees) between the killer's view direction at first-sight and at the moment the first shot was fired. Measures how much the player had to adjust aim after seeing the enemy. |
| **% Correction < 2°** | Percentage of duels where the pre-shot correction was under 2°. Higher = already on-target when spotting. |

---

### AWP Death Classifier

Every death-by-AWP is automatically classified across three non-exclusive categories:

| Category | Definition |
|----------|------------|
| **Dry Peek (DRY%)** | No flash was thrown at the victim in the 3 seconds before the kill. The player peeked an AWP without cover. |
| **Re-Peek (REPEEK%)** | The victim had already gotten a kill earlier in that round before dying to the AWP. Indicates fighting over an angle the player already won once. |
| **Isolated (ISOLATED%)** | No alive teammates were within 512 units of the victim at kill time. The player was playing alone with no support. |

High DRY% → practice using flashes before peeking AWP angles.
High ISOLATED% → positioning/rotation issue; dying without support.
High REPEEK% → discipline issue; should reset after getting first kill.

---

### Flash Quality

| Metric | Definition |
|--------|------------|
| **Effective Flashes** | Enemy flashes where a blinded enemy was killed by the flasher's teammate within 1.5 seconds. Measures utility that directly converted to a kill. |

---

### Weapon Breakdown

Per player, per weapon (accessed via `show --player`):

| Column | Definition |
|--------|------------|
| K | Kills with this weapon |
| HS% | Headshot kill percentage |
| A | Assists |
| D | Deaths to this weapon |
| DAMAGE | Total health damage dealt |
| HITS | Total times a bullet connected |
| DMG/HIT | Average health damage per hit |

---

## Database

Default location: `~/.csmetrics/metrics.db` (SQLite, WAL mode, foreign keys on).

### Schema overview

**`demos`**

| Column | Type | Description |
|--------|------|-------------|
| `hash` | TEXT PK | SHA-256 of the raw `.dem` file |
| `map_name` | TEXT | Normalized title-case name, e.g. `Mirage` (stored without `de_` prefix) |
| `match_date` | TEXT | ISO 8601 date (demo file mtime — set by CS2 at match end) |
| `match_type` | TEXT | e.g. `Competitive`, `FACEIT`, `Scrim` |
| `tickrate` | REAL | Demo tickrate (64 or 128) |
| `ct_score` | INTEGER | Rounds won by CT |
| `t_score` | INTEGER | Rounds won by T |
| `tier` | TEXT | Skill tier label (e.g. `faceit-5`); auto-populated from `event.json` sidecar if present |
| `is_baseline` | INTEGER | 1 if reference corpus, 0 if personal match |
| `event_id` | TEXT | Event identifier from `event.json` sidecar (e.g. `iem_cologne_2025`); empty if unknown |

**`player_match_stats`** — one row per player per demo, with all aggregated metrics (36 columns). Unique on `(demo_hash, steam_id)`.

**`player_round_stats`** — one row per player per round per demo, for drill-down. Unique on `(demo_hash, steam_id, round_number)`.

**`player_weapon_stats`** — one row per player per weapon per demo. Unique on `(demo_hash, steam_id, weapon)`.

Schema migrations run automatically at startup via `ALTER TABLE ... ADD COLUMN` statements (errors on duplicate columns are silently ignored).

---

## Architecture

```
.dem file
    │
    ▼
┌──────────────────────────────┐
│  parser (internal/parser)    │  tick-level event extraction
│  demoinfocs-golang v4        │  kills, damage, flashes,
│  SHA-256 hash for dedup      │  weapon fires, spotted flags
└──────────────┬───────────────┘
               │  RawMatch
               ▼
┌──────────────────────────────┐
│  aggregator (internal/       │  11-pass aggregation:
│  aggregator)                 │  trade annotation + timing,
│                              │  opening kills, round W/L,
│                              │  KAST, crosshair, duel engine,
│                              │  AWP classifier, flash quality,
│                              │  role, TTK/TTD, counter-strafe
└──────────────┬───────────────┘
               │  PlayerMatchStats
               │  PlayerRoundStats
               │  PlayerWeaponStats
               ▼
┌──────────────────────────────┐
│  storage (internal/storage)  │  SQLite via modernc/sqlite
│  schema.sql embedded         │  INSERT OR REPLACE idempotency
│  WAL + foreign keys          │  automatic migrations
└──────────────┬───────────────┘
               │
               ▼
┌──────────────────────────────┐
│  report (internal/report)           │  terminal tables via
│  cmd/{parse,show,list,player,rounds, │  tablewriter, focus highlighting
│      trend,sql,analyze}             │
└─────────────────────────────────────┘
```

**Package layout:**

```
.
├── main.go
├── cmd/
│   ├── root.go      # cobra root, --db flag
│   ├── parse.go     # parse command
│   ├── list.go      # list command
│   ├── show.go      # show command
│   ├── player.go    # player command (cross-match aggregate report, --map/--since/--last)
│   ├── rounds.go    # rounds command (per-round drill-down)
│   ├── trend.go     # trend command (chronological per-match trend)
│   ├── sql.go       # sql command (raw SQL query)
│   └── analyze.go   # analyze command (AI-powered grounded analysis)
├── internal/
│   ├── model/       # data model structs (RawMatch, PlayerMatchStats, ...)
│   ├── parser/      # demo parsing, crosshair angle computation
│   ├── aggregator/  # multi-pass metric aggregation
│   ├── storage/     # SQLite schema + queries
│   ├── report/      # terminal table rendering
│   ├── faceit/      # FACEIT Data API v4 client (non-functional, preserved for future work)
│   └── steam/       # Steam share code decoder + Web API client (non-functional, preserved for future work)
└── Makefile
```

---

## Development

```sh
# Build
make build

# Run all tests
make test

# Verbose tests
make test-v

# Test coverage report (opens browser)
make test-cover

# Vet
make vet

# Tidy module graph
make tidy

# Remove binary and coverage output
make clean

# All checks + build
make all
```

---

## Testing

Unit tests live alongside their packages:

- `internal/aggregator/aggregator_test.go` — trade logic, KAST, opening kill detection
- `internal/storage/storage_test.go` — round-trip insert/query

Run a single test:
```sh
go test ./internal/aggregator/... -run TestTradeKill -v
```

**Validation approach:**

- **Golden demos**: parse a known match (e.g. a match with a published scoreboard) and assert that total kills, ADR, and score match the external source.
- **Trade invariants**: every trade kill must have a corresponding prior kill within the window by the same victim; every trade death must have a subsequent kill of the killer within the window.
- **KAST bounds**: KAST% must be in [0, 100] and must be ≥ survival rate.

---

## Known Limitations & Roadmap

### Current limitations

- **Match date**: Uses the demo file's modification time (`os.Stat` mtime), which reflects when CS2 wrote the demo to disk (end of match).
- **Crosshair placement**: Uses server-side `m_bSpottedByMask` as a proxy for first-sight. This may fire slightly before the player's client renders the enemy. Values should be treated as directional, not absolute.
- **Schema changes**: New columns are added automatically at startup via `ALTER TABLE ... ADD COLUMN ... DEFAULT 0/''`. Existing demos default to `0` for new integer columns (e.g. `rounds_won`, `won_round`) — re-parse demos to get accurate values for newly added metrics. A full DB rebuild is only required if a column type or table structure changes.
- **Automated demo download**: Both FACEIT and Valve MM automated download are non-functional due to platform authentication changes. See `docs/demo-download-automation.md` for details and a path forward.

### Planned

- ~~**CT/T side split table**~~ — done (per-side breakdown in `show`/`parse`).
- ~~**Role detection**~~ — done (AWPer/Entry/Support/Rifler heuristic, shown in player table).
- ~~**Buy type**~~ — done (eco/half/force/full per round from equipment value).
- ~~**Drill-down**~~ — done (`rounds` command shows per-round detail with buy type and flags).
- ~~**TTK/TTD**~~ — done (median ms from first hit to kill/death).
- ~~**Counter-strafe %**~~ — done. Shots fired at horizontal speed ≤ 34 u/s (≈ stopped/counter-strafed); shown as `CS%` in aim timing tables and `AVG_CS%` in the `player` command.
- ~~**Trend view**~~ — done (`trend` command, chronological KPR/ADR/KAST% and TTK/TTD tables per match).
- ~~**Round context**~~ — done (`POST_PLT` and `CLUTCH_1vN` flags in `rounds` drill-down).
- ~~**Player filters**~~ — done (`--map`, `--since`, `--last` on the `player` command).
- ~~**Raw SQL access**~~ — done (`sql` command for ad-hoc queries against the SQLite DB).
- ~~**Bulk demo parsing**~~ — done (`parse --dir` and multi-file args with compact bulk output).
- ~~**Round W/L tracking**~~ — done (`won_round` per round, aggregated as win rate; buy-profile and post-plant win rates in `analyze`).
- ~~**Trade timing**~~ — done (median ms between trade kill and the traded kill, and between trade death and teammate's retaliatory kill; surfaced in `analyze` context).
- ~~**AI-powered analysis**~~ — done (`analyze player` / `analyze match` via Anthropic API with grounded context; terminal markdown rendering via `glamour`).
- **Percentile comparison**: given a tier corpus, automatically show where your stats land (p25 / p50 / p75).
- **Local web UI**: lightweight browser-based dashboard for non-terminal users.
