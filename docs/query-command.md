# `query` — Round Pattern Search

The `query` command scans `.csdem.gz` files and returns every round that satisfies a [CEL](https://cel.dev) expression. It reads the converted demo files directly — no database required.

```sh
go-cs-metrics query --dir <dir> '<expression>' [--csv] [--html <file>]
```

---

## How it works

Every round in every `.csdem.gz` file under `--dir` is evaluated against the expression. A round is included in the output if the expression evaluates to `true`.

The expression is written in **CEL** (Common Expression Language) — a simple, readable expression language used in Firebase, Kubernetes, and Google Cloud. You don't need to know SQL or the internal data model. The variables are named to match the concepts directly.

```sh
# Find all rounds where CTs retook the site with 2 or fewer players alive
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT" && alive_plant_ct <= 2'
```

**Output modes:**
- Default: aligned text table — identity columns + key stats, good for terminal browsing
- `--csv`: full record with all variables, pipe to a spreadsheet or script
- `--html <file>`: self-contained interactive 2D radar replay — one clip per matching round, playable in any browser

**HTML viewer flags:**
- `--html <file>`: output path for the HTML file
- `--limit N`: max clips to embed (default 200; rounds beyond the limit appear in the table but not the viewer)
- `--radar-dir <dir>`: directory containing map radar PNGs (default: `~/git/cs/cs-demo-viewer/internal/maps/overviews`)

---

## Variables reference

All variables are round-scoped. There is no player-level granularity — each variable represents a count or state for an entire round (or for one side within a round).

### Identity

| Variable | Type | Description |
|---|---|---|
| `map` | string | Map name, e.g. `"de_inferno"` |
| `round_num` | int | Round number (1-based) |
| `winner` | string | `"T"` or `"CT"` |
| `entry_side` | string | Which side got the first kill: `"T"`, `"CT"`, or `""` if no kills |

### Buy types

Buy type is derived from the total equipment value (USD) for all 5 players on a side at freeze-end.

| Variable | Type | Values |
|---|---|---|
| `type_t` | string | `"pistol"` · `"eco"` · `"force"` · `"full"` |
| `type_ct` | string | `"pistol"` · `"eco"` · `"force"` · `"full"` |
| `equip_t` | int | Raw USD sum for T side |
| `equip_ct` | int | Raw USD sum for CT side |

**Thresholds** (team total, 5 players):

| Label | Total equip |
|---|---|
| `"pistol"` | ≤ $4 000 |
| `"eco"` | ≤ $9 000 |
| `"force"` | ≤ $20 000 |
| `"full"` | > $20 000 |

### Utility thrown

Count of grenades of each type thrown by each side during the round.

| Variable | Type | Description |
|---|---|---|
| `smokes_t` / `smokes_ct` | int | Smoke grenades |
| `flashes_t` / `flashes_ct` | int | Flashbangs |
| `molotovs_t` / `molotovs_ct` | int | Molotovs / incendiaries |
| `hes_t` / `hes_ct` | int | HE grenades |

### HE grenade damage

Total HP damage dealt to opponents via HE grenades.

| Variable | Type | Description |
|---|---|---|
| `he_damage_t` | int | HP damage dealt by T-side HEs |
| `he_damage_ct` | int | HP damage dealt by CT-side HEs |

### Flash-assisted kills

Kills where the victim was flinded (flash duration > 0) at the moment of death, grouped by the killer's side.

| Variable | Type | Description |
|---|---|---|
| `flash_kills_t` | int | Kills by T while victim was blind |
| `flash_kills_ct` | int | Kills by CT while victim was blind |

### Alive player counts

| Variable | Type | Description |
|---|---|---|
| `alive_start_t` / `alive_start_ct` | int | Players alive at round start (after freeze-end) |
| `alive_plant_t` / `alive_plant_ct` | int | Players alive at the bomb plant tick; `0` if no plant |

### Bomb events

| Variable | Type | Description |
|---|---|---|
| `planted` | bool | Bomb was planted this round |
| `defused` | bool | Bomb was defused |
| `exploded` | bool | Bomb exploded |

---

## CEL expression syntax

CEL is close to what you'd write in most programming languages. The main things to know:

```
==   equal                "CT" == winner
!=   not equal            map != "de_dust2"
<  <=  >  >=              alive_plant_ct <= 2
&&   and                  planted && winner == "CT"
||   or                   type_t == "force" || type_t == "eco"
!    not                  !planted
+    arithmetic           smokes_t + molotovs_t >= 3
```

**String comparisons are case-sensitive.** Use `"T"` not `"t"`, `"CT"` not `"ct"`, `"de_inferno"` not `"De_Inferno"`.

---

## Use cases

### 1. T-side site executes

A coordinated execute is a round where T side commits heavy utility onto a site and plants the bomb. The key signal is the combination of smokes (to cut off CT rotations), flashes (to blind defenders), and molotovs (to clear corners).

```sh
# Broad: any round with significant T utility + plant
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && smokes_t + molotovs_t + flashes_t >= 4'

# Stricter: must have smokes AND molotovs (committed execute)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && smokes_t >= 2 && molotovs_t >= 1 && flashes_t >= 1'

# With outcome — find executes that won the round
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "T" && smokes_t >= 2 && molotovs_t >= 1'

# Specific map
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'map == "de_inferno" && planted && smokes_t >= 2 && molotovs_t >= 2'
```

**Reading the output:** Look at `SMK_T`, `FLA_T`, `MOL_T` columns. High flash counts alongside smokes suggest the execute had an active entry with pop-flashes. Cross-reference `ENTRY` — if it's `T`, Ts got the first kill even before or during the execute.

---

### 2. Flash-assisted kills

A flash-assisted kill means a teammate flashed an opponent, and the kill happened while that opponent was blind. This captures:
- T entries where a lurker or second-in flashes for the entry fragger
- CT pop-flash aggression on a choke point (e.g. mid control, ramp)

```sh
# Any round with flash-assisted kills by T (T entries)
go-cs-metrics query --dir ~/demos/converted-pro/ 'flash_kills_t >= 1'

# CT aggression: flash kills before the bomb is planted
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'flash_kills_ct >= 1 && !planted'

# Both sides using flash kills in the same round (high coordination)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'flash_kills_t >= 1 && flash_kills_ct >= 1'

# Multiple flash kills by CT — coordinated double or triple blind
go-cs-metrics query --dir ~/demos/converted-pro/ 'flash_kills_ct >= 2'

# Map-specific: Inferno CT pop-flashes on banana/A
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'map == "de_inferno" && flash_kills_ct >= 1 && !planted'
```

**Reading the output:** `FK_T` and `FK_CT` show the count per side. `FLA_T` / `FLA_CT` show total flashes thrown — a high flash count with flash kills suggests systematic use rather than lucky blind shots.

---

### 3. CT retakes

A retake is any round where the bomb was planted and CTs won. Filtering by `alive_plant_ct` captures the difficulty of the retake — the fewer CTs alive at plant, the more impressive the retake.

```sh
# All CT retakes
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT"'

# Low-man retakes: 2 or fewer CTs alive when bomb was planted
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT" && alive_plant_ct <= 2'

# 1v1 clutch retakes
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT" && alive_plant_ct == 1'

# Retakes from disadvantage: more Ts alive than CTs at plant
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT" && alive_plant_ct < alive_plant_t'

# Retakes after a trade at the plant: exactly 1 T and 1 CT alive
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT" && alive_plant_t == 1 && alive_plant_ct == 1'

# Retake with a defuse (not a time win or wipe)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && defused'
```

**Reading the output:** `T_PLANT` and `CT_PLANT` (in `--csv` these are `alive_plant_t` / `alive_plant_ct`) tell the story — a `CT_PLANT` of 1 against a `T_PLANT` of 3 is a 1v3 clutch retake. The `ENTRY` column shows whether CTs immediately traded back at the plant or went in cold.

---

### 4. CT HE coordinated damage

On maps like Inferno, CTs frequently use coordinated HE throws to finish or soften Ts pushing through a choke point (B banana, A ramp). A single HE rarely kills, but a simultaneous volley of 2-3 HEs on a stacked push can take out multiple players.

```sh
# Basic CT HE aggression: multiple HEs with meaningful damage
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'hes_ct >= 2 && he_damage_ct >= 80'

# High-damage CT HE volley (likely killing or near-killing players)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'hes_ct >= 3 && he_damage_ct >= 150'

# Inferno-specific: CT HE spam on B banana or A ramp
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'map == "de_inferno" && hes_ct >= 2 && he_damage_ct >= 100'

# T-side HE usage (e.g. clearing CT stacks or aggressive pushes)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'hes_t >= 2 && he_damage_t >= 80'

# Both sides using HEs heavily (grenade-heavy round)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'hes_ct + hes_t >= 5 && he_damage_ct + he_damage_t >= 200'

# Export to CSV for analysis across maps
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'hes_ct >= 2 && he_damage_ct >= 80' --csv > ct_he_aggression.csv
```

**Reading the output:** `HE_CT` is the grenade count, `HED_CT` is total HP damage. Damage ≥ 100 from ≥ 2 grenades means someone took more than one HE — either the timing was tight or they were caught in the open. Cross-reference with `PLANT` — CT HE aggression that also prevented the plant is the most impactful case.

---

### 5. Force buy wins

A force buy is when a team spends on a round they can't fully commit to — below a full rifle+util setup. Winning a force buy swings both the economy and the momentum of the match.

```sh
# T-side force wins
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'type_t == "force" && winner == "T"'

# CT-side force wins
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'type_ct == "force" && winner == "CT"'

# Either side forced and won
go-cs-metrics query --dir ~/demos/converted-pro/ \
  '(type_t == "force" && winner == "T") || (type_ct == "force" && winner == "CT")'

# Eco wins (even harder — pistols or cheap rifles against full buys)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  '(type_t == "eco" && winner == "T") || (type_ct == "eco" && winner == "CT")'

# Force wins that also had T-side utility use (setup force, not just rushes)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'type_t == "force" && winner == "T" && smokes_t >= 1'

# Force win AND flash kills — aggressive coordination despite limited budget
go-cs-metrics query --dir ~/demos/converted-pro/ \
  '(type_t == "force" && winner == "T" && flash_kills_t >= 1) || (type_ct == "force" && winner == "CT" && flash_kills_ct >= 1)'
```

**Reading the output:** `TYPE_T` and `TY_CT` show the buy classification. `EQUIP_T` and `equip_ct` (visible in `--csv`) give the raw USD total — you can see exactly how poor the force was. Look for patterns in `ENTRY`: a forced side that wins the entry duel often snowballs the round from there.

---

## Combining conditions

CEL operators compose freely. You can mix any variables:

```sh
# Coordinated T execute that CTs still retook (CT won despite the execute)
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT" && smokes_t >= 2 && molotovs_t >= 1'

# Flash-assisted T entry that led to a T round win with plant
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'flash_kills_t >= 1 && planted && winner == "T"'

# Chaotic rounds: both sides used flash kills, bomb was planted, CT won
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'flash_kills_t >= 1 && flash_kills_ct >= 1 && planted && winner == "CT"'

# Nuke-specific: any T-side utility play on a specific map
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'map == "de_nuke" && smokes_t >= 3 && planted'
```

---

## Interactive 2D viewer

Add `--html <file>` to generate a self-contained interactive replay viewer:

```sh
# CT retakes on Inferno → interactive HTML replay
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'map == "de_inferno" && planted && winner == "CT"' \
  --html inferno_retakes.html

# Execute highlights → HTML with limit
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && smokes_t >= 2 && molotovs_t >= 1 && flashes_t >= 1 && winner == "T"' \
  --html t_executes.html --limit 100
```

Open the output file in any modern browser. For each matching round the viewer shows:
- **Sidebar**: match name, date, map, round number, buy types, util counts, winner badge, planted/defused indicators, entry side
- **Radar**: 2D overhead view with live player positions, kill events, grenade trails, HP panels
- **Controls**: play/pause, timeline scrubber, 0.5×/1×/2×/4× speed
- **Kill feed**: kills shown as they happen, with weapon and headshot indicator

The `--radar-dir` flag points to the directory containing `<mapname>.png` files. The default path is `~/git/cs/cs-demo-viewer/internal/maps/overviews`. Multi-level maps (de_nuke, de_vertigo, de_train) use `<mapname>_lower.png` when available.

The `--limit N` flag caps how many clips are embedded in the viewer (default 200). Rounds beyond the limit still appear in the text table or CSV output.

---

## CSV output and scripting

Add `--csv` to get all variables as columns. Pipe directly to tools:

```sh
# All CT retakes → CSV for spreadsheet analysis
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'planted && winner == "CT"' --csv > retakes.csv

# Count force wins per map using awk
go-cs-metrics query --dir ~/demos/converted-pro/ \
  '(type_t == "force" && winner == "T") || (type_ct == "force" && winner == "CT")' \
  --csv | awk -F, 'NR>1 {count[$2]++} END {for (m in count) print count[m], m}' | sort -rn

# Extract only the high-impact CT HE rounds on Inferno
go-cs-metrics query --dir ~/demos/converted-pro/ \
  'map == "de_inferno" && hes_ct >= 2 && he_damage_ct >= 100' \
  --csv | column -t -s,
```

---

## Performance

The command reads `.csdem.gz` files sequentially, one per match. Each file is decompressed and all rounds evaluated in memory before moving on. No database is touched.

**Typical throughput:** ~800–1 200 rounds/second on a normal event directory. A full `converted-pro/` scan (1 100+ demos, 50 000+ rounds) completes in under a minute.
