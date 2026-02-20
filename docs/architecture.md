# go-cs-metrics — Architecture & Design Notes

## Overview

`go-cs-metrics` is a Go CLI tool that ingests Counter-Strike 2 `.dem` files, computes player performance metrics, persists results in a local SQLite database, and prints formatted tables to the terminal. The goal is repeatable, automated analysis of your own match history: ingest a demo once, query the results as many times as needed.

---

## Repository Layout

```
go-cs-metrics/
├── main.go                          # entry point — delegates to cmd.Execute()
├── cmd/
│   ├── root.go                      # root cobra command, --db flag
│   ├── parse.go                     # "parse <demo.dem>" — full pipeline
│   ├── list.go                      # "list" — tabulate stored demos
│   └── show.go                      # "show <hash-prefix>" — replay stored match
└── internal/
    ├── model/model.go               # all shared types; no external deps
    ├── parser/parser.go             # .dem → RawMatch
    ├── aggregator/
    │   ├── aggregator.go            # RawMatch → PlayerMatchStats + PlayerRoundStats
    │   └── aggregator_test.go       # unit tests for metric logic
    ├── storage/
    │   ├── schema.sql               # embedded SQL (go:embed)
    │   ├── storage.go               # DB open / schema apply
    │   ├── queries.go               # insert / query helpers
    │   └── storage_test.go          # round-trip tests against :memory:
    └── report/
        └── report.go                # terminal table formatting
```

All business logic lives under `internal/`. The `cmd/` layer is thin: it only wires flags to the pipeline and handles top-level errors.

---

## Processing Pipeline

```
.dem file
    │
    ▼
[parser]       ParseDemo(path, matchType) → *RawMatch
    │           • SHA-256 hash for idempotency key
    │           • streams events; builds flat slices of raw events
    │
    ▼
[aggregator]   Aggregate(raw) → ([]PlayerMatchStats, []PlayerRoundStats)
    │           • 4-pass algorithm over raw event slices
    │           • no I/O, no external dependencies
    │
    ▼
[storage]      InsertDemo / InsertPlayerMatchStats / InsertPlayerRoundStats
    │           • SQLite via modernc.org/sqlite (pure Go, no CGo)
    │           • INSERT OR REPLACE for full idempotency
    │
    ▼
[report]       PrintMatchSummary / PrintPlayerTable → stdout
```

The parser and aggregator are intentionally decoupled by the `RawMatch` intermediate representation. This means:

- The aggregator can be unit-tested with hand-crafted fixtures (no demo file required).
- The parser can be swapped or extended without touching metric logic.
- Future output targets (JSON, HTML, Postgres) only need to replace the storage/report stages.

---

## Key Design Decisions

### 1. SHA-256 hash as primary key

The demo file is hashed before parsing. This hash becomes the primary key in the `demos` table and the foreign key in both stats tables.

**Why:** Demo filenames are not stable (Steam renames them). The hash guarantees that re-parsing the exact same file is a no-op — the `parse` command detects the duplicate and shows cached results instead of re-inserting.

**Trade-off:** Hashing reads the entire file before parsing begins, requiring two sequential passes over the file (hash then parse). For typical demo files (100–400 MB) this is measurable but acceptable for a CLI tool that runs once per match. A future optimisation could interleave hashing and parsing with an `io.TeeReader`.

### 2. Two-level output from the aggregator

`Aggregate` returns two slices:

- `[]PlayerMatchStats` — one row per player per match (all metrics summed).
- `[]PlayerRoundStats` — one row per player per round (individual flags and counts).

Storing round-level data enables future drill-down queries ("show me all rounds where I had an opening kill but still lost the round") without re-parsing the demo. The cost is a larger database; at ~10 players × 30 rounds, each match adds ~300 round-stat rows, which is negligible for SQLite.

### 3. Pure-Go SQLite (`modernc.org/sqlite`)

CGo-based SQLite drivers require a C compiler and complicate cross-compilation. `modernc.org/sqlite` is a transpilation of the upstream SQLite C source to Go, requiring no CGo. The trade-off is a slightly larger binary and marginally slower performance, both irrelevant for this workload.

Connection options:
- `_foreign_keys=on` — enforces referential integrity between stats tables and `demos`.
- `_journal_mode=WAL` — better concurrent read performance; also safer for abrupt termination (WAL mode recovers cleanly on next open).

### 4. SteamID64 stored as TEXT

SteamID64 values exceed the range of a signed 64-bit integer (they use the full unsigned range up to 2^64). SQLite's `INTEGER` type is always signed; storing a large SteamID64 as an integer would corrupt values above `2^63 - 1`. Storing as `TEXT` with `strconv.FormatUint`/`ParseUint` in Go avoids this entirely.

### 5. Team captured at event time

Each `RawKill`, `RawDamage`, and `RawFlash` stores the team of each participant **at the time the event occurred**, not as a post-hoc lookup. This is critical because teams switch sides at halftime; a player who was CT in round 1 is T in round 16. If team were looked up from a global map at aggregation time, side-dependent metrics (entry kills by side, trade heuristics) would be wrong for half the rounds.

The parser's `RoundEnd` handler also snapshots `PlayerRoundEndState` including team at that moment, for the same reason.

### 6. `INSERT OR REPLACE` everywhere

All three insert operations use `INSERT OR REPLACE` (SQLite's upsert). This means:

- Re-running `parse` on an already-stored demo is safe (hash check catches it first, but the DB layer is also safe).
- If a parse run is interrupted after `InsertDemo` but before `InsertPlayerMatchStats`, re-running will cleanly overwrite the partial data.

The two bulk-insert operations (`InsertPlayerMatchStats`, `InsertPlayerRoundStats`) are each wrapped in a single transaction with a prepared statement, minimising round-trips to the database.

---

## Aggregator: Four-Pass Algorithm

The aggregator is the most complex component. It makes four sequential passes over the raw event data.

### Pass 1 — Trade annotation

Kills are grouped by round and sorted ascending by tick. For each kill `K` at index `i`:

**TradeKill** (backward scan): scan `j = i-1` downward while `K.Tick - kills[j].Tick ≤ tradeWindowTicks`. A prior kill `P` qualifies if:
- `P.KillerSteamID == K.VictimSteamID` — the player that K just killed had previously made a kill
- `P.VictimTeam == K.KillerTeam` — P's victim was a teammate of K's killer (i.e., P killed one of K's allies)

If so, K is a **trade kill**: K's killer avenged a fallen teammate.

**TradeDeath** (forward scan): scan `j = i+1` upward while `kills[j].Tick - K.Tick ≤ tradeWindowTicks`. A subsequent kill `N` qualifies if:
- `N.VictimSteamID == K.KillerSteamID` — K's killer is the one who gets killed next
- `N.KillerTeam == K.VictimTeam` — K's victim's teammates are doing the killing

If so, K is flagged `isTradeDeath`: K's killer was subsequently traded.

**Window**: `tradeWindowTicks = int(5.0 * raw.TicksPerSecond)`. The window is computed from the demo's actual tick rate so it represents the same real-time duration (5 seconds) regardless of whether the server runs at 64 or 128 Hz.

**Round isolation**: kill slices are per-round. The backward/forward scans never see kills from adjacent rounds, so a late-round kill can never trade with an early-next-round kill.

#### Semantic distinction between `IsTradeDeath` and `WasTraded`

These flags have distinct meanings applied to different players:

| Flag | Applied to | Meaning |
|------|-----------|---------|
| `IsTradeKill` | the killer of K | "I killed someone who had just killed my teammate" |
| `IsTradeDeath` | the **killer** of an `isTradeDeath`-flagged kill | "I made a kill but was then killed in retaliation" |
| `WasTraded` | the **victim** of an `isTradeDeath`-flagged kill | "I died, but my killer was subsequently killed by my teammate" |

`WasTraded` is the "T" in KAST — it earns the round for the victim because their death was not wasted. `IsTradeDeath` tracks the aggressor's behaviour: they opened a kill but were immediately countered.

This distinction was discovered during test writing. An initial implementation set `IsTradeDeath` on the victim (conflating it with `WasTraded`). The test `TestTradeKill_ExactlyAtWindow` caught this: it asserts that playerB (the killer who was traded) carries `IsTradeDeath`, while playerA (the victim who was avenged) carries `WasTraded`.

### Pass 2 — Opening kills

For each round, the first kill whose tick is `≥ round.FreezeEndTick` is the opening kill. The killer gets `IsOpeningKill`, the victim gets `IsOpeningDeath`.

The freeze-end guard filters out any kills that occur during the freeze period (which should not happen in normal gameplay but can appear in some demo recordings during warmup or glitch scenarios).

### Pass 3 — Per-round per-player stats

For every round, the set of participating players is the union of:
- Players present in `round.PlayerEndState` (snapshotted at `RoundEnd`)
- Players who appear as killer or victim in any kill that round

This union is necessary because a player who dies early may not appear in the end-state snapshot, but they still participated.

Damage and utility damage are indexed by `(playerID, roundNumber)` maps built before the main loop, avoiding O(n²) re-scanning of the damage slice.

Flash assists are derived from kills: if a kill has `AssistedFlash=true` and `AssisterSteamID != 0`, the assister gets a flash assist count increment. This piggybacks on the demo library's own flash-assist attribution rather than re-implementing it.

### Pass 4 — Match-level rollup

Match-level accumulators are incremented round-by-round in pass 3. Deaths and headshot kills are counted in a separate final loop over the raw kills list (rather than per-round), because a death is a property of the kill event, not of any particular player's round participation — a player who dies round 15 may not appear in the round-15 end-state snapshot if the snapshot was taken before the kill event processed.

---

## Parser: Event Handling Notes

The parser registers handlers for six event types from `demoinfocs-golang`:

| Event | Action |
|-------|--------|
| `RoundStart` | Increment round counter (skipped during warmup via `IsWarmupPeriod()`); record start tick |
| `RoundFreezetimeEnd` | Update freeze-end tick for current round |
| `RoundEnd` | Snapshot all active players' end-states; record round metadata |
| `Kill` | Append to kills slice; guard nil Killer/Victim |
| `PlayerHurt` | Append to damages slice; skip self-damage; classify utility weapons |
| `PlayerFlashed` | Append to flashes slice; use `FlashDuration()` method (not a field); skip zero-duration events |

**Warmup filtering**: `roundNumber` starts at 0 and only increments on `RoundStart` events that occur outside the warmup period. All event handlers guard on `roundNumber == 0`, so warmup events are silently discarded.

**Metadata**: Map name and tick rate are read from `p.Header()` and `p.TickRate()` **after** `ParseToEnd()` completes — they are not available before parsing.

**Match date**: The demo format does not reliably encode the wall-clock match date in the header. The parser records `time.Now()` at parse time as a reasonable approximation. This is a known limitation; a future improvement could extract the date from the demo filename (Steam's naming convention is `YYYYMMDD_HHMMSS`).

**Utility classification**: `isUtilityWeapon` returns true for `EqHE`, `EqMolotov`, and `EqIncendiary`. Fire grenades appear as either Molotov or Incendiary depending on which side threw them, so both are included. Flashbangs are explicitly excluded here (their contribution is tracked separately through flash events and the `AssistedFlash` flag on kills).

---

## Storage Schema

Three tables:

```
demos                        (hash PK, map, date, type, tickrate, ct_score, t_score)
  │
  ├── player_match_stats      (demo_hash FK, steam_id, aggregated metrics per match)
  │                           UNIQUE(demo_hash, steam_id)
  │
  └── player_round_stats      (demo_hash FK, steam_id, round_number, per-round flags)
                              UNIQUE(demo_hash, steam_id, round_number)
```

All three tables are created with `CREATE TABLE IF NOT EXISTS`, making schema application idempotent. Foreign keys are declared but not yet indexed on the child side — a future migration should add indexes on `(demo_hash)` in both stats tables for faster per-match queries.

Boolean columns are stored as `INTEGER` (0/1) rather than SQLite's `BOOLEAN` type alias, since SQLite has no true boolean type and explicit integers avoid any ambiguity across drivers.

---

## CLI Design

Three subcommands, all accessed via a persistent `--db` flag on the root command:

```
csmetrics --db /path/to/metrics.db parse match.dem [--player <steamid64>] [--type Label]
csmetrics list
csmetrics show <hash-prefix>
```

**`--db` default**: `~/.csmetrics/metrics.db`. The directory is created automatically by `parse` if it does not exist.

**`--player`**: A SteamID64 passed as a `uint64` flag. When set, the matching row in the player table is prefixed with `>` as a visual marker. This enables quick scanning of your own row in a 10-player table.

**Idempotency on `parse`**: The `parse` command hashes the demo file, then calls `DemoExists` before aggregating. If the hash is already in the database, it skips aggregation and storage entirely, printing the cached results. This makes it safe to re-run `parse` on the same file without duplicating data or wasting CPU time.

**`show <hash-prefix>`**: Rather than requiring the full 64-character SHA-256 hash, `show` accepts any prefix and uses a `LIKE` query (`hash LIKE 'prefix%'`). Typically 8–12 characters is enough to identify a specific match.

---

## Testing Strategy

### Aggregator tests (`internal/aggregator/aggregator_test.go`)

Tests operate on hand-crafted `RawMatch` values — no demo file is needed. Helper functions `makeRound` and `makeRaw` reduce fixture boilerplate.

| Test | What it verifies |
|------|-----------------|
| `TestTradeKill_ExactlyAtWindow` | Trade is detected at exactly 5.0 s (inclusive boundary) |
| `TestTradeKill_JustOverWindow` | Trade is NOT detected at 5.1 s (exclusive) |
| `TestTradeKill_DoesNotCrossRounds` | Trade logic is scoped per round; cross-round scenarios produce no trade |
| `TestKAST_Survived` | A player who survives without a kill/assist still earns KAST |
| `TestKAST_Traded` | A player who was killed but whose killer was traded earns KAST |
| `TestOpeningKill` | Only kills after `FreezeEndTick` qualify; pre-freeze kills are excluded |
| `TestADR_Basic` | Damage is accumulated correctly; ADR formula is correct |

The trade window tests deliberately probe the boundary condition that is most error-prone in practice: whether the comparison is `≤` (inclusive) or `<` (exclusive). The window is `int(5.0 * ticksPerSecond)` ticks, making the boundary tick-exact.

### Storage tests (`internal/storage/storage_test.go`)

Tests use an in-memory SQLite database (`:memory:`), opened via the same `Open()` function used in production. Each test opens a fresh database, so tests are fully isolated.

| Test | What it verifies |
|------|-----------------|
| `TestDemoInsertAndExists` | Insert then existence check; negative case |
| `TestListDemos` | Multiple demos ordered by date descending |
| `TestGetDemoByPrefix` | Prefix lookup; negative case returns nil, not error |
| `TestPlayerMatchStatsRoundTrip` | Full insert + query round-trip; field-level assertions on SteamID64 and team |
| `TestInsertIdempotency` | Second `InsertDemo` with same hash does not error |

---

## Known Limitations and Future Work

- **Match date**: Stored as `time.Now()` at parse time. Should parse from demo filename or header.
- **Demo file read**: Two sequential passes (hash, then parse). Could be made single-pass with `io.TeeReader`.
- **Flash tracking**: `RawFlash` is collected but only partially used (flash assists via kill events). Average blind duration and enemy flash counts are not yet surfaced in the output.
- **No composite rating**: `PlayerMatchStats` has all the ingredients for a composite score but none is computed yet. The label should be "Composite Rating (beta)" when added, not "HLTV Rating", until validation against known matches is complete.
- **Phase 2 metrics** (TTK, crosshair placement, recoil control) are explicitly out of scope for the current implementation and would require a different data collection strategy from the parser.
- **Schema migrations**: The current schema is applied with `IF NOT EXISTS`, which is safe for initial creation but provides no migration path for adding columns. A versioned migration scheme (e.g. tracking schema version in a `meta` table) would be needed before the schema is considered stable.
- **No index on FK columns**: `player_match_stats.demo_hash` and `player_round_stats.demo_hash` are not indexed. This is fine for the current query patterns (always full-scan of a single demo's rows) but will degrade as the database grows.
