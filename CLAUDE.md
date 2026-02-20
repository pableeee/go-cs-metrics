# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go tool for parsing Counter-Strike 2 match demo files (`.dem`) and computing player/team performance metrics. The goal is automated, repeatable analysis: ingest demos, extract events, aggregate metrics, and surface actionable insights (what to train, where performance is weak).

## Build & Test Commands

Once implemented, standard Go tooling applies:

```sh
go build ./...
go test ./...
go test ./... -run TestName     # single test
go vet ./...
```

## Architecture

The processing pipeline has four stages:

1. **Ingestion** — Accept a `.dem` file, compute its hash, and store it.
2. **Parsing** — Convert the demo into structured, tick-based events.
3. **Aggregation** — Compute metrics from events and persist them.
4. **Presentation** — CLI output and/or local HTML report; later a lightweight web UI.

Storage: **SQLite** for MVP (portable, no server); Postgres if multi-user later.

## Data Model

Core entities:

- **Demo**: file hash, map, date, match type, tickrate
- **Match**: teams, score, rounds, players
- **Round**: economy summary, ordered event list
- **PlayerMatchStats**: aggregated metrics per player per match
- **PlayerRoundStats**: per-round breakdown (enables drill-down)
- **Event**: kill, damage, flash, grenade throw, bomb plant/defuse, etc.

## Metric Definitions

### MVP v1 (implement first)

- **General**: K/A/D, K/D ratio, HS kill %, ADR
- **Entry**: opening kill/death (first kill or first death of the round, split by side)
- **Trades**: trade kill success (player kills the enemy who killed a teammate within a 3–5 s window); trade death success (player dies and teammate trades the killer within the window). Implement successes before "fail" metrics—fail requires opportunity modeling and is prone to false positives.
- **Utility**: flash assists, enemies/friends flashed, average blind duration (enemy-only), utility damage (HE/molotov/incendiary), unused utility count at round end

Use "Composite Rating (beta)" as the label for any aggregate score—do not call it "HLTV Rating" until it matches closely.

### Phase 2 (aim metrics — defer)

TTK, TTD, crosshair placement, counter-strafe %, headshot hit %, recoil control proxies. These require careful engineering and validation; see `scope.md §5.2`.

## Key Validation Rules

- Total kills must match scoreboard kills.
- ADR should roughly align with known sources for the same match.
- Use golden demos (known matches with manually verified counts) as regression fixtures.
- Unit-test trade logic thoroughly—the time-window and proximity heuristics are the most error-prone part of the aggregation layer.
