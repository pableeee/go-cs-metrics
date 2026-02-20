# 🎯 CS2 Demo Analyzer v2 — Duel Intelligence Engine

**Owner:** bennett
**Dataset Source:** MM demos (GOTV)
**Primary Goal:**
Move from descriptive stats (ADR, K/D, HS%) to **causal duel diagnostics** that explain *why* fights are won or lost.

---

# 1️⃣ Problem Statement

Current system measures:

* Output metrics (ADR, K/D, entry rate)
* Crosshair deviation at first sight
* Utility volume
* Trade counts

It does **not** measure:

* Correction magnitude before first shot
* First bullet efficiency
* Vertical vs horizontal alignment error
* Exposure duration during fights
* Duel structure (isolated vs multi-threat)
* AWP death context

As a result:
We can see inefficiency (e.g., low AK HS%), but not precisely diagnose its mechanical cause.

---

# 2️⃣ Objectives

### Primary Objectives

1. Model duels at a granular level.
2. Quantify correction cost (degrees + time).
3. Separate vertical vs horizontal aim error.
4. Measure bullet efficiency.
5. Measure exposure efficiency.
6. Identify systemic patterns (AWP deaths, overpeeks, etc.).

### Secondary Objectives

* Improve utility impact modeling.
* Improve role-fit validation.
* Enable longitudinal tracking (before/after training).

---

# 3️⃣ System Architecture Overview

### Current Model

Round-level → encounter-level → aggregated stats

### Proposed Model

Round-level
→ Encounter-level
→ Duel-level (NEW CORE ENTITY)
→ Bullet-level (subset of duel)
→ Context classification layer

---

# 4️⃣ Core New Modules

---

## 🔴 Module 1 — Duel Efficiency Engine

### Definition

A duel = first sight tick → one player dies (or disengages for >X ms).

### Metrics

#### 1. First Bullet Headshot Rate

% of duels where:

* First bullet fired
* Hits head
* Leads to kill

**Why:**
Most predictive rifle efficiency stat.

---

#### 2. Angular Correction Before First Shot

Measure:

* Angle at first sight
* Angle at first bullet fired
* Δ pitch
* Δ yaw
* Total angular delta

Outputs:

* Median correction magnitude
* % duels with <2° correction
* % duels with >5° correction

---

#### 3. Bullets-to-Kill Distribution

Track:

* 1 bullet kills
* 2 bullet kills
* 3–5 bullet kills
* 6+ bullet kills

Segment by:

* Weapon
* Map
* Side (CT/T)

---

#### 4. Duel Exposure Time

Measure:

* Time from first sight → kill/death

Segment:

* Wins vs losses
* AWP vs rifle
* Entry vs non-entry

---

### Deliverable

New report section:

```
Duel Efficiency
- First Bullet HS%: X%
- Median Correction: Y°
- Vertical vs Horizontal split
- Median Exposure Time (Win): ...
- Median Exposure Time (Loss): ...
- Bullet Distribution: ...
```

---

## 🔴 Module 2 — Vertical vs Horizontal Deviation Split

### Purpose

Test hypothesis:
Vertical misalignment is primary limiter on complex maps.

### Metrics

At first sight:

* Δ pitch (vertical)
* Δ yaw (horizontal)

At first shot:

* Δ pitch
* Δ yaw

Track:

* Median pitch error
* Median yaw error
* Pitch correction magnitude
* Yaw correction magnitude

Segment by:

* Map
* Weapon
* Elevation-dense fights (stairs/ramp/roof)

---

## 🔴 Module 3 — Overpeek & Exposure Model

### Goals

Detect:

* Wide swings
* Multi-angle exposure
* AWP vulnerability patterns

### Metrics

#### 1. Lateral velocity at first shot

High lateral velocity + death → overpeek indicator.

#### 2. Angle cleared before shot

Degrees of camera rotation before firing.

#### 3. Multi-enemy visibility flag

Were ≥2 enemies visible within X ms?

---

## 🟠 Module 4 — AWP Death Context Classifier

Instead of:

> 31 AWP deaths

Classify into:

* Dry peek (no flash in last 3s)
* No teammate within X units
* Post-kill re-peek
* Holding angle stationary
* Rotating exposed

Output:
% AWP deaths by category.

---

## 🟠 Module 5 — Utility Impact Engine

Replace flash assist count with:

### 1. Flash → Teammate Contact Window

Did teammate engage within 1.5s of flash detonation?

### 2. Utility → Duel Occurrence

Did duel occur within 3s of your HE/molotov?

### 3. Utility Saved Potential

Expected damage lost by unused util (context-aware).

---

# 5️⃣ Data Model Changes

Add new entities:

```plaintext
Duel
- round_id
- encounter_id
- attacker_id
- defender_id
- weapon
- first_sight_tick
- first_shot_tick
- death_tick
- correction_pitch
- correction_yaw
- bullets_to_kill
- exposure_time
- lateral_velocity
- enemies_visible_count
```

---

# 6️⃣ KPIs for Success

Tool is successful if:

* Can explain AK HS% gap mechanistically.
* Can correlate vertical deviation with Nuke inefficiency.
* Can identify primary AWP death pattern.
* Can quantify improvement after training cycles.

---

# 7️⃣ Implementation Phases

---

## Phase 1 — Core Duel Engine (2–3 weeks)

* Duel segmentation
* Correction magnitude
* First bullet HS
* Bullets-to-kill
* Exposure time

Most value.

---

## Phase 2 — Vertical/Horizontal Split (1–2 weeks)

* Pitch/yaw separation
* Map segmentation
* Elevation-based tagging

---

## Phase 3 — Context Classifiers (2–4 weeks)

* AWP death categories
* Overpeek detection
* Multi-threat detection
* Utility correlation

---

# 8️⃣ Risks & Limitations

* Spotted flag timing inaccuracies (1–2 tick drift)
* Demo tick rate variance
* Multi-enemy chaos misclassification
* Need robust duel segmentation logic

Mitigation:

* Use medians, not means.
* Use confidence thresholds.
* Flag low-sample maps.

---

# 9️⃣ Long-Term Extensions

* Machine learning duel outcome predictor
* Duel Efficiency Score (weighted composite metric)
* Map-specific vertical heatmaps
* Player vs player style matching

---

# 🔥 Final Strategic Note

Right now your tool tells you:

> “You deal a lot of damage.”

v2 will tell you:

> “You required 3.7 bullets per rifle kill because your vertical correction median is 2.3° on Nuke ramps.”

That’s the leap.

---

If you want next, I can:

* Draft a **Duel Efficiency Score formula**
* Or help you design a **clean event pipeline**
* Or turn this into a proper internal RFC-style engineering doc

This project is legitimately becoming interesting.
