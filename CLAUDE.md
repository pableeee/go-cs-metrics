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
3. **Aggregation** — 8-pass algorithm producing `[]PlayerMatchStats`, `[]PlayerRoundStats`, `[]PlayerWeaponStats`, `[]PlayerDuelSegment`.
4. **Presentation** — CLI output via `tablewriter`; storage is SQLite.

Storage: **SQLite** via `modernc.org/sqlite` (pure Go, no CGo). Default DB: `~/.csmetrics/metrics.db`.

## CLI Commands

| Command | Description |
|---------|-------------|
| `parse <demo.dem>` | Parse + store a demo; print all tables |
| `list` | List all stored demos |
| `show <hash-prefix>` | Re-display a stored demo's tables |
| `fetch` | Download and ingest FACEIT baseline demos |
| `player <steamid64>...` | Cross-match aggregate report for one or more players |

All commands share `--db` to point at an alternate database.

## Data Model

Core types (all in `internal/model/model.go`):

- **`PlayerMatchStats`** — aggregated metrics per player per demo (35+ columns)
- **`PlayerRoundStats`** — per-round breakdown for drill-down
- **`PlayerWeaponStats`** — per-weapon kill/damage breakdown
- **`PlayerDuelSegment`** — FHHS counts per (weapon_bucket, distance_bin) per demo
- **`PlayerAggregate`** — cross-demo sums/averages used by the `player` command

## Aggregator: 8 Passes

1. Trade annotation (backward + forward scan within 5 s window)
2. Opening kills (first kill after `FreezeEndTick`)
3. Per-round per-player stats
4. Match-level rollup
5. Crosshair placement (from `RawFirstSight` / `m_bSpottedByMask`)
6. Duel engine + FHHS segments (exposure time, pre-shot correction, weapon+distance bins)
7. AWP death classifier (dry/repeek/isolated)
8. Flash quality window (effective flashes within 1.5 s)

## Key Implementation Notes

- **SteamID64 stored as TEXT** — avoids signed integer overflow for IDs above `2^63`.
- **`INSERT OR REPLACE`** everywhere — full idempotency; re-parsing the same demo hash is safe.
- **Wilson CI** used for FHHS proportions (stable for small samples unlike Wald).
- **Distance** computed as `||attackerPos − victimPos|| * 0.01905` (Hammer units → meters).
- **`player` command aggregation**: integers summed directly; float medians averaged across matches (approximate); FHHS rate recomputed from raw segment count totals (accurate).
- **Schema migrations**: no versioning yet — a DB rebuild (`rm metrics.db`) is required when the schema changes.

## Key Validation Rules

- Total kills must match scoreboard kills.
- ADR should roughly align with known sources for the same match.
- Unit-test trade logic thoroughly — the time-window heuristics are the most error-prone part.
