# Spec: CSRaw v2 Intermediate Format

> Successor to `.csdem.gz` (v1, defined in `csdem-spec.md`).
>
> **Status: draft / proposal.** Not yet implemented. This document defines the
> target schema; the converter and reader will follow once the schema is agreed.

---

## Motivation

CSRaw v1 (`.csdem.gz`) solved disk + reprocessing cost (519 GB → 821 MB across
1,123 demos, 647× compression). It is *lossy*: the converter materialises only
the events and fields the aggregator needed at conversion time, then the original
`.dem` is discarded.

The lossy choice has bitten us. New metrics that require fields v1 did not capture
cannot be added retroactively — the only fix is to re-parse from `.dem`, which we
no longer keep. Concretely:

- v1 does not sample player state during freeze-time → cannot study buy-time positioning, fake-buy detection.
- v1 frames are 16-tick (4 Hz) and viewer-only → not dense enough for any tick-precise gameplay metric.
- v1 captures view angles only at `kill`, `weapon_fire`, `first_sight` → cannot study flick/peek/off-angle dynamics.
- v1 has no velocity vector outside `weapon_fire` → cannot extend counter-strafe analysis.
- v1 has no spotted-by mask → no engine-truth visibility data for visibility-based metrics.
- v1 has no per-tick ammo / scope / reload state → blocks weapon-handling metrics.

v2 is designed to be **bulletproof for any future gameplay metric** while keeping
total archive size in the low-GB range.

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
├── header.json                # match metadata + sampling config + schema version
├── events.parquet             # all game events (kills, damages, fires, …)
├── player_samples.parquet     # tick-sampled player state, partitioned by round
├── projectile_samples.parquet # in-flight grenades + bomb state
└── viewer.parquet             # OPTIONAL: viewer-only frames/trails/grenades/shots
```

Why not a single `.parquet`? Different streams have different schemas and very
different row counts; one file per stream keeps row-group sizing sensible and lets
go-cs-metrics skip `viewer.parquet` entirely (it never reads it).

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
  "csraw_schema_version": "2.0.0",
  "cs2_protocol_version": 13992,
  "writer": "go-cs-metrics/converter v1.4.0",
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
  "csraw_schema_version": "2.0.0",
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
      "phase": "regulation"         // "regulation" | "ot1" | "ot2" | …
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

## Stream 2: `events.parquet`

One row per game event. Replaces v1's separate `kills`/`damages`/`flashes`/…
arrays with a single tagged-union table.

### Common columns (every row)

| Column | Type | Notes |
|---|---|---|
| `tick` | int32 | Tick when the event resolved |
| `round` | int16 | 1-indexed round number |
| `event_type` | uint8 enum | See enum below |

### `event_type` enum

| Id | Name | Notes |
|---|---|---|
| 1 | `kill` | Includes suicide if killer == victim |
| 2 | `damage` | Per `player_hurt` event |
| 3 | `weapon_fire` | Single shot; one row per bullet (continuous fire = many rows) |
| 4 | `flash` | Effective flash (positive duration) only |
| 5 | `first_sight` | Crosshair-placement event (existing v1 logic) |
| 6 | `grenade_throw` | Replaces v1 `grenade_events` |
| 7 | `grenade_detonate` | Smoke pop, HE detonate, flash detonate, molotov ignite |
| 8 | `bomb_plant_begin` | |
| 9 | `bomb_plant_complete` | |
| 10 | `bomb_defuse_begin` | |
| 11 | `bomb_defuse_complete` | |
| 12 | `bomb_explode` | |
| 13 | `item_pickup` | Weapon, armour, kit, grenade, dropped C4 |
| 14 | `item_drop` | |
| 15 | `equip_change` | Active weapon switch |
| 16 | `score_change` | OT detection, regulation/OT boundary marker |
| 17 | `chat_command` | Player chat / radio command (text + sender) |
| 18 | `round_start` | Mirrors `header.rounds[i].start_tick`; convenience |
| 19 | `round_end` | Mirrors `header.rounds[i].end_tick` + win reason |

### Type-specific columns (nullable; populated only for matching `event_type`)

`kill` (event_type = 1):

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

`damage` (event_type = 2):

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

`weapon_fire` (event_type = 3):

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

`flash` (event_type = 4):

| Column | Type | Notes |
|---|---|---|
| `attacker_slot` | uint8 | |
| `victim_slot` | uint8 | |
| `flash_duration_ms` | uint16 | |
| `victim_pos_x`, `_y`, `_z` | int16 | |
| `victim_yaw_deg`, `victim_pitch_deg` | int16 | |
| `victim_through_smoke` | bool | NEW |

`first_sight` (event_type = 5):

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

`grenade_throw` (event_type = 6):

| Column | Type | Notes |
|---|---|---|
| `thrower_slot` | uint8 | |
| `grenade_type` | uint8 enum | smoke/flash/he/molotov/decoy/incendiary |
| `throw_pos_x`, `_y`, `_z` | int16 | |
| `throw_yaw_deg`, `throw_pitch_deg` | int16 | |
| `throw_vel_x`, `_y`, `_z` | int16 | NEW — exact throw vector |
| `is_jumpthrow` | bool | NEW — derived from velocity heuristics |
| `projectile_id` | uint32 | Foreign key to `projectile_samples.projectile_id` |

`grenade_detonate` (event_type = 7):

| Column | Type | Notes |
|---|---|---|
| `projectile_id` | uint32 | |
| `grenade_type` | uint8 enum | |
| `pos_x`, `_y`, `_z` | int16 | |

`bomb_*` (event_type = 8..12):

| Column | Type | Notes |
|---|---|---|
| `player_slot` | int8 | -1 for `bomb_explode` |
| `pos_x`, `_y`, `_z` | int16 | |
| `site` | uint8 enum | A / B |
| `had_kit` | bool | Defuse only |

`item_pickup` / `item_drop` (event_type = 13/14):

| Column | Type | Notes |
|---|---|---|
| `player_slot` | uint8 | |
| `item_id` | uint8 | weapon_id or special id (kit, armor, defuser) |
| `pos_x`, `_y`, `_z` | int16 | |
| `from_slot` | int8 | NEW: who dropped it (item_pickup); -1 if from spawn/floor |

`equip_change` (event_type = 15):

| Column | Type | Notes |
|---|---|---|
| `player_slot` | uint8 | |
| `weapon_id` | uint8 | new active weapon |
| `prev_weapon_id` | uint8 | |

`chat_command` (event_type = 17):

| Column | Type | Notes |
|---|---|---|
| `player_slot` | uint8 | |
| `text` | string | |
| `is_team_only` | bool | |

### Forward-compat field

All event rows include:

| Column | Type | Notes |
|---|---|---|
| `extra` | binary (nullable) | CBOR-encoded map for fields not yet in the schema. Reader contract: ignore unknown keys. |

This is the escape hatch for fields a future converter wants to add without
bumping the schema major version (e.g. a new flag from a CS2 patch we want to
preserve immediately).

---

## Stream 3: `player_samples.parquet`

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
| `extra` | binary | nullable forward-compat |

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
- For every row in `events.parquet` involving a player (any of `killer_slot`, `victim_slot`, `attacker_slot`, `shooter_slot`, `observer_slot`, `enemy_slot`, `thrower_slot`, `player_slot`):
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

## Stream 4: `projectile_samples.parquet`

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

## Stream 5: `viewer.parquet` (optional)

Mirrors v1 viewer streams (`frames`, `bombs`, `grenades`, `trails`, `shots`)
in columnar form. **Not read by go-cs-metrics.** Preserved so cs-demo-viewer
can still render the match without the original `.dem`.

Single Parquet with a `viewer_kind` enum tag. Estimated **~1.0 MB** Parquet+zstd
(roughly the v1 viewer payload re-encoded).

When converting in headless mode (no viewer needed), this file is omitted and
the corresponding header field is set:

```json
"viewer": { "included": false }
```

---

## Total size budget (median pro match)

| Stream | Compressed |
|---|---:|
| `header.json` | ~5 KB |
| `events.parquet` | ~200 KB |
| `player_samples.parquet` | ~1.6 MB |
| `projectile_samples.parquet` | ~30 KB |
| `viewer.parquet` (optional) | ~1.0 MB |
| **Total without viewer** | **~1.85 MB** |
| **Total with viewer** | **~2.85 MB** |

Across 1,123 demos:

- Without viewer: ~2.1 GB (vs v1 821 MB, vs .dem 519 GB)
- With viewer: ~3.2 GB

---

## Schema evolution rules

1. **Adding a column is a minor bump (`2.x.0`).** New column must be nullable.
   Old readers built against the previous minor must decode the file unchanged
   (Parquet column projection ignores unknown columns).
2. **Removing a column requires a major bump (`x.0.0`).** Avoid. Prefer marking
   it deprecated in the schema doc and leaving it as nullable.
3. **Changing a column's type or units requires a major bump.** Avoid by adding
   a new column with a clearer name and deprecating the old one.
4. **The `extra` binary column** (CBOR-encoded map) lets the converter emit
   fields not yet in the schema without any version bump. Readers ignore unknown
   keys. Use sparingly — fields that prove useful should be promoted to first-class
   columns at the next minor bump.
5. **Header `sampling` block is authoritative.** A reader doing a metric that
   needs >`baseline_hz` resolution must check `event_window_hz` and the relevant
   event types are present.

---

## Validation strategy

For every metric currently produced by the aggregator, define a fixture set:
- N=20 demos spanning short/long matches, all maps, an OT, a forfeit.
- Baseline: aggregator output from re-parsing the original `.dem` (ground truth).
- Test: aggregator output from `replay --csraw2 <file>`.

A v2 converter is considered correct only when **every metric matches the .dem
baseline byte-for-byte** across the fixture set. The two-demo `won_round` drift
observed in v1 is unacceptable in v2.

Stretch test: for each fixture demo, also confirm that querying
`player_samples.parquet` at any event tick reproduces the per-event positions
already in `events.parquet` (internal consistency).

---

## Migration plan

### Phase 1: ship the writer
1. Implement `convert --schema v2 --dir <event>` alongside existing v1 converter.
2. Validate against fixture set (above).
3. v1 writer remains the default until parity is proven.

### Phase 2: dual-format archive
1. Re-download (via demoget) and re-convert one event end-to-end as a smoke test.
2. Convert the rest of the archive to v2 in parallel with v1 still authoritative.
3. Aggregator gains `replay-v2` reader; `parse` auto-detects `.csraw2.tar`
   alongside `.csdem.gz`.

### Phase 3: cutover
1. v2 writer becomes the default.
2. Original `.dem` files may be deleted **after** the per-event fixture validation
   passes for that event.
3. v1 `.csdem.gz` files retained for cs-demo-viewer until viewer is updated to
   read v2 `viewer.parquet`.

### Phase 4: retire v1
1. Update cs-demo-viewer to read v2 `viewer.parquet`.
2. Delete v1 `.csdem.gz` files.

---

## Open questions

1. **Parquet library choice.** `parquet-go` (Apache) vs `xitongsys/parquet-go`
   vs writing via DuckDB/Arrow CLI. First two are pure Go; preference is for
   pure Go to stay consistent with the modernc/sqlite "no CGo" stance.
2. **Tar vs separate files in a directory.** Tar simplifies move/copy/checksum;
   directory simplifies streaming reads. Decision: tar for now, revisit if
   per-stream random access becomes a hot path.
3. **`is_in_smoke` truth source.** The engine flag is reliable post-detonation
   but the volume only exists for ~18 s. Need to confirm demoinfocs exposes it
   or we derive it from grenade detonation pos + time.
4. **Wallbang detection in v1.** v1 silently dropped `penetrated_count`. For
   pre-v2 demos we still re-parse to backfill, or accept this as a v2-only metric.
5. **Sampling baseline for low-tickrate demos.** FACEIT is 128 tickrate; pro is
   64; some old demos are 32. `baseline_hz` = 16 means 8 ticks at 128, 4 at 64,
   2 at 32. Confirm 16 Hz is enough at all tickrates for the metrics we care
   about (counter-strafe needs the velocity at exact `weapon_fire` tick, which
   is captured directly in `events.parquet` regardless of sample rate, so this
   should be fine).
