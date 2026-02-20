Below is a solid **definition / scope** doc you can paste into an RFC template. It’s written to keep you focused on an MVP while leaving room to grow into “Refrag-like” depth later.

---

# CS2 Demo Review Metrics Tool — Definition & Scope

## 1. Purpose

Build a tool that **parses Counter-Strike 2 match demos**, extracts **player and team performance metrics**, and presents them in a way that helps me **identify weaknesses, track improvement over time, and compare myself against teammates/opponents and my own baselines**.

The output should answer:

* *What am I doing well / poorly (aim, entry, trading, utility)?*
* *Is this improving over time?*
* *In which situations do I lose fights or fail to convert advantages?*

## 2. Goals

### Primary goals

* **Automated demo ingestion**: given one or more demo files, compute metrics consistently.
* **Player-centric reporting**: metrics for me plus context for all players in the match.
* **Repeatable tracking**: store results so I can view trends over time (per map, per role, per weapon, per side, etc.).
* **Actionable insights**: not just numbers—surface “what to train next” based on worst metrics.

### Non-goals (initially)

* Building a full coaching product with human-like narrative analysis.
* Real-time live match analysis.
* Anti-cheat / suspicious behavior detection.
* Perfect equivalence to HLTV’s official rating formulas (we can approximate if needed).

## 3. Users & Use Cases

### Primary user

* Me (solo user), reviewing my own improvement and drilling down into specific mistakes.

### Core use cases

1. Upload/select a demo → compute match + player metrics.
2. Compare my metrics across matches (last 10 / last 30 / per map).
3. Drill down: click a metric → see the **rounds / events** that contributed to it.
4. Segment metrics by filters: map, side (T/CT), weapon, round type (eco/force/buy), clutch situations.

## 4. Inputs & Outputs

### Inputs

* CS2 demo files (e.g., `.dem`).
* Optional metadata:

  * My SteamID / player identifier.
  * Tags (scrim / faceit / premiere / etc.)
  * “Role” label (entry / lurk / anchor) if I want manual classification later.

### Outputs

* Match summary (score, map, teams, rounds).
* Player table (all players) with core metrics.
* My deep-dive dashboard:

  * General
  * Aim
  * Entry & Trades
  * Utility
* Drill-down views linking metrics → specific rounds/timecodes.

## 5. Metric Definitions (Scope)

### 5.1 General (MVP-ready)

**Kills / Assists / Deaths**

* Directly from events.

**K/D Ratio**

* kills / deaths (handle 0 deaths as kills or “∞” depending on display).

**HS Kill %**

* headshot kills / total kills.

**ADR (Average Damage per Round)**

* total damage dealt / rounds played.

**HLTV Rating**

* Option A (MVP): show **KAST**, ADR, K/D, and a *“composite score”* with a clear disclaimer.
* Option B (later): implement an approximation of HLTV 2.0/2.1 style rating if feasible.

> MVP recommendation: don’t call it “HLTV Rating” until it matches closely; call it **“Composite Rating (beta)”**.

---

### 5.2 Aim (Phase 2 MVP, because definitions must be careful)

**Time To Kill (TTK)**

* Time between first shot fired by player at an enemy and the kill event for that enemy.
* Needs: shot events + victim mapping + time windows.

**Time To Damage (TTD)**

* Time between first contact opportunity (spotted or shot opportunity) and first successful damage.
* Needs a clear “start point” definition; recommended:

  * start = first shot OR first enemy spotted within X degrees of crosshair.

**Spotted Accuracy**

* Accuracy (hits/shots) while enemy was visible/spotted.
* Needs visibility/spotted model (may rely on demo data availability).

**Headshot %**

* headshot hits / total hits (different from HS kill %).
* Might be phase 2 depending on hit granularity.

**Counter Strafe %**

* Percentage of shot instances where player velocity is below a threshold shortly before the shot (e.g., < 20 units) AND key transition indicates counter-strafe behavior.
* Needs velocity + input state or inferred decel.

**Crosshair Placement**

* A proxy metric (since “perfect” crosshair placement is subjective):

  * At time of first enemy visibility, distance in degrees between crosshair direction and enemy hitbox center.
  * Report median/percentiles + % of “good placement” under thresholds.

**Recoil Control Accuracy**

* Compare spray pattern compensation vs expected recoil offsets; proxy could be:

  * accuracy on sustained sprays (>= N bullets) after first bullet,
  * hit rate on bullets 4–12 for rifles,
  * adjusted for distance.

> These are valuable, but require careful engineering + validation. Treat as **Phase 2**.

---

### 5.3 Entry & Trades (Strong MVP candidates)

**Opening Kill Successes / Fails**

* Opening duel participation:

  * Success: player gets the first kill of the round.
  * Fail: player is the first death of the round.
* Optionally split by side (T entries matter differently than CT).

**Trade Kill Successes / Fails**

* Define a trade window (common: 3–5 seconds) and proximity/line-of-sight heuristic:

  * Success: player kills the enemy who killed a teammate within the trade window.
  * Fail: player had an opportunity (nearby) but didn’t convert (harder; can be “attempted trades” later).

**Trade Death Successes / Fails**

* Success: when player dies, teammate trades the killer within the window.
* Fail: player dies and is *not* traded within the window.

> MVP suggestion: implement **trade success metrics first**; trade “fails” require opportunity modeling and can be misleading.

---

### 5.4 Utility (MVP-ready if flash events are accessible)

**Flash Assists**

* From event logs.

**Enemies Flashed / Friends Flashed**

* Count unique players affected + total instances.

**Average Flash Time**

* Average blind duration inflicted (enemy-only and optionally total).

**Utility Damage**

* Total damage from HE / molotov / incendiary.

**Unused Utility Value**

* End-of-round utility inventory value remaining (or count of unused nades).
* Requires tracking purchases + throws + inventory at round end.

## 6. MVP Scope (What we build first)

### MVP v1 (recommended)

* Demo import + parsing pipeline
* Match + player table
* Implement these metric groups:

  * **General**: K/A/D, K/D, HS kill %, ADR
  * **Entry & Trades**: opening kills/deaths, trade kills (success), trade deaths (success)
  * **Utility**: flash assists, enemies/friends flashed, avg blind time, util damage, unused util (simple count)
* Drill-down: click a metric → list rounds + timestamps where it occurred.

### MVP v1.1

* Filters: map, side, weapon class, buy type (eco/force/buy)
* Trend view across matches (rolling averages)

### Phase 2

* Aim metrics (TTK, crosshair placement, counter-strafe, recoil proxies)
* Better composite rating
* Round context labels (clutch, man-advantage conversion, post-plant/retake)

## 7. Data Model (High level)

### Entities

* **Demo**

  * id, file hash, map, date, match type, tickrate
* **Match**

  * teams, score, rounds, players
* **Round**

  * economy summary (optional), events list
* **PlayerMatchStats**

  * aggregated metrics per match/player
* **PlayerRoundStats**

  * per-round metrics (for drill-down)
* **Event**

  * kill, damage, flash, grenade throw, bomb plant/defuse, etc.

## 8. Architecture (Suggested)

### Pipeline

1. **Ingestion**

   * Accept demo file, compute hash, store raw file.
2. **Parsing**

   * Convert demo → structured events (tick/time-based).
3. **Aggregation**

   * Compute metrics and store results.
4. **Presentation**

   * UI or CLI to browse match summaries and drill-downs.

### Storage

* MVP: SQLite (fast, simple, portable).
* Later: Postgres if multi-user / cloud.

### UX options

* MVP fastest: CLI + local HTML report export.
* Next: Local web app (desktop-like) or lightweight React UI.

## 9. Quality & Validation

* Unit tests for metric definitions (especially trade logic).
* Golden demos: known matches where you verify counts manually.
* “Sanity checks”:

  * total kills should match scoreboard kills, etc.
  * ADR roughly aligns with known sources.

## 10. Risks / Open Questions

* Availability and fidelity of certain signals in CS2 demos (visibility, input state, detailed hitbox data).
* Defining “fail” metrics without false positives (trade fail, crosshair placement).
* HLTV rating replication is non-trivial; avoid overclaiming.

## 11. Success Criteria

After MVP:

* I can drop in 10 demos and instantly get:

  * a stable table of match/player stats,
  * my entry/trade + util impact,
  * a list of the rounds that define my worst metrics,
  * trends over time that are consistent enough to guide training.

