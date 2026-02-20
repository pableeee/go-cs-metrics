Below is a focused spec doc to implement the **“split first-hit headshot rate by weapon + distance”** improvement (and make it actually useful, not just another table).

---

# Spec: First-Hit Headshot Rate by Weapon + Distance

## 0. Summary

Add a v2 breakdown of **First-Hit Headshot Rate (FHHS)** segmented by:

* **Weapon class** (AK/M4/Deagle/AWP/etc.)
* **Engagement distance** (binned meters)
* (Optional but recommended) **Zoom state** and **movement state** at first shot

This allows you to distinguish:

* “low FHHS because fights are close chaos” vs
* “low FHHS even at mid-range rifling, which indicates pre-aim / head-height alignment issues”

---

## 1. Goals

### Primary Goals

1. Compute **First-Hit Headshot Rate** per **(weapon_bucket, distance_bin)**.
2. Provide confidence-aware reporting (avoid over-interpreting small samples).
3. Identify which bins are your highest-leverage deficits (e.g., AK @ 10–20m).

### Non-Goals (for this iteration)

* Full aim model / ML predictor
* Per-map strat suggestions
* Hitbox occlusion detection
* Per-angle classification (stairs/ramp) — can come later

---

## 2. Definitions

### Duel Window (existing)

A duel win/loss is defined as you already do: from **first-sight** (`m_bSpottedByMask` transition) to **kill/death/disengage**.

### First Hit (existing)

First bullet hit event from attacker → victim within the duel window.

### First-Hit Headshot (new key measure)

A duel is **FHHS-positive** if:

* The attacker’s **first bullet hit** is a hit event, and
* That hit’s hitgroup is **HEAD**.

Report FHHS as:

[
FHHS = \frac{#duels_with_first_hit_head}{#duels_with_first_hit_any_hit}
]

Important: denominator is duels where the first shot actually hit. (See “Alternate denominators” below.)

---

## 3. Distance Computation

### Distance at First Shot (recommended)

Distance should be computed at **first_shot_tick**:

* `attacker_pos = player.origin at first_shot_tick`
* `victim_pos = victim.origin at first_shot_tick`
* `distance = ||victim_pos - attacker_pos||`

Units: CS coordinates are typically in Hammer units; convert to meters via your chosen conversion constant.

#### Conversion Constant

Pick a consistent constant and document it. (Exact conversion is less important than consistency.)
Example approach:

* Use `units_to_meters = 0.01905` if you’ve validated it.
* If not validated: keep “units” internally and label bins as “approx meters” in output.

### Edge cases

* If victim_pos missing at tick: fallback to closest previous tick within ±2 ticks.
* If still missing: exclude duel from distance-segmented view and count it in “Unknown distance”.

---

## 4. Weapon Buckets

### Weapon normalization

Map specific weapon IDs/names to buckets:

**Rifles**

* AK-47
* M4A1-S
* M4A4
* Galil
* FAMAS
* AUG/SG (optional separate “Scoped Rifles” bucket)

**Pistols**

* Deagle
* USP / Glock / P250 etc. (you can aggregate “Pistols” + keep “Deagle” separate)

**Snipers**

* AWP
* SSG08

**SMG / Heavy**

* optional (likely not critical for your questions)

### Bucket rules

* Use bucket first, then optionally a per-weapon breakdown if sample size is large enough.

---

## 5. Distance Binning

### Default bins (good for rifles)

Use bins that match common fight ranges:

* 0–5m
* 5–10m
* 10–15m
* 15–20m
* 20–30m
* 30m+

These bins make mid-range rifle fights visible.

### Alternate bins for pistols/AWP (optional)

* Pistols: 0–5, 5–10, 10–20, 20+
* AWP: 0–10, 10–20, 20–30, 30+

Implementation should support per-weapon bin schemas, but v1 can keep one schema for simplicity.

---

## 6. Metrics to Output per Segment

For each (weapon_bucket, distance_bin):

### Core

* `duels_count` (N)
* `first_hit_any_count` (denominator)
* `first_hit_head_count` (numerator)
* `fhhs_rate`

### Quality / confidence

* `95% CI` (Wilson interval recommended)
* `sample_flag`:

  * `OK` if denom ≥ 50
  * `LOW` if denom 20–49
  * `VERY_LOW` if denom < 20

### Diagnostic add-ons (high value, low effort)

* `median_preshot_correction_deg`
* `median_first_sight_angle_deg`
* `median_exposure_win_ms` and/or `median_exposure_ms` (if segmented by win/loss separately)

These make the segment actionable:

* Low FHHS + low correction = **pre-aim issue**
* Low FHHS + high correction = **reaction / snap issue**

---

## 7. Alternate Denominators (Important)

You should support (at least internally) two FHHS variants:

### FHHS-Hit (recommended default)

As defined earlier: only consider duels where first shot hit.

Pros: isolates aim placement quality when you *did hit*.
Cons: ignores first-shot misses which are also meaningful.

### FHHS-Shot (optional)

[
FHHS_shot = \frac{#duels_first_shot_head}{#duels_first_shot_fired}
]

Where numerator requires the first shot to be a head hit; denominator is all duels with a shot fired.

Pros: penalizes misses; more “truthful”.
Cons: more sensitive to demo hit registration quirks and spray starts.

**Recommendation:** report FHHS-Hit in the main table, keep FHHS-Shot as a debug/advanced metric.

---

## 8. Data Pipeline Changes

### New fields to store per duel (or per duel-event)

Add (if not already stored):

* `first_shot_tick`
* `first_shot_weapon_id`
* `first_shot_hitgroup` (or “none” if miss)
* `attacker_pos_at_first_shot`
* `victim_pos_at_first_shot`
* `distance_at_first_shot`
* `weapon_bucket`
* `distance_bin`

If storage size matters: store only computed scalars:

* `distance_m`
* `distance_bin_idx`
* `weapon_bucket_id`

---

## 9. Report Output

Add a new section under Duel Intelligence Engine:

### Section: “First-Hit Headshot Rate by Weapon + Distance”

**Table format example:**

| Weapon | Distance | N (hits) | FHHS% | 95% CI | Median pre-shot corr | Notes             |
| ------ | -------- | -------: | ----: | -----: | -------------------: | ----------------- |
| AK     | 10–15m   |      128 |   18% | 12–25% |                 1.5° | Low for mid-range |
| AK     | 15–20m   |       94 |   14% |  8–22% |                 1.6° | Priority bin      |
| M4     | 10–15m   |       77 |   16% |  9–26% |                 1.7° | —                 |
| Deagle | 10–20m   |       33 |   42% | 27–59% |                 2.1° | Small sample      |

### Highlight logic (automatic)

Compute “priority bins”:

* High sample (denom ≥ 50)
* FHHS below your overall FHHS by ≥ X percentage points (e.g., 6pp)
* Distance between 10–30m for rifles (your biggest efficiency zone)

Then print a short summary like:

* “AK at 15–20m is your weakest stable bin: 14% FHHS (N=94).”

---

## 10. Validation & Tests

### Unit tests

* Distance computation correctness (known positions)
* Bin assignment edges (exactly 10m goes to 10–15m)
* Weapon bucket mapping coverage

### Integration tests

* Run on 1–2 known demos and sanity-check:

  * FHHS overall matches existing 21% (within tolerance if denom differs)
  * AK mid-range bins have plausible N and rates
  * CI values monotonic with sample size

### Data quality checks

* % duels missing first_shot_tick
* % duels missing positions at first_shot_tick
* % duels with unknown weapon bucket

Fail/alert thresholds:

* Missing positions > 5% → warn
* Unknown weapon bucket > 1% → warn

---

## 11. Performance Considerations

* Computing distance at first_shot_tick is cheap.
* Avoid per-tick distance tracking; only compute on event ticks.
* Precompute weapon bucket + distance bin at ingest time.

---

## 12. Future Extensions (Not in scope, but designed for)

* Split pitch vs yaw error by distance bin
* Movement state segmentation (standing / walking / running)
* Counter-strafe correctness at first shot
* “Expected head height error” on stairs/ramps (requires nav/elevation features)

---

## 13. Acceptance Criteria

This feature is “done” when:

1. Report prints FHHS by (weapon, distance) with N and CI.
2. Low-sample segments are flagged, not over-emphasized.
3. At least one “priority bin” is automatically identified for AK/M4.
4. Output is stable across reruns (deterministic given same demos).

---