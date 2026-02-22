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
| `parse [<demo.dem>...] [--dir <dir>]` | Parse + store one or more demos; bulk mode prints compact status per demo |
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

## Key Implementation Notes

- **SteamID64 stored as TEXT** — avoids signed integer overflow for IDs above `2^63`.
- **`INSERT OR REPLACE`** everywhere — full idempotency; re-parsing the same demo hash is safe.
- **Wilson CI** used for FHHS proportions (stable for small samples unlike Wald).
- **Distance** computed as `||attackerPos − victimPos|| * 0.01905` (Hammer units → meters).
- **`player` command aggregation**: integers summed directly; float medians averaged across matches (approximate); FHHS rate recomputed from raw segment count totals (accurate).
- **Schema migrations**: new columns are added automatically at startup via `ALTER TABLE ... ADD COLUMN ... DEFAULT` statements (duplicate-column errors silently ignored). Existing rows default to `0`/`''`. A full DB rebuild is only required if a column type or a table structure changes (not just additions).

## Documentation Rule

**Every change — bug fix, feature, refactor, or behavioural tweak — must be reflected in ALL relevant docs files before the work is considered done.** This includes `README.md`, `docs/architecture.md`, and any other file under `docs/` that covers the modified area. When adding or changing a command, flag, metric, output table, or pipeline behaviour, update those files as part of the same change. Do not commit code changes without the corresponding doc updates.

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
