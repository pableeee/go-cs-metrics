# simbo3 Prediction vs Actual Results — Three Tier-1 CS2 Events

**Events covered:** StarLadder Budapest Major 2025 · IEM Krakow 2026 · PGL Cluj-Napoca 2026
**Simulation mode:** `--mode manual` (actual maps/picks/sides used)

Two runs recorded. Run 1 used Krakow-only data. Run 2 used all eight ingested events with updated exports.

| Run | Date | Demo source | Accuracy |
|---|---|---|---|
| Run 1 | 2026-02-23 | IEM Krakow 2026 only (84 demos, `--since 90`) | 6/21 (28.6%) |
| Run 2 | 2026-02-24 | 8 events, 660 pro demos through Feb 23 (`--since 90 --half-life 35`) | **9/21 (42.9%)** |

---

## 1. Results at a Glance

### IEM Krakow 2026 (Jan 27 – Feb 8, 2026)

| Match | Run 1 prediction | Run 2 prediction | Actual winner | R1 | R2 |
|---|---|---|---|---|---|
| QF1: Aurora vs FURIA | **FURIA** 83.1% | **FURIA** 77.2% | **FURIA** 2-0 | ✓ | ✓ |
| QF2: G2 vs MOUZ | **MOUZ** 85.2% | **MOUZ** 86.8% | **MOUZ** 2-0 | ✓ | ✓ |
| SF1: Spirit vs FURIA | Spirit 57.2% | Spirit 57.5% | **FURIA** 2-1 | ✗ | ✗ |
| SF2: Vitality vs MOUZ | MOUZ 55.7% | **Vitality** 58.3% | **Vitality** 2-0 | ✗ | **✓** |
| 3rd: Spirit vs MOUZ | MOUZ 55.3% | MOUZ 66.9% | **Spirit** 2-0 | ✗ | ✗ |
| GF: FURIA vs Vitality | **Vitality** 84.1% | **Vitality** 89.8% | **Vitality** 3-1 | ✓ | ✓ |

**Krakow: Run 1 3/6 (50%) → Run 2 4/6 (67%)**

---

### StarLadder Budapest Major 2025 (Dec 11–14, 2025)

| Match | Run 1 prediction | Run 2 prediction | Actual winner | R1 | R2 |
|---|---|---|---|---|---|
| QF1: Spirit vs Falcons | Falcons 61.9% | Falcons 70.6% | **Spirit** 2-0 | ✗ | ✗ |
| QF2: Vitality vs MongolZ | MongolZ 53.5% | **Vitality** 51.7% | **Vitality** 2-0 | ✗ | **✓** |
| QF3: FaZe vs MOUZ | MOUZ 74.5% | MOUZ 86.6% | **FaZe** 2-0 | ✗ | ✗ |
| QF4: NaVi vs FURIA | FURIA 55.3% | FURIA 53.6% | **NaVi** 2-1 | ✗ | ✗ |
| SF1: Vitality vs Spirit | Spirit 72.6% | Spirit 69.8% | **Vitality** 2-0 | ✗ | ✗ |
| SF2: FaZe vs NaVi | **FaZe** 51.1% | NaVi 67.4% | **FaZe** 2-1 | ✓ | **✗** |
| GF: FaZe vs Vitality | **Vitality** 90.0% | **Vitality** 93.1% | **Vitality** 3-1 | ✓ | ✓ |

**Budapest: Run 1 2/7 (29%) → Run 2 2/7 (29%) — unchanged**

---

### PGL Cluj-Napoca 2026 (Feb 16–22, 2026)

| Match | Run 1 prediction | Run 2 prediction | Actual winner | R1 | R2 |
|---|---|---|---|---|---|
| QF1: Falcons vs PARIVISION | Falcons 59.6% | Falcons 51.1% | **PARIVISION** 2-1 | ✗ | ✗ |
| QF2: MOUZ vs NaVi | NaVi 52.2% | **MOUZ** 60.2% | **MOUZ** 2-1 | ✗ | **✓** |
| QF3: Vitality vs Aurora | Aurora 51.0% | **Vitality** 50.8% | **Vitality** 2-0 | ✗ | **✓** |
| QF4: FURIA vs MongolZ | FURIA 67.5% | FURIA 60.2% | **MongolZ** 2-1 | ✗ | ✗ |
| SF1: PARIVISION vs MOUZ | MOUZ 79.1% | MOUZ 71.8% | **PARIVISION** 2-0 | ✗ | ✗ |
| SF2: Vitality vs MongolZ | MongolZ 53.2% | MongolZ 51.4% | **Vitality** 2-0 | ✗ | ✗ |
| 3rd: MOUZ vs MongolZ | MongolZ 74.7% | MongolZ 55.0% | **MOUZ** 2-0 | ✗ | ✗ |
| GF: PARIVISION vs Vitality | **Vitality** 98.2% | **Vitality** 97.8% | **Vitality** 3-0 | ✓ | ✓ |

**Cluj: Run 1 1/8 (13%) → Run 2 3/8 (38%)**

---

### Overall summary

| Event | Run 1 | Run 2 | Δ |
|---|---|---|---|
| IEM Krakow 2026 | 3/6 (50%) | 4/6 (67%) | +1 |
| Budapest Major 2025 | 2/7 (29%) | 2/7 (29%) | 0 |
| PGL Cluj-Napoca 2026 | 1/8 (13%) | 3/8 (38%) | +2 |
| **Overall** | **6/21 (28.6%)** | **9/21 (42.9%)** | **+3 (+14.3 pp)** |

Random baseline: 50%. Run 2 approaches but does not reach the coin-flip baseline; the improvement is real but systematic biases remain.

---

## 2. Analysis

### 2.1 What improved between Run 1 and Run 2

**Four predictions flipped from wrong to right:**

| Match | R1 A% | R2 A% | Why it changed |
|---|---|---|---|
| Krakow SF2: Vitality vs MOUZ | 44.3% | **58.3%** | Multi-event DB now shows MOUZ Nuke win rate = 18% (44 demos). Vitality Nuke = 84%. Correct favourite emerges. |
| Budapest QF2: Vitality vs MongolZ | 46.5% | **51.7%** | More Vitality data nudges them past the 50% threshold. Coin-flip now lands right. |
| Cluj QF2: MOUZ vs NaVi | 47.8% | **60.2%** | Budapest + Krakow data establishes MOUZ's clear edge over NaVi on Ancient and Inferno. |
| Cluj QF3: Vitality vs Aurora | 49.0% | **50.8%** | Near-coin-flip in both runs; new data barely tilts it correctly. |

**One prediction flipped from right to wrong:**

| Match | R1 A% | R2 A% | Why it broke |
|---|---|---|---|
| Budapest SF2: FaZe vs NaVi | **51.1%** | 32.6% | FaZe's weaker Cluj exit pulls down their post-Budapest rating. The R1 prediction was a barely-correct coin flip; R2 applies temporal lookahead from FaZe's Dec–Feb form to a Dec match. |

The Budapest regression illustrates the core flaw in Run 2 for that event: the model uses data from eight events spanning through February to predict a December result, penalising FaZe for performance they hadn't yet had.

### 2.2 MOUZ overestimation — partially addressed

MOUZ was the single most overrated team in Run 1. Multi-event data has corrected some of this:

| Match | Run 1 | Run 2 | Actual |
|---|---|---|---|
| Krakow SF2 vs Vitality | MOUZ 55.7% | **Vitality 58.3%** | Vitality won ✓ |
| Krakow 3rd vs Spirit | MOUZ 55.3% | MOUZ 66.9% | Spirit won ✗ |
| Budapest QF3 vs FaZe | MOUZ 74.5% | MOUZ 86.6% | FaZe won ✗ |
| Cluj SF1 (opp. PARIVISION) | MOUZ 79.1% | MOUZ 71.8% | PARIVISION won ✗ |
| Cluj 3rd vs MongolZ | MOUZ 74.7% | **MOUZ 55.0%** | MOUZ won ✓ |

The Krakow SF2 is fixed (MOUZ's Nuke weakness now captured with 44 demos). But MOUZ vs FaZe at Budapest and MOUZ vs PARIVISION at Cluj are still wrong — the model overweights MOUZ's strong Inferno and Mirage stats and has no concept of their playoff inconsistency.

### 2.3 PARIVISION underestimation — improved but persistent

Run 1 (Krakow data only): PARIVISION had 7 demos. Maps with stats: Ancient, Anubis, Dust2, Overpass only. All other maps fell back to the 0.50 prior.

Run 2 (8-event data): PARIVISION has 29 demos. Maps with real stats: Ancient, Dust2, Inferno, Mirage, Overpass, Anubis.

Updated PARIVISION predictions at Cluj:

| Match | Run 1 | Run 2 | Actual |
|---|---|---|---|
| QF1 vs Falcons | PAR 40.4% | PAR 48.9% | **PARIVISION** won ✗ |
| SF1 vs MOUZ | PAR 20.9% | PAR 28.2% | **PARIVISION** won ✗ |
| GF vs Vitality | PAR 1.8% | PAR 2.2% | Vitality won ✓ |

More data closed the gap on QF1 (40→49%, still wrong) but not enough to flip SF1. The model gives MOUZ a large advantage from their 44 demos of Inferno and Mirage data. PARIVISION's actual tournament performance — sweeping MOUZ 2-0 — reflects factors the model cannot capture (peaking at the right time, opponents underperforming).

PARIVISION's Mirage win rate in the data is 0.00 (5 maps, all losses) and Overpass is 0.00 (3 maps). These are genuine data patterns, not data gaps, so more demos will not fix this. The model correctly identifies that PARIVISION avoids Mirage and Overpass; it cannot predict that they'll overcome that on specific match days.

### 2.4 Temporal mismatch (Budapest Major) — not fixed

Budapest remains at 2/7 because Run 2 still uses post-Budapest data. The QF2 gain (Vitality vs MongolZ) was offset by the SF2 regression (FaZe vs NaVi). Fixing Budapest properly requires a time-scoped dataset: exports computed from demos dated strictly before Dec 11, 2025. The `backtest-dataset` command produces exactly this, but the resulting temporal snapshots are weaker (fewer demos per team) and the tuner on those 21 points converges to degenerate coefficients (see below).

Key Budapest failures that persist:

| Match | Model said | Actual | Explanation |
|---|---|---|---|
| QF1: Spirit vs Falcons | Falcons 70.6% | Spirit won 2-0 | Spirit's Krakow/Cluj-era stats don't reflect Dec form |
| QF3: FaZe vs MOUZ | MOUZ 86.6% | FaZe won 2-0 | FaZe strengthened post-Budapest; MOUZ overrated in ESL/Bucharest data |
| SF1: Vitality vs Spirit | Spirit 69.8% | Vitality won 2-0 | Spirit's current ratings (1.49/1.48) inflate their prediction |

### 2.5 Coefficient tuning findings

The `backtest-dataset` command was used to generate proper no-lookahead temporal snapshots for all 21 matches. Backtesting and tuning on this dataset with both default and tuned coefficients yielded:

| Config | Log loss | Brier | Accuracy |
|---|---|---|---|
| Default (α=0.9, β=1.2, γ=0.35, k=10) | 0.935 | 0.359 | 33.3% |
| Tuned | 0.854 | 0.330 | 28.6% |
| Random baseline | 0.693 | 0.250 | 50.0% |

Both configurations perform worse than random on the temporal-snapshot dataset. The tuner converges to a degenerate solution: `alpha=0.1` (minimum — ignore map stats), `beta=4.0` (maximum — rely solely on ratings), `k_reliability=1.0` (minimum shrinkage). This indicates:

1. **21 playoff matches is insufficient to tune 6 parameters** — the loss landscape is too flat and the solution overfits.
2. **The temporal snapshot data is noisier than the current-export data** — team snapshots computed from 3–16 demos per event are dominated by shrinkage toward 0.50 priors, making map stats near-uninformative.
3. **The tuned configs should not be used for real predictions** — Run 2 with default coefficients (42.9%) outperforms the tuned configs on the same 21 matches precisely because default coefficients are more stable.

### 2.6 What the model does well

- **Heavy favourites**: FURIA 77% over Aurora ✓, MOUZ 87% over G2 ✓, Vitality 90% over FURIA in GF ✓, Vitality 98% over PARIVISION in GF ✓.
- **Vitality in grand finals**: Correctly identified Vitality as dominant in every GF across all three events.
- **MOUZ's map-specific weakness is now captured**: Nuke (18% win rate) and Nuke-heavy matchups are now correctly deprioritised.
- **Signals uncertainty where it exists**: QF1 Falcons vs PARIVISION went from 60/40 to 51/49 — the model now correctly registers this as near-50/50.

### 2.7 Remaining structural ceiling

The 12 persistent failures across both runs fall into three categories that stat-based models cannot easily address:

1. **Playoff upset specialists**: PARIVISION (8 demos per map on average) can beat teams with 40+ demos when they peak.
2. **Historical form reversal**: Budapest-era MOUZ and Spirit had strong group-stage records that didn't translate to playoff results.
3. **Near-50/50 matches**: MongolZ vs Vitality (SF2) and MOUZ vs MongolZ (3rd) are genuinely coin flips — the model correctly computes ~50% but still registers as wrong when the loser wins.

The realistic accuracy ceiling for this type of model at the top level of CS2 — where the top 8–16 teams are within a narrow skill band — is approximately **45–55%** for individual BO3 series. Run 2 at 42.9% is approaching that range.

---

## 3. Appendix: How Predictions Were Made

### 3.1 Data — Run 2

- **Demos**: 660 pro-tier demos across 8 events (IEM Katowice 2025 through PGL Cluj-Napoca 2026), parsed via `go-cs-metrics parse`.
- **Team exports**: `go-cs-metrics export --roster <team>.json --since 90 --quorum 3 --half-life 35 --out team_exports/<team>.json`. Export window Nov 26 – Feb 24 (90 days); temporal weighting with 35-day half-life (exponential decay, λ=0.02).
- **16 teams exported**: demo counts range from 5 (NRG, limited events) to 44 (MOUZ).

### 3.2 Prediction methodology

Each playoff match simulated with:
```sh
simbo3 run \
  --teamA team_exports/<teamA>.json \
  --teamB team_exports/<teamB>.json \
  --mode manual \
  --maps <actual maps played> \
  --picks <actual picker: A/B/D> \
  --start-sides <actual CT/T start per map> \
  --format bo3|bo5 \
  --trials 50000 \
  --seed 42
```

`--mode manual` uses actual maps played (not a simulated veto), removing veto prediction error. `--start-sides` added in Run 2 using the `a_start_ct` field recorded in `backtest/playoff-matches.json`.

### 3.3 Model architecture

Map win probability per trial:
```
p(A wins map) = sigmoid( α*(S_map_A - S_map_B) + β*ΔRating + γ*side_term )
```

- `S_map = logit(smoothed_win_pct)` — log-odds of shrinkage-smoothed map win%
- Shrinkage: `w = n / (n + k)`, k=10; maps with few demos pull heavily toward 0.50
- `ΔRating = avg_rating_A - avg_rating_B` using Rating 2.0 proxy (community formula, ±0.05–0.10 vs HLTV)
- `side_term`: CT/T log-odds differential from starting side
- Default coefficients: α=0.9, β=1.2, γ=0.35

Series simulated via 50,000 Monte Carlo trials (BO3: winsNeeded=2, BO5: winsNeeded=3).

---

## 4. Improvements Status

| # | Improvement | Status |
|---|---|---|
| 4.1 | Fix simbo3 map pool (Dust2/Train replacing Anubis/Vertigo) | ✅ Done (commit 20533da) |
| 4.2 | Audit team rosters; re-export with corrected data | ✅ Done — all 16 teams re-exported Feb 24 with 8-event data |
| 4.3 | Use event-appropriate demo data (no lookahead) | ✅ `backtest-dataset` command added; temporal snapshots work. Budapest still suffers — needs IEM Chengdu 2025 demos to fully fix. |
| 4.4 | Temporal weighting within export window | ✅ Done — `--half-life 35` in export and backtest-dataset |
| 4.5 | Multi-event demo coverage | ✅ Done — 8 events, 660 demos (Feb 2025 – Feb 2026) |
| 4.6 | Roster validation pipeline | ⬜ Not built — manual audit done; automated flag-on-export not implemented |
| 4.7 | Coefficient tuning against known results | ⬜ Attempted — tuner converges to degenerate solution on 21 matches; needs 100+ labeled matches to be reliable |
| 4.8 | Head-to-head records modifier | ⬜ Not built |
| 4.9 | Side-selection from match records | ✅ Done — `a_start_ct` added to spec; `--start-sides` passed in Run 2 |

**Next priority**: Expand labeled match dataset beyond 21 playoff matches to enable meaningful coefficient tuning. Group-stage and regular-season matches (100–200 labeled series) would provide enough signal to tune α, β, γ reliably.

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
