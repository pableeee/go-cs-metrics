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
│   ├── parse.go                     # "parse <demo.dem|.csraw2.tar>" — full pipeline; accepts both formats
│   ├── convert.go                   # "convert --dir <dir> --tier <tier>" — .dem → .csraw2.tar (one demoinfocs pass via parserv2, no DB)
│   ├── replay.go                    # "replay --dir <dir>" — .csraw2.tar → DB (no demoinfocs, many workers)
│   ├── query.go                     # "query --dir <dir> <expr>" — find rounds matching a CEL expression from .csraw2.tar (no DB); --html for interactive 2D viewer
│   ├── query_html.go                # HTML viewer generation (gzip+base64 injection)
│   ├── query_template.html          # embedded 2D viewer template (compiled in via go:embed)
│   ├── info.go                      # "info <demo.dem|.csraw2.tar>" — file metadata + DB status
│   ├── list.go                      # "list" — tabulate stored demos
│   ├── show.go                      # "show <hash-prefix>" — replay stored match
│   ├── player.go                    # "player <steamid64>..." — cross-match aggregate
│   ├── player_roles.go              # "player --roles" helpers: HLTV-style 7-role decomposition
│   ├── rounds.go                    # "rounds <hash> <steamid>" — per-round drill-down
│   ├── trend.go                     # "trend <steamid64>" — chronological per-match trend
│   ├── deaths.go                    # "deaths <steamid64>" — death drill-down over player_death_events
│   ├── swing.go                     # "swing <steamid64>" — round swing + duel swing
│   ├── sql.go                       # "sql <query>" — ad-hoc SQL query
│   └── drop.go                      # "drop [--force]" — delete the metrics database
└── internal/
    ├── model/
    │   └── model.go                 # all shared types (RawMatch, PlayerMatchStats, …); no external deps
    ├── csraw2/                      # CSRaw v2 archive format
    │   ├── csraw2.go                # constants (version, stream names, enums)
    │   ├── types.go                 # header + event row types (parquet-tagged)
    │   ├── reader.go                # Read / ReadHeader from a .csraw2.tar
    │   └── writer.go                # Write a Match to .csraw2.tar
    ├── parserv2/                    # .dem → csraw2.Match (Source 2 demoinfocs walker)
    ├── csraw2bridge/                # csraw2.Match → model.RawMatch adapter for the aggregator
    ├── parser/parser.go             # legacy .dem → RawMatch direct walker (used by csraw2-compare validation only)
    ├── aggregator/
    │   ├── aggregator.go            # RawMatch → PlayerMatchStats + all segment types (19-pass pipeline)
    │   ├── aggregator_test.go       # unit tests for metric logic (incl. Pass 14 save/assist, Pass 15 HLTV flash assists, Pass 16 liveness)
    │   ├── aggregator_scan_test.go  # unit tests for Pass 17 scan volatility
    │   ├── aggregator_shots_test.go # unit tests for Pass 18 shot accounting
    │   ├── aggregator_context.go    # Pass 19: round context (resources, spacing, timing)
    │   └── aggregator_context_test.go
    ├── storage/
    │   ├── schema.sql               # embedded SQL (go:embed)
    │   ├── storage.go               # DB open / schema apply
    │   ├── queries.go               # insert / query helpers
    │   ├── export_queries.go        # export command queries (QualifyingDemos, MapWinOutcomes, RoundSideStats, RosterMatchTotals, PlayerDemoCounts)
    │   ├── role_queries.go          # `player --roles` queries (per-round, per-weapon, sniper/utility/flash event aggregates)
    │   ├── death_queries.go         # `deaths` command aggregations over player_death_events (by phase/weapon/distance/map)
    │   └── storage_test.go          # round-trip tests against :memory:
    ├── roundquery/                  # CEL-based round filter + 2D viewer record builder over csraw2.Match
    └── report/
        ├── report.go                # terminal table formatting
        └── deaths.go                # `deaths` command tables + phase/distance ordering
```

All business logic lives under `internal/`. The `cmd/` layer is thin: it only wires flags to the pipeline and handles top-level errors.

---

## Processing Pipeline

Two equivalent paths feed the same aggregator → storage → report chain:

```
.dem file                              .csraw2.tar file
    │                                       │
    ▼                                       ▼
[parserv2] ParseDemoV2 → *csraw2.Match  [csraw2] Read → *csraw2.Match
    │       • SHA-256 hash                  │  • parquet decode
    │       • streams events to slices      │  • RAM: a few MB
    │       • RAM: 4–29 GB peak             │  • workers: many
    │       • workers: 1 (forced)           │  • speed: ~150 ms/demo
    │       • speed: ~3 min/demo            │
    │                                       │
    └──────────────────┬────────────────────┘
                       │  *csraw2.Match
                       ▼
[csraw2bridge] ToRawMatch(m) → *model.RawMatch
                       │
                       ▼
[aggregator]   Aggregate(raw) → ([]PlayerMatchStats, []PlayerRoundStats,
    │                            []PlayerWeaponStats, []PlayerDuelSegment, error)
    │           • 19-pass algorithm over raw event slices
    │           • no I/O, no external dependencies
    │
    ▼
[storage]      InsertDemo / InsertPlayerMatchStats / InsertPlayerRoundStats
    │           / InsertPlayerWeaponStats / InsertPlayerDuelSegments
    │           • SQLite via modernc.org/sqlite (pure Go, no CGo)
    │           • INSERT OR REPLACE for full idempotency
    │
    ▼
[report]       PrintMatchSummary / PrintPlayerTable / PrintPlayerSideTable
               / PrintDuelTable / PrintAWPTable / PrintFHHSTable
               / PrintWeaponTable / PrintAimTimingTable → stdout
               PrintRoundDetailTable (rounds command — with POST_PLT/CLUTCH_1vN flags)
               PrintPlayerAggregateAimTable (player command)
               PrintTrendTable / PrintAimTrendTable (trend command)
```

The parser and aggregator are decoupled by `csraw2.Match` (intermediate, on-disk
schema) and the `csraw2bridge` adapter that produces `*model.RawMatch` for the
aggregator. This means:

- The aggregator can be unit-tested with hand-crafted fixtures (no demo file required).
- The parser can be swapped or extended without touching metric logic.
- `.csraw2.tar` files bypass demoinfocs entirely — the reader produces the same `*csraw2.Match`, which the bridge converts to `*model.RawMatch`.
- Future output targets (JSON, HTML, Postgres) only need to replace the storage/report stages.

---

## `.csraw2.tar` Intermediate Format

### Motivation

Parsing `.dem` files with demoinfocs-golang is memory-intensive (~10 GB of
cumulative allocation per demo) and constrained to a single worker to avoid
OOM. For large datasets (hundreds to thousands of demos), this becomes a
bottleneck:

| Scenario | `.dem` via `parse` | `.csraw2.tar` via `replay`/`parse` |
|---|---|---|
| Workers | 1 (forced — OOM with >1) | many (no demoinfocs pressure) |
| RAM peak | 4–29 GB per demo | a few MB (parquet decode only) |
| GOMEMLIMIT required | yes | no |
| Re-ingest after schema change | re-parse .dem (slow) | `replay` from .csraw2.tar (fast) |

### Format

`.csraw2.tar` is an uncompressed tar archive of:

- `header.json` — match metadata, sampling config, player roster, rounds, weapon table
- one zstd-compressed parquet file per event stream (kills, damages, weapon
  fires, flashes, first sights, grenade throws/detonations, bomb actions,
  item pickups/drops, equip changes, chat commands, player samples,
  projectile samples)

Forward-compatible: adding a new column is a minor bump and old readers keep working. The reader/writer are `archive/tar` + `parquet-go/parquet-go` — pure Go, no CGo.

### Accuracy

Validated on the 45-demo personal corpus: every aggregator output matches the
`.dem`-direct baseline byte-for-byte. Two duel-engine-sensitive carve-outs
keep `float32` precision (`weapon_fires.pos_*` + `damages.victim_pos_*` for
distance bins; `weapon_fires.{yaw,pitch}_deg` +
`first_sights.observer_{yaw,pitch}_deg` for the 2° pre-shot correction
boundary). Everything else is `int16`-quantised.

### Recommended workflow

```sh
# Step 1 — one-time convert (requires GOMEMLIMIT; --workers 1)
GOMEMLIMIT=4294967296 ./go-cs-metrics convert --dir ~/demos/pro/<event>/ --tier pro

# Step 2 — fast re-ingest (no GOMEMLIMIT; --workers 32)
./go-cs-metrics replay --dir ~/demos/pro/<event>/
```

See `docs/csraw-v2-spec.md` for the full format specification.

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

The `player` command adds a fifth derived type, `PlayerAggregate`, built in-memory from the above stored slices:
- Integer stats are summed directly across matches.
- Float medians (exposure, correction, hits-to-kill) are averaged across matches (approximate cross-demo signal).
- FHHS segments are merged by (weapon_bucket, distance_bin), summing raw counts for an accurate aggregate rate.

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

## Aggregator: Eighteen-Pass Algorithm

The aggregator makes eighteen sequential passes over the raw event data. (Passes 12–16 — death events, flash events, save/assist annotation, HLTV flash assists, and liveness — are summarized in CLAUDE.md; Passes 17 and 18 are documented below.)

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

**Buy type classification**: equipment value at freeze-end (`PlayerEquipValues[playerID]`, snapshotted by the parser in the `RoundFreezetimeEnd` handler) is thresholded: ≥$4500 = full, ≥$2000 = force, ≥$1000 = half, <$1000 = eco. Stored as `BuyType` on `PlayerRoundStats`.

**Post-plant flag**: `IsPostPlant = round.BombPlantTick > 0`. The parser captures the tick of the `BombPlanted` event in `RawRound.BombPlantTick`.

**Clutch detection** (`computeClutch`): called once per round before the per-player loop. All round participants start alive; kills are processed in tick order, marking victims dead after each. After each death the alive counts per team are checked — if `myTeamAlive == 1 && enemyAlive >= 1` for a player, that player is in a clutch. `ClutchEnemyCount` records the maximum enemy-alive count seen during their clutch.

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

### Pass 9 — Role Classification

Assigns a heuristic role label to each player based on their match statistics:
- **AWPer**: AWP kills > 30% of total kills
- **Entry**: opening kills > 12% of rounds played
- **Support**: flash assists > 8% of rounds, or utility damage > 15/round
- **Rifler**: default (none of the above thresholds met)

### Pass 10 — TTK, TTD, and one-tap kills

For each kill, uses the weapon-fire index (`wfIdx`) to find the **first shot fired** by the killer within a 3-second rolling window before the kill tick (not the first damage tick — missed shots are included, matching external tools like Refrag).

- If `firstFiredTick == killTick`: one-tap. Counted in `OneTapKills`, excluded from TTK/TTD samples.
- Otherwise: `ms = (killTick − firstFiredTick) / tps * 1000`
  - `MedianTTKMs` (attacker): median ms from first shot to kill across all multi-hit kills.
  - `MedianTTDMs` (victim): median ms from enemy's first shot to victim's death.

### Pass 11 — Counter-strafe %

Scans `raw.WeaponFires` per player. Each shot where `HorizontalSpeed ≤ 34.0` u/s (captured at fire tick via `e.Shooter.Velocity()`) is counted as counter-strafed. `CounterStrafePercent = strafed / total * 100`. Utility/knife fires are excluded by the parser.

### Pass 6 — Duel engine and the round dimension

Duel segments are keyed by `(player, round, weapon_bucket, distance_bin)`. The round is what lets FHHS be sliced by side, buy type, or round phase — join `player_round_stats` on `(demo_hash, steam_id, round_number)`. Without it the FHHS table could only ever be read whole, which is the wrong granularity for questions like "is my T-side entry aim worse than my CT-side hold aim".

Two consequences worth knowing:

- **Storage granularity is near per-duel.** Measured on the corpus, segments average ~1.37 duels per row even before the round split, so `median_corr_deg` and friends were already close to single values rather than true medians. Adding the round makes that explicit rather than changing it materially.
- **Aggregation must be count-weighted.** `mergeSegments` combines the per-segment medians weighted by `DuelCount`. An unweighted mean would let a one-duel round pull as hard as a five-duel round, making the merged value depend on how rows happen to be partitioned — which now varies with the round split. It is still a mean-of-medians, but a partition-invariant one.

The round is part of the `UNIQUE` key, so the migration rebuilds the table rather than adding a column; pre-existing rows carry `round_number = -1`, meaning "aggregated before the round dimension existed". `InsertPlayerDuelSegments` clears a demo's rows before inserting, so a re-aggregation can never leave rows written under an older key shape behind to double-count.

**Weapon bucketing.** `weaponBucket` maps weapon names to the buckets FHHS reports on. It matches on the exact strings the csraw2 weapon table emits — a mismatch is silent, since unmatched names fall through to `"Other"`. That bug shipped once: the table names the silenced M4 `"M4A1"` while the bucket matched only `"M4A1-S"`, so ~44k duels with the most-used CT rifle sat in `"Other"` and never appeared as an `M4` row. `TestWeaponBucket_CoversPrimaryWeapons` pins every observed name against its bucket to keep that from recurring.

**Win-side only.** Segments are accumulated on the killer's side of a duel. A lost duel contributes only to the match-level `median_exposure_loss_ms`; it produces no segment, no distance bin, and no correction sample. Every FHHS / `MED_CORR` / `MED_SIGHT` / `SHOT_DLY` figure therefore describes duels the player **won**. Reading them as "how I aim" overstates their scope — they are "how I aim in the duels I win".

**`median_shot_delay_ms`** records the gap from the enemy becoming visible to the player's *first shot* in that duel (not to the kill — `median_expo_win_ms` already covers that). It exists to disambiguate a large `median_corr_deg`: a big correction with a short delay is a reaction to something unexpected, while a big correction with a long delay means the time was there and the crosshair still travelled too far. Only the first weapon fire inside `[sightTick, killTick]` is used.

### Pass 17 — Scan volatility ("panic swiping")

Measures out-of-combat crosshair discipline from `raw.ViewSamples` (16 Hz view-state rows the bridge reduces from csraw2 `player_samples`; empty for data paths without samples, in which case all scan metrics stay 0).

**Qualifying time**: consecutive same-round samples where the player is alive, past `FreezeEndTick`, gap ≤ 0.3 s, no enemy in the player's spotted mask, and neither endpoint within ±2 s of the player's own kills, deaths, damage dealt/taken, weapon fires, or grenade throws (utility lineups are deliberate aim, not scanning). Yaw is unwrapped across the ±180° seam; per-step deltas > 160° (a flick or teleport artifact at 16 Hz) are skipped entirely.

Per qualifying step, three accumulators:
- **Dwell** — yaw speed < 25 °/s counts as settled time. `ScanDwellPct = dwell / total * 100`; low dwell = the crosshair never settles.
- **Reversals** — a yaw-direction flip where both legs are ≥ 60 °/s. Two consecutive settled steps break the chain, so a pause followed by an opposite-direction scan is a new scan, not a reversal. `ScanReversalsPerMin` at match level, raw `ScanReversals` per round.
- **Travel** — summed |Δyaw| → `ScanAvgYawDegPerSec`.

Rationale: raw angular speed punishes fast angle-clearing (snap → dwell → snap). What distinguishes panic is the *pattern* — continuous back-and-forth swiping with no dwell — hence reversal rate + dwell fraction rather than mean speed alone. Rounds with < 5 s of qualifying time render as `—` in the `rounds` table (values are still stored; filter on `scan_ooc_seconds` in SQL). Match-level values are time-weighted across rounds; the `player` cross-match aggregate is time-weighted by `scan_ooc_seconds` per match.

---

### Pass 18 — Shot accounting (accuracy + aimed/blind split)

Counts, per `(player, weapon)`: every weapon fire (`ShotsFired`), every head-hitbox hit (`HeadHits`, lethal or not — unlike `HeadshotKills`), and the subset of both taken with an enemy visible (`ShotsVisible`, `HitsVisible`, `HeadHitsVisible`).

**Why the split exists.** Raw accuracy (hits ÷ shots) is not comparable across skill tiers, because tiers differ enormously in how many shots are deliberately aimed at nobody: smoke spam, prefire, wallbangs, suppressing fire. Measured on this corpus, ~79% of pro rifle shots are fired with no enemy visible against ~59% for a mid-tier player — enough to *invert* the raw-accuracy ranking between them. `AccuracyVisible()` is the comparable number; `Accuracy()` is kept because it is what in-game stats report.

**Visibility source.** Each fire and each damage row is matched to the player's nearest `RawViewSample` (the same 16 Hz stream Pass 17 uses) and tagged with its `EnemiesVisible` flag — the engine's spotted mask. The tolerance is `ceil(tickrate/16) + 2` ticks: weapon fires are events, so csraw2's 64 Hz event-window burst normally puts a sample 0–1 ticks away, and the slack covers the baseline-sampling case. A shot with no sample in range counts toward `ShotsFired` but not toward `ShotsVisible`, so "not measured" never masquerades as "aimed".

Damage rows use the same filters as the weapon-hit accumulator earlier in `Aggregate` (attacker known, team damage excluded), which guarantees `HitsVisible ≤ Hits`.

**The visible bucket is phase-biased — important when reading head-hit rates.** The spotted mask is set *after* an enemy is acquired, so the opening shot of an engagement usually lands in the blind bucket while the follow-up spray lands in the visible one. Head-hit rate is therefore systematically *higher* blind than visible, for everyone: measured on this corpus, AK-47 head hits per hit run 26.7% blind vs 13.8% visible for pros, and 22.0% vs 6.4% for a mid-tier player — same direction in every weapon and every tier. Read `HeadHitPctVisible()` as "head rate once the duel is running" (spray discipline), not as first-shot precision. The bias applies equally to both sides of a cross-player comparison, so relative results still hold; it is the absolute interpretation that changes. `AccuracyVisible()` is not affected the same way — excluding fire aimed at nobody is the point there.

**Limitations.** The spotted mask has latency and is FOV-gated, so the blind bucket is somewhat over-inclusive — it is the same measure on both sides of any comparison, so relative results hold. "Blind" cannot be decomposed into smoke / prefire / wallbang: `csraw2`'s in-smoke player flag exists in the schema but the converter never populates it, and re-converting is not possible for most of the pro corpus (the source `.dem` files are gone).

Weapons fired but never landed still produce a row, so the worst-accuracy weapons do not silently vanish.

## Swing metrics (`swing` command)

Two win-probability-added metrics, both from **empirical tables counted over the corpus** rather than a fitted model. `internal/swing` builds the tables; `internal/storage/swing_queries.go` loads their inputs from `player_death_events` and `player_round_stats`; `cmd/swing.go` drives it.

**Round swing** — walk each round's kills in tick order, tracking alive counts and plant state. At every kill, look up `P(CT wins | aliveCT, aliveT, planted)` before and after; the delta is credited to the killer and debited from the victim.

**Duel swing** — every kill is one duel with a winner and a loser. Look up `P(win | first-sight advantage, distance bin, weapon class)`; the winner scores `1 - P`, the loser `-(1 - P)`. Positive means beating expectation.

### Why counted, not fitted

With ~42k rounds the cells are dense enough that a frequency beats a regression on both accuracy and auditability — `--show-tables` prints them so a cell can be checked against intuition ("3v2 unplanted should be around 0.7"; it is 0.780). Sparse cells back off to a coarser key rather than returning a wild frequency from `n = 1`; `MinCellSamples` is the floor.

### The mirroring step

`BuildDuelTable` enters every kill **twice**: once as observed and once mirrored from the loser's perspective with the advantage negated. Entering only the observed direction would make every cell 1.0 by construction, since the killer always won. This is the single easiest way to get this metric silently wrong, so it has its own test.

A consequence worth knowing: the symmetric buckets (`even`, `neither`) are forced to exactly 0.500 by the mirroring, and they hold the large majority of duels. The model only truly discriminates through `unseen` / `blind` and the asymmetric advantage buckets.

### Zero-sum validation

Both metrics are zero-sum across all players by construction. `Validate` sums every player's totals and refuses to print if either is non-zero beyond floating-point slack — a leak means attribution is broken and the numbers are not trustworthy. The command runs it before any output. It also checks that each player's `CT` and `T` slices reconstruct their totals, that the per-weapon slices reconstruct them too, and that each weapon class is itself zero-sum across players — both parties of a duel share the killer's weapon bucket, so every class is a closed slice.

### The side split (`--by-side`)

`PlayerSwing.CT` / `.T` carry both metrics restricted to the side the player was on at the time; the duel's victim is on the opposite side by construction, so every attribution knows its side without extra lookups. Sides swap at halftime, so almost every player has both populated.

The split is the cut that matters for role analysis: holding a site above expectation and taking space below it average to nothing in the combined number. `--by-side` prints two rows per player and adds `CT_DUEL/DUEL` / `T_DUEL/DUEL` columns to the `--top` leaderboard, which is the axis pair to plot as a scatter — one point per player, corners as role archetypes.

**A side is not zero-sum on its own.** Only the two together are. Summed over every player, CT duel swing is the whole CT side's net over expectation, and that is zero only if the probability tables happen to be side-neutral. **They are not**: over the pro corpus every one of the top 15 players by round swing has a higher CT duel swing than T, typically by 0.04–0.11. The duel table keys on first-sight advantage, range and weapon class, none of which capture that the CT side is usually the one holding an angle. So a raw CT number and a raw T number are on different scales — compare a CT value against other players' CT values, and centre each axis on its own side's population mean before plotting the two together.

### The weapon split (`--by-weapon`)

`PlayerSwing.Weapons` slices both metrics by the weapon class that **resolved** each duel — the killer's weapon, since the victim's own is not stored. The printed legend carries the caveat because it is the most likely misreading: a player's "sniper" slice is duels *decided by* a sniper, which on their losses means the opponent's AWP. Rounds stay 0 within a slice (a round is not played "with a weapon class"), so only per-duel rates and totals are reported. Classes come from `swing.WeaponClass`, the same taxonomy the duel table keys on.

### Floor / ceiling (`--floor-ceiling`)

`ComputeByDemo` (internal/swing/distribution.go) partitions rounds and kills by demo hash in one pass and runs `Compute` per demo with the corpus-wide tables — same principle as `--by-map`: tables global, attribution per subset. Summed over demos, the per-player totals reconstruct the full-corpus run exactly (tested). `FloorCeilings` then takes p25/p50/p75 of the per-demo rates per player, skipping demos below 10 rounds (fragment appearances produce garbage rates) and players below `--min-demos` (default 8). With `--top N` it prints a population leaderboard ranked by round-swing floor: the players who beat expectation even in their bad games. A per-demo `DuelSwingSE` is huge (~20–40 duels), which is exactly why this reports quantiles of the distribution rather than any single demo's number.

### Team shape (`--roster` / ≥3 ids)

Median vs best `DuelSwingPerDuel` across the queried set, and the gap — the "hard carried" measure. It is only meaningful when the queried ids are an actual roster; there is no population version because the DB stores no team identity (sides only), so team membership must come from the caller.

### Round context (`--context`)

Prints the Pass 19 aggregates per player per side against the population per-side mean: good-gun % (rifle+sniper share of alive in-round samples), pack distance, first-contact and death timing. This is the denominator axis the swing numbers should be read against — a star's swing is bought with resources and freedom a support never gets. Loaders live in `internal/storage/context_queries.go`; they count only measured rounds (`gun_samples > 0`) and exclude `-1` sentinels from every mean, so demos aggregated before the Pass 19 backfill never dilute the numbers.

### Reading the numbers

Round swing per round is a fraction of a probability point, which means nothing without the spread. Measured over 271 players with at least 300 rounds: max +0.053, p95 +0.025, median -0.002, p05 -0.026, min -0.040. Multiply by ~24 to read it as rounds per map.

`--top N` prints that distribution plus the queried player's rank. **The rank is not a cross-tier skill ranking** — a personal-tier player earns their swing against personal-tier opposition, so it conveys scale, not equivalence with a pro's.

### Known limitations

- The tables condition on states *reached*, which is not a random sample: teams that arrive at 3v2 differ systematically from those that do not. HLTV's round swing shares this property. Read a cell as "how this state resolves in practice", not as a causal claim.
- This is **not** HLTV's published round swing. That one also credits damage, flash assists, trading and economy, so the scales are not comparable and an outside number must not be used as a reference for this one.
- Round swing is attributed at kills only. Plants, defuses and utility damage move win probability too and are invisible to it.
- The first-sight advantage is **not monotonic on its own**: `+1000ms` wins only 51.8% against `+400ms`'s 61.7%. The duel table therefore keys on a second axis, **freshness** — how long both players had been mutually aware when the duel resolved. It carries most of the missing signal: a `+400ms` lead is worth 0.680 when the duel resolves within 300ms of mutual awareness and only 0.503 once both sides have known for over three seconds. A stale lead is not a lead; the player saw and did not act, and the opponent picked the moment. Even at matched freshness `+1000ms` still trails `+400ms` (0.548 vs 0.680), so some of the effect remains unexplained — most likely a very long lead is a peripheral or distant spot rather than real awareness.
- Duel swing conditions on first-sight advantage, range and weapon class. HP, armour, flashed state and movement are all in the corpus and would refine it.
- **Duel swing per duel is printed with its standard error** (`DUEL/DUEL` reads `+0.0624 ±0.0187`). Each duel is one Bernoulli draw whose variance the table already supplies, so `Compute` accumulates `Σ p(1−p)` alongside the swing itself and `DuelSwingSE = √DuelVarSum / Duels`. Most duels sit in a bucket mirroring pins at exactly 0.500, so the result tracks `0.5/√duels` closely and runs a little tighter where the sighting was asymmetric. A player with ~700 T-side duels — a full season on one side — lands near ±0.018, so a 95% interval is ~0.07 wide, the same order as the entire spread between five teammates. **Ranking players inside one team is therefore usually not resolvable**; separating the top pair from the bottom pair often is. Two caveats the number does not cover: duels cluster within rounds and matches instead of being independent draws, so it is a floor; and it is sampling error only, silent about the features the table omits and the systematic CT/T offset those omissions produce.
- `--by-map` recomputes attribution per map but keeps the tables corpus-wide, on purpose: splitting the tables per map would shrink every cell far more than map identity changes what a 3v2 is worth.
- Symmetric advantage buckets (`even`, `neither`) sit at exactly 0.500 no matter what else is in the key: mirroring forces it. They hold the large majority of duels, so the model only truly discriminates where the sighting was asymmetric. Making the common case informative needs an asymmetric feature that is not the sighting — HP, armour, or who was holding versus moving.
- `WeaponBucket` is the weapon the *kill* was made with, and the mirrored entry reuses it for the loser. The victim's own weapon is not stored, so a rifle-versus-pistol duel is keyed as if both sides held a rifle.

## Parser: Event Handling Notes

The parser registers handlers for eight event types from `demoinfocs-golang`:

| Event | Action |
|-------|--------|
| `RoundStart` | Increment round counter (skipped during warmup); record start tick; reset `currentEquipVals` and `currentBombPlantTick` |
| `RoundFreezetimeEnd` | Update freeze-end tick; snapshot equipment values (`EquipmentValueFreezeTimeEnd()`) per player into `currentEquipVals` |
| `RoundEnd` | Snapshot all active players' end-states; attach `currentEquipVals` and `currentBombPlantTick` to `RawRound`; record round metadata |
| `BombPlanted` | Record `p.CurrentFrame()` into `currentBombPlantTick`; used by Pass 3 to set `IsPostPlant` |
| `Kill` | Append to kills slice; count nearby alive teammates for AWP kills (512-unit radius) |
| `PlayerHurt` | Append to damages slice with hitgroup and victim position; skip self-damage |
| `PlayerFlashed` | Append to flashes slice; skip zero-duration events |
| `WeaponFire` | Append to weapon-fires slice with shooter position; skip utility/knife/warmup |

**Parser captures:**
- **Equipment value**: `pl.EquipmentValueFreezeTimeEnd()` — post-buy equipment value per player, snapshotted in the `RoundFreezetimeEnd` handler and stored in `RawRound.PlayerEquipValues`. Used by Pass 3 to classify buy type.
- **Bomb plant tick**: `p.CurrentFrame()` in the `BombPlanted` handler — stored in `RawRound.BombPlantTick`. Used by Pass 3 to set `IsPostPlant`.

Additionally, the **frame-walk loop** inspects `m_bSpottedByMask` transitions every tick to emit `RawFirstSight` events — one per (observer, enemy, round) pair, recording crosshair deviation angles and absolute view angles.

**Absolute vs deviation angles in `RawFirstSight`**:
- `AngleDeg`, `PitchDeg`, `YawDeg` — deviation magnitudes (used for crosshair placement metrics in Pass 5)
- `ObserverPitchDeg`, `ObserverYawDeg` — absolute view angles at first-sight tick (used for pre-shot correction in Pass 6; combining deviation fields with weapon-fire angles would produce nonsensical deltas)

---

## Storage Schema

Seven tables:

```
demos                         (hash PK, map_name, date, type, tickrate, ct_score, t_score, tier, is_baseline, event_id)
  │
  ├── player_match_stats       (demo_hash FK, steam_id, ~35 aggregated metric columns)
  │                            UNIQUE(demo_hash, steam_id)
  │
  ├── player_round_stats       (demo_hash FK, steam_id, round_number, per-round flags,
  │                             is_post_plant, is_in_clutch, clutch_enemy_count)
  │                            UNIQUE(demo_hash, steam_id, round_number)
  │
  ├── player_weapon_stats      (demo_hash FK, steam_id, weapon, kills, hs_kills, damage, hits,
  │                             shots_fired, shots_visible, hits_visible, head_hits, head_hits_visible)
  │                            UNIQUE(demo_hash, steam_id, weapon)
  │
  ├── player_duel_segments     (demo_hash FK, steam_id, round_number, weapon_bucket, distance_bin,
  │                             duel_count, first_hit_count, first_hit_hs_count, median_shot_delay_ms,
  │                             median_corr_deg, median_sight_deg, median_expo_win_ms)
  │                            UNIQUE(demo_hash, steam_id, weapon_bucket, distance_bin)
  │
  ├── grenade_events           (demo_hash FK, match_date, map_name, round_number,
  │                             throw_tick, end_tick, thrower_id, thrower_team,
  │                             grenade_type, throw_x/y/z, land_x/y/z)
  │                            raw event log (no UNIQUE) — replaced per demo on re-parse
  │
  └── player_death_events      (demo_hash FK, match_date, map_name, round_number, tick,
                                victim_id/team, killer_id/team, weapon, is_headshot,
                                victim_x/y/z, killer_x/y/z, victim_yaw, distance_m,
                                was_flashed, was_traded, is_opening_death, round_phase)
                               raw event log (no UNIQUE) — replaced per demo on re-parse
```

**`demos` column notes:**
- `map_name` is normalized to title-case at storage time — the `de_` prefix is stripped and the first letter is uppercased (e.g. raw `de_mirage` → stored as `Mirage`). All query commands show normalized names.
- `tier` (e.g. `"faceit-5"`) is auto-populated from an `event.json` sidecar written by `cs-demo-downloader` if present in the demo directory; the `--tier` flag overrides it.
- `event_id` is populated from the same sidecar (e.g. `"iem_cologne_2025"`); empty string if unknown.
- `is_baseline INTEGER` — 1 for reference corpus demos, 0 for personal matches.

All tables use `CREATE TABLE IF NOT EXISTS`; new columns are added at startup via `ALTER TABLE ... ADD COLUMN ... DEFAULT` migrations (duplicate-column errors silently ignored). Indexes on frequently queried columns (`demos.match_date`; `steam_id` and `demo_hash` on all three child stats tables) are declared with `CREATE INDEX IF NOT EXISTS` in schema.sql — safe for both fresh and existing databases.

---

## CLI Design

Subcommands, all accessed via a persistent `--db` flag on the root command:

```
csmetrics parse [<demo.dem|.csraw2.tar>...] [--dir <dir>] [--player <steamid64>] [--type Label] [--tier Label] [--baseline] [--workers N]
csmetrics convert --dir <event_dir> --tier <tier> [--out-dir <dir>] [--workers N] [--force]
csmetrics replay --dir <event_dir> [--workers N] [--force]
csmetrics info <demo.dem|.csraw2.tar> [...]
csmetrics list
csmetrics show <hash-prefix> [--player <steamid64>]
csmetrics player <steamid64> [<steamid64>...] [--map <name>] [--since <date>] [--last <N>] [--top <N>] [--top-min <N>]
csmetrics rounds <hash-prefix> <steamid64>
csmetrics trend <steamid64>
csmetrics sql "<query>"
csmetrics drop [--force]
csmetrics summary
csmetrics query --dir <dir> '<cel-expression>' [--csv]
```

See **[docs/query-command.md](query-command.md)** for the full variable reference, CEL syntax guide, and worked examples for all major use cases.

All commands also accept `--silent` / `-s` (persistent flag on root). When set, the one-line column legend printed before each table is suppressed. Verbose output (legends) is shown by default; section titles (`--- Name ---`) are always printed regardless of `--silent`.

**Output order** for `parse` (single file):
0. Timing line — `  parse: Xs  aggregate: Xs  total: Xs` printed immediately after processing, before the tables
1. Match summary (map, date, score, hash)
2. Player roster — compact name → SteamID64 listing
3. Player table — K/A/D, ADR, KAST%, role, entries, trades, flash assists, effective flashes, xhair median
4. Duel table — W/L counts, median exposure win/loss ms, hits/kill, first-hit HS%, pre-shot correction
5. AWP table — AWP deaths with dry%/repeek%/isolated%
6. Weapon table — per-weapon kills, HS%, damage, hits
7. Aim timing — median TTK, median TTD, one-tap%
8. Clutch table — 1v1–1v5 attempt/win counts per player

**Bulk mode** (`parse` with multiple files or `--dir`): full tables are suppressed. Demos are parsed and aggregated in parallel across `--workers` goroutines (default: `runtime.NumCPU()`). Database writes are always serialised on the main goroutine — no SQLite contention regardless of worker count. Results arrive out of input order (each line carries a `[i/n] filename` tag). Each status line includes map, date, score, player count, round count, and `(parse Xs  agg Xs  total Xs)` timing.

**Output order** for `show`:
1. Match summary (map, date, score, hash)
2. Player roster — compact name → SteamID64 listing
3. Player table — K/A/D, ADR, KAST%, role, entries, trades, flash assists, effective flashes, xhair median
4. Per-side breakdown — K/A/D, ADR, KAST%, entry/trade counts split by CT and T halves
5. Duel table — W/L counts, median exposure win/loss ms, hits/kill, first-hit HS%, pre-shot correction
6. AWP table — AWP deaths with dry%/repeek%/isolated%
7. Weapon table — per-weapon kills, HS%, damage, hits
8. Aim timing — median TTK, median TTD, one-tap%
9. Clutch table — 1v1–1v5 attempt/win counts per player

**`--top N` ranking**: `GetTopPlayersByRating` aggregates raw integer stats per player via a single `GROUP BY steam_id` query (with optional `--map`/`--since` filters applied in SQL), then computes the Rating 2.0 proxy in Go, sorts descending, and returns the top N. Players already in the explicit arg list are skipped. `--last` is not applied to ranking (per-player recency windowing is too expensive for a bulk ranking query). The rating formula is the same as the `export` command.

**Output order** for `player <steamid64>...` (all players as rows in combined tables):
1. Overview table — K/A/D, K/D, HS%, ADR, KAST%, entry kills/deaths, trade kills/deaths, flash assists, effective flashes
2. Duel profile — wins/losses, avg exposure win/loss ms, avg hits-to-kill, avg pre-shot correction
3. AWP breakdown — total AWP deaths, dry%/repeek%/isolated%
4. Map & side split — K/D, HS%, ADR, KAST%, entry/trade counts broken down by map and CT/T side
5. Aim timing aggregate — role, avg TTK, avg TTD, one-tap%
6. Clutch aggregate — 1v1–1v5 attempt/win counts per player
7. FHHS table — per-player; built from merged cross-demo segment counts (not printed by parse/show)

**Output for `rounds <hash-prefix> <steamid64>`**:
Per-round table: round number, side, buy type, K/A/damage, KAST ✓/blank, tactical flags (OPEN_K/D, TRADE_K/D, POST_PLT, CLUTCH_1vN). Footer: buy profile summary (full/force/half/eco counts and percentages).

**Output for `trend <steamid64>`**:
1. Performance Trend — one row per match in ascending date order: DATE, MAP, RD, K, A, D, K/D, KPR, ADR, KAST%
2. Aim Timing Trend — DATE, MAP, RD, MEDIAN_TTK, MEDIAN_TTD, ONE_TAP% (only rendered if any match has TTK/TTD/one-tap data)

**Output for `summary`**:
1. Overview block — demos stored, date range, unique maps, unique players, total rounds
2. Maps table — MAP, MATCHES, CT WINS, T WINS, CT WIN% (ordered by match count desc)
3. Most Active Players table — NAME, STEAM ID, MATCHES, AVG K/D, AVG ADR, AVG KAST% (top 10 by match count)
4. Match Types table — TYPE, MATCHES (only rendered when more than one match type is present)

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
| `TestMapNameNormalization` | `de_`-prefixed raw names are stored and read back as normalized title-case; idempotent (already-normalized names unchanged) |
| `TestNormalizeMapName` | Unit-tests `normalizeMapName()` directly, including the edge case where stripping `de_` leaves an empty string (original name is preserved) |

---

## Known Limitations and Future Work

- ~~**Match date**: Stored as `time.Now()` at parse time.~~ Now uses `os.Stat(path).ModTime()` — CS2 writes the demo file when the match ends, so mtime is a reliable proxy. Falls back to today if stat fails.
- ~~**Demo file read**: Two sequential passes (hash, then parse). Could be made single-pass with `io.TeeReader`.~~ (still open — acceptable for current use)
- ~~**Flash tracking**: Only partially used.~~ Effective flashes (blinded enemy killed by team within 1.5 s) are now tracked. Average blind duration and per-enemy flash counts remain unimplemented.
- **No composite rating**: `PlayerMatchStats` has all the ingredients for a composite score but none is computed yet. The label should be "Composite Rating (beta)" when added, not "HLTV Rating", until validation against known matches is complete.
- ~~**Phase 2 metrics (crosshair placement)**~~: Crosshair placement (median angle, pitch/yaw split, pct under 5°) and pre-shot correction are now implemented.
- ~~**Round context**~~: Post-plant (`IsPostPlant`) and clutch detection (`IsInClutch`, `ClutchEnemyCount`) are now implemented and shown as `POST_PLT`/`CLUTCH_1vN` flags in the `rounds` command.
- ~~**Trend view**~~: The `trend` command shows chronological per-match KPR/ADR/KAST% and TTK/TTD/one-tap% tables.
- ~~**Counter-strafe %**~~: Implemented. Player horizontal speed is captured at each `WeaponFire` event via `e.Shooter.Velocity()`; shots at ≤ 34 u/s (counter-strafed) are counted vs total shots per player to produce `CS%`. Shown in aim timing tables and `AVG_CS%` in the `player` command.
- **Schema migrations**: The current schema is applied with `IF NOT EXISTS`, which is safe for initial creation but provides no migration path for adding columns. A versioned migration scheme (e.g. tracking schema version in a `meta` table) would be needed before the schema is considered stable. Currently, a DB rebuild (`rm metrics.db`) is required whenever the schema changes.
- ~~**No index on FK columns**: `demo_hash` columns in child tables are not indexed. Fine for current query patterns (always full-scan of a single demo's rows) but will degrade as the database grows.~~ Fixed: indexes on `demo_hash` and `steam_id` in all child tables (plus `match_date` on `demos`) are now declared in schema.sql with `CREATE INDEX IF NOT EXISTS`.
- **Distance bin for "unknown"**: Duels where the attacker had no weapon-fire event in the duel window (e.g., kill grenade, knife) or where the victim had no hit recorded are placed in the `"unknown"` distance bin. These are not surfaced as a quality warning in the current output.
- **FHHS for losing duels**: `PlayerDuelSegment` only accumulates data from duels the player *won* (had a sight of the victim before the kill). FHHS for duels the player lost is not yet computed.
- **Movement state segmentation** (standing/walking/running at first shot): Not implemented. Spec'd as a future extension in `docs/iteration-2.md`.
- **Lateral velocity tracking** (Module 3): Excluded from implementation — unreliable at GOTV 32 Hz demo rate.
- ~~**Per-map segment queries**: No multi-demo aggregation view.~~ The `player` command now aggregates stats and FHHS segments across all stored demos for a given SteamID64. Per-map filtering within that aggregate is not yet implemented.
