# HLTV-style Metrics — Implementation Roadmap

Plan to surface every metric from §1 (top-level summary) and §2 (role-based
decomposition) of `hltv-metrics-reference.md`. Slices are ordered easy → medium.
**Round swing is deliberately skipped** — see §99.

Each slice is a single self-contained PR. Schema changes are additive
(`ALTER TABLE ADD COLUMN ... DEFAULT`), so old demos populate the new column
with the default value and **can be backfilled via `replay --force`** against
the `.csraw2.tar` archive (no `.dem` re-parse required).

---

## Slice 1 — Pure derivation, no schema change — **SHIPPED**

Everything in this slice is one SQL query off existing rows in
`player_match_stats`, `player_round_stats`, `player_weapon_stats`,
`player_death_events`, `flash_events`, or `grenade_events`. **No new aggregator
pass, no new columns, no re-parse.**

**Status:** shipped. Invoke with `./go-cs-metrics player <steamid> --roles`.
See [README.md](../README.md) for the flag reference and
[hltv-metrics-reference.md](hltv-metrics-reference.md) for what each metric
measures.

**Known caveats:**
- Pistol-round detection is CS2-only (rounds 1 + 13). CSGO MR15 demos miss the
  round-16 pistol and incorrectly include a non-pistol round 13. Our corpus is
  pro CS2, so this is fine in practice — revisit when historical-CSGO support
  becomes a priority.
- Sniper round-with-kill / Sniper multi-kill / Sniper opening kill rely on
  `player_death_events`. Utility kills/100R rely on the same table. Flashes
  thrown rely on `grenade_events`; opponent-flash seconds on `flash_events`.
  All four event tables are sparse on legacy data (~12% of corpus at time of
  shipping). The renderer prints `—` instead of misleading zeros when the
  source has no rows for the player. Run `replay --dir <event>/ --force` over
  the `.csraw2.tar` archives to backfill.
- Cohort percentile bars (the HLTV "Good / Okay / Below" labels) are not in
  Slice 1 — moved to Slice 5.

### Files to touch
- `internal/storage/role_queries.go` (new) — one helper per role/sub-metric.
- `internal/model/model.go` — add `Rating2(side)` to `PlayerSideStats` /
  `PlayerMapSideAggregate`; add `ClutchPoints()` / `OneOnOneWinPct()` on a
  new `PlayerClutchAggregate` value type derived from `player_round_stats`.
- `cmd/player.go` — wire a new `--roles` flag (or an `hltv` subcommand) that
  prints the seven-role + sub-metric table.
- `cmd/player_test.go` — golden-file test against one known demo.

### What gets implemented

**Top-level rail (§1)**
- T Rating / CT Rating — `Rating2()` restricted to one side via
  `PlayerSideStats`. Schema already carries everything (per-side KAST counts
  are derivable from `player_round_stats.team`).
- Multi-kill % — `COUNT(*) FILTER (WHERE kills >= 2) / COUNT(*)` on
  `player_round_stats`.

**Firepower (§2.1)**
- Rounds with a kill % — `SUM(got_kill) / rounds_played`.
- Kills per round win — `SUM(kills) WHERE won_round=1 / rounds_won`.
- Damage per round win — `SUM(damage) WHERE won_round=1 / rounds_won`.
- Rounds with multi-kill % — `COUNT(*) FILTER (WHERE kills >= 2) / rounds_played`.
- Pistol round rating — restrict to first round of each half. Encode the rule
  `is_pistol = (round_number == 1 OR round_number == half_length + 1)`. We have
  no `cs_version` column, but we have `ct_score + t_score` per demo to infer
  the regulation length (24 for CS2 / 30 for CSGO). One-line CASE expression.

**Entrying (§2.2)**
- Opening-deaths-traded % — `SUM(is_opening_death AND is_trade_death) /
  SUM(is_opening_death)`.
- Support rounds % — `SUM((got_assist OR survived OR was_traded) AND NOT
  got_kill) / rounds_played`.

**Trading (§2.3)**
- Damage per kill — `total_damage / kills`.

**Opening (§2.4)**
- Opening attempts % — `(opening_kills + opening_deaths) / rounds_played`.
- Opening success % — `opening_kills / (opening_kills + opening_deaths)`.
- Win% after opening kill — `SUM(is_opening_kill AND won_round) /
  SUM(is_opening_kill)`.

**Clutching (§2.5)** — derive `PlayerClutchAggregate` on the fly from
`player_round_stats` (no separate table needed). Per (steam_id, demo_hash):
- `Attempts[i] = COUNT(is_in_clutch=1 AND clutch_enemy_count=i)`
- `Wins[i] = COUNT(is_in_clutch=1 AND clutch_enemy_count=i AND won_round=1)`
- Sub-metrics:
  - Clutch points per round — `Σ Wins[i] * 2^(i-1) / rounds_played`.
  - 1v1 win % — `Wins[1] / Attempts[1]`.
  - Saves per round loss — `SUM(survived AND NOT won_round) / SUM(NOT won_round)`.

**Sniping (§2.6)** — `player_weapon_stats` filtered to `weapon IN
('AWP', 'SSG 08')`, plus `player_death_events` for per-round-per-weapon
queries:
- Sniper kills per round, sniper kills %.
- Rounds with sniper kill % — `COUNT(DISTINCT (round_number)) FROM
  player_death_events WHERE killer_id=p AND weapon IN (...)` / rounds_played.
- Sniper multi-kill rounds — same, but with `HAVING COUNT(*) >= 2`.
- Sniper opening kills per round — filter `player_death_events` by weapon AND
  `is_opening_death` from the *paired* kill (need a small JOIN against
  `player_round_stats` on the killer side, or just key off `is_opening_kill`
  in PRS combined with weapon from PDE).

**Utility (§2.7)** — partial:
- Utility damage / round — `utility_damage / rounds_played` (already have).
- Utility kills per 100 rounds — `player_death_events` filtered to
  `weapon IN ('hegrenade', 'inferno', 'molotov')`.
- Flashes thrown / round — `COUNT(*) FROM grenade_events WHERE thrower_id=p
  AND grenade_type='flash'` / rounds_played.
- Time opponent flashed / round — `SUM(duration_s) FROM flash_events WHERE
  thrower_id=p AND is_team_flash=0` / rounds_played.

### Acceptance
- `./go-cs-metrics player <steamid> --roles` prints seven role headers with
  scores plus all sub-metrics, with CT/T/Both side toggles via a `--side` flag.
- Golden test on `meyern` matches a hand-calculated reference for 3+ metrics.
- `go test ./...` green; `go vet ./...` clean.

---

## Slice 2 — Save / Trade-assist pass — **SHIPPED**

Three §2 metrics share one new aggregator pass that walks damages alongside
kills with tight time windows:
- **Saved by teammate** (§2.2) — your teammate killed your last attacker within
  1 s of the attack.
- **Saved teammate** (§2.3) — you killed an opponent attacking a teammate
  within 1 s of the attack.
- **Assisted kill %** (§2.3) — your kill was on an opponent a teammate had
  already damaged earlier in the round (any window).

**Status:** shipped. New columns on `player_match_stats`:
`saved_by_teammate`, `saved_teammate`, `assisted_kills`. Surfaced in the
Entrying / Trading tables of `player --roles` (`SAVED_BY/RD`, `SAVED/RD`,
`ASSISTED_K%`).

**Coverage caveat:** existing rows default to 0; backfill via
`replay --dir <event>/ --force` against the `.csraw2.tar` archives. Smoke-test
on the `blast_bounty_2026_s1` event (20 demos, January 2026):
non-zero credits flow end-to-end with plausible per-match magnitudes
(top fraggers showing 5–7 saves and 11–18 assisted kills per map).

### Files to touch
- `internal/aggregator/aggregator.go` — add **Pass 14: Save & Assist
  Annotation**. Slot it after Pass 1 (trades), before Pass 3 (per-round). It
  needs the kill stream and damage stream, both of which Pass 1 already has
  in memory.
- `internal/model/model.go` — three new `int` fields on `PlayerMatchStats`:
  - `SavedByTeammate`
  - `SavedTeammate`
  - `AssistedKills`
- `internal/storage/schema.sql` + `storage.go` migrations — three `ALTER
  TABLE ... ADD COLUMN ... DEFAULT 0` statements.
- `internal/storage/role_queries.go` — extend Slice 1 helpers.
- `internal/aggregator/aggregator_test.go` — hand-constructed scenario:
  one kill where the killer's last damage was from the trade victim (= a save).

### Algorithm sketch (Pass 14)
```
for each kill K (killer=A, victim=V, tick=T):
    # Save (mirror of trade)
    for each teammate T_a of A:
        last_dmg = latest damage on T_a from V before T
        if last_dmg exists and T - last_dmg.tick <= 1s:
            SavedTeammate[A] += 1
            SavedByTeammate[T_a] += 1
            break  # at most one save credit per kill
    # Assisted-kill
    for each teammate T_a of A:
        if T_a dealt damage to V earlier in the same round and T_a != A:
            AssistedKills[A] += 1
            break
```

The 1-second window is HLTV's published rule. We already use 5 s for trades
(Pass 1), so this is a tighter and structurally simpler scan. Both shared
constants live in one place at the top of the aggregator.

### Backfill
After the schema migration, **all existing rows have the three new columns =
0**. To repopulate:
```sh
./go-cs-metrics replay --dir <event>/ --force
```
on every event directory. No `.dem` re-parse needed because Pass 14 only uses
data already present in `csraw2.Match` (kills + damages).

### Acceptance
- `cmd/player --roles` now prints non-zero values for the three new metrics.
- Sanity check vs HLTV's published numbers on at least 3 demos: should be
  within ~5% (HLTV's 1-s window is approximate too).

---

## Slice 3 — HLTV-style flash assists — **SHIPPED** (as Pass 15)

§2.7 sub-metric "Flash assists per round" uses HLTV's own rule (≥25 damage on
a blinded enemy within the blind window), distinct from the in-game kill-feed
rule (≥40 damage). We already capture both legs (`FlashEvent.DurationSec` per
victim, `RawDamage` per damage incident, `RawKill` per kill) but never join
them under HLTV's rule.

**Status:** shipped as **Pass 15** (the roadmap originally said "Pass 16" but
we numbered sequentially after Pass 14). New column on `player_match_stats`:
`hltv_flash_assists`. Surfaced in the Utility table of `player --roles` as
`FLASH_A/RD`. The original in-game `FlashAssists` field is preserved for
side-by-side comparison.

**Coverage caveat:** existing rows default to 0; backfill via
`replay --dir <event>/ --force`. Smoke-test on `blast_bounty_2026_s1`:
HLTV vs in-game flash assists land 1.0–1.5× as expected (HLTV's 25 dmg
threshold is looser than in-game's ~40, so HLTV counts more).

### Files to touch
- `internal/aggregator/aggregator.go` — add **Pass 16: HLTV Flash Assists**.
  Reuses the FlashEvent rows already produced by Pass 13 and the damages /
  kills already in memory.
- `internal/model/model.go` — new `HltvFlashAssists int` on `PlayerMatchStats`.
  Keep the existing `FlashAssists` field (in-game definition) for backwards
  compatibility and side-by-side comparison.
- `internal/storage/schema.sql` + `storage.go` — one `ALTER TABLE`.

### Algorithm sketch (Pass 16)
```
for each kill K (killer=A, victim=V, tick=T_k):
    # Find the most recent blind on V before T_k
    flash = latest FlashEvent where victim_id=V AND tick + duration*tickrate >= T_k
    if flash and flash.thrower_id != A and same team(flash.thrower, A):
        # how much damage did A deal V during the blind window?
        dmg = sum(RawDamage where attacker=A AND victim=V AND
                  flash.tick <= dmg.tick <= flash.tick + duration*tickrate)
        if dmg >= 25:
            HltvFlashAssists[flash.thrower_id] += 1
```

### Acceptance
- Per-player `HltvFlashAssists` ≈ in-game `FlashAssists` within ±20% on
  average across our 989-demo DB.
- Outliers (e.g. flash-heavy supports) should show `HltvFlashAssists >
  FlashAssists` because the 25-dmg threshold is looser than 40.

---

## Slice 4 — Time alive & last-alive-on-server

Two §2.5 (Clutching) metrics need a new "per-round liveness" pass:
- **Time alive per round** — `Σ (death_tick − round_start_tick) / tickrate`
  across all rounds. Cap at round end if survived.
- **Last alive on server %** — % of rounds where the player was at any point
  the sole survivor across both teams (different from `is_in_clutch`, which is
  last-on-team).

### Files to touch
- `internal/aggregator/aggregator.go` — add **Pass 17: Liveness**. Reuses
  `RawRound.StartTick / EndTick / PlayerEndState` plus `RawKill.Tick` to mark
  each player's death tick. Then sweep ticks in increasing order to detect
  "sole survivor" moments.
- `internal/model/model.go` — two new fields on `PlayerMatchStats`:
  - `TotalAliveTicks int64`
  - `LastAliveServerRounds int`
- `internal/storage/schema.sql` + `storage.go` — two `ALTER TABLE`s.

### Algorithm sketch (Pass 17)
```
for each round R:
    death_tick_by_player = map[steam_id]tick built from kills in R
    # Time alive
    for each player p with rounds_played in R:
        end = death_tick_by_player.get(p, R.EndTick)
        TotalAliveTicks[p] += end - R.StartTick
    # Last alive on server — at every death tick in R, who is alive?
    alive = set(all players who started R)
    sorted_kills = kills in R sorted by tick
    for kill in sorted_kills:
        alive.remove(kill.victim)
        if len(alive) == 1:
            LastAliveServerRounds[sole_survivor] += 1
            break  # only credit once per round
```

### Acceptance
- Per-player time-alive-per-round in seconds, averaged across all matches,
  lands in the 50–90 s range for active players (matches HLTV's distribution).
- "Last alive on server" count <= `last alive on team` (clutch attempts).

---

## Slice 5 — Surface & UX polish

The metrics are in the DB; the UI is the work. This is bigger than a single
diff but it's all CLI surface — no schema/aggregator risk.

### What to ship
- `cmd/player` — replace the current flat table with a section-per-role
  rendering, mirroring HLTV's layout. Use `tablewriter` boxes per role.
- **Side toggle** — `--side {both,ct,t}` flag (default `both`).
- **Normalization toggle** — `--per {round,24}` flag (default `round`).
  Per-24 = `per_round * 24`.
- **Sample-size disclosure** — every section header prints `(N maps, M
  rounds)` so the reader knows what they're looking at.
- **Cohort label** — for each metric, classify into
  `below / average / above` against the all-players-in-DB cohort. Compute on
  the fly: simple percentiles over the current `player_match_stats` rows
  filtered by the same `--since` / `--tier` / `--map` predicates used to pick
  the player's stats. Cache results per `player` invocation.
- **Opponent-tier breakdown** — `cmd/player --vs-top {5,10,20,30,50}`.
  Requires a date-indexed opponent ranking. For now, *approximate* via the
  static `tier` column on `demos` (since we only have `pro / semi-pro /
  faceit-5`). Mark in `--help` that this is an approximation.

### Files to touch
- `cmd/player.go` — major rewrite of the output renderer.
- `cmd/player_test.go` — golden-file tests for each output shape (both,
  CT-only, T-only, per-24, with cohort).
- `internal/storage/role_queries.go` — cohort percentile helper.
- `README.md` + `docs/architecture.md` — document the new flags.

### Acceptance
- `./go-cs-metrics player 76561198034202275 --roles --side ct --per 24` prints
  a HLTV-like card with seven role panels, each showing CT-side per-24-round
  numbers + cohort label, in <1 s on the current 989-demo DB.

---

## Suggested order

Slice 1 is a single weekend's work and ships ~25 metrics. Slices 2–4 are each
a small new aggregator pass + one schema migration + one replay backfill.
Slice 5 is mostly CLI polish that benefits from having the metrics in place.

Cut order:
1. **Slice 1** — biggest payoff per LOC; unblocks everything in Slice 5 except
   the six metrics from Slices 2–4.
2. **Slice 5 (partial)** — wire the side/per-24 toggles and the role layout
   against the Slice 1 metrics, so reviewers can see the shape early.
3. **Slice 2** — saves/trade-assists; ~3 new fields, one pass.
4. **Slice 3** — HLTV flash assists; 1 new field, one pass.
5. **Slice 4** — time-alive / last-alive; 2 new fields, one pass.
6. **Slice 5 (rest)** — cohort labels, opponent-tier breakdown.

Slices 2 / 3 / 4 can be done in any order; they share no state. Each one ends
with a `replay --force` over the corpus, which is fast (≈5 min for 989
demos with 32 workers, no demoinfocs pressure).

---

## 99. Deferred: Round swing (§1)

> "How much, on average, a player changed their team's chances of winning a
> round based on team economy, side, kills, deaths, damage, trading, and flash
> assists."

This is the only metric on the entire HLTV profile that we can't ship from
derivation + small passes. It requires a **per-round win-probability model**:
a function `P(team wins | game-state at tick t)` that we can evaluate at every
state transition (kill, damage, plant, defuse) and accumulate the player's
contribution.

There are two plausible paths:

1. **Logistic / xgboost regression on round outcomes** trained over our
   `player_round_stats` corpus, with features: side, equipment value
   difference, alive counts per side, bomb planted flag, post-plant time
   remaining. ~30k rounds in DB is enough for a small model.
2. **Borrow simbo3's coefficients** — simbo3 already has a calibrated
   `P(team wins map | ratings, side, map)`. The match-level granularity is
   wrong but the structural form (logistic, side-aware) is reusable for a
   round-level fit if we re-tune on round-level features.

**Revisit when:** we want either (a) a "single-number summary" beyond
Rating 2.0, or (b) to surface per-round impact for game review (which kill
swung the round). Until then, the seven role scores from Slice 5 give a
similar amount of explanatory power without the modelling overhead.

Open work items to schedule when this becomes a priority:
- Decide model class (linear / GBDT / neural).
- Build a per-tick feature extractor from `csraw2.Match` (currently we only
  emit kill / damage / flash / grenade events; we'd need alive-counts and
  equipment-deltas computed per state transition).
- Calibrate against held-out demos; cross-check with HLTV's published round
  swing on the same players.
- Add `round_swing` as a new column on `player_match_stats` plus a per-round
  contribution column on `player_round_stats` for game-review use.
