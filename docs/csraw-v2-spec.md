# Spec: CSRaw v2 Intermediate Format

> **Status: implementation in progress.**
>
> Slice 1 (this commit): `internal/csraw2` package — Go types matching the
> schema, tar+parquet writer/reader, round-trip tested. No CLI / converter
> wiring yet.
>
> v2 is a clean break: there is no migration path from v1 (`.csdem.gz`).
> The DB will be wiped and demos re-parsed from `.dem` once the v2 pipeline
> is functional.

---

## Motivation

The original lossy intermediate format captured only the events and fields the
aggregator needed at conversion time. New metrics that need any unrecorded
field — visibility mask, full velocity vector, freeze-time positioning, ammo
state — could not be added retroactively without re-parsing from `.dem`.
Concretely, the gaps that drove v2:

- No player state sampled during freeze-time → no buy-time positioning, no
  fake-buy detection.
- 4 Hz post-freeze-end frame sampling, viewer-only → too sparse for any
  tick-precise gameplay metric.
- View angles captured only at `kill`, `weapon_fire`, `first_sight` → no
  flick / peek / off-angle dynamics.
- No velocity vector outside `weapon_fire` → no extension of counter-strafe.
- No spotted-by mask → no engine-truth visibility metrics.
- No per-tick ammo / scope / reload state → no weapon-handling metrics.

v2 is designed to be **bulletproof for any plausible future gameplay metric**
while keeping total archive size in the low-GB range.

---

## Goals

| Goal | Priority |
|---|---|
| Capture every gameplay-relevant field present in the `.dem`, at tick resolution sufficient for any plausible future metric | primary |
| Forward-compatible schema: adding a new column does not require re-converting old files | primary |
| Re-aggregation from v2 must remain "minutes for the full archive" (v1 baseline: ~80 s for 1,100 demos) | primary |
| Total archive size for 1,000 pro demos ≤ 3 GB | primary |
| Lossless round-trip for every metric currently produced by the aggregator | primary |
| Zero dependency on demoinfocs at re-aggregation time | constraint |
| Pure-Go writer + reader (no CGo) | constraint |
| Discardable original `.dem` once v2 conversion is verified | outcome |

## Non-goals

- Bit-exact reconstruction of the `.dem` byte stream.
- Replay rendering (animation poses, particle state, sounds, voice).
- Anti-cheat / replay-protection invariants.
- Network-layer replay (server messages, command queue).

---

## Format

### Serialisation

**Per-match directory of streams**, packaged as a single uncompressed `.tar` for
filesystem ergonomics:

```
the-mongolz-vs-natus-vincere-m1-inferno.csraw2.tar
├── header.json                  # match metadata + sampling config + schema version
├── kills.parquet                # one row per kill
├── damages.parquet              # one row per player_hurt
├── weapon_fires.parquet         # one row per shot fired
├── flashes.parquet              # one row per effective flash
├── first_sights.parquet         # one row per crosshair-placement event
├── grenade_throws.parquet       # one row per projectile thrown
├── grenade_detonations.parquet  # one row per smoke-pop / HE / flash / molotov ignite
├── bomb_actions.parquet         # plant_begin / plant_complete / defuse_* / explode
├── item_pickups.parquet         # weapon / kit / armor / grenade pickups
├── item_drops.parquet           # item drops (death drops + manual drops)
├── equip_changes.parquet        # active-weapon switches
├── chat_commands.parquet        # chat / radio commands
├── player_samples.parquet       # tick-sampled player state
└── projectile_samples.parquet   # in-flight grenades + bomb state
```

One file per event type instead of a single tagged-union table — keeps each
schema dense and narrow, makes ad-hoc DuckDB queries (`SELECT * FROM
'kills.parquet'`) natural, and means tools like go-cs-metrics never touch
streams they don't need (e.g. the converter doesn't have to read
`chat_commands.parquet` to compute kills).

Why `.tar` and not gzip? Parquet files are already zstd-compressed internally;
re-gzipping the tar gains <2% and breaks random access into individual streams.

Files written atomically: stage to `*.csraw2.tar.tmp`, fsync, rename.

### Compression

- **Parquet streams:** zstd level 9, dictionary encoding on string columns, RLE on bools/enums.
- **JSON header:** uncompressed; <10 KB per match.

### File naming

Same directory as `.dem` (or `.csdem.gz`), same base name, `.csraw2.tar` extension.

```
~/demos/pro/iem_cologne_2025/navi-vs-faze-m3-nuke.dem
~/demos/pro/iem_cologne_2025/navi-vs-faze-m3-nuke.csdem.gz   # v1, deprecated
~/demos/pro/iem_cologne_2025/navi-vs-faze-m3-nuke.csraw2.tar # v2
```

---

## Schema version

```json
{
  "csraw_version": 2,
  "csraw_schema_version": "2.2.0",
  "cs2_protocol_version": 13992,
  "writer": "go-cs-metrics/converter v0.1.0",
  "writer_demoinfocs_version": "v4.x.y"
}
```

Schema follows semver:

- **Patch** (`2.0.x`): documentation only, no on-disk change.
- **Minor** (`2.x.0`): new column added, nullable. **Old readers must work unchanged** (Parquet ignores unknown columns; new readers tolerate column absence).
- **Major** (`x.0.0`): breaking change (column removed, type change, semantics change). Requires re-conversion. Avoided unless absolutely necessary.

---

## Stream 1: `header.json`

Match-level metadata. Small; fully read into memory at aggregator start.

```json
{
  "csraw_version": 2,
  "csraw_schema_version": "2.2.0",
  "cs2_protocol_version": 13992,

  "demo_hash": "sha256:…",          // full SHA-256 of original .dem
  "quick_hash": "sha256:…",         // first 64 KB SHA-256 (matches v1 quick-hash)
  "source_dem_size_bytes": 487123456,
  "source_dem_mtime": "2025-07-29T19:14:00Z",

  "tier": "pro",                    // "pro" | "faceit-5" | …
  "match_type": "competitive",
  "map": "de_nuke",
  "tickrate": 64,
  "match_date": "2025-07-29",       // from .dem mtime

  "sampling": {
    "baseline_hz": 16,              // player_samples baseline
    "event_window_hz": 64,          // dense sampling around events
    "event_window_radius_ticks": 32,// half-window in ticks (0.5 s @ 64 Hz)
    "freeze_time_sampled": true,    // v1 was false
    "projectile_hz": 32
  },

  "players": [
    {
      "slot": 0,                    // 0..9; matches player_samples.player_idx
      "steam_id": "76561198…",
      "name": "s1mple",
      "starting_team": "T",         // "T" | "CT"
      "is_bot": false
    }
  ],

  "rounds": [
    {
      "n": 1,
      "start_tick": 1234,
      "freeze_end_tick": 1488,
      "first_kill_tick": 1612,
      "bomb_plant_tick": 0,         // 0 if no plant
      "end_tick": 4321,
      "winner": "CT",               // "T" | "CT" | "DRAW"
      "win_reason": "ct_win_elimination", // engine reason string
      "ct_score_after": 1,
      "t_score_after": 0,
      "phase": "regulation",        // "regulation" | "ot1" | "ot2" | …
      "players_at_end": [0,1,2,3,4,5,6,7,8,9] // slot list — Participants().Playing() at RoundEnd (schema 2.2.0)
    }
  ],

  "weapon_table": {                 // numeric ids used in events + samples
    "0":  "knife",
    "1":  "glock",
    "2":  "usp_silencer",
    "…":  "…"
  }
}
```

---

## Event streams

Each event type is a separate parquet file inside the archive (see file
layout above). Every row has at minimum a `tick` (int32) and `round` (int16);
type-specific columns follow. All position columns are int16 in Hammer units;
view angles int16 quantised to 1/100°; velocities int16 in u/s.

### `kills.parquet`

| Column | Type | Notes |
|---|---|---|
| `killer_slot` | uint8 | -1 if world/suicide |
| `victim_slot` | uint8 | |
| `assister_slot` | int8 | -1 if none |
| `weapon_id` | uint8 | Index into `header.weapon_table` |
| `is_headshot` | bool | |
| `is_wallbang` | bool | NEW vs v1 — penetrated count > 0 |
| `penetrated_count` | uint8 | NEW |
| `is_no_scope` | bool | |
| `is_through_smoke` | bool | |
| `is_attacker_blind` | bool | |
| `flash_assist` | bool | |
| `assisted_flash_attacker_slot` | int8 | NEW — who threw the flash |
| `nearby_victim_teammates` | uint8 | AWP death classifier |
| `killer_pos_x`, `_y`, `_z` | int16 | Hammer units |
| `victim_pos_x`, `_y`, `_z` | int16 | |
| `killer_yaw_deg`, `killer_pitch_deg` | int16 | 1/100° quantization |
| `victim_yaw_deg`, `victim_pitch_deg` | int16 | |
| `victim_view_distance` | uint16 | NEW — distance from victim crosshair to killer position |
| `killer_active_weapon_id` | uint8 | NEW — what was held when the kill registered (may differ on weapon-swap kills) |

### `damages.parquet`

| Column | Type | Notes |
|---|---|---|
| `attacker_slot` | int8 | -1 if world/fall damage |
| `victim_slot` | uint8 | |
| `weapon_id` | uint8 | |
| `health_damage` | uint16 | |
| `armor_damage` | uint16 | NEW vs v1 |
| `pre_damage_hp`, `pre_damage_armor` | uint8 | NEW |
| `post_damage_hp`, `post_damage_armor` | uint8 | NEW |
| `hit_group` | uint8 enum | head/chest/stomach/leftarm/rightarm/leftleg/rightleg/neck/gear |
| `is_utility` | bool | |
| `attacker_pos_x`, `_y`, `_z` | int16 | NEW (v1 only had victim pos) |
| `victim_pos_x`, `_y`, `_z` | int16 | |

### `weapon_fires.parquet`

| Column | Type | Notes |
|---|---|---|
| `shooter_slot` | uint8 | |
| `weapon_id` | uint8 | |
| `pos_x`, `_y`, `_z` | int16 | |
| `yaw_deg`, `pitch_deg` | int16 | 1/100° |
| `vel_x`, `_y`, `_z` | int16 | NEW — full velocity vector (v1 had h_speed only) |
| `is_scoped` | bool | NEW |
| `clip_ammo_after` | uint8 | NEW |
| `is_silenced` | bool | NEW |
| `recoil_index` | uint8 | NEW — Nth shot in burst (0 = first) |

### `flashes.parquet`

| Column | Type | Notes |
|---|---|---|
| `attacker_slot` | uint8 | |
| `victim_slot` | uint8 | |
| `flash_duration_ms` | uint16 | |
| `victim_pos_x`, `_y`, `_z` | int16 | |
| `victim_yaw_deg`, `victim_pitch_deg` | int16 | |
| `victim_through_smoke` | bool | NEW |

### `first_sights.parquet`

| Column | Type | Notes |
|---|---|---|
| `observer_slot` | uint8 | |
| `enemy_slot` | uint8 | |
| `angle_deg` | int16 | 1/100° |
| `pitch_deg`, `yaw_deg` | int16 | |
| `observer_pitch_deg`, `observer_yaw_deg` | int16 | |
| `observer_pos_x`, `_y`, `_z` | int16 | NEW |
| `enemy_pos_x`, `_y`, `_z` | int16 | NEW |
| `was_already_visible` | bool | NEW — distinguishes "first sight after los break" vs "true first sight" |

### `grenade_throws.parquet`

| Column | Type | Notes |
|---|---|---|
| `thrower_slot` | uint8 | |
| `grenade_type` | uint8 enum | smoke/flash/he/molotov/decoy/incendiary |
| `throw_pos_x`, `_y`, `_z` | int16 | |
| `throw_yaw_deg`, `throw_pitch_deg` | int16 | |
| `throw_vel_x`, `_y`, `_z` | int16 | NEW — exact throw vector |
| `is_jumpthrow` | bool | NEW — derived from velocity heuristics |
| `projectile_id` | uint32 | Foreign key to `projectile_samples.projectile_id` |

### `grenade_detonations.parquet`

| Column | Type | Notes |
|---|---|---|
| `projectile_id` | uint32 | |
| `grenade_type` | uint8 enum | |
| `pos_x`, `_y`, `_z` | int16 | |

### `bomb_actions.parquet`

`action` enum: 1=plant_begin, 2=plant_complete, 3=defuse_begin,
4=defuse_complete, 5=explode.

| Column | Type | Notes |
|---|---|---|
| `player_slot` | int8 | -1 for `bomb_explode` |
| `pos_x`, `_y`, `_z` | int16 | |
| `site` | uint8 enum | A / B |
| `had_kit` | bool | Defuse only |

### `item_pickups.parquet` / `item_drops.parquet`

| Column | Type | Notes |
|---|---|---|
| `player_slot` | uint8 | |
| `item_id` | uint8 | weapon_id or special id (kit, armor, defuser) |
| `pos_x`, `_y`, `_z` | int16 | |
| `from_slot` | int8 | NEW: who dropped it (item_pickup); -1 if from spawn/floor |

### `equip_changes.parquet`

| Column | Type | Notes |
|---|---|---|
| `player_slot` | uint8 | |
| `weapon_id` | uint8 | new active weapon |
| `prev_weapon_id` | uint8 | |

### `chat_commands.parquet`

| Column | Type | Notes |
|---|---|---|
| `player_slot` | uint8 | |
| `text` | string | |
| `is_team_only` | bool | |

---

## Stream: `player_samples.parquet`

The big one. Per-tick player state. Partitioned by round (one row group per
round) for cheap per-round seek.

| Column | Type | Notes |
|---|---|---|
| `tick` | int32 | Absolute demo tick |
| `round` | int16 | 1-indexed |
| `player_slot` | uint8 | 0..9 |
| `density_tier` | uint8 enum | `0=baseline` (16 Hz), `1=event_window` (64 Hz around events) |
| `pos_x`, `pos_y`, `pos_z` | int16 | Hammer units, 1u = 1.905 cm |
| `yaw_deg`, `pitch_deg` | int16 | 1/100° |
| `vel_x`, `vel_y`, `vel_z` | int16 | u/s |
| `hp` | uint8 | 0 = dead |
| `armor` | uint8 | |
| `flags` | uint16 | bitfield (see below) |
| `active_weapon_id` | uint8 | |
| `clip_ammo` | uint8 | |
| `reserve_ammo` | uint16 | |
| `flash_remaining_ms` | uint16 | 0 = not flashed |
| `money` | uint16 | |
| `equip_value` | uint16 | total $ value of inventory |
| `visible_enemies_mask` | uint16 | bit `i` = enemy slot `i` is in this player's spotted set |
| `last_shot_tick_offset` | uint8 | ticks since last shot, capped 255 |
| `last_damage_tick_offset` | uint8 | ticks since last damage taken, capped 255 |

### `flags` bitfield

| Bit | Name | Notes |
|---|---|---|
| 0 | `alive` | redundant with `hp > 0` but kept for filter speed |
| 1 | `has_kit` | defuse kit |
| 2 | `has_helmet` | |
| 3 | `is_scoped` | AWP/SSG/AUG/SG553 scoped |
| 4 | `is_reloading` | |
| 5 | `is_walking` | shift held |
| 6 | `is_ducking` | crouch held |
| 7 | `is_on_ground` | |
| 8 | `is_in_air` | redundant with !on_ground; kept for clarity |
| 9 | `is_bomb_carrier` | |
| 10 | `is_planting` | actively planting C4 |
| 11 | `is_defusing` | actively defusing |
| 12 | `is_in_smoke` | engine-truth: player position inside an active smoke volume |
| 13 | `is_in_flames` | inside molotov/incendiary AOE |
| 14 | `is_jumping` | velocity_z > 0 and not on ground |
| 15 | `is_blind` | flash_remaining_ms > 1500 (gameplay-impactful blind, not just glow) |

### Sampling rules

**Baseline track:**
- Every player, every 4 ticks (16 Hz at 64 tickrate), throughout the entire match including freeze-time and warmup-end.
- Dead players: emit one row at death tick, then suppress until respawn (next round).

**Event-window track:**
- For every event row across all event streams that involves a player (any of `killer_slot`, `victim_slot`, `attacker_slot`, `shooter_slot`, `observer_slot`, `enemy_slot`, `thrower_slot`, `player_slot`):
  - Emit a row for that player at every tick in `[event.tick - 32, event.tick + 32]`.
- Deduplicate against baseline rows at the same `(tick, player_slot)` (event-window wins; `density_tier=1`).

**Quantization:**
- `pos_*`: int16 covers ±32,768 Hammer units. CS2 maps fit in ±16,384. **Lossless** at integer-unit precision; CS2 internal positions are fixed-point with sub-unit precision lost.
- `yaw_deg, pitch_deg`: int16 = ±327.67°, 1/100° step. CS2 view angles are float32; quantizing to 1/100° loses sub-degree-hundredth precision (irrelevant for any metric).
- `vel_*`: int16 = ±327.67 u/s. CS2 max player velocity ~260 u/s. Lossless at 1 u/s precision.

### Estimated size (median pro match)

44 min, 64 tickrate, 10 players:

- Baseline rows: 10 × 16 Hz × 44 min × 60 s = 422,400 rows
- Event-window rows (after dedup): ~80,000 rows
- Total: ~500,000 rows
- Raw row size: ~32 bytes
- Raw stream: ~16 MB
- Parquet+zstd (10× typical for this kind of data): **~1.6 MB**

---

## Stream: `projectile_samples.parquet`

In-flight grenades and bomb state.

| Column | Type | Notes |
|---|---|---|
| `tick` | int32 | |
| `round` | int16 | |
| `projectile_id` | uint32 | unique within match |
| `kind` | uint8 enum | `0=grenade`, `1=bomb` |
| `subtype` | uint8 enum | grenade type, or bomb state (idle/planted/defusing/exploded) |
| `owner_slot` | int8 | -1 = world |
| `pos_x`, `pos_y`, `pos_z` | int16 | |
| `vel_x`, `vel_y`, `vel_z` | int16 | nullable (bomb has zero velocity) |
| `fuse_remaining_ms` | int16 | nullable; -1 if N/A |
| `defuse_progress_pct` | uint8 | nullable; bomb only |

Sampled at `header.sampling.projectile_hz` (default 32 Hz, i.e. every 2 ticks)
while a projectile is active, plus dense (every tick) within the same event
windows as `player_samples`.

Estimated: ~5,000 rows, ~150 KB raw, **~30 KB** Parquet+zstd.

---

---

## Total size budget (median pro match)

| Stream | Compressed (estimate) |
|---|---:|
| `header.json` | ~5 KB |
| All event parquets combined | ~200 KB |
| `player_samples.parquet` | ~1.6 MB |
| `projectile_samples.parquet` | ~30 KB |
| **Per-match total** | **~1.85 MB** |

Across ~1,000 pro demos: ~2 GB total. To be verified against real data once
the converter (Slice 3) is wired up.

---

## Schema evolution rules

1. **Adding a column is a minor bump (`2.x.0`).** New column must be nullable.
   Old readers built against the previous minor must decode the file unchanged
   (Parquet column projection ignores unknown columns).
2. **Removing a column requires a major bump (`x.0.0`).** Avoid. Prefer marking
   it deprecated in the schema doc and leaving it as nullable.
3. **Changing a column's type or units requires a major bump.** Avoid by adding
   a new column with a clearer name and deprecating the old one.
4. **Header `sampling` block is authoritative.** A reader doing a metric that
   needs >`baseline_hz` resolution must check `event_window_hz` and the relevant
   event streams are present.

---

## Validation strategy

Validation happens against `.dem` directly, not against any prior format —
v2 is the only intermediate that exists. For every metric the aggregator
produces, we want a fixture set:

- N=20 demos spanning short/long matches, all maps, an OT, a forfeit.
- Baseline: aggregator output from parsing the `.dem` directly.
- Test: aggregator output from reading `.csraw2.tar` and re-aggregating.

A v2 converter is considered correct only when **every metric matches the
`.dem` baseline byte-for-byte** across the fixture set.

Stretch test: for each fixture demo, also confirm that querying
`player_samples.parquet` at any event tick reproduces the per-event positions
already in the matching event stream (internal consistency).

---

## Implementation slices

| Slice | Status | Deliverable |
|---|---|---|
| 1 | done | `internal/csraw2` package: types, tar+parquet writer/reader, round-trip tested |
| 2 | done | `internal/parserv2`: walks `.dem` and produces a `csraw2.Match` directly. Captures every event/sample field the v2 schema asks for (visibility mask, full velocity, freeze-time samples, ammo state, scoped/reload flags, penetrated_count, equip changes, item pickup/drop, chat). Event-window dense sampling deferred — Slice 2 emits player_samples at 16 Hz baseline only |
| 3 | done | `cmd/csraw2-probe`: end-to-end .dem → .csraw2.tar + readback verifier. Measured **1.69 MB / 14-round comp match** (149 MB .dem → 88× compression, 6.6 s parse). Player samples are 83% of the archive. Pro-rated to a 20-round match: ~2.4 MB, in line with the spec's 1.85 MB estimate |
| 4 | done | `internal/csraw2bridge`: `csraw2.Match → model.RawMatch` adapter so the existing 11-pass aggregator runs unchanged. Schema bumped 2.0.0 → 2.1.0 (added `team` to `PlayerSample` so the bridge can resolve per-round teams across MR12 halftime flips). `cmd/csraw2-compare` validated parity on two real demos: every player row matches v1 byte-for-byte on K/D/Damage/ADR/Rounds; every event count except `duel_segs` (1-tick edge case, ~2%) matches exactly |
| 5 | done | Validation fixture set: `csraw2-compare -batch` swept all 45 personal `.dem` files (13 GB; pro `.dem` no longer on disk — only `.csdem.gz` remain). **First sweep (schema 2.1.0): 41/45 perfect parity (91.1%)**. Of the 4 divergent demos, 2 hit the known 1-tick `duel_segs` edge case and 2 hit a presence-heuristic divergence in `player_round_stats`. The latter was traced to v2's bridge inferring "did this player play this round?" from samples, while v1 reads it from `Participants().Playing()` at RoundEnd — so a mid-round disconnect's stale samples wrongly credited the player with the round. **Resolved in schema 2.2.0** (added `Round.PlayersAtEnd`). **Re-sweep on 2.2.0: 43/45 (95.6%)** — only the 2 known `duel_segs` cases remain |
| 6 | next | Resolve the `duel_segs` 1-tick edge (deferred), then rewire `convert` / `parse` / `replay` for v2; delete v1 (`.csdem.gz`) code paths |

---

## Open questions

1. **`is_in_smoke` truth source.** The engine flag is reliable post-detonation
   but the volume only exists for ~18 s. Need to confirm demoinfocs exposes it
   or we derive it from grenade detonation pos + time.
2. **Wallbang detection.** Need to confirm demoinfocs exposes `PenetratedObjects`
   on the kill event. If yes, `is_wallbang` and `penetrated_count` populate
   directly.
3. **Sampling baseline for low-tickrate demos.** FACEIT is 128 tickrate; pro is
   64; some old demos are 32. `baseline_hz` = 16 means 8 ticks at 128, 4 at 64,
   2 at 32. Confirm 16 Hz is enough at all tickrates for the metrics we care
   about (counter-strafe needs the velocity at exact `weapon_fire` tick, which
   is captured directly in `weapon_fires.parquet` regardless of sample rate,
   so this should be fine).
4. **`duel_segs` 1-tick edge case.** ~4% of demos produce one extra/missing
   duel segment relative to v1. Likely a boundary in how the duel engine
   decides the start/end tick of a segment when working from `player_samples`
   vs the original event stream. Needs a one-demo bisection.
5. ~~**`player_round_stats` presence heuristic.**~~ **Resolved in schema 2.2.0.**
   Root cause was v2 inferring per-round presence from samples, while v1 reads
   it from `Participants().Playing()` at RoundEnd. Mid-round disconnects
   left stale samples that wrongly credited the player with the round.
   Fix: parserv2 now snapshots the engine-truth slot list into
   `Round.PlayersAtEnd`, and the bridge gates `PlayerEndState` on that list.
   Confirmed on the two original divergent demos
   (`match730_003781333593737920753_0359605065_202`,
   `match730_003804576015418655085_0300186795_201`) and across the full
   45-demo sweep — zero per-player K/D/Dmg/Rds divergences post-fix.
