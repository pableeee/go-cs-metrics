# HLTV Player Profile — Metrics Reference

Reverse-engineered from the HLTV player stats page (`hltv.org/stats/players/<id>/<nickname>`),
captured from meyern's profile on 2026-05-16. The page reveals how HLTV decomposes a
player into **measurable roles**, the **sub-metrics** that feed each role score, and the
**dimensions** (side, time, opponent tier, map, event type) along which everything can
be sliced. This file is a reference for what we could surface in `go-cs-metrics` —
not a spec for what we will build.

---

## 1. Top-level summary box

Always visible at the top of a player profile. The five-stat rail is HLTV's
"first glance" elevator pitch.

| Stat | Description | Notes |
|---|---|---|
| **Rating 2.0** | Combined performance index, 1.00 = average | HLTV's flagship metric. Tooltip: "Kills, Damage, Survival, Impact, and round-to-round consistency." |
| **T Rating** | Rating 2.0 restricted to T-side rounds | |
| **CT Rating** | Rating 2.0 restricted to CT-side rounds | |
| **Maps played** | Sample size for the entire filter | Drives credibility of all numbers below. |
| **Round swing** | Avg. WP delta the player produced per round | Tooltip: "How much, on average, a player changed their team's chances of winning a round based on team economy, side, kills, deaths, damage, trading, and flash assists." (shown as N/A for this player — likely behind a sample-size gate.) |
| **DPR** | Deaths per round | Lower = better. Shown with cohort-percentile bar. |
| **KAST** | % rounds with kill / assist / survive / trade | |
| **Multi-kill** | % rounds with 2+ kills | Sometimes swapped for **MK rating** (economy-adjusted variant). |
| **ADR** | Avg damage per round | |
| **KPR** | Kills per round | |

Each stat in the rail is rendered with:
- a numeric value,
- a **qualitative label** ("Good" / "Okay" / "N/A" / "Below avg" — derived from
  cohort percentile),
- a **distribution bar** with an "Avg." marker and a dot for where this player
  falls (left-position is a real percentile, e.g. `80.12%` for DPR).

A toggle **"Show player average"** lets you compare against this player's own
career mean rather than the cohort.

---

## 2. Role-based decomposition (the big one)

HLTV scores every player on **seven roles**, each 0–100, each with its own
sub-metric breakdown. This is the heart of the profile and the most actionable
thing for us to mimic. All seven respect the **Side: Both / CT / T** and
**Stats per: Round / 24 rounds** toggles independently.

Each role exposes:
- a **headline score 0–100** (combined, CT, T),
- a **cohort-percentile bar**,
- a **tooltip-style description** of what the role represents,
- a **per-metric breakdown** (each sub-metric also has its own cohort bar).

### 2.1 Firepower

> "A player's raw output, with a score based on kills, damage, and multi-kills.
> One for the star players — or those on a particularly high hot streak.
> Survival doesn't matter at all here; this is just the fraggers."

Sub-metrics:
- Kills per round (KPR)
- Rounds with a kill (%)
- Kills per round **win** (kills in won rounds / wins)
- **Rating 2.0** (yes, it appears as a sub-metric inside Firepower)
- Damage per round (ADR)
- Rounds with a multi-kill (%) (2+ kills)
- Damage per round win
- **Pistol round rating** (Rating 2.0 restricted to first round of each half)

### 2.2 Entrying

> "How likely a player is to be the one sacrificed for his teammates.
> High-scoring players are those who go round risky corners first (often knowing
> certain death is coming), or into bomb-sites in the mid-round to make space
> for their star players. Score is based on traded deaths (both % and per
> round) and how often they are 'saved' by a teammate per round."

Sub-metrics:
- **Saved by teammate per round** — teammate killed an attacker within 1 s of
  the last damage taken by this player.
- Traded deaths per round — player died and the killer died within 5 s.
- Traded deaths percentage — share of deaths that were traded.
- **Opening deaths traded percentage** — share of *opening* deaths that were
  traded.
- Assists per round (25+ damage threshold in CS2; 40+ in CSGO).
- **Support rounds** — rounds with an assist, survival, or traded death **but
  no kills**.

### 2.3 Trading

> "The baiters, but that isn't a negative term. Being able to trade your
> teammates is a crucial skill in pro CS. Trade kills (both % and per round)
> are key here but so is saving your teammate while they're taking damage."

Sub-metrics:
- **Saved teammate per round** — killed an opponent attacking a teammate within
  1 s of the attacker's last damage.
- **Trade kills per round** — killed an opponent within 5 s of them killing a
  teammate.
- Trade kills percentage — share of kills that were trades.
- **Assisted kills percentage** — share of kills on opponents teammates had
  already damaged.
- **Damage per kill** — `damage / kills`. <100 ⇒ "more low-damage kills" / kill-stealing.

### 2.4 Opening

> "How likely a player is to make the difference with an early-round opening
> frag. Pros win 5v4s more than 70% of the time on average, so aggression is a
> skillset every team needs. Score is based off of opening kills per round and
> opening attempts."

Sub-metrics:
- Opening kills per round (first kill of the round).
- Opening deaths per round (first death of the round).
- **Opening attempts** — % of rounds where the player was *involved* in the
  opening duel (won or lost).
- **Opening success** — % of opening duels won.
- **Win% after opening kill** — round win % of 5v4s the player created.
- **Attacks per round** — separate damage-dealing incidents (incl. molotov
  ticks). A coarse aggression proxy.

### 2.5 Clutching

> "The late round players and 1vX specialists, players you can rely on in
> post-plants and retakes. Score is based mostly on clutches won with an added
> stylistic measure in the form of time alive per round to identify late-round
> specialists."

Sub-metrics:
- **Clutch points per round** — clutches weighted by opponent count.
  `1v1 = 1, 1v2 = 2, 1v3 = 4, 1v4 = 8, 1v5 = 16` (exponential).
- **Last alive percentage** — share of rounds where the player is last man
  standing on the server.
- **1on1 win percentage** — note: cohort average is ~60% (not 50%), because
  1v1s are created from 2v1s and often inherit a free trade.
- **Time alive per round** — average seconds alive per round.
- **Saves per round loss** — % of round losses the player survives (saves the
  gun for next round).

### 2.6 Sniping

> "Primary AWPers. Based on kills and multi-kills with the AWP and SSG-08. Low
> = rifler, medium = hybrid, high = full-time sniper."

Sub-metrics (all restricted to AWP + SSG-08):
- Sniper kills per round
- Sniper kills percentage (share of all kills)
- Rounds with sniper kills percentage
- Sniper multi-kill rounds (per round)
- Sniper opening kills per round

This is essentially a **role classifier disguised as a score** — meyern scores
14/100, correctly identifying him as a rifler.

### 2.7 Utility

> "The side's grenadier. The overall score is a combination of flashbang
> statistics and grenade damage per round."

Sub-metrics:
- Utility damage per round (HE + molotovs/incendiaries).
- **Utility kills per 100 rounds** (note: per *100* rounds, not per round —
  because the rate is tiny).
- Flashes thrown per round (cap of 2 buyable per round; teammate drops count).
- **Flash assists per round** — opponent killed *while* flashed by this player.
  HLTV uses a custom 25-dmg / timing rule, **different from the in-game
  kill-feed flash assists**.
- **Time opponent flashed per round** — total seconds of enemy blind time the
  player produced.

---

## 3. Raw "Statistics" block

A bare numeric dump below the role rollup. No qualitative labels here — just
career totals at the current filter.

Column 1:
- Total kills, Headshot %, Total deaths, K/D ratio, Damage / Round,
  **Grenade dmg / Round** (note: HLTV publishes this separately from ADR),
  Maps played.

Column 2:
- Rounds played, Kills / round, Assists / round, Deaths / round,
  Saved by teammate / round, Saved teammates / round,
- **Impact rating** — "Measures the impact made from multikills, opening kills,
  and clutches." (Separate from Rating 2.0, fed by the same components.)

---

## 4. Featured ratings — opponent-tier breakdown

A grid of Rating 2.0 numbers restricted to maps where the *opposing team* was
in a given top-N at the time of the match (within a ±30-day window — see
sidebar Ranking filter description). The grid includes the sample size in maps
so you know which numbers are noise.

Columns: vs Top 5 / Top 10 / Top 20 / Top 30 / Top 50.
Example for meyern: `0.97 (18 maps)` / `0.88 (38 maps)` / `0.95 (120 maps)` /
`0.97 (171 maps)` / `1.00 (318 maps)`.

This is the simplest "strength of schedule" adjustment HLTV offers and it's
genuinely informative: meyern's drop from 1.00 vs top-50 to 0.88 vs top-10 is
the single most useful number on the page for a simulator's prior.

---

## 5. Teammates

A "best-fit" carousel of recurring teammates, each with their Rating 2.0 while
on the same team. Each row links to a `lineup=<id1>&lineup=<id2>` aggregate so
you can pull duo / trio stats.

Conceptually this is a **co-occurrence join** on `player_match_stats`:
`group by (steam_id_a, steam_id_b) where both appear in the same demo`.

---

## 6. Form chart

> "Sliding window of rating 1.0 for matches in the past 3 months — 1 day
> increments."

A FusionCharts line series of **trailing-3-month Rating** with one point per
3-day-ish step. Each datapoint's tooltip exposes the window dates and map
count: e.g. `Feb 13th – May 13th – maps: 90`. The y-axis floor is dynamic
(here `0.71`).

This is just a rolling average — but rendering it as a long time series makes
career arcs (career-ender slumps, recovery surges, prime years) extremely
obvious. The dataset on this page goes back to **2017**.

---

## 7. Dimensions / filters (the lattice)

Every metric in §1–6 respects the following slicers. This is the lattice of
"cuts" HLTV maintains over the entire stats DB.

| Filter | Values |
|---|---|
| Event type | All / Majors / Big events / MVP events / LAN / Online |
| Time window | All / Last 1m / 3m / 6m / 12m / per-year buckets back to 2012 + free date range |
| Opponent ranking | All / Top 5 / 10 / 20 / 30 / 50 (within ±30 d of match) |
| Map | All + each map in the active pool, plus historical maps (Cache, Cobble, Season, Train, Tuscan, Vertigo, …) |
| CS version | Both / CS2 / CSGO |
| Match length | All / Bo1 / Bo3 / Bo5 |
| Valve-ranked | Both / Ranked / Unranked (i.e. VRS-eligible vs not) |
| Playoff stage | All / Grand final / Playoffs / Pre-playoff |
| Side | Both / CT / T (toggle inside role section) |
| Normalization | Per round / Per 24 rounds (toggle inside role section) |
| Compare vs | Cohort avg / This player's career avg (toggle: "Show player average") |

The "Add to context" search lets you stack a team / event / opponent on top of
any of these — i.e. arbitrary `WHERE` predicates composed via the URL.

---

## 8. Other tabs on a player profile (not on this page but linked)

The top sub-nav advertises eight sibling views. Each is a deeper drilldown of
the same lattice:

- **Individual** — same metrics, sliced harder per-player rather than
  per-role-aggregate.
- **Matches** — every map of the player, with per-map Rating, K/D, ADR.
- **Events** — per-tournament rollup (MVP / EVP claims, placement, rating).
- **Career** — timeline of teams played for + cumulative achievements.
- **Weapons** — per-weapon kill share, headshot rate, accuracy.
- **Clutches** — every 1vN situation, situation-by-situation.
- **Multi-kills** — every 2k / 3k / 4k / 5k (ace) with context.
- **Opponents** — Rating vs every team the player has ever played. The
  ultimate "matchup history" view.

---

## 9. Mapping to `go-cs-metrics` — what we already have vs. what we don't

Cross-reference of HLTV's surface area against our current `PlayerMatchStats`
+ `PlayerRoundStats` + duel-engine output.

### Already in our data model (raw or one SQL away)

- KPR, DPR, ADR, KAST, K/D — `player_match_stats`
- Headshot % — `player_weapon_stats`
- Multi-kill % — derivable from `player_round_stats` (count rounds with ≥2k)
- Opening kills / deaths per round — `opening_kills` / `opening_deaths` cols
- Trade kills, traded deaths — pass 1 of the aggregator (with delay ms)
- Clutch counts — `player_round_stats.flags` (CLUTCH_1vN)
- Pistol-round rating — group by `round_idx in {1, 13}` + Rating proxy
- Time alive per round — duel engine has exposure_time inputs
- Per-map / per-event / per-opponent slicing — `demos` table joins
- Per-side (CT/T) — round-side already tracked
- Per-round vs per-24-round normalization — trivial post-query rescale

### Easy to add (SQL or one aggregator pass)

- **Saved by teammate / saved teammate** — we have damages + kill timing;
  apply the 1 s post-damage rule.
- **Trade kill percentage** — `trade_kills / total_kills` (have both).
- **Opening attempts %** — anyone involved in the first duel (winner or loser).
- **Opening success %** — `opening_kills / opening_attempts`.
- **Win% after opening kill** — `won_round & opener=us` over `opener=us`.
- **Damage per kill** — `damage / kills`.
- **Damage per round win** / **Kills per round win** — restrict to
  `won_round = 1`.
- **Support rounds** — assist|survive|traded-death AND no kill.
- **Sniper kills % / rounds with sniper kill** — `player_weapon_stats` filter
  on AWP / SSG-08.
- **Featured-ratings opponent-tier matrix** — we already have a `tier` column
  on `demos`; we just don't currently apply it at the **per-match-opponent**
  granularity HLTV uses (top-N at the time of the match). Would need an
  HLTV-style team-ranking history table to be exact.
- **Form chart** — rolling 3-month rating, 1 / 3-day increment.

### Hard / would need new instrumentation

- **Round swing** — requires a per-round win-probability model. We don't have
  one; simbo3's tuning loop could in principle produce one but it operates at
  the match level, not the round level.
- **Impact rating** — HLTV's published formula is closed; we'd ship our own
  proxy from multi-kills + openings + clutches.
- **Flash assists (HLTV definition)** — needs the custom 25-dmg-while-flashed
  rule; we capture flash events but not the kill→blind-window join.
- **Time opponent flashed per round** — we don't currently aggregate
  per-shooter blind-seconds delivered. Solvable from `csraw2` flash events but
  not yet in the schema.
- **Utility kills per 100 rounds** — needs explicit attribution of HE /
  molotov final-blow kills. Probably already in damages but not surfaced.
- **Attacks per round** — needs separating damage *incidents* from damage
  *totals*. Doable, not done.
- **Clutch points (exponential weighting)** — easy formula, just unimplemented
  on the `player` command.
- **Opponent-tier matrix with per-match ranking** — needs HLTV-style ranking
  history snapshot (we currently only carry the static tier label on each
  demo, not a date-indexed opponent ranking).
- **Teammate / lineup aggregate** — easy schema-wise (pair joins on
  `player_match_stats`), but no command surfaces it today.

---

## 10. Reusable ideas worth stealing

A short list of the framing decisions on HLTV's page that we should keep in
mind whether or not we implement the exact metrics:

1. **Roles as a top-level decomposition.** A single Rating number is hard to
   act on. Seven role scores ("you're a 76 trader and a 14 sniper") instantly
   suggest training direction. Our `analyze` command could output this shape.
2. **Side-split everything.** Every role and sub-metric is rendered for
   Both / CT / T. The CT vs T delta per role is often the most diagnostic
   single comparison.
3. **Per-round AND per-24-round normalization.** The per-24 figure is more
   intuitive ("16.7 kills per map") but per-round is statistically correct.
   Offering both side-by-side is cheap and good UX.
4. **Cohort percentile bars, not just numbers.** Telling a user "ADR 74"
   means nothing; "ADR 74, 38th percentile vs top-50 riflers" means
   everything. Requires keeping a cohort distribution per metric.
5. **Sample-size disclosure beside every number.** Map counts are right next
   to every opponent-tier rating. Same principle for our `--quorum` and
   `--since` filters.
6. **Opponent-tier breakdown is the most useful single slice.** A simulator
   prior should weight `vs Top-10` more than `vs all` in almost every case.
   Our `MapWinOutcomes` query could trivially carry an opponent-tier dimension.
7. **The form chart is just a 3-month rolling Rating.** Cheap to compute, very
   evocative — and the best onboarding visual for an analyst trying to
   understand a player's arc.
8. **The lattice is the product.** What makes the page powerful isn't any one
   metric — it's that *every* metric is sliceable by *every* dimension. Our
   stats DB is already shaped for this; what we lack is the lattice-aware UI
   (right now most commands hard-code one or two slices).
