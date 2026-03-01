# Spec: Unified Demo Intermediate Format (`.csdem.gz`)

> Supersedes `rawmatch-gz-spec.md` (go-cs-metrics-only `RawMatch` approach).

---

## Motivation

Demo files are 200–750 MB each; the current archive is ~526 GB. Problems:

1. **Disk usage** — impractical to store long-term at scale.
2. **Reprocessing cost** — full DB rebuilds require re-parsing every `.dem` through
   demoinfocs (~minutes per demo, ~4–29 GB RAM peaks). Parallelism is capped at
   `--workers 1` due to GC pressure; sequential rebuilds of the full archive take hours.
3. **No 2D review** — `cs-demo-viewer` currently requires the original `.dem`, so
   deleted demos can no longer be reviewed.

A single intermediate format — produced once via a conversion pass, replacing the `.dem`
— solves all three: it is small enough to keep forever, fast enough to re-aggregate with
multiple workers, and rich enough to drive the 2D viewer.

**Measured size:** 580–740× reduction per demo (e.g. 400 MB `.dem` → ~700 KB `.csdem.gz`).
Across 18 events (520 GB of `.dem`): **423 GB of converted demos → 679 MB** (638× average).

---

## Goals

| Goal | Status |
|---|---|
| Reduce archive disk usage significantly | primary |
| Enable fast full DB rebuilds (more workers, less RAM) | primary |
| Enable 2D viewer from the intermediate file (no `.dem` needed) | primary |
| Zero new library dependencies (standard library only) | constraint |
| Backward compatible: old `.dem` workflow untouched | constraint |

---

## Format

### Serialisation

**gzip-compressed JSON** (`encoding/json` + `compress/gzip`, standard library only).

Written atomically: encode to a temp file, then `os.Rename` to the final path.

### File naming

Same directory as the `.dem`, same base name, `.csdem.gz` extension.

```
~/demos/pro/iem_katowice_2025/liquid-vs-spirit-m1-nuke.dem
~/demos/pro/iem_katowice_2025/liquid-vs-spirit-m1-nuke.csdem.gz
```

### File wrapper

```go
// UnifiedMatchFile is the top-level structure serialised to disk.
type UnifiedMatchFile struct {
    Version int           `json:"version"` // = 1; bump on any breaking schema change
    Tier    string        `json:"tier"`    // "pro", "faceit-5" — set at conversion time
    Match   *UnifiedMatch `json:"match"`
}

const UnifiedMatchFileVersion = 1
```

`Tier` is baked in at conversion time so `replay` and `demoview` require no `--tier` flag.

---

## Structs

### UnifiedMatch

```go
type UnifiedMatch struct {
    DemoHash  string  `json:"demo_hash"`
    MapName   string  `json:"map"`
    MatchDate string  `json:"match_date"` // from .dem file mtime at conversion time
    MatchType string  `json:"match_type"`
    Tickrate  float64 `json:"tickrate"`

    // Player roster — index into this slice is the player idx used in Frames,
    // Grenades, Trails, and Shots. SteamID is used in all event structs.
    Players []UnifiedPlayer `json:"players"`

    // Round structure
    Rounds []UnifiedRound `json:"rounds"`

    // Flat event streams — round number embedded in each entry.
    // go-cs-metrics reads all of these; cs-demo-viewer reads only Kills.
    Kills       []UnifiedKill       `json:"kills"`
    Damages     []UnifiedDamage     `json:"damages"`
    Flashes     []UnifiedFlash      `json:"flashes"`
    FirstSights []UnifiedFirstSight `json:"first_sights"`
    WeaponFires []UnifiedWeaponFire `json:"weapon_fires"`

    // Viewer-only streams — round number embedded in each entry.
    // go-cs-metrics ignores these entirely.
    Frames   []UnifiedFrame        `json:"frames"`
    Bombs    []UnifiedBombAction   `json:"bombs"`
    Grenades []UnifiedGrenade      `json:"grenades"`
    Trails   []UnifiedGrenadeTrail `json:"trails"`
    Shots    []UnifiedShot         `json:"shots"` // deduplicated weapon-fire for muzzle flash
}
```

### UnifiedPlayer

```go
// UnifiedPlayer is the static identity of a match participant.
// Position in Match.Players defines the player idx used in viewer structs.
type UnifiedPlayer struct {
    SteamID uint64 `json:"id,string"` // string encoding avoids JS uint64 precision loss
    Name    string `json:"name"`
}
```

### UnifiedRound

```go
type UnifiedRound struct {
    Number        int  `json:"n"`
    StartTick     int  `json:"start_tick"`
    FreezeEndTick int  `json:"freeze_end_tick"`
    EndTick       int  `json:"end_tick"`
    WinnerTeam    Team `json:"winner"` // TeamCT | TeamT | TeamUnknown

    // Running score after this round resolves (viewer scoreboard).
    CTScore int `json:"ct_score"`
    TScore  int `json:"t_score"`

    // Metrics: per-player state at round end.
    BombPlantTick     int                            `json:"bomb_plant_tick"` // 0 = no plant
    PlayerEndState    map[uint64]PlayerRoundEndState `json:"player_end_state"`
    PlayerEquipValues map[uint64]int                 `json:"player_equip_values"` // USD at freeze-end
}
```

### UnifiedKill

Used by both tools: metrics needs SteamIDs and flag fields; viewer needs positions.

```go
type UnifiedKill struct {
    Tick            int    `json:"tick"`
    Round           int    `json:"round"`
    KillerSteamID   uint64 `json:"killer,string"`
    VictimSteamID   uint64 `json:"victim,string"`
    AssisterSteamID uint64 `json:"assister,string,omitempty"` // 0 = no assist
    KillerTeam      Team   `json:"killer_team"`
    VictimTeam      Team   `json:"victim_team"`
    Weapon          string `json:"weapon"`

    IsHeadshot            bool `json:"hs,omitempty"`
    AssistedFlash         bool `json:"flash_assist,omitempty"`
    NoScope               bool `json:"no_scope,omitempty"`
    ThroughSmoke          bool `json:"through_smoke,omitempty"`
    AttackerBlind         bool `json:"attacker_blind,omitempty"`
    NearbyVictimTeammates int  `json:"nearby_teammates,omitempty"` // AWP death classifier

    // Positions — viewer heatmaps and kill markers; also available for future metrics.
    KillerX, KillerY int `json:"kx,ky"`
    VictimX, VictimY int `json:"vx,vy"`
}
```

### Metrics-only event structs

```go
// UnifiedDamage — go-cs-metrics only; viewer ignores.
type UnifiedDamage struct {
    Tick            int    `json:"tick"`
    Round           int    `json:"round"`
    AttackerSteamID uint64 `json:"attacker,string"`
    VictimSteamID   uint64 `json:"victim,string"`
    AttackerTeam    Team   `json:"attacker_team"`
    HealthDamage    int    `json:"hp_dmg"`
    Weapon          string `json:"weapon"`
    IsUtility       bool   `json:"utility,omitempty"`
    HitGroup        string `json:"hit_group"`
    VictimPos       Vec3   `json:"victim_pos"`
}

// UnifiedFlash — go-cs-metrics only (flash quality, KAST).
type UnifiedFlash struct {
    Tick            int           `json:"tick"`
    Round           int           `json:"round"`
    AttackerSteamID uint64        `json:"attacker,string"`
    VictimSteamID   uint64        `json:"victim,string"`
    AttackerTeam    Team          `json:"attacker_team"`
    VictimTeam      Team          `json:"victim_team"`
    FlashDuration   time.Duration `json:"duration_ns"`
}

// UnifiedFirstSight — go-cs-metrics only (crosshair placement).
type UnifiedFirstSight struct {
    Tick             int     `json:"tick"`
    Round            int     `json:"round"`
    ObserverID       uint64  `json:"observer,string"`
    EnemyID          uint64  `json:"enemy,string"`
    AngleDeg         float64 `json:"angle_deg"`
    PitchDeg         float64 `json:"pitch_deg"`
    YawDeg           float64 `json:"yaw_deg"`
    ObserverPitchDeg float64 `json:"obs_pitch_deg"`
    ObserverYawDeg   float64 `json:"obs_yaw_deg"`
}

// UnifiedWeaponFire — go-cs-metrics only (counter-strafe %, duel engine, TTK).
type UnifiedWeaponFire struct {
    Tick            int     `json:"tick"`
    Round           int     `json:"round"`
    ShooterID       uint64  `json:"shooter,string"`
    Weapon          string  `json:"weapon"`
    PitchDeg        float64 `json:"pitch_deg"`
    YawDeg          float64 `json:"yaw_deg"`
    AttackerPos     Vec3    `json:"pos"`
    HorizontalSpeed float64 `json:"h_speed"`
}
```

### Viewer-only structs

All player references use `Idx int` — an index into `Match.Players`. This keeps
the dominant data volume (frames) compact and avoids JS uint64 precision loss.

```go
// UnifiedFrame is one 16-tick position keyframe; sampled after freeze-end only.
// JS viewer interpolates between keyframes to produce smooth 60 fps playback.
type UnifiedFrame struct {
    Tick   int                  `json:"tick"`
    Round  int                  `json:"round"`
    States []UnifiedPlayerState `json:"p"`
}

// UnifiedPlayerState encodes one player's state at a sampled tick as a compact
// JSON array: [idx, flags, hp, x, y, z, yaw, weapon, utility, money]
// flags bits: 0=dead, 1=T(vs CT), 2=bomb carrier, 3=has kevlar, 4=has helmet
// utility bits: 0=smoke, 1=HE, 2-3=flash count (0-2), 4=molotov, 5=decoy
type UnifiedPlayerState struct {
    Idx     int    // index into Match.Players
    Flags   int
    HP      int
    X, Y, Z int
    Yaw     int
    Weapon  string
    Utility int
    Money   int
}

// MarshalJSON serialises as a compact array to minimise frame data size.
func (ps UnifiedPlayerState) MarshalJSON() ([]byte, error) {
    return json.Marshal([]any{ps.Idx, ps.Flags, ps.HP, ps.X, ps.Y, ps.Z,
        ps.Yaw, ps.Weapon, ps.Utility, ps.Money})
}

// UnifiedBombAction — viewer bomb overlay.
// action: 0=plant_begin 1=planted 2=defuse_begin 3=defused 4=exploded 5=dropped 6=pickup
type UnifiedBombAction struct {
    Tick   int    `json:"tick"`
    Round  int    `json:"round"`
    Action int    `json:"action"`
    X, Y   int    `json:"x,y"`
    Site   string `json:"site,omitempty"`
}

// UnifiedGrenade — viewer grenade overlay.
// type: 0=smoke 1=flash 2=HE 3=molotov 4=CT-smoke 5=T-smoke; EndTick=0 means instant.
type UnifiedGrenade struct {
    StartTick  int `json:"t0"`
    EndTick    int `json:"t1"`
    Round      int `json:"round"`
    Type       int `json:"type"`
    X, Y       int `json:"x,y"`
    ThrowerIdx int `json:"thrower"` // index into Match.Players; -1 if unknown
}

// UnifiedGrenadeTrail — viewer throw arc.
type UnifiedGrenadeTrail struct {
    StartTick  int      `json:"t0"`
    EndTick    int      `json:"t1"`
    Round      int      `json:"round"`
    Type       int      `json:"type"`
    ThrowerIdx int      `json:"thrower"` // index into Match.Players; -1 if unknown
    Points     [][3]int `json:"pts"`     // [tickOffset, x, y]; max 80 points
}

// UnifiedShot — deduplicated weapon-fire event for viewer muzzle-flash rendering.
// One entry per player per SampleTicks (16-tick) window — same dedup as the
// current cs-demo-viewer parser. Pre-computed at conversion time so the viewer
// reader needs no dedup logic.
type UnifiedShot struct {
    Tick  int `json:"tick"`
    Round int `json:"round"`
    Idx   int `json:"i"` // index into Match.Players
}
```

---

## Player Identity Convention

| Struct | Player reference | Rationale |
|---|---|---|
| `UnifiedPlayerState` | `Idx int` | High-volume; viewer-only; JS-safe |
| `UnifiedGrenade` | `ThrowerIdx int` | Viewer-only; consistent with frames |
| `UnifiedGrenadeTrail` | `ThrowerIdx int` | Viewer-only; consistent with frames |
| `UnifiedShot` | `Idx int` | Viewer-only; consistent with frames |
| `UnifiedKill` | `KillerSteamID / VictimSteamID uint64` | Dual-use; metrics aggregator needs SteamIDs |
| `UnifiedDamage` | `AttackerSteamID / VictimSteamID uint64` | Metrics only |
| `UnifiedFlash` | `AttackerSteamID / VictimSteamID uint64` | Metrics only |
| `UnifiedFirstSight` | `ObserverID / EnemyID uint64` | Metrics only |
| `UnifiedWeaponFire` | `ShooterID uint64` | Metrics only |

The viewer builds a `steamID → idx` map from `Match.Players` once at load, then uses
it only to resolve kill/assist SteamIDs for the scoreboard. All positional data is
already index-based.

---

## Converter Tool

`go-cs-metrics convert` performs the single demoinfocs parse pass that produces the `.csdem.gz` file.

It does **not** insert anything into the metrics DB — that is `replay`'s job.

```
go-cs-metrics convert --dir <event_dir> --tier <tier> [--out-dir <output_dir>] [--workers N] [--force]
```

- Globs `*.dem` in `--dir` (non-recursive, same as `parse`).
- For each file: parse → build `UnifiedMatch` → write `.csdem.gz` to `--out-dir` (default: same as `--dir`).
- `--out-dir` allows separating source `.dem` files from the converted `.csdem.gz` output.
- Skips files where `.csdem.gz` already exists in the output directory (unless `--force`).
- Same `GOMEMLIMIT` and `--workers 1` recommendations as `parse` (same demoinfocs pressure).
- `--tier` required (baked into the file wrapper).

After conversion the `.dem` files may be deleted at the user's discretion.

### CLI compatibility: `parse` and `info` accept `.csdem.gz`

Both commands have been extended to handle `.csdem.gz` files in addition to `.dem`:

- `parse match.csdem.gz` — loads the unified file and runs the aggregator; no demoinfocs needed.
- `parse --dir <dir>` — globs both `*.dem` and `*.csdem.gz`.
- `info match.csdem.gz` — reads the embedded `DemoHash` and metadata directly from the file
  instead of the 64 KB quick-hash approach used for `.dem` files.

---

## Updated Workflow

```sh
# 1–3. Download and fix mtimes (unchanged)
demoget sync --out ~/demos/pro
demoget touch-dates --out ~/demos/pro

# 4a. Convert .dem → .csdem.gz (new step; run once per event)
for dir in ~/demos/pro/*/; do
  GOMEMLIMIT=4294967296 go-cs-metrics convert --dir "$dir" --tier pro --workers 1
done

# 4b. Delete .dem files to reclaim disk (optional, user decision)
rm ~/demos/pro/*/*.dem

# 5. Ingest into metrics DB (from .csdem.gz; fast, low RAM, can use more workers)
for dir in ~/demos/pro/*/; do
  go-cs-metrics replay --dir "$dir"
done

# 6–7. Export and simulate (unchanged)
go-cs-metrics export --roster navi-roster.json --since 90 --quorum 3 --out navi.json
simbo3 run --teamA navi.json --teamB faze.json --format bo3

# 2D review (no .dem needed)
demoview liquid-vs-spirit-m1-nuke.csdem.gz
```

---

## Implementation Status

### `go-cs-metrics` — complete

| File | Status |
|---|---|
| `internal/model/unified.go` | Done — all `Unified*` structs + `UnifiedMatchFile` |
| `internal/model/unified_io.go` | Done — `Save(path) error` + `LoadUnifiedMatchFile(path) (*UnifiedMatchFile, error)` |
| `internal/converter/converter.go` | Done — single demoinfocs pass producing `*UnifiedMatchFile` |
| `cmd/convert.go` | Done — `convert` command (`--dir`, `--out-dir`, `--tier`, `--workers`, `--force`) |
| `cmd/replay.go` | Done — `replay` command; reads `.csdem.gz` → aggregator → DB |
| `cmd/parse.go` | Done — now accepts `.csdem.gz` alongside `.dem` (both single-file and `--dir` bulk) |
| `cmd/info.go` | Done — reads embedded hash/metadata from `.csdem.gz` directly |
| `cmd/root.go` | Done — `convert` and `replay` registered |
| `docs/cs2-pipeline-flow.md` | Done — documents new format, `convert`, `replay` |

### `cs-demo-viewer` — complete

| File | Status |
|---|---|
| `internal/demo/unified_reader.go` | Done — `LoadUnified(path) (*DemoData, error)` |
| `cmd/demoview/main.go` | Done — routes `.csdem.gz` to `LoadUnified`; `-dir` globs both formats |

---

## Size Results (Measured)

### Per-component estimates

| Component | Uncompressed | Compressed (gzip) |
|---|---|---|
| Metrics events (kills, damages, flashes, weapon fires, first sights) | ~300–600 KB | ~80–150 KB |
| Position frames (16-tick, 10 players, ~30 rounds) | ~5–10 MB | ~400–800 KB |
| Viewer events (bombs, grenades, trails, shots) | ~100–200 KB | ~30–60 KB |
| **Total per demo** | **~6–11 MB** | **~500 KB – 1 MB** |
| **vs original .dem** | 200–750 MB | — |
| **Reduction** | — | **~580–740×** |

### Per-event results (measured on 18 pro events, Mar 2026)

| Event | DEMs | Source (.dem) | Output (.csdem.gz) | Ratio |
|---|---|---|---|---|
| blast_bounty_2026_s1 | 60 | 28.3 GB | 47 MB | 620× |
| blast_bounty_2026_s1_finals | 18 | 8.7 GB | 15 MB | 608× |
| blast_rivals_2025_s2 | 34 | 17.7 GB | 30 MB | 600× |
| blasttv-austin-major-2025-stage-1 | 52 | 24.7 GB | 39 MB | 645× |
| esl_pro_league_s22 | 111 | 52.2 GB | 78 MB | 688× |
| esl_pro_league_s23_finals | 3 | 1.5 GB | 3 MB | 583× |
| fl0m_mythical_lan_las_vegas_2026 | 30 | 11.6 GB | 20 MB | 583× |
| fragadelphia_miami_2026 | 67 | 25.7 GB | 48 MB | 545× |
| fragadelphia_ultra_mega_jersey_2025 | 64 | 21.9 GB | 39 MB | 571× |
| iem_cologne_2025 | 84 | 38.6 GB | 58 MB | 683× |
| iem_katowice_2025 | 90 | 41.4 GB | ~62 MB* | ~680×* |
| iem_krakow_2026 | 101 | 47.9 GB | ~73 MB* | ~670×* |
| iem_krakow_2026_stage1 | 54 | 23.0 GB | 36 MB | 658× |
| pgl_cluj_napoca_2026 | 103 | 53.7 GB | ~82 MB* | ~663×* |
| pgl_masters_bucharest_2025 | 103 | 51.3 GB | 81 MB | 649× |
| starladder_budapest_major_2025 | 65 | 31.4 GB | 50 MB | 627× |
| starladder_budapest_major_2025_stage2 | 51 | 24.4 GB | 40 MB | 633× |
| starladder_starseries_fall_2025 | 33 | 16.7 GB | 23 MB | 702× |
| **Total** | **1,133** | **~520 GB** | **~823 MB** | **~638×** |

\* Extrapolated from partially converted demos at time of measurement (sequential OOM-safe conversion still in progress for these 4 events; requires `GOMEMLIMIT=4294967296 --workers 1`).

**Note:** 3 demos across these events failed with `ErrUnexpectedEndOfDemo` (corrupt HLTV archives) and produced no `.csdem.gz`. These are permanent gaps in the source data.

---

## Verification

```sh
cd ~/git/cs/go-cs-metrics

# Build
go build ./...

# Convert one event
GOMEMLIMIT=4294967296 ./go-cs-metrics convert --dir ~/demos/pro/iem_katowice_2025/ --tier pro
ls -lh ~/demos/pro/iem_katowice_2025/*.csdem.gz   # expect ~500 KB – 1 MB each

# Inspect (valid JSON)
gunzip -c ~/demos/pro/iem_katowice_2025/<some>.csdem.gz | python3 -m json.tool | head -60

# Replay into fresh DB
./go-cs-metrics drop --force
./go-cs-metrics replay --dir ~/demos/pro/iem_katowice_2025/
./go-cs-metrics list   # confirm demos appear

# Spot-check stats match original parse
./go-cs-metrics player <steam_id> --since 90

# 2D viewer
cd ~/git/cs/cs-demo-viewer
./demoview ~/demos/pro/iem_katowice_2025/<some>.csdem.gz

# Run tests
cd ~/git/cs/go-cs-metrics && go test ./...
cd ~/git/cs/cs-demo-viewer && go test ./...
```
