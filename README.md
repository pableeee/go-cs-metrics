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
  - [fetch](#fetch)
  - [player](#player)
  - [rounds](#rounds)
  - [trend](#trend)
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
  - [FACEIT API Key](#faceit-api-key)
  - [Fetching Baseline Demos](#fetching-baseline-demos)
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
- **Aim timing & movement** — Median TTK (ms from first hit to kill), Median TTD (ms from first hit received to death), and Counter-Strafe % (shots fired while horizontal speed ≤ 34 u/s).
- **FHHS breakdown** — first-hit headshot rate segmented by weapon bucket and distance bin, with Wilson 95% CI and automatic priority bin detection.
- **Cross-match player analysis** — `player` command aggregates stats across all stored demos for one or more SteamID64s, producing a full overview + duel + AWP + FHHS + aim timing report per player.
- **Per-round drill-down** — `rounds` command shows per-round side, buy type, K/A/damage, KAST, and tactical flags for one player in one match, with a buy profile summary.
- **Per-weapon breakdown** — kills, HS%, assists, deaths, damage, hits, damage-per-hit per weapon per player.
- **Idempotent ingestion** — demos are SHA-256 hashed; re-parsing the same file is a no-op.
- **SQLite storage** — portable single-file database at `~/.csmetrics/metrics.db`; no server required.
- **FACEIT baseline fetching** — download demos from any FACEIT player's match history, tag them by tier, and build a reference corpus to compare yourself against.
- **Focus mode** — any output command accepts `--player <SteamID64>` to highlight your row and filter weapon tables to your stats only.

---

## Prerequisites

- **Go 1.24+**
- A CS2 `.dem` file, or a FACEIT API key for automated demo fetching

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

# 5. Fetch FACEIT baselines
./go-cs-metrics fetch --player <your-nickname> --count 10 --tier faceit-2
./go-cs-metrics fetch --player <level5-nickname> --level 5 --map de_mirage --count 20 --tier faceit-5

# 6. Cross-match analysis for a player (all stored demos)
./go-cs-metrics player 76561198XXXXXXXXX

# 7. Compare two players side-by-side
./go-cs-metrics player 76561198XXXXXXXXX 76561198012345678
```

---

## Commands

All commands share two global flags:

| Flag | Description |
|------|-------------|
| `--db <path>` | Path to SQLite database (default: `~/.csmetrics/metrics.db`) |
| `-v` / `--verbose` | Print a one-line explanation of each table's columns before rendering it |

```sh
./go-cs-metrics --db /custom/path/metrics.db <command>
./go-cs-metrics -v player 76561198XXXXXXXXX
```

---

### parse

Parse a `.dem` file, aggregate all metrics, and store the results. If the demo was already parsed (same SHA-256 hash), the cached results are shown instead.

```
./go-cs-metrics parse <demo.dem> [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--player` | `0` | SteamID64 of the player to highlight in output tables |
| `--type` | `Competitive` | Match type label stored in the database (e.g. `FACEIT`, `Scrim`) |
| `--tier` | `""` | Tier label for baseline comparisons (e.g. `faceit-5`, `premier-10k`) |
| `--baseline` | `false` | Mark this demo as a baseline reference match |

**Output tables:**

1. **Match summary** — map, date, type, score, hash prefix
2. **Player stats** — K/A/D, K/D, HS%, ADR, KAST%, role, entry kills/deaths, trade kills/deaths, flash assists, effective flashes, utility damage, crosshair median angle
3. **Per-side breakdown** — K/A/D, K/D, ADR, KAST%, entry/trade counts split by CT and T halves
4. **Duel engine** — duel wins/losses, median exposure time on wins and losses, median hits-to-kill, first-bullet HS rate, pre-shot correction angle and % under 2°
5. **AWP death classifier** — total AWP deaths, % dry-peek, % re-peek, % isolated
6. **FHHS table** — first-hit headshot rate by weapon bucket × distance bin, Wilson 95% CI, sample flags (OK/LOW/VERY_LOW), priority bins marked `*`
7. **Weapon breakdown** — per-weapon kills, HS%, assists, deaths, damage, hits, damage-per-hit (filtered to `--player` if specified)
8. **Aim timing & movement** — median TTK, median TTD, counter-strafe %

**Example:**

```sh
./go-cs-metrics parse match.dem --player 76561198XXXXXXXXX --type Competitive
```

```
Map: de_mirage  |  Date: 2026-02-20  |  Type: Competitive  |  Score: CT 13 – T 7  |  Hash: a3f9c2d81b40

 ┌─────────────────────────────────────────────────────────────────────────────────────────────────────┐
 │                                          PLAYER STATS                                               │
 ├────┬────────────┬──────┬───┬───┬───┬──────┬─────┬───────┬───────┬─────────┬─────────┬─────────┬───┤
 │    │ NAME       │ TEAM │ K │ A │ D │  K/D │ HS% │   ADR │ KAST% │ ENTRY_K │ ENTRY_D │ TRADE_K │...│
 ├────┼────────────┼──────┼───┼───┼───┼──────┼─────┼───────┼───────┼─────────┼─────────┼─────────┼───┤
 │ > │ YourName   │ CT   │ 18│  4│ 12│ 1.50 │ 44% │ 87.3  │  73%  │       3 │       2 │       4 │...│
 ...
```

---

### list

List all demos stored in the database, ordered by match date (newest first).

```
./go-cs-metrics list
```

**Output columns:** hash prefix, map, date, type, CT–T score, tickrate.

```
HASH            MAP           DATE        TYPE          SCORE   TICK
──────────────  ────────────  ──────────  ────────────  ──────  ────
a3f9c2d81b40    de_mirage     2026-02-20  Competitive   13-7    128
b7e1a4f03c22    de_inferno    2026-02-18  FACEIT        16-14   64
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

### fetch

Download demos from a FACEIT player's recent match history, parse them, and store them with a tier tag as baseline reference data. Requires a FACEIT Data API v4 key (see [FACEIT API Key](#faceit-api-key)).

```
./go-cs-metrics fetch --player <nickname|SteamID64> [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--player` | *(required)* | FACEIT nickname **or** Steam ID64 to use as the match source |
| `--map` | `""` | Only ingest matches played on this map (e.g. `de_mirage`) |
| `--level` | `0` | Only ingest matches at this FACEIT skill level (1–10) |
| `--count` | `10` | Number of matches to ingest |
| `--tier` | auto | Tier label stored in DB. Defaults to `faceit-N` when `--level` is set, `faceit` otherwise |

The command fetches up to 5× `--count` matches from the player's history to allow for map/level filtering, downloads and decompresses each `.dem.gz`, parses it, and stores it with `is_baseline=1`.

**Examples:**

```sh
# Your own recent matches
./go-cs-metrics fetch --player <your-nickname> --count 15 --tier faceit-2

# Level 5 baseline on Mirage (point at any level-5 seed player)
./go-cs-metrics fetch --player <nickname> --level 5 --map de_mirage --count 20

# Level 8 baseline, any map
./go-cs-metrics fetch --player <nickname> --level 8 --count 20 --tier faceit-8
```

**Progress output:**

```
Player: somePlayer  level=5  ELO=1247  region=EU
[1/10] 1-abc123  map=de_mirage       level=5  date=2026-02-15
  stored: 10 players, 24 rounds
[2/10] 1-def456  map=de_inferno      level=5  date=2026-02-14
  stored: 10 players, 29 rounds
[3/10] 1-ghi789  map=de_ancient      level=5  date=2026-02-14  [skip — map filter]
...

Done: 10/10 matches ingested (tier="faceit-5", is_baseline=true)
```

---

### player

Aggregate all stored demo data for one or more SteamID64s and print a full cross-match performance report. Each player gets a sequential report with four tables.

```
./go-cs-metrics player <steamid64> [<steamid64>...] [flags]
```

**Output tables per player:**

1. **Overview** — matches played, K/A/D, K/D, HS%, ADR, KAST%, entry kills/deaths, trade kills/deaths, flash assists, effective flashes
2. **Duel profile** — duel wins/losses, average exposure time (win and loss), average hits-to-kill, average pre-shot correction
3. **AWP breakdown** — total AWP deaths with dry-peek %, re-peek %, and isolated %
4. **Map & side split** — K/D, HS%, ADR, KAST%, entry/trade counts broken down by map and side (CT/T)
5. **Aim timing & movement** — role, average TTK, average TTD, average counter-strafe %
6. **FHHS table** — first-hit headshot rate by weapon bucket × distance bin, Wilson 95% CI, sample quality flags, priority bins marked with `*`

**Example:**

```sh
./go-cs-metrics player 76561198XXXXXXXXX
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
./go-cs-metrics rounds <hash-prefix> <steamid64>
```

**Example:**

```sh
./go-cs-metrics rounds a3f9c2 76561198XXXXXXXXX
```

```
=== PlayerName — de_mirage — 25 rounds ===

 RD | SIDE | BUY   | K | A | DMG | KAST | FLAGS
  1 | CT   | full  | 2 | 0 | 150 | ✓    | OPEN_K
  2 | CT   | full  | 0 | 1 |  45 | ✓    |
  3 | CT   | eco   | 0 | 0 |   0 |      |
 ...

Buy Profile: full=14 (56%)  force=5 (20%)  half=3 (12%)  eco=3 (12%)
```

FLAGS: `OPEN_K` = opening kill, `OPEN_D` = opening death, `TRADE_K` = trade kill, `TRADE_D` = trade death, `POST_PLT` = bomb was planted this round, `CLUTCH_1vN` = player was last alive on their team facing N enemies.

> **Note:** Schema changes require a DB rebuild: `rm ~/.csmetrics/metrics.db` and re-parse your demos.

---

### trend

Chronological per-match performance trend for a single player. Shows two tables in ascending match-date order.

```
./go-cs-metrics trend <steamid64>
```

**Table 1 — Performance Trend:** DATE, MAP, RD (rounds), K, A, D, K/D, KPR (kills per round), ADR, KAST%

**Table 2 — Aim Timing Trend** (only shown if TTK/TTD data exists): DATE, MAP, RD, MEDIAN_TTK, MEDIAN_TTD, ONE_TAP%

**Example:**

```sh
./go-cs-metrics trend 76561198XXXXXXXXX
```

```
--- Performance Trend ---
 DATE        | MAP     | RD | K  | A | D  | K/D  | KPR  | ADR   | KAST%
 2026-01-10  | mirage  | 24 | 18 | 5 | 14 | 1.29 | 0.75 |  82.3 |  71%
 2026-01-15  | inferno | 26 | 22 | 3 | 11 | 2.00 | 0.85 |  97.1 |  77%
 ...
```

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

## Baseline Comparisons

The database stores a `tier` label and `is_baseline` flag on every demo. This enables future queries comparing your metrics against a reference population at any skill level.

### FACEIT API Key

Register at [developers.faceit.com](https://developers.faceit.com) and create a server-side API key. Then store it in one of two ways:

**Environment variable** (add to `~/.zshrc` or `~/.bashrc`):
```sh
export FACEIT_API_KEY=your-key-here
```

**File** (read automatically, takes lower priority than the env var):
```sh
mkdir -p ~/.csmetrics
echo "your-key-here" > ~/.csmetrics/faceit_api_key
chmod 600 ~/.csmetrics/faceit_api_key
```

The `fetch` command checks `FACEIT_API_KEY` first, then falls back to `~/.csmetrics/faceit_api_key`.

---

### Fetching Baseline Demos

The strategy is to collect match demos from players at each skill tier you want to compare against. Since FACEIT skill levels map roughly to:

| FACEIT Level | ELO Range | CS2 Premier (approx) |
|-------------|-----------|----------------------|
| 1–2 | < 801 | < 8 000 |
| 3–4 | 801–1 100 | 8 000–12 000 |
| 5–6 | 1 101–1 500 | 12 000–16 000 |
| 7–8 | 1 501–2 100 | 16 000–20 000 |
| 9–10 | 2 100+ | 20 000+ |

A recommended baseline corpus (per map you care about):

```sh
# Your own tier
./go-cs-metrics fetch --player <your-nickname> --count 20 --tier faceit-2

# One step above
./go-cs-metrics fetch --player <level4-seed> --level 4 --map de_mirage --count 20

# Aspirational
./go-cs-metrics fetch --player <level7-seed> --level 7 --map de_mirage --count 20
./go-cs-metrics fetch --player <level7-seed> --level 7 --map de_inferno --count 20
```

To find seed players at a given level: check FACEIT leaderboards, ask in community Discord servers, or look at opponents from your own match history.

---

### Tier Tags

Any demo — whether fetched automatically or parsed manually — can carry a tier tag:

```sh
# Manually tag a demo as a baseline for Premier ~10k
./go-cs-metrics parse match.dem --tier premier-10k --baseline

# Tag a downloaded FACEIT demo at level 6
./go-cs-metrics parse faceit_match.dem --tier faceit-6 --baseline
```

Demos without a `--baseline` flag have `is_baseline=0` and represent your own personal matches. The separation lets you query:
```sql
-- Your stats on Mirage
SELECT * FROM player_match_stats
JOIN demos ON demos.hash = player_match_stats.demo_hash
WHERE demos.is_baseline = 0 AND demos.map_name = 'de_mirage'
AND player_match_stats.steam_id = '76561198XXXXXXXXX';

-- Level-5 player pool on Mirage for comparison
SELECT AVG(kills), AVG(total_damage / rounds_played) AS avg_adr
FROM player_match_stats
JOIN demos ON demos.hash = player_match_stats.demo_hash
WHERE demos.is_baseline = 1 AND demos.tier = 'faceit-5'
AND demos.map_name = 'de_mirage';
```

---

## Database

Default location: `~/.csmetrics/metrics.db` (SQLite, WAL mode, foreign keys on).

### Schema overview

**`demos`**

| Column | Type | Description |
|--------|------|-------------|
| `hash` | TEXT PK | SHA-256 of the raw `.dem` file |
| `map_name` | TEXT | e.g. `de_mirage` |
| `match_date` | TEXT | ISO 8601 date (parse date, not embedded match time) |
| `match_type` | TEXT | e.g. `Competitive`, `FACEIT`, `Scrim` |
| `tickrate` | REAL | Demo tickrate (64 or 128) |
| `ct_score` | INTEGER | Rounds won by CT |
| `t_score` | INTEGER | Rounds won by T |
| `tier` | TEXT | Skill tier label (e.g. `faceit-5`) |
| `is_baseline` | INTEGER | 1 if reference corpus, 0 if personal match |

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
│  aggregator (internal/       │  8-pass aggregation:
│  aggregator)                 │  trade annotation, opening
│                              │  kills, KAST, crosshair,
│                              │  duel engine, AWP classifier,
│                              │  flash quality, weapon stats
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
│  report (internal/report)    │  terminal tables via
│  cmd/{parse,show,list,       │  tablewriter, focus highlighting
│      fetch,player}           │
└──────────────────────────────┘

FACEIT baseline path:
  fetch cmd → internal/faceit/client → FACEIT Data API v4
            → download + gzip decompress → same parser/aggregator/storage
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
│   ├── fetch.go     # fetch command (FACEIT baseline download)
│   └── player.go    # player command (cross-match aggregate report)
├── internal/
│   ├── model/       # data model structs (RawMatch, PlayerMatchStats, ...)
│   ├── parser/      # demo parsing, crosshair angle computation
│   ├── aggregator/  # multi-pass metric aggregation
│   ├── storage/     # SQLite schema + queries
│   ├── report/      # terminal table rendering
│   └── faceit/      # FACEIT Data API v4 client
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

- **Match date**: Uses the demo file's modification time (`os.Stat` mtime), which reflects when CS2 wrote the demo to disk (end of match). FACEIT-fetched demos use the match's `started_at` API timestamp.
- **Crosshair placement**: Uses server-side `m_bSpottedByMask` as a proxy for first-sight. This may fire slightly before the player's client renders the enemy. Values should be treated as directional, not absolute.
- **Schema changes**: Adding new columns requires a DB rebuild (`rm ~/.csmetrics/metrics.db` and re-parse demos). Automatic migrations handle this for existing DBs via `ALTER TABLE`.
- **Demo availability**: FACEIT demo URLs are time-limited and may expire. Download soon after a match is played.
- **South America region**: The FACEIT player pool at specific levels is smaller than EU/NA; fetching large baseline corpora may require pulling from multiple regions.

### Planned

- ~~**CT/T side split table**~~ — done (per-side breakdown in `show`/`parse`).
- ~~**Role detection**~~ — done (AWPer/Entry/Support/Rifler heuristic, shown in player table).
- ~~**Buy type**~~ — done (eco/half/force/full per round from equipment value).
- ~~**Drill-down**~~ — done (`rounds` command shows per-round detail with buy type and flags).
- ~~**TTK/TTD**~~ — done (median ms from first hit to kill/death).
- ~~**Counter-strafe %**~~ — done (shots at horizontal speed ≤ 34 u/s).
- ~~**Trend view**~~ — done (`trend` command, chronological KPR/ADR/KAST% and TTK/TTD tables per match).
- ~~**Round context**~~ — done (`POST_PLT` and `CLUTCH_1vN` flags in `rounds` drill-down).
- **Percentile comparison**: given a tier corpus, automatically show where your stats land (p25 / p50 / p75).
- **Local web UI**: lightweight browser-based dashboard for non-terminal users.
