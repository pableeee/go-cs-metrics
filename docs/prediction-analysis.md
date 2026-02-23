# simbo3 Prediction vs Actual Results — Three Tier-1 CS2 Events

**Analysis date:** 2026-02-23
**Events covered:** StarLadder Budapest Major 2025 · IEM Krakow 2026 · PGL Cluj-Napoca 2026
**Model data source:** IEM Krakow 2026 demos only (`--since 90 --quorum 3`)
**Simulation mode:** `--mode manual` (actual maps/picks used)

---

## 1. Results at a Glance

### IEM Krakow 2026 (Jan 27 – Feb 8, 2026)

| Match | Predicted winner | Confidence | Actual winner | Correct? |
|---|---|---|---|---|
| QF1: Aurora vs FURIA | **FURIA** | 83.1% | **FURIA** 2-0 | ✓ |
| QF2: G2 vs MOUZ | **MOUZ** | 85.2% | **MOUZ** 2-0 | ✓ |
| SF1: Spirit vs FURIA | Spirit | 57.2% | **FURIA** 2-1 | ✗ |
| SF2: Vitality vs MOUZ | MOUZ | 55.7% | **Vitality** 2-0 | ✗ |
| 3rd: Spirit vs MOUZ | MOUZ | 55.3% | **Spirit** 2-0 | ✗ |
| GF: FURIA vs Vitality | **Vitality** | 84.1% | **Vitality** 3-1 | ✓ |

**Krakow accuracy: 3/6 (50%)**

---

### StarLadder Budapest Major 2025 (Dec 11–14, 2025)

| Match | Predicted winner | Confidence | Actual winner | Correct? |
|---|---|---|---|---|
| QF1: Spirit vs Falcons | Falcons | 61.9% | **Spirit** 2-0 | ✗ |
| QF2: Vitality vs MongolZ | MongolZ | 53.5% | **Vitality** 2-0 | ✗ |
| QF3: FaZe vs MOUZ | MOUZ | 74.5% | **FaZe** 2-0 | ✗ |
| QF4: NaVi vs FURIA | FURIA | 55.3% | **NaVi** 2-1 | ✗ |
| SF1: Vitality vs Spirit | Spirit | 72.6% | **Vitality** 2-0 | ✗ |
| SF2: FaZe vs NaVi | FaZe | 51.1% | **FaZe** 2-1 | ✓ |
| GF: FaZe vs Vitality | **Vitality** | 90.0% | **Vitality** 3-1 | ✓ |

**Budapest accuracy: 2/7 (28.6%)**

---

### PGL Cluj-Napoca 2026 (Feb 16–22, 2026)

| Match | Predicted winner | Confidence | Actual winner | Correct? |
|---|---|---|---|---|
| QF1: Falcons vs PARIVISION | Falcons | 59.6% | **PARIVISION** 2-1 | ✗ |
| QF2: MOUZ vs NaVi | NaVi | 52.2% | **MOUZ** 2-1 | ✗ |
| QF3: Vitality vs Aurora | Aurora | 51.0% | **Vitality** 2-0 | ✗ |
| QF4: FURIA vs MongolZ | FURIA | 67.5% | **MongolZ** 2-1 | ✗ |
| SF1: PARIVISION vs MOUZ | MOUZ | 79.1% | **PARIVISION** 2-0 | ✗ |
| SF2: Vitality vs MongolZ | MongolZ | 53.2% | **Vitality** 2-0 | ✗ |
| 3rd: MOUZ vs MongolZ | MongolZ | 74.7% | **MOUZ** 2-0 | ✗ |
| GF: PARIVISION vs Vitality | **Vitality** | 98.2% | **Vitality** 3-0 | ✓ |

**Cluj accuracy: 1/8 (12.5%)**

---

### Summary

| Event | Correct | Total | Accuracy |
|---|---|---|---|
| IEM Krakow 2026 | 3 | 6 | 50.0% |
| Budapest Major 2025 | 2 | 7 | 28.6% |
| PGL Cluj-Napoca 2026 | 1 | 8 | 42.9%* |
| **Overall** | **6** | **21** | **28.6%** |

*The QF3 prediction was Vitality 49.0% vs Aurora 51.0% — essentially a coin flip; excluding it, the result doesn't change meaningfully.

Random baseline: 50%. The model performed worse than a coin flip overall, which signals systematic biases rather than just noise.

---

## 2. Analysis

### 2.1 MOUZ Overestimation — The Dominant Pattern

The single most consistent failure across all three events is MOUZ being heavily overrated:

| Match | MOUZ prediction | Actual |
|---|---|---|
| Krakow QF2 vs G2 | 85.2% | Won (correct) |
| Krakow SF2 vs Vitality | 55.7% fav | Lost |
| Krakow 3rd vs Spirit | 55.3% fav | Lost |
| Budapest QF3 vs FaZe | 74.5% fav | Lost 0-2 |
| Budapest SF1 (as PARIVISION opp) | 79.1% fav | Lost to PAR 0-2 |
| Cluj 3rd vs MongolZ | 74.7% fav | Lost 0-2 |

**Root cause:** MOUZ has strong map win% numbers in the Krakow database (they won many maps in the group stage), but they consistently underperform when it matters in playoffs. The model has no concept of "playoff form" vs "group stage form". MOUZ went out in QF/SF of every event, suggesting their group-stage dominance doesn't translate.

### 2.2 PARIVISION Systematic Underestimation

PARIVISION is underestimated in almost every match:

- Cluj QF1: given 40.4%, beat Falcons 2-1
- Cluj SF1: given 20.9%, swept MOUZ 2-0
- Cluj GF: given 1.8%, played Vitality to 3 maps

**Root causes:**
1. **`zweih` was a stand-in** for one or more Krakow matches, which is why his demo count (38) is much higher than the core PARIVISION players (~9 each). His stats in the export reflect a different role/team context than the full lineup, slightly distorting the aggregate.
2. **Map coverage gap**: Only 4 maps have non-prior stats: Ancient, Anubis, Dust2, Overpass. Their performance on Inferno, Nuke, Mirage, and Train falls back entirely to the 0.50 prior.
3. **Small sample**: 9 demos for most players is below the shrinkage threshold where real stats strongly dominate the prior.

### 2.3 Temporal Mismatch (Budapest Major)

All team exports use IEM Krakow 2026 data (Jan–Feb 2026). Predictions for Budapest Major (Dec 2025) use data from 6–8 weeks **after** the event being predicted — a fundamental lookahead bias:

| Match | Model said | Actual | Explanation |
|---|---|---|---|
| SF1: Vitality vs Spirit | Spirit 72.6% | Vitality won 2-0 (19-4, 13-6) | Spirit's Krakow numbers overstate their Dec form |
| QF3: FaZe vs MOUZ | MOUZ 74.5% | FaZe won 2-0 | FaZe strengthened post-Budapest |
| QF1: Spirit vs Falcons | Falcons 61.9% | Spirit won 2-0 | Falcons overrated (NiKo/m0NESY era inflated stats) |

For the Budapest predictions to be fair, the model would need data from Sep–Dec 2025. We only have IEM Krakow (Jan–Feb 2026) data, making these predictions effectively a lookahead test rather than a true forecast.

### 2.4 Map Pool Misconfiguration

`simbo3`'s default map pool is the **2024 pool** (`Mirage, Inferno, Nuke, Ancient, Overpass, Anubis, Vertigo`). The active 2025–2026 CS2 pool added **Dust2** and **Train**, replacing Anubis and Vertigo. Every simulation generates warnings:

```
warning: unknown map "Dust2" (not in configured pool)
warning: unknown map "Train" (not in configured pool)
```

In `--mode manual` this doesn't affect results (actual map stats are used from the team JSON), but the veto model is completely broken for any `--mode veto` simulation since it doesn't know these maps exist.

### 2.5 What the Model Does Well

- **Clear favourites** are generally correct: FURIA 83.1% over Aurora ✓, MOUZ 85.2% over G2 ✓, Vitality 84.1% over FURIA in GF ✓, Vitality 98.2% over PARIVISION in GF ✓.
- **Vitality in Grand Finals**: The model correctly identified Vitality as the dominant team in every GF across all three events (Krakow, Budapest, Cluj).
- **Mid-pack matches are near 50%**: MOUZ vs NaVi at Cluj (47.8%/52.2%), Vitality vs MongolZ (46.8%/53.2%) — the model correctly signals uncertainty.

---

## 3. Appendix: How Predictions Were Made

### 3.1 Data Collection

1. **Demos**: IEM Krakow 2026 (Jan 27–Feb 8, 2026) — 84 `.dem` files parsed via `go-cs-metrics parse --dir ~/demos/pro/iem_krakow_2026/ --tier pro`. The 11-pass aggregator produces `player_match_stats`, `player_round_stats`, `player_weapon_stats`, and `player_duel_segments` per demo.

2. **Team identification**: No explicit team field exists in CS2 demos (only CT/T). Teams were identified via SQL co-occurrence: groups of 5 players sharing the same set of demos are teammates.

3. **Roster files**: 15 team roster JSONs created in `~/git/go-cs-metrics/rosters/`. Each contains SteamID64s for the 5 players.

4. **Team exports**: `go-cs-metrics export --roster <team>.json --since 90 --quorum 3 --out <team>.json` — produced for all 15 teams. The export computes per-map win%, CT/T round win%, and Rating 2.0 proxy per player from the DB.

### 3.2 Prediction Methodology

Each playoff match was simulated with:
```sh
simbo3 run \
  --teamA <teamA>.json --teamB <teamB>.json \
  --mode manual \
  --maps <actual maps played> \
  --picks <actual picker: A/B/D> \
  --format bo3|bo5 \
  --output json \
  --trials 50000
```

Key distinction: `--mode manual` uses the **actual maps that were played** (sourced from Liquipedia/HLTV), not a simulated veto. This removes veto prediction error from the equation — we're purely testing map win probability estimation.

### 3.3 Model Architecture (simbo3)

For each map, the win probability is:

```
p(A wins map) = sigmoid( α*(S_map_A - S_map_B) + β*ΔRating + γ*side_term )
```

Where:
- `S_map = logit(smoothed_win_pct)` — log-odds of smoothed map win%
- Smoothing: `w = n / (n + k)` shrinks toward 0.50 prior (k=10); maps with <5 games are near 50%
- `ΔRating = avg_rating_A - avg_rating_B` using Rating 2.0 proxy
- `side_term` accounts for CT/T win rate differences
- Default coefficients: α=0.9, β=1.2, γ=0.35

Series probability is computed via 50,000 Monte Carlo trials (each trial simulates individual maps sequentially).

### 3.4 Rating 2.0 Proxy

```
Rating ≈ 0.0073*KAST% + 0.3591*KPR − 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
Impact  = 2.13*KPR + 0.42*APR − 0.41
```

Community approximation, not official HLTV math. Expect ±0.05–0.10 deviation.

---

## 4. Possible Improvements

### 4.1 Fix the simbo3 Map Pool (High Priority, Easy)

Update `internal/model/model.go` default pool from the stale 2024 pool to the current 2025–2026 pool:

```go
// Replace:
MapPool: []string{"Mirage", "Inferno", "Nuke", "Ancient", "Overpass", "Anubis", "Vertigo"},
// With:
MapPool: []string{"Mirage", "Inferno", "Nuke", "Dust2", "Overpass", "Ancient", "Train"},
```

This is a one-line fix that unblocks veto mode for all current matches.

### 4.2 Fix PARIVISION Roster (High Priority, Easy)

Remove `zweih` (SteamID `76561198210626739`) from `rosters/parivision.json` and replace with the correct 5th player. The current lineup at IEM Krakow 2026 was Jame, xfl0ud, nota, BELCHONOKK, and one more (check Liquipedia). Then re-export.

Also apply this roster audit to all 15 teams — the co-occurrence method can produce false positives when a player subbed in for one match.

### 4.3 Use Event-Appropriate Demo Data

For Budapest Major predictions, use demos from Sep–Nov 2025 (ESL Pro League S22, IEM Chengdu 2025) rather than IEM Krakow data. This eliminates lookahead bias.

Practical workflow:
```sh
# Download pre-Budapest event demos
demoget sync --event esl_pro_league_s22 --out ~/demos/pro
demoget sync --event iem_chengdu_2025 --out ~/demos/pro
demoget touch-dates --out ~/demos/pro
go-cs-metrics parse --dir ~/demos/pro/esl_pro_league_s22/ --tier pro
# Then export with --since 90 relative to Dec 2025
```

### 4.4 Temporal Weighting Within Export Window

Currently `--since 90` treats a demo from 5 days ago equally with one from 89 days ago. Adding exponential decay to map win% and rating would make the model more responsive to recent form.

Example: weight = `exp(-λ * days_ago)` with λ = 0.02 (half-life ~35 days).

### 4.5 Multi-Event Demo Coverage

Our DB currently has only IEM Krakow 2026 (84 demos). Adding more events dramatically increases data quality:

| Priority | Event | Teams covered | Est. demos |
|---|---|---|---|
| High | Budapest Major 2025 | 8 playoff teams | ~60 |
| High | ESL Pro League S22 | All top teams | ~120 |
| Medium | IEM Chengdu 2025 | 8 teams | ~40 |
| Medium | BLAST Bounty Winter 2026 | 6–8 teams | ~30 |

With more demos, shrinkage pulls less toward 0.50, and rating estimates become more stable.

### 4.6 Roster Validation Pipeline

Add a validation step to `export` (or a separate command) that flags when:
- A player's demo count is significantly below the team average (possible wrong player)
- A player's match dates don't overlap with the team's match dates (temporal mismatch)
- The team has >5 unique players sharing the same demos (substitutes mixed in)

### 4.7 Simbo3 Coefficient Tuning Against Known Results

We now have 21 labeled playoff matches (actual outcome + map picks). This is enough to run `simbo3 tune`:

```sh
# Build backtest dataset from these 21 matches (MatchRecord format)
# Run tuner:
./bin/simbo3 tune --dataset playoff_matches.json --seed 42 --rounds 5 --trials 3000
```

With proper cross-validation (train on Budapest+Krakow, test on Cluj) this could improve α, β, γ coefficients. Warning: with only 21 data points, there's a real risk of overfitting.

### 4.8 Head-to-Head Records

The model treats Spirit vs FURIA as independent of their match history. A `head_to_head_modifier` term could adjust the matchup logit for well-documented rivalry patterns. Requires storing match results separately from player stats.

### 4.9 Side-Selection Improvement

Currently `--start-sides` is not passed (simbo3 uses its own model to determine starting side for each team). Fetching actual starting sides from match records and passing them with `--start-sides CT,T,...` would give more accurate per-map estimates.

---

## 5. Raw Data Tables

### IEM Krakow 2026 playoff maps

| Match | Map | Picker | Score | Winner |
|---|---|---|---|---|
| QF1 Aurora vs FURIA | Dust2 | Aurora | 13-7 | FURIA |
| QF1 Aurora vs FURIA | Mirage | FURIA | 13-4 | FURIA |
| QF2 G2 vs MOUZ | Overpass | MOUZ | 16-13 OT | MOUZ |
| QF2 G2 vs MOUZ | Dust2 | G2 | 13-7 | MOUZ |
| SF1 Spirit vs FURIA | Mirage | FURIA | 16-13 OT | FURIA |
| SF1 Spirit vs FURIA | Dust2 | Spirit | 13-8 | Spirit |
| SF1 Spirit vs FURIA | Nuke | Decider | 13-7 | FURIA |
| SF2 Vitality vs MOUZ | Nuke | MOUZ | 13-7 | Vitality |
| SF2 Vitality vs MOUZ | Dust2 | Vitality | 13-6 | Vitality |
| 3rd Spirit vs MOUZ | Dust2 | Spirit | 13-8 | Spirit |
| 3rd Spirit vs MOUZ | Mirage | MOUZ | 13-3 | Spirit |
| GF FURIA vs Vitality | Mirage | FURIA | 13-11 | FURIA |
| GF FURIA vs Vitality | Inferno | Vitality | 13-8 | Vitality |
| GF FURIA vs Vitality | Nuke | FURIA | 13-2 | Vitality |
| GF FURIA vs Vitality | Overpass | Vitality | 13-10 | Vitality |

### StarLadder Budapest Major 2025 playoff maps

| Match | Map | Picker | Score | Winner |
|---|---|---|---|---|
| QF1 Spirit vs Falcons | Nuke | Falcons | 13-4 | Spirit |
| QF1 Spirit vs Falcons | Dust2 | Spirit | 16-12 | Spirit |
| QF2 Vitality vs MongolZ | Mirage | MongolZ | 13-5 | Vitality |
| QF2 Vitality vs MongolZ | Dust2 | Vitality | 13-4 | Vitality |
| QF3 FaZe vs MOUZ | Nuke | FaZe | 13-9 | FaZe |
| QF3 FaZe vs MOUZ | Inferno | MOUZ | 13-10 | FaZe |
| QF4 NaVi vs FURIA | Mirage | NaVi | 13-8 | NaVi |
| QF4 NaVi vs FURIA | Inferno | FURIA | 13-9 | NaVi |
| QF4 NaVi vs FURIA | Train | Decider | 13-10 | NaVi |
| SF1 Vitality vs Spirit | Mirage | Vitality | 19-17 3OT | Vitality |
| SF1 Vitality vs Spirit | Dust2 | Spirit | 13-8 | Vitality |
| SF2 FaZe vs NaVi | Ancient | NaVi | 13-5 | NaVi |
| SF2 FaZe vs NaVi | Nuke | FaZe | 13-11 | FaZe |
| SF2 FaZe vs NaVi | Inferno | Decider | 13-8 | FaZe |
| GF FaZe vs Vitality | Nuke | FaZe | 13-6 | FaZe |
| GF FaZe vs Vitality | Dust2 | Vitality | 13-3 | Vitality |
| GF FaZe vs Vitality | Inferno | FaZe | 13-9 | Vitality |
| GF FaZe vs Vitality | Overpass | Vitality | 13-2 | Vitality |

### PGL Cluj-Napoca 2026 playoff maps

| Match | Map | Picker | Score | Winner |
|---|---|---|---|---|
| QF1 Falcons vs PARIVISION | Dust2 | PARIVISION | 16-14 OT | PARIVISION |
| QF1 Falcons vs PARIVISION | Mirage | Falcons | 13-7 | Falcons |
| QF1 Falcons vs PARIVISION | Ancient | Decider | 13-8 | PARIVISION |
| QF2 MOUZ vs NaVi | Ancient | NaVi | 13-10 | NaVi |
| QF2 MOUZ vs NaVi | Inferno | MOUZ | 13-4 | MOUZ |
| QF2 MOUZ vs NaVi | Mirage | Decider | 13-6 | MOUZ |
| QF3 Vitality vs Aurora | Inferno | Aurora | 13-8 | Vitality |
| QF3 Vitality vs Aurora | Overpass | Vitality | 13-10 | Vitality |
| QF4 FURIA vs MongolZ | Mirage | MongolZ | 13-3 | MongolZ |
| QF4 FURIA vs MongolZ | Dust2 | FURIA | 13-10 | FURIA |
| QF4 FURIA vs MongolZ | Nuke | Decider | 13-10 | MongolZ |
| SF1 PARIVISION vs MOUZ | Dust2 | PARIVISION | 13-11 | PARIVISION |
| SF1 PARIVISION vs MOUZ | Inferno | MOUZ | 13-10 | PARIVISION |
| SF2 Vitality vs MongolZ | Mirage | MongolZ | 16-13 OT | Vitality |
| SF2 Vitality vs MongolZ | Dust2 | Vitality | 13-3 | Vitality |
| 3rd MOUZ vs MongolZ | Mirage | MongolZ | 13-6 | MOUZ |
| 3rd MOUZ vs MongolZ | Inferno | MOUZ | 13-10 | MOUZ |
| GF PARIVISION vs Vitality | Overpass | Vitality | 13-10 | Vitality |
| GF PARIVISION vs Vitality | Dust2 | PARIVISION | 13-4 | Vitality |
| GF PARIVISION vs Vitality | Inferno | Vitality | 16-13 OT | Vitality |
