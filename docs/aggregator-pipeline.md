# Aggregator Pipeline

The aggregator transforms a parsed `RawMatch` into four output slices:

- `[]PlayerMatchStats` — one row per player per demo
- `[]PlayerRoundStats` — one row per player per round (drill-down source)
- `[]PlayerWeaponStats` — one row per player per weapon
- `[]PlayerDuelSegment` — one row per (player, weapon bucket, distance bin)

The pipeline runs 10 sequential passes over the raw event data. Each pass reads from the raw events and/or the output of earlier passes. No pass modifies raw input.

---

## Pass 1 — Trade annotation

**Input:** `raw.Kills`
**Output:** `killsByRound` — kills grouped by round, each annotated with `isTradeKill` and `isTradeDeath`

Kills are grouped by round and sorted ascending by tick. Then for each kill K:

- **TradeKill** — scan backward within a 5-second window. If a previous kill had its killer equal to K's victim (i.e. K's victim previously killed someone), and that someone was a teammate of K's killer, then K is a trade kill. K avenged a prior loss.
- **TradeDeath** — scan forward within a 5-second window. If a subsequent kill targets K's killer, and is made by the opposing team, then K's death was itself traded. K killed someone but was then traded back.

The 5-second window is calculated in ticks using the demo's actual tickrate (`raw.TicksPerSecond`).

---

## Pass 2 — Opening kill / death

**Input:** `raw.Rounds`, `killsByRound` from Pass 1
**Output:** `openingByRound` — the killer and victim of the first post-freeze kill per round

For each round, the kills list (already sorted by tick) is scanned forward. The first kill whose tick is at or after the round's `FreezeEndTick` is the opening kill. The killer gets an opening kill credit; the victim gets an opening death credit.

---

## Pass 3 — Per-round per-player stats

**Input:** `raw.Damages`, `raw.Kills`, `raw.Rounds`, `raw.Flashes`, annotations from Passes 1–2
**Output:** `allRoundStats []PlayerRoundStats`, `matchAccums` (intermediate)

This is the heaviest pass. For every round, every player who appeared in that round (via `PlayerEndState` or kill events) gets a `PlayerRoundStats` row.

### What is computed per player per round:

| Field | Source |
|---|---|
| `Kills`, `GotKill` | Count of kills where `killerID == playerID` |
| `Assists`, `GotAssist` | Count of kills where `assisterID == playerID` |
| `IsTradeKill`, `IsTradeDeath` | From Pass 1 annotations on that player's kills |
| `WasTraded` | Player was a victim whose kill was later traded (from `isTradeDeath` on the kill event targeting this player) |
| `Survived` | From `round.PlayerEndState[playerID].IsAlive` |
| `IsOpeningKill`, `IsOpeningDeath` | From Pass 2 `openingByRound` |
| `Damage` | Sum of `HealthDamage` dealt by player in this round across all `RawDamage` events |
| `UnusedUtility` | Grenade count remaining from `PlayerEndState` |
| `KASTEarned` | True if any of: GotKill, GotAssist, Survived, WasTraded |
| `BuyType` | Derived from `round.PlayerEquipValues[playerID]` (equipment value at freeze-end): ≥$4500 = full, ≥$2000 = force, ≥$1000 = half, <$1000 = eco |
| `IsPostPlant` | True when `round.BombPlantTick > 0` — the bomb was planted at some point in this round (captured by the parser's `BombPlanted` event handler) |
| `IsInClutch`, `ClutchEnemyCount` | From `computeClutch` — see below |

### Clutch detection (`computeClutch`)

Before the per-player inner loop, `computeClutch` is called once per round:

1. All players in the round are initially marked alive.
2. Kills are processed in tick order. After each kill, the victim is marked dead.
3. After each death, every still-alive player is checked: if `myTeamAlive == 1 && enemyAlive >= 1`, that player is in a clutch. The maximum `enemyAlive` count seen during the clutch is stored as `ClutchEnemyCount`.
4. Returns a map of `playerID → {isClutch, enemyCount}` used to populate the round stats.

Match-level accumulators (`matchAccums`) are updated incrementally per round — kills, assists, deaths, damage, KAST rounds, opening kills/deaths, trade kills/deaths, unused utility.

Weapon-level maps (`weaponKills`, `weaponHS`, `weaponDeaths`, `weaponDamage`, `weaponHits`) are also built here by iterating all damage and kill events.

---

## Pass 4 — Match-level rollup

**Input:** `matchAccums` from Pass 3, `raw.PlayerNames`, `playerDominantTeam`
**Output:** `matchStats []PlayerMatchStats` (sorted by kills descending)

One `PlayerMatchStats` struct is created per player by reading from their accumulator. Fields populated: `Kills`, `Assists`, `Deaths`, `HeadshotKills`, `FlashAssists`, `TotalDamage`, `UtilityDamage`, `RoundsPlayed`, `OpeningKills`, `OpeningDeaths`, `TradeKills`, `TradeDeaths`, `KASTRounds`, `UnusedUtility`.

The `weaponStats []PlayerWeaponStats` output slice is also assembled here from the weapon-level maps.

---

## Pass 5 — Crosshair placement

**Input:** `raw.FirstSights`
**Output:** Updates `matchStats[i].CrosshairMedianDeg`, `CrosshairPctUnder5`, `CrosshairMedianPitchDeg`, `CrosshairMedianYawDeg`, `CrosshairEncounters`

`RawFirstSight` events are emitted by the parser when a player's spotting mask (`m_bSpottedByMask`) changes — i.e. the moment an enemy first enters a player's line of sight. The angle captured is the deviation between the observer's current aim direction and the direction to the enemy at that instant.

For each player, all their first-sight angles are collected. The median of all angles gives `CrosshairMedianDeg` — a lower value means the player's crosshair was closer to the enemy's position when they spotted them (better pre-aim). `CrosshairPctUnder5` is the fraction of encounters where the deviation was under 5°.

---

## Pass 6 — Duel engine + FHHS segments

**Input:** `raw.FirstSights`, `raw.Damages`, `raw.Kills`, `raw.WeaponFires`
**Output:** Updates `matchStats` with duel stats; produces `duelSegments []PlayerDuelSegment`

This is the most complex pass. Three indexes are built upfront:

- **`firstSightIdx`** — `(observerID, enemyID, roundN)` → first `RawFirstSight` for that pair
- **`duelDmgIdx`** — `(roundN, attackerID, victimID)` → sorted slice of non-utility `RawDamage`
- **`wfIdx`** — `(shooterID, roundN)` → sorted slice of `RawWeaponFire`

For each kill, two sides are processed:

### Win side (killer)
If a first-sight record exists for `(killerID → victimID)` at or before the kill tick:

- **`winMs`** = `(killTick - firstSightTick) / tps * 1000` — exposure time from spotting to kill
- **Hits to kill** — count of `duelDmgIdx` entries within `[sightTick, killTick]`
- **First-hit headshot** — whether the first damage in that window targeted the head
- **Pre-shot correction** — angular delta between the aim direction at first-sight and the aim direction at the first weapon fire in the window; captures how much the player adjusted before pulling the trigger
- **Distance** — 3D distance between attacker position (from first weapon fire) and victim position (from first damage), converted from Hammer units to metres
- **Segment** — the `(playerID, weaponBucket, distanceBin)` key that receives this duel's data for FHHS output

### Loss side (victim)
If a first-sight record exists for `(victimID → killerID)`, loss exposure time is recorded. If the victim never spotted the killer, 0ms is recorded (surprise kill).

### FHHS output
Each segment accumulates: duel count, first-hit count, first-hit HS count, correction degrees, sight angles, exposure win times. At the end of the pass these are converted to `PlayerDuelSegment` rows. The FHHS rate is `firstHitHSCount / firstHitCount` and is reported with a Wilson 95% confidence interval to handle small sample sizes.

---

## Pass 7 — AWP death classifier

**Input:** `raw.Kills` (AWP only), `raw.Flashes`, `killsByRound` from Pass 1
**Output:** Updates `matchStats[i].AWPDeaths`, `AWPDeathsDry`, `AWPDeathsRePeek`, `AWPDeathsIsolated`

For every kill made with an AWP, the victim receives one of more of these flags:

| Flag | Condition |
|---|---|
| `AWPDeaths` | Always — counts total AWP deaths |
| `AWPDeathsDry` | No flash hit the victim in the 3 seconds before the kill |
| `AWPDeathsRePeek` | The victim had a kill earlier in the same round (they peeked again after already winning a fight) |
| `AWPDeathsIsolated` | Zero teammates within 512 Hammer units (~10m) of the victim at kill time |

These flags are not mutually exclusive — a death can be dry AND isolated AND a re-peek.

---

## Pass 8 — Flash quality window

**Input:** `raw.Flashes`, `killsByRound` from Pass 1
**Output:** Updates `matchStats[i].EffectiveFlashes`

For each non-team flash with positive duration, a 1.5-second window is opened from the flash tick. If any kill occurs within that window where:
- the victim is the flashed player, and
- the killer is on the same team as the flasher,

then the flash is counted as effective. The flasher's `EffectiveFlashes` counter is incremented.

---

## Pass 9 — Role classification

**Input:** `matchStats`, `weaponKills` map from Pass 3
**Output:** Updates `matchStats[i].Role`

A single heuristic label is assigned per player per match using a priority-ordered switch:

| Role | Condition |
|---|---|
| `AWPer` | AWP kills > 30% of total kills |
| `Entry` | Opening kills > 12% of rounds played |
| `Support` | Flash assists > 8% of rounds played, OR utility damage > 15 per round |
| `Rifler` | Default — none of the above |

The order matters: AWPer is checked first. A player who both AWPs and entries frequently is classified as AWPer.

---

## Pass 10 — TTK, TTD, and one-tap kills

**Input:** `raw.Kills`, `wfIdx` from Pass 6
**Output:** Updates `matchStats[i].MedianTTKMs`, `MedianTTDMs`, `OneTapKills`

For each kill, the weapon-fire index (`wfIdx`) is used to find the first shot the killer fired within a **3-second rolling window** before the kill tick. This approach includes missed shots (unlike a damage-based start), making TTK comparable to external tools like Refrag.

```
windowStart = killTick - (3.0 * tps)
firstFiredTick = first wfIdx[{killerID, roundN}] entry in [windowStart, killTick]
TTK = (killTick - firstFiredTick) / tps * 1000
```

### One-tap detection
If `firstFiredTick == killTick`, the killing shot was the first shot in the window — a one-tap. These are counted in `OneTapKills` and **excluded** from the TTK/TTD median samples, since 0ms has no meaning in a multi-hit context.

### TTD
The same duration is recorded on the victim's `ttdSamples`. TTD answers: after the enemy started shooting at you (within 3s), how long did you survive?

### Medians
All non-one-tap TTK samples per player are sorted and the median is taken. Same for TTD.

### Limitation
`wfIdx` is keyed by `(shooterID, roundN)` — not by target. If a player fires at multiple enemies within the same 3-second window, the earliest shot in the window is used regardless of intended target. In practice this is a minor source of noise since most engagements resolve quickly.
