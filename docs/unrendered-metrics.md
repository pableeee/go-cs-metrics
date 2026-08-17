# Unrendered Metrics — Audit of Stored-but-Unsurfaced Data

Audited 2026-08-03 against the full schema (`internal/storage/schema.sql` +
the `ALTER TABLE` migrations in `internal/storage/storage.go`), the model
(`internal/model/model.go`), the aggregator, every `Print*` function in
`internal/report/report.go`, and every `cmd/` renderer. Each claim cites the
code that verifies it.

Categories:
- **(c) stored but never surfaced** — in the DB, no command shows it.
- **(b) analyze-only** — reaches the `analyze` AI context but no table.
- **(d) dropped before storage** — computed by parser/aggregator, discarded.

Related: [hltv-metrics-roadmap.md](hltv-metrics-roadmap.md) tracks the
HLTV-profile gap specifically; this file is the whole-DB inventory.

> **Status update 2026-08-03 — most of this audit has been actioned.** Items
> 1–6 of the original surfacing plan shipped in one pass: trade timing, scan
> yaw speed, per-round unused utility, the Crosshair Placement tables, the
> per-bin FHHS exposure/sight columns, the team-flash column, and the new
> `deaths` command (which lights up `player_death_events`, previously the
> largest write-only table). Each section below is annotated with what
> remains. The genuinely open work is now: `flash_events` and
> `grenade_events` (still write-only), and everything under category (d),
> which needs schema changes.

---

## Preliminary: `schema.sql` is not the full column list

Four columns exist only as migrations in `storage.go` (lines ~56–59) and are
absent from `schema.sql`:

- `player_round_stats.won_round`
- `player_match_stats.rounds_won`
- `player_match_stats.median_trade_kill_delay_ms`
- `player_match_stats.median_trade_death_delay_ms`

They are written by the inserts in `internal/storage/queries.go`. Any audit
(or tooling) driven purely from `schema.sql` misses them.

Also: `sql`'s built-in schema help text (`cmd/sql.go`) omits every event
table (`player_death_events`, `flash_events`, `grenade_events`) and every
Pass 14–17 column — the richest unrendered data is also the least
discoverable. `cmd/query.go` reads `.csraw2.tar` archives, not the DB, so it
surfaces none of this either.

---

## (c) Stored but never surfaced by any command

### `player_match_stats` — **all resolved 2026-08-03**

| Column | Meaning | Now rendered as |
|---|---|---|
| `crosshair_pct_under5` | % of first-sight encounters with deviation < 5° | `<5°%` in Crosshair Placement |
| `crosshair_median_pitch_deg` / `crosshair_median_yaw_deg` | Pitch/yaw split of first-sight deviation | `PITCH` / `YAW` in Crosshair Placement |
| `crosshair_encounters` | Sample count behind the crosshair medians | `N` in Crosshair Placement |
| `scan_avg_yaw_deg_per_sec` | Pass 17 mean out-of-combat yaw speed | `YAW°/S` in the Aim Timing tables |
| `median_trade_kill_delay_ms` / `median_trade_death_delay_ms` | Trade reaction timing (was category (b)) | `TRADE_K_MS` / `TRADE_D_MS` in the Aim Timing tables |
| `scan_ooc_seconds` | Pass 17 qualifying out-of-combat seconds | **Still gate-only** (`>= 5`). The Pass 17 columns render `—` below the threshold, which conveys the same thing implicitly. |

### `player_round_stats` — partially resolved 2026-08-03

| Column | Meaning | Status |
|---|---|---|
| `scan_avg_yaw_deg_per_sec` | Per-round mean yaw speed | **Resolved** — `YAW°/S` in the `rounds` drill-down |
| `unused_utility` | Grenades still held at round end | **Not surfaced — the column is dead. See below.** |

#### `unused_utility` is broken, not merely unrendered (found 2026-08-03)

A `UNUSED` column was added to the `rounds` table and then **removed**, because
it renders a permanent zero. The aggregator reads
`RawRound.PlayerEndState.GrenadeCount` (`aggregator.go:717`), but
`internal/csraw2bridge/bridge.go:135` never populates it — the comment there
reads *"GrenadeCount left at 0 — not tracked by csraw2 yet."*

Because every demo now reaches the aggregator through `convert` → `replay`
(the csraw2 path), the field is zero corpus-wide:

| tier | round rows | rows with `unused_utility > 0` |
|---|---|---|
| personal | 10,065 | **0** |
| pro | 414,237 | **147** (0.035%) |

The 147 survivors are demos ingested through the legacy direct-`.dem` walker
before the csraw2 cutover. The same applies to the match-level
`player_match_stats.unused_utility` (48 nonzero rows of 20,429), which is
still fed to the `analyze` AI context — where it silently reports "0 unused
utility" as if that were a measurement.

**To fix properly:** add a grenade-count-at-round-end field to the csraw2
round stream (a schema bump, so it needs a re-convert from `.dem`, not just a
replay), populate it in the bridge, then surface the column. Until then the
metric should be treated as absent rather than as "this player never wastes
utility."

### `player_duel_segments` — **resolved 2026-08-03**

| Column | Meaning | Now rendered as |
|---|---|---|
| `median_expo_win_ms` | Median first-sight→kill exposure per weapon×distance bin | `EXPO_WIN` in the FHHS table — shows *which* bin you're slow in |
| `median_sight_deg` | Median first-sight deviation per bin (was category (b)) | `MED_SIGHT` in the FHHS table |

### `player_death_events` — **resolved 2026-08-03 by the `deaths` command**

Was write-only except `demo_hash`, `round_number`, `killer_id`, `weapon`.
The `deaths` command now aggregates `victim_id`, `match_date`, `map_name`,
`weapon`, `is_headshot`, `distance_m`, `was_flashed`, `was_traded`,
`is_opening_death`, and `round_phase` into four breakdowns (phase, distance,
weapon, map) plus a totals row. Cross-checked against `player_match_stats`:
death counts and DPR match the aggregate tables exactly.

**Still unread:** the position/yaw payload (`victim_x/y/z`, `killer_x/y/z`,
`victim_yaw`), `tick`, `victim_team`, `killer_team`. The schema comment
promises death heatmaps; `deaths` gives the statistical view, but no spatial
renderer exists. The `query --html` 2D viewer is the natural place for that.

### `flash_events` — still largely write-only

Only `demo_hash`, `thrower_id`, `is_team_flash`, `duration_s` are read.
**Resolved 2026-08-03:** `is_team_flash=1` rows now feed `TEAM_FLASH_S/RD`
in the roles Utility section (previously the only consumer filtered them
out, so team flashes were stored and never reported).

**Still unsurfaced:** `round_number`, `tick`, `thrower_team`,
`victim_id/team`, victim view angles, flash position, `blind_angle_deg`,
`distance_m`.

`blind_angle_deg` remains the standout: a nontrivial derivation (dot product
of victim view vector vs victim→flash vector; <45° ≈ full blind) with no
reader. It would separate "flashed them from behind" (cheap, full blind)
from "flashed them head-on" (they turned away) — a real flash-quality
signal beyond raw blind seconds.

### `grenade_events` — write-only except 3 columns

Only `demo_hash`, `thrower_id`, `grenade_type='flash'` are read. Smoke / HE
/ molotov / decoy rows are stored and never counted anywhere; throw/land
positions (the lineup-clustering use case in the schema comment) have no
consumer.

### `demos`

| Column | Notes |
|---|---|
| `is_baseline` | Written and scanned into `MatchSummary`, never printed (`info` and `list` both skip it). |
| `quick_hash` | Internal dedupe key; never rendered — arguably by design. |

---

## (b) Surfaced only in `analyze` AI context (no table, not in export JSON)

- ~~`median_trade_kill_delay_ms` / `median_trade_death_delay_ms`~~ —
  **resolved 2026-08-03**, now `TRADE_K_MS` / `TRADE_D_MS`.
- ~~`player_duel_segments.median_sight_deg`~~ — **resolved**, now `MED_SIGHT`.
- `player_match_stats.unused_utility` → `utility.unused_utility`. (The
  per-round twin now renders as `UNUSED` in `rounds`; the match-level total
  still has no column.)
- `player_match_stats.rounds_won` → `overview.rounds_won` / `win_rate`; the
  derived forms (`K/RWIN`, `DMG/RWIN`, export W/L) render, the raw count
  doesn't.
- `player_duel_segments.duel_count` → `duels` + confidence bucketing (the
  FHHS table's `N` is `first_hit_count`, not duel count).

  **Correction (verified 2026-08-03):** this column is not worth surfacing —
  it carries almost no independent information. A duel segment row is only
  written when the player *wins* the duel (the accumulator lives inside the
  kill loop, `aggregator.go` ~line 1098), and `first_hit_count` increments
  whenever that won duel landed at least one hit — which a kill essentially
  always does. DB-wide, `first_hit_count == duel_count` in **99.77%** of
  128,398 rows (175,042 vs 175,334 totals). Two consequences worth
  remembering when reading the FHHS table: `N` is a count of *won* duels, so
  FHHS is **not** a duel win rate — it's "of the duels you won, how often did
  the first bullet hit the head" (a first-bullet precision measure); and
  there is no loss-side equivalent stored per bin (see item 6 under category
  (d)).

`export` / `backtest-dataset` are team-level and touch only the narrow
kill/damage/round/buy/side column set — they widen coverage for none of the
above.

---

## (d) Computed but dropped before storage

Schema work required to recover any of these:

1. **Exact equipment value per player per round** — `RawRound.PlayerEquipValues`
   is bucketed into `buy_type` and discarded. No $4600-vs-$7000 distinction,
   no equipment-differential features.
2. **Bomb plant tick** — collapses to the `is_post_plant` boolean.
   Time-to-plant and post-plant duration are unrecoverable from the DB.
3. **No rounds table** — `StartTick` / `FreezeEndTick` / `EndTick` /
   `WinnerTeam` are never persisted; the winner survives only denormalized
   per player as `won_round`.
4. **Hitgroup distribution** — `RawDamage.HitGroup` is consumed at exactly
   one place (`firstHitHS = hitGroup == "head"`); chest/legs/arms damage
   split is thrown away.
5. **Kill isolation for non-AWP kills** — `RawKill.NearbyVictimTeammates`
   feeds only the AWP `isolated` classifier; the same signal exists for
   every kill.
6. **Per-segment exposure-loss** — the duel engine tracks win-side exposure
   per weapon×distance bin only; the loss side exists only at match level.
7. **Total shots fired** — `counter_strafe_pct` is stored without its
   denominator.
8. **Dead code** — `wilsonCI` in `internal/aggregator/aggregator.go` is
   defined and never called (duplicate of the live copy in
   `internal/report/report.go`).

---

## Surfacing plan

Items 1–7 below all shipped on 2026-08-03.

1. ~~Trade-timing columns~~ → `TRADE_K_MS` / `TRADE_D_MS` in the Aim Timing
   tables (match, aggregate, trend).
2. ~~Deaths drill-down command~~ → the `deaths` command.
3. ~~Scan yaw-speed column~~ → `YAW°/S` in the Aim Timing tables and `rounds`.
4. ~~Segmented exposure time~~ → `EXPO_WIN` (+ `MED_SIGHT`) in the FHHS table.
5. ~~Team-flash count~~ → `TEAM_FLASH_S/RD` in the roles Utility section.
6. ~~Crosshair yaw/pitch split + encounter count~~ → the Crosshair Placement
   tables (match + aggregate).
7. ~~`sql` help text~~ → now lists the three event tables and every Pass
   14–17 column, with a pointer to the `deaths` command.

### Remaining, in payoff order

1. **`blind_angle_deg` flash-quality metric** — the one genuinely
   interesting derived value still unread. Separates full blinds from
   glancing ones.
2. **Grenade-type breakdown** from `grenade_events` — smoke/HE/molotov
   counts per round are stored and never counted anywhere; only flashes are
   queried.
3. **Death heatmaps** — the `player_death_events` position payload, most
   naturally rendered through the existing `query --html` 2D viewer rather
   than a terminal table.
4. **Equipment value / plant tick** (category (d)) — needs schema changes,
   but unlocks economy-aware analysis and time-to-plant.
