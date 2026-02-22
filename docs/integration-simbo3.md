# Integration with cs2-pro-match-simulator (simbo3)

`go-cs-metrics export` bridges the demo parser to
[cs2-pro-match-simulator](https://github.com/pable/cs2-pro-match-simulator), which
forecasts CS2 BO3 outcomes via Monte Carlo simulation. This page covers the full
workflow from raw demos to a match prediction.

---

## How it works

`simbo3` takes one JSON file per team, containing:
- 5 player HLTV Rating 2.0 values (last 3 months)
- Per-map aggregate stats: win %, CT round win %, T round win %, matches played

`go-cs-metrics export` computes all of these from the demo database:

| simbo3 field | Source in go-cs-metrics |
|---|---|
| `map_win_pct` | `rounds_won * 2 > rounds_played` per demo, averaged |
| `ct_round_win_pct` | `player_round_stats` where `team='CT'`, `won_round=1` |
| `t_round_win_pct` | `player_round_stats` where `team='T'`, `won_round=1` |
| `matches_3m` | Count of qualifying demos in the `--since` window |
| `players_rating2_3m` | Rating 2.0 proxy (see below) |

The output also includes provenance and freshness fields that simbo3 ignores (unrecognised fields are discarded by standard JSON unmarshalling):

| Field | Description |
|---|---|
| `generated_at` | RFC3339 UTC timestamp of when the export was run |
| `window_days` | Actual look-back window used (value of `--since`; `_3m` naming is kept for simbo3 compatibility even if the window differs from 90 days) |
| `latest_match_date` | Date of the most recent qualifying demo in the sample (`YYYY-MM-DD`); use this to detect stale exports |
| `demo_count` | Total number of qualifying demos included |

---

## Rating 2.0 proxy

The official HLTV Rating 2.0 formula is proprietary. The exporter uses the
community approximation:

```
Rating ≈ 0.0073×KAST% + 0.3591×KPR − 0.5329×DPR + 0.2372×Impact + 0.0032×ADR + 0.1587
Impact  = 2.13×KPR + 0.42×APR − 0.41
```

Where all stats are summed across qualifying demos for the `--since` window, then
divided by total rounds played. Expect ±0.05–0.10 deviation from official HLTV numbers.

The 5 players with the most rounds played in the window are selected. Missing slots
(fewer than 5 players with data) are padded with `1.00` (neutral prior).

---

## Step-by-step workflow

### 1. Parse demos

Parse every demo you have for the teams involved:

```sh
cd ~/git/go-cs-metrics

# Parse all demos in a directory
./go-cs-metrics parse --dir /path/to/demos/

# Or parse individual files
./go-cs-metrics parse navi_vs_faze_mirage.dem navi_vs_faze_inferno.dem
```

If the demo directory contains an `event.json` sidecar (written automatically by `cs-demo-downloader`), the tier and event ID are read from it without any extra flags:

```sh
# Sidecar is present → tier and event_id populated automatically
./go-cs-metrics parse --dir ~/demos/pro/iem_cologne_2025/

# Override the auto-detected tier if needed
./go-cs-metrics parse --dir ~/demos/pro/iem_cologne_2025/ --tier pro
```

### 2. Create roster files

Each roster file maps a team name to its players' SteamID64s. Find SteamIDs from
HLTV, Steam profiles, or with:

```sh
./go-cs-metrics sql "SELECT DISTINCT steam_id, name FROM player_match_stats ORDER BY name"
```

Create one file per team:

```json
// navi-roster.json
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

```json
// faze-roster.json
{
  "team": "FaZe Clan",
  "players": [
    "76561197987713664",
    "76561198040577200",
    "76561198001695061",
    "76561198160786998",
    "76561198046033695"
  ]
}
```

### 3. Export team JSONs

```sh
./go-cs-metrics export --roster navi-roster.json --since 90 --quorum 3 --out navi.json
./go-cs-metrics export --roster faze-roster.json --since 90 --quorum 3 --out faze.json
```

**Flags:**

| Flag | Default | When to change |
|------|---------|----------------|
| `--since 90` | 90 days | Increase to 180 for more data; decrease to 60 for recency |
| `--quorum 3` | 3 players | Lower to 2 if demos are sparse; raise to 4 for stricter team-match filtering |
| `--out` | stdout | Omit to preview before writing |

**Diagnostic output** (stderr):

```
Querying demos for 5 players since 2025-11-23 (quorum=3)...
Found 34 qualifying demos
  Mirage        18 matches  win=0.67  CT=0.56  T=0.52
  Inferno       14 matches  win=0.71  CT=0.58  T=0.54
  ...
  s1mple               18 rounds  KPR=0.92 DPR=0.62 KAST=79%  ADR=91.3  → rating 1.19
  ...
Wrote navi.json
```

### 4. Run the simulator

```sh
cd ~/git/cs2-pro-match-simulator

# Full veto simulation (default)
go run ./cmd/simbo3/ run --teamA navi.json --teamB faze.json

# Manual maps
go run ./cmd/simbo3/ run \
  --teamA navi.json --teamB faze.json \
  --mode manual \
  --maps Mirage,Inferno,Nuke \
  --picks A,B,D \
  --start-sides CT,T,rand

# JSON output for scripting
go run ./cmd/simbo3/ run --teamA navi.json --teamB faze.json --output json
```

---

## Script: full pipeline in one shot

Save as `predict.sh` and run `./predict.sh navi-roster.json faze-roster.json`:

```sh
#!/usr/bin/env bash
set -euo pipefail

METRICS=~/git/go-cs-metrics/go-cs-metrics
SIM=~/git/cs2-pro-match-simulator

ROSTER_A=$1
ROSTER_B=$2
SINCE=${3:-90}
QUORUM=${4:-3}

OUT_A=$(mktemp /tmp/team_a_XXXXXX.json)
OUT_B=$(mktemp /tmp/team_b_XXXXXX.json)
trap 'rm -f "$OUT_A" "$OUT_B"' EXIT

echo "=== Exporting Team A ===" >&2
$METRICS export --roster "$ROSTER_A" --since "$SINCE" --quorum "$QUORUM" --out "$OUT_A"

echo "=== Exporting Team B ===" >&2
$METRICS export --roster "$ROSTER_B" --since "$SINCE" --quorum "$QUORUM" --out "$OUT_B"

echo "=== Running simulation ===" >&2
cd "$SIM"
go run ./cmd/simbo3/ run --teamA "$OUT_A" --teamB "$OUT_B"
```

---

## Caveats and limitations

- **No official team identities** — the database stores SteamIDs, not org names.
  You must maintain roster files externally.
- **Rating 2.0 is approximate** — official formula is proprietary; expect ±0.05–0.10
  deviation. Recalibrate weights if you have ground-truth HLTV ratings.
- **Small samples** — demos for a specific map may be sparse. Watch for maps with
  `matches_3m < 5`; simbo3 applies reliability shrinkage automatically, but very low
  counts will default heavily toward the 0.50 prior.
- **Mixed-team demos** — the `--quorum` filter ensures demos are relevant, but scrims
  or PUGs where players were on different sides may skew stats. Parse only competitive
  or FACEIT demos for best accuracy.
- **Draw handling** — draws (12–12 in MR12, 15–15 in MR15) are counted as 0.5 wins.
- **Stale exports** — check `latest_match_date` in the JSON before simulating. If the
  most recent qualifying demo is more than a few weeks old, re-parse newer demos and
  re-export before running a prediction.
