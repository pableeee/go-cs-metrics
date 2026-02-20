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
│   ├── fetch.go                     # "fetch" — ingest FACEIT demos via API
│   ├── list.go                      # "list" — tabulate stored demos
│   └── show.go                      # "show <hash-prefix>" — replay stored match
└── internal/
    ├── model/model.go               # all shared types; no external deps
    ├── parser/parser.go             # .dem → RawMatch
    ├── aggregator/
    │   ├── aggregator.go            # RawMatch → PlayerMatchStats + all segment types
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
    │           • captures: kills, damages (with positions), flashes,
    │             first-sight angles, weapon fires (with positions)
    │
    ▼
[aggregator]   Aggregate(raw) → ([]PlayerMatchStats, []PlayerRoundStats,
    │                            []PlayerWeaponStats, []PlayerDuelSegment, error)
    │           • 8-pass algorithm over raw event slices
    │           • no I/O, no external dependencies
    │
    ▼
[storage]      InsertDemo / InsertPlayerMatchStats / InsertPlayerRoundStats
    │           / InsertPlayerWeaponStats / InsertPlayerDuelSegments
    │           • SQLite via modernc.org/sqlite (pure Go, no CGo)
    │           • INSERT OR REPLACE for full idempotency
    │
    ▼
[report]       PrintMatchSummary / PrintPlayerTable / PrintDuelTable
               / PrintAWPTable / PrintFHHSTable / PrintWeaponTable → stdout
```

The parser and aggregator are intentionally decoupled by the `RawMatch` intermediate representation. This means:

- The aggregator can be unit-tested with hand-crafted fixtures (no demo file required).
- The parser can be swapped or extended without touching metric logic.
- Future output targets (JSON, HTML, Postgres) only need to replace the storage/report stages.

---

## Key Design Decisions

### 1. SHA-256 hash as primary key

The demo file is hashed before parsing. This hash becomes the primary key in the `demos` table and the foreign key in all stats tables.

**Why:** Demo filenames are not stable (Steam renames them). The hash guarantees that re-parsing the exact same file is a no-op — the `parse` command detects the duplicate and shows cached results instead of re-inserting.

**Trade-off:** Hashing reads the entire file before parsing begins, requiring two sequential passes over the file (hash then parse). For typical demo files (100–400 MB) this is measurable but acceptable for a CLI tool that runs once per match. A future optimisation could interleave hashing and parsing with an `io.TeeReader`.

### 2. Multi-level output from the aggregator

`Aggregate` returns four slices:

- `[]PlayerMatchStats` — one row per player per match (all metrics summed).
- `[]PlayerRoundStats` — one row per player per round (individual flags and counts).
- `[]PlayerWeaponStats` — one row per player per weapon (kill/damage breakdown).
- `[]PlayerDuelSegment` — one row per player per (weapon_bucket, distance_bin) (FHHS breakdown).

Storing all levels enables drill-down queries without re-parsing demos. Round-level data supports "show me all rounds where I had an opening kill but lost". Segment-level data supports "which weapon+distance combination has my lowest first-hit headshot rate".

### 3. Pure-Go SQLite (`modernc.org/sqlite`)

CGo-based SQLite drivers require a C compiler and complicate cross-compilation. `modernc.org/sqlite` is a transpilation of the upstream SQLite C source to Go, requiring no CGo. The trade-off is a slightly larger binary and marginally slower performance, both irrelevant for this workload.

Connection options:
- `_foreign_keys=on` — enforces referential integrity between stats tables and `demos`.
- `_journal_mode=WAL` — better concurrent read performance; also safer for abrupt termination (WAL mode recovers cleanly on next open).

### 4. SteamID64 stored as TEXT

SteamID64 values exceed the range of a signed 64-bit integer (they use the full unsigned range up to 2^64). SQLite's `INTEGER` type is always signed; storing a large SteamID64 as an integer would corrupt values above `2^63 - 1`. Storing as `TEXT` with `strconv.FormatUint`/`ParseUint` in Go avoids this entirely.

### 5. Team captured at event time

Each `RawKill`, `RawDamage`, and `RawFlash` stores the team of each participant **at the time the event occurred**, not as a post-hoc lookup. This is critical because teams switch sides at halftime.

### 6. `INSERT OR REPLACE` everywhere

All insert operations use `INSERT OR REPLACE` (SQLite's upsert). Re-running `parse` on an already-stored demo is safe — the hash check catches it first, but the DB layer is also idempotent. All bulk-insert operations are wrapped in a single transaction with a prepared statement, minimising round-trips.

### 7. Position capture in events (iteration 2)

World-space positions (`Vec3{X, Y, Z float64}` in Hammer units) are captured at event time:
- `RawWeaponFire.AttackerPos` — shooter position at fire tick.
- `RawDamage.VictimPos` — victim position at hurt tick.

These are used in the duel engine to compute distance at first-shot time and assign `distance_bin` to each duel. Distance in meters uses the constant `0.01905 units/meter`. This is cheap (one extra struct copy per event) and avoids the need for per-tick position tracking.

### 8. Wilson CI for FHHS segments

First-hit headshot rate per segment is reported with a 95% Wilson score confidence interval rather than a normal approximation. The Wilson CI is numerically stable for small proportions and small sample sizes (unlike the Wald interval). Segments are additionally flagged OK/LOW/VERY_LOW based on the denominator (≥50, 20–49, <20), so low-sample segments are visible but not over-emphasised.

---

## Aggregator: Eight-Pass Algorithm

The aggregator makes eight sequential passes over the raw event data.

### Pass 1 — Trade annotation

Kills are grouped by round and sorted ascending by tick. For each kill `K` at index `i`:

**TradeKill** (backward scan): scan `j = i-1` downward while `K.Tick - kills[j].Tick ≤ tradeWindowTicks`. A prior kill `P` qualifies if:
- `P.KillerSteamID == K.VictimSteamID` — the player that K just killed had previously made a kill
- `P.VictimTeam == K.KillerTeam` — P's victim was a teammate of K's killer

**TradeDeath** (forward scan): scan `j = i+1` upward while `kills[j].Tick - K.Tick ≤ tradeWindowTicks`. A subsequent kill `N` qualifies if:
- `N.VictimSteamID == K.KillerSteamID` — K's killer is the one who gets killed next
- `N.KillerTeam == K.VictimTeam` — K's victim's teammates are doing the killing

**Window**: `tradeWindowTicks = int(5.0 * raw.TicksPerSecond)`.

#### Semantic distinction between `IsTradeDeath` and `WasTraded`

| Flag | Applied to | Meaning |
|------|-----------|---------|
| `IsTradeKill` | the killer of K | "I killed someone who had just killed my teammate" |
| `IsTradeDeath` | the **killer** of an `isTradeDeath`-flagged kill | "I made a kill but was then killed in retaliation" |
| `WasTraded` | the **victim** of an `isTradeDeath`-flagged kill | "I died, but my killer was subsequently killed by my teammate" |

### Pass 2 — Opening kills

For each round, the first kill whose tick is `≥ round.FreezeEndTick` is the opening kill. The killer gets `IsOpeningKill`, the victim gets `IsOpeningDeath`.

### Pass 3 — Per-round per-player stats

For every round, participating players are the union of those in `round.PlayerEndState` and those who appear in kills. Damage and utility damage are indexed by `(playerID, roundNumber)` maps built before the main loop.

### Pass 4 — Match-level rollup

Match-level accumulators are incremented round-by-round in pass 3. Deaths and headshot kills are counted in a separate final loop over the raw kills list.

### Pass 5 — Crosshair placement (pitch/yaw split)

`RawFirstSight` events (emitted by the parser from server-side `m_bSpottedByMask` transitions) are aggregated per player. Metrics computed:
- `CrosshairMedianDeg` — total angular deviation (acos dot-product of forward vectors)
- `CrosshairMedianPitchDeg` — vertical component (atan2 decomposition)
- `CrosshairMedianYawDeg` — horizontal component (wrapped to [0, 180])
- `CrosshairPctUnder5` — fraction of encounters with deviation < 5°

### Pass 6 — Duel Engine + FHHS Segments

Builds three indexes: `firstSightIdx` (first-sight per observer/enemy/round), `duelDmgIdx` (non-utility damages sorted by tick), `wfIdx` (weapon fires sorted by tick).

For each kill, **win accounting** (killer had sight of victim before kill tick):
- Exposure time: `(killTick − sightTick) / tps * 1000` ms
- Hit count and first-hit hitgroup: scan damage list in `[sightTick, killTick]`
- Pre-shot correction: angle between observer's view at first-sight tick and at first weapon-fire tick (using absolute `ObserverPitchDeg`/`ObserverYawDeg` stored in `RawFirstSight`, not deviation fields)
- Attacker position: from first `RawWeaponFire` in window; victim position: from first `RawDamage` hit in window
- Distance (meters): `||attackerPos − victimPos|| * 0.01905`
- Bucket + bin → segment accumulator `(playerID, weaponBucket, distanceBin)`

For each kill, **loss accounting** (victim side): looks up victim's sight of killer; lossMs computed if found, otherwise 0ms (blind-side death).

After the kill loop, segment accumulators are converted to `[]PlayerDuelSegment` with median correction, median first-sight angle, and median exposure.

### Pass 7 — AWP Death Classifier

For each AWP kill, classifies the victim's death as:
- **DryPeek**: no flash on victim within the prior `3 * tps` ticks
- **RePeek**: victim had made a kill earlier in the same round
- **Isolated**: `NearbyVictimTeammates == 0` (captured by the parser at kill time)

These are non-exclusive — a death can be all three simultaneously.

### Pass 8 — Flash Quality Window

For each cross-team flash with `FlashDuration > 0`, checks if the blinded player was killed by the attacker's team within `1.5 * tps` ticks. Each such event increments `EffectiveFlashes` for the flash attacker.

---

## Parser: Event Handling Notes

The parser registers handlers for seven event types from `demoinfocs-golang`:

| Event | Action |
|-------|--------|
| `RoundStart` | Increment round counter (skipped during warmup); record start tick |
| `RoundFreezetimeEnd` | Update freeze-end tick for current round |
| `RoundEnd` | Snapshot all active players' end-states; record round metadata |
| `Kill` | Append to kills slice; count nearby alive teammates for AWP kills (512-unit radius) |
| `PlayerHurt` | Append to damages slice with hitgroup and victim position; skip self-damage |
| `PlayerFlashed` | Append to flashes slice; skip zero-duration events |
| `WeaponFire` | Append to weapon-fires slice with shooter position; skip utility/knife/warmup |

Additionally, the **frame-walk loop** inspects `m_bSpottedByMask` transitions every tick to emit `RawFirstSight` events — one per (observer, enemy, round) pair, recording crosshair deviation angles and absolute view angles.

**Absolute vs deviation angles in `RawFirstSight`**:
- `AngleDeg`, `PitchDeg`, `YawDeg` — deviation magnitudes (used for crosshair placement metrics in Pass 5)
- `ObserverPitchDeg`, `ObserverYawDeg` — absolute view angles at first-sight tick (used for pre-shot correction in Pass 6; combining deviation fields with weapon-fire angles would produce nonsensical deltas)

---

## Storage Schema

Six tables:

```
demos                         (hash PK, map, date, type, tickrate, ct_score, t_score, tier, is_baseline)
  │
  ├── player_match_stats       (demo_hash FK, steam_id, ~35 aggregated metric columns)
  │                            UNIQUE(demo_hash, steam_id)
  │
  ├── player_round_stats       (demo_hash FK, steam_id, round_number, per-round flags)
  │                            UNIQUE(demo_hash, steam_id, round_number)
  │
  ├── player_weapon_stats      (demo_hash FK, steam_id, weapon, kills, hs_kills, damage, hits)
  │                            UNIQUE(demo_hash, steam_id, weapon)
  │
  └── player_duel_segments     (demo_hash FK, steam_id, weapon_bucket, distance_bin,
                                duel_count, first_hit_count, first_hit_hs_count,
                                median_corr_deg, median_sight_deg, median_expo_win_ms)
                               UNIQUE(demo_hash, steam_id, weapon_bucket, distance_bin)
```

`demos` also carries `tier TEXT` (e.g. `"faceit-5"`) and `is_baseline INTEGER` for cross-demo comparison purposes. All tables use `CREATE TABLE IF NOT EXISTS`; schema migration is not yet versioned.

---

## CLI Design

Subcommands, all accessed via a persistent `--db` flag on the root command:

```
csmetrics parse match.dem [--player <steamid64>] [--type Label] [--tier Label] [--baseline]
csmetrics list
csmetrics show <hash-prefix> [--player <steamid64>]
csmetrics fetch [flags]
```

**Output order** for `parse` and `show`:
1. Match summary (map, date, score, hash)
2. Player table — K/A/D, ADR, KAST%, entries, trades, flash assists, effective flashes, xhair median
3. Duel table — W/L counts, median exposure win/loss ms, hits/kill, first-hit HS%, pre-shot correction
4. AWP table — AWP deaths with dry%/repeek%/isolated%
5. FHHS table — first-hit HS rate by (weapon, distance bin) with Wilson 95% CI and sample flags; priority bins marked with `*` and summarised below the table
6. Weapon table — per-weapon kills, HS%, damage, hits

---

## Testing Strategy

### Aggregator tests (`internal/aggregator/aggregator_test.go`)

Tests operate on hand-crafted `RawMatch` values — no demo file is needed.

| Test | What it verifies |
|------|-----------------|
| `TestTradeKill_ExactlyAtWindow` | Trade detected at exactly 5.0 s (inclusive boundary) |
| `TestTradeKill_JustOverWindow` | Trade NOT detected at 5.1 s (exclusive) |
| `TestTradeKill_DoesNotCrossRounds` | Trade logic scoped per round |
| `TestKAST_Survived` | Surviving without kill/assist earns KAST |
| `TestKAST_Traded` | Dying and having killer traded earns KAST |
| `TestOpeningKill` | Only kills after `FreezeEndTick` qualify |
| `TestCrosshairAggregation` | First-sight events produce correct median and pct-under-5 |
| `TestCrosshairAggregation_NoData` | No first-sight events → all fields zero |
| `TestDuelEngine_BasicWin` | One kill with head-hit damage + first sight → DuelWins=1, FirstHitHSRate=100 |
| `TestWeaponBucket` | Weapon name strings map to correct bucket labels |
| `TestDistanceBin` | Distance values map to correct bins; edge cases at boundaries |
| `TestFHHSSegment` | Duel with weapon fire (position) + head-hit damage → correct segment bucket and counts |
| `TestADR_Basic` | Damage accumulated correctly; ADR formula correct |

### Storage tests (`internal/storage/storage_test.go`)

Tests use an in-memory SQLite database (`:memory:`). Each test opens a fresh database.

| Test | What it verifies |
|------|-----------------|
| `TestDemoInsertAndExists` | Insert then existence check; negative case |
| `TestListDemos` | Multiple demos ordered by date descending |
| `TestGetDemoByPrefix` | Prefix lookup; negative case returns nil, not error |
| `TestPlayerMatchStatsRoundTrip` | Full insert + query round-trip; field-level assertions |
| `TestInsertIdempotency` | Second `InsertDemo` with same hash does not error |

---

## Known Limitations and Future Work

- **Match date**: Stored as `time.Now()` at parse time. Should parse from demo filename or header.
- ~~**Demo file read**: Two sequential passes (hash, then parse). Could be made single-pass with `io.TeeReader`.~~ (still open — acceptable for current use)
- ~~**Flash tracking**: Only partially used.~~ Effective flashes (blinded enemy killed by team within 1.5 s) are now tracked. Average blind duration and per-enemy flash counts remain unimplemented.
- **No composite rating**: `PlayerMatchStats` has all the ingredients for a composite score but none is computed yet. The label should be "Composite Rating (beta)" when added, not "HLTV Rating", until validation against known matches is complete.
- ~~**Phase 2 metrics (crosshair placement)**~~: Crosshair placement (median angle, pitch/yaw split, pct under 5°) and pre-shot correction are now implemented.
- **Schema migrations**: The current schema is applied with `IF NOT EXISTS`, which is safe for initial creation but provides no migration path for adding columns. A versioned migration scheme (e.g. tracking schema version in a `meta` table) would be needed before the schema is considered stable. Currently, a DB rebuild (`rm metrics.db`) is required whenever the schema changes.
- **No index on FK columns**: `demo_hash` columns in child tables are not indexed. Fine for current query patterns (always full-scan of a single demo's rows) but will degrade as the database grows.
- **Distance bin for "unknown"**: Duels where the attacker had no weapon-fire event in the duel window (e.g., kill grenade, knife) or where the victim had no hit recorded are placed in the `"unknown"` distance bin. These are not surfaced as a quality warning in the current output.
- **FHHS for losing duels**: `PlayerDuelSegment` only accumulates data from duels the player *won* (had a sight of the victim before the kill). FHHS for duels the player lost is not yet computed.
- **Movement state segmentation** (standing/walking/running at first shot): Not implemented. Spec'd as a future extension in `docs/iteration-2.md`.
- **Lateral velocity tracking** (Module 3): Excluded from implementation — unreliable at GOTV 32 Hz demo rate.
- **Per-map segment queries**: No multi-demo aggregation view. Cross-match FHHS trends require manual SQL queries against the DB.
