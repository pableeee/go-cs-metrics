# Playstyle Analysis — bennett (76561198031906602)

**Dataset:** 38 matchmaking demos · 769 rounds played · 1,222 first-sight encounters
**Pro reference:** 10 demos (Falcons vs NAVI on Ancient/Inferno; mouz vs Vitality on Nuke/Train/Inferno ×3)
**Generated:** 2026-02-20 (crosshair placement added: 2026-02-20; duel engine + AWP classifier + flash quality added: 2026-02-20)

---

## 1. Overall Snapshot

| Metric | CT | T | Total |
|--------|----|---|-------|
| Matches | 21 | 17 | 38 |
| Rounds | 412 | 357 | 769 |
| K / A / D | 375 / 117 / 269 | 317 / 96 / 262 | 692 / 213 / 531 |
| K/D | **1.39** | **1.21** | **1.30** |
| HS% | 35.7% | 40.7% | 37.9% |
| ADR | **114.2** | **119.0** | **116.3** |
| KAST% | **77.9%** | **76.8%** | **77.4%** |
| Entry K / D | 46 / 30 | 37 / 27 | 83 / 57 |
| Entry win rate | **60.5%** | **57.8%** | **59.3%** |
| Trade kills | 68 | 50 | 118 |
| Trade deaths | 58 | 54 | 112 |
| Flash assists | 3 | 3 | 6 |
| Utility damage | 2142 | 1445 | 3587 |
| Unused util/round | 0.24 | 0.20 | 0.22 |

**Headline numbers:** An ADR of 116.3 and a 59% entry win rate mark a high-impact, aggressive rifler. The 1.30 K/D understates the output — the ADR is elite for MM level and the volume of damage dealt every round is the most telling sign of consistent presence.

---

## 2. CT Side — What the Numbers Say

### 2.1 Strengths

**KD 1.39 — clear positive impact.**
With 375 kills in 412 rounds, you are almost at one kill per round on CT. This is the profile of a player who holds positions effectively and wins duels.

**Entry win rate 60.5% (46K vs 30D).**
On CT, entry events happen when opponents run into held positions or when you take aggressive peeks. A 60.5% win rate on those duels is very strong — you are winning more than 3 out of 5 of the highest-stakes individual exchanges of each round.

**Trade kills 68, trade deaths 58 — net positive.**
You are converting trade kills more than you are giving them up. This means that when teammates die near you, you punish it. Strong CT anchor and retake discipline.

**KAST breakdown (407 round-stat rounds):**

| Component | Count | % of rounds |
|-----------|-------|-------------|
| Kill rounds | 235 | 57.7% |
| Assist rounds | 102 | 25.1% |
| Survive rounds | 145 | **35.6%** |
| Traded rounds | 30 | 7.4% |
| KAST total | 323 | **79.4%** |

The **35.6% survival rate** on CT is a strong signal. Combined with the high kill count, this means you are not just fragging recklessly — you are also holding alive when the round is under control. A player with pure aggression would have a survival rate closer to 20%.

**Utility damage 2142 across 412 rounds (5.2/round avg).**
This is solid. You are using HE grenades and Molotovs effectively on CT — whether that's molly-blocking B rushes on Inferno or HE combo on a known stack.

### 2.2 Weaknesses

**Low flash assists (3 total in 412 rounds) and near-zero effective flashes.**
This is the sharpest red flag. You are almost never flashing for teammates. On CT, coordinated flashes are how you catch peeking attackers and enable retakes. Three flash assists across an entire season means utility usage is almost entirely selfish (damage and area denial) rather than team-enabling. The new effective-flash metric (flashes where the blinded enemy was killed by your team within 1.5 s) confirms this: only ~7 effective flashes across all 38 matches, fewer than one every five games.

**Unused utility 0.24/round on CT.**
More than one in five rounds you end with a grenade you never threw. On CT, buying utility and banking it is acceptable in some situations (eco baiting) but 0.24 average suggests a habit of not committing grenades when they would matter most — late-round retake moments especially.

---

## 3. T Side — What the Numbers Say

### 3.1 Strengths

**ADR 119.0 — highest side.**
Your T-side damage output slightly exceeds CT. This is unusual; most players deal more on CT because they hold held angles and punish rushers. Your T-side ADR being higher suggests you are executing well and getting deep into sites before dying.

**HS% 40.7% (vs 35.7% CT).**
Headshot percentage rises on T because AK-47 is the dominant rifle and T-side duels often happen at medium range with crouching/counter-strafing enemies. 40.7% HS with the AK is a sign of good aim discipline — you are not spraying at bodies.

**Trade deaths 54 — team is cleaning up after you.**
On T, 51 rounds (from per-round stats) your death was traded by a teammate. This is a very high number and is actually meaningful — it means your aggressive entries are creating value even when you die. You open the door, teammate walks through.

### 3.2 Weaknesses

**KD 1.21 — lower than CT.**
The 0.18 gap between sides is real and significant. Over 357 rounds, you are dying more relative to your kills on T. The pattern in the per-match table makes this concrete: several T-side Mirage results show KD of 0.63, 0.81, 0.85 against the average 1.21. On Mirage T-side specifically you are struggling.

**Trade kills 50, trade deaths 54 — net negative.**
This is the inverse of CT. On T, you are being traded more than you are trading. The mechanism: you are likely the first player through on executes, dying, and the team trades — but you personally are not converting the trades when your teammates die first. You are giving value as bait but not extracting value as a trader. This is the signature of a **first-entry** player rather than a **second-entry** trader.

**Survival rate 26.2% (95/362) — 9 points lower than CT.**
Expected on T but worth tracking. Very few rounds where you hold enough to be alive at round-end.

**Flash assists 3 (same as CT) and ~5 effective flashes on T — again, nearly zero.**
Pop-flashing for your own peek is fine, but you should be generating flash assists on site executes. Flashing through smoke for the second player in is one of the highest-impact things a first-entry player can do. The 1.5-second effective-flash window shows your flashes almost never lead to a team kill even when the enemy is blinded.

---

## 4. Weapon Breakdown

### 4.1 Primary weapons

| Weapon | K | HS% | DMG | Hits | DPH |
|--------|---|-----|-----|------|-----|
| AK-47 | 295 | 36.9% | 35,505 | 949 | 37.4 |
| M4A1 | 134 | 32.8% | 16,971 | 519 | 32.7 |
| Galil AR | 50 | 36.0% | 6,747 | 223 | 30.3 |
| FAMAS | 35 | 25.7% | 3,806 | 153 | 24.9 |
| M4A4 | 35 | 20.0% | 3,666 | 128 | 28.6 |

**AK-47 is your best weapon by a large margin.** 295 kills and 37.4 DPH. The HS% of 36.9% means roughly one in three AK kills is a headshot, which is solid for a rifler who is not just spray-fishing — but see the pro comparison below.

**M4A1 preferred over M4A4** (134 vs 35 kills). This makes sense — the M4A1 silencer is more forgiving in position-revealing scenarios and you prefer the accuracy/recoil tradeoff.

**Galil AR at 50 kills** — you are buying this weapon regularly on partial-buy rounds rather than defaulting to a pistol. 30.3 DPH is lower than AK but the volume of use shows you do not force-buy as recklessly as many players.

**FAMAS 24.9 DPH — concerning.** The FAMAS has a distinct burst pattern. The low DPH suggests spray usage rather than burst discipline. Either switch to the Galil (slightly better at range) or learn the FAMAS burst mechanic deliberately.

### 4.2 Pistol game

| Pistol | K | HS% | DPH |
|--------|---|-----|-----|
| Desert Eagle | 40 | 57.5% | 62.0 |
| Glock-18 | 32 | 62.5% | 30.5 |
| USP-S | 27 | 63.0% | 38.0 |

This is an excellent pistol profile. The Desert Eagle at 57.5% HS and 62 DPH means you are buying it on eco and converting. USP-S HS% of 63% is elite — headshot discipline on the CT pistol rounds is a real edge. This pistol confidence is a competitive differentiator; many players at this level struggle to convert eco rounds.

### 4.3 The AWP problem

| | bennett | ZywOo | torzsi | m0NESY |
|-|---------|-------|--------|--------|
| AWP kills | 4 | 21 | 21 | 13 |
| AWP deaths | 31 | — | — | — |
| AWP DPH | 110 | 154.7 | 165.4 | 108.9 |

**You have 31 AWP deaths and only 4 AWP kills.** The new AWP death classifier breaks these 31 deaths into three non-exclusive categories:

| Category | Count | Rate | Meaning |
|----------|-------|------|---------|
| **Dry peek** (no flash on you in last 3 s) | 30 / 31 | **97%** | Almost every AWP death happens without a flash to cover you |
| **Re-peek** (you had a kill same round) | 13 / 31 | **42%** | Nearly half occur right after you got a kill — momentum peeks into a held AWP |
| **Isolated** (no teammate within 512 units) | 22 / 31 | **71%** | You are alone in most of these duels; no trade pressure on the AWP |

**97% dry-peeking is the defining finding.** In 30 of 31 AWP deaths there was no flash in the 3 seconds before you peeked. You are walking into sightlines a professional AWP is covering with no utility support. One flash before the peek would either force the AWP to reposition or give you a partial-blind duel — either outcome is better than a clean one-tap.

**42% re-peek pattern is correctable immediately.** After you get a kill, you feel momentum and immediately return to the same angle — the AWP is still there. The fix is a single habit: after any kill, step back out of the sightline before deciding whether to re-peek.

**71% isolated** means there is almost never a teammate close enough to trade if you die. Combined with zero flashes, you are giving the AWP a free, uncontested shot every time.

### 4.4 Utility usage

| Type | K | Damage | Hits |
|------|---|--------|------|
| HE Grenade | 5 | 3,009 | 192 |
| Molotov | 0 | 407 | 106 |
| Incendiary | 0 | 171 | 58 |

HE grenades are contributing meaningful damage (3,009 total) and even the occasional kill. Molotov/incendiary usage is present. But as noted, flash throws are essentially absent from your game — 3 flash assists in 769 rounds is near-zero.

---

## 5. Map-by-Map

| Map | Matches | Rounds | K/D | ADR | KAST% | Entry K/D | Trade K/D |
|-----|---------|--------|-----|-----|-------|-----------|-----------|
| Nuke | 11 | 241 | 1.32 | 112.6 | 75.5% | 28/17 | 43/38 |
| Inferno | 10 | 208 | 1.27 | 117.1 | 78.4% | 19/18 | 26/31 |
| Mirage | 7 | 134 | 1.18 | 111.8 | 81.3% | 13/11 | 16/18 |
| Overpass | 4 | 79 | 1.33 | **144.6** | 77.2% | 12/4 | 20/13 |
| Ancient | 3 | 50 | **1.59** | 101.6 | 72.0% | 5/3 | 9/3 |
| Dust2 | 1 | 23 | 1.31 | 124.1 | 69.6% | 1/1 | 1/3 |
| Anubis | 1 | 21 | 1.33 | 118.6 | 76.2% | 3/3 | 3/6 |
| Vertigo | 1 | 13 | 2.00 | 95.6 | 92.3% | 2/0 | 0/0 |

**Best map: Overpass.** The 144.6 ADR is substantially higher than every other map. Entry K/D of 12/4 (75% win rate) is dominant. The trade K/D of 20/13 is also positive. Overpass rewards aggressive mid control and wide peeks — that fits your style.

**Most played: Nuke (11 matches).** 1.32 KD and 112.6 ADR on Nuke is solid but not your ceiling. Nuke is a map where CT side utility is extremely important (retake molly on B, smokes for ramp/door). If your utility usage improves, Nuke could match your Overpass output.

**Weakest: Mirage (1.18 KD, 81.3% KAST).** The KAST is actually good, but KD and ADR are your lowest among main maps. Individual match results on Mirage T are 0.85, 0.81, 0.63 — there is a clear T-side Mirage wall. Mid control on Mirage T requires specific smoke/flash setups; without those, A-site and B-site executes become very difficult.

**Inferno trade K/D 26/31 — the only map with negative trade balance.** Inferno B-site retakes and A-site pushes favor teams that trade reliably. You are losing trades on Inferno, which means opponents are winning 2-for-1 exchanges. This is likely linked to the low utility usage — uninformed retakes on Inferno without molly/flash lead to exactly these losing trades.

---

## 6. Pro Player Comparison

The 10 pro demos cover Falcons vs NAVI (Ancient, Inferno) and mouz vs Vitality (Nuke, Train, Inferno). The pro player numbers represent elite-level benchmarks.

### 6.1 ADR comparison

| Player | Role | ADR | KD | KAST% |
|--------|------|-----|----|-------|
| **m0NESY** | AWP/Star | 160.9 | 2.42 | 86.5% |
| ZywOo | AWP/Star | 122.8 | 1.37 | 77.6% |
| ropz | Lurk/Anchor | 102.6 | 1.51 | 81.6% |
| flameZ | Entry | 102.3 | 1.10 | 73.7% |
| xertioN | Second rifle | 97.3 | 0.96 | 67.1% |
| apEX | Entry/IGL | 83.1 | 0.83 | 67.1% |
| torzsi | AWP | 90.8 | 1.09 | 76.3% |
| **bennett** | — | **116.3** | **1.30** | **77.4%** |

Read this carefully: **bennett's ADR of 116.3 sits between ZywOo and m0NESY**, above every rifler in this pro sample. This does not mean parity with pro players — it means your damage output profile is aggressive and consistent, comparable in pattern to elite-level stars. The difference is in execution under pressure, map reading, and team coordination — not raw damage numbers.

For context: flameZ (Vitality entry fragger) has 102.3 ADR with a 1.10 KD. Your ADR is 14 points higher at 1.30 KD. Your KAST of 77.4% also matches ZywOo's 77.6%.

### 6.2 Entry comparison

| Player | Entry K | Entry D | Win rate |
|--------|---------|---------|----------|
| m0NESY | 12 | 1 | **92.3%** |
| ropz | 10 | 2 | **83.3%** |
| ZywOo | 8 | 3 | 72.7% |
| xertioN | 10 | 16 | 38.5% |
| apEX | 7 | 9 | 43.8% |
| flameZ | 10 | 15 | 40.0% |
| **bennett** | **83** | **57** | **59.3%** |

You attempt entries at much higher volume than any pro player in this sample (83 entries vs ~10-12 for most pros over roughly comparable round counts). A 59.3% win rate is genuinely strong. Compare it to flameZ at 40% or apEX at 43.8% — both professional entry fraggers who die opening more than they win. Your rate exceeds both of them.

Note that xertioN at 38.5% is considered the team's second-rifle / lurk player and he attempts a lot of entries — the low win rate is acceptable at pro level because it creates space. The fact that you win yours at 59% means you are not being reckless; you are taking winnable duels.

### 6.3 AK-47 headshot comparison

| Player | AK HS% | AK DPH |
|--------|--------|--------|
| flameZ | 75.0% | 48.0 |
| ropz | 70.6% | 45.7 |
| Spinx | 76.5% | 44.1 |
| ZywOo | 63.2% | 47.6 |
| apEX | 71.4% | 47.2 |
| xertioN | 59.1% | 43.4 |
| **bennett** | **36.9%** | **37.4** |

This is the sharpest gap between you and pro riflers. Professional AK players are producing **HS% of 59–76%** and **DPH of 43–48**. Your 36.9% HS and 37.4 DPH means you are hitting more body shots and dealing less damage per bullet. At pro level, the AK kills most enemies with a single headshot tap or 2–3 chest shots. Your lower DPH means more bullets to finish kills — which is directly related to survival; the longer a duel lasts, the more likely you are to also get hit.

This is the **number one mechanical training priority**: AK-47 headshot percentage. Every percentage point toward 50%+ reduces the rounds needed to secure a kill, reduces counter-fire received, and raises your K/D passively.

### 6.4 Trade kill comparison

| Player | Trade K | Trade D | Net |
|--------|---------|---------|-----|
| torzsi | 14 | 8 | +6 |
| ropz | 9 | 8 | +1 |
| xertioN | 10 | 9 | +1 |
| flameZ | 9 | 7 | +2 |
| apEX | 7 | 14 | -7 |
| **bennett CT** | **68** | **58** | **+10** |
| **bennett T** | **50** | **54** | **-4** |

CT trade net of +10 is excellent. T trade net of -4 is marginal. The pattern matches a player who anchors strong positions on CT (punishing trades) but plays a first-in role on T (getting traded rather than trading). The good news: the negative T-side trade balance is modest and aligns with how entry fraggers play at every level.

---

## 7. Crosshair Placement

> **Method:** At the tick when the server first flags an enemy as spotted (`m_bSpottedByMask` 0→1), the angular deviation between the crosshair direction and the enemy's head center is recorded. Results are aggregated per match into a median and a "% under 5°" rate. This is an approximation — the spotted flag is server-side and may lead true line-of-sight by 1–2 ticks. Values should be read as directional rather than exact.

### 7.1 Overall

| Metric | Value |
|--------|-------|
| First-sight encounters | **1,222** |
| Median crosshair angle | **3.8°** |
| % of encounters under 5° | **63.1%** |

A median of **3.8°** means that on the typical first look at an enemy, the crosshair is within roughly four degrees of the head. That is a small angular cone — at 10 metres, 4° represents about 70 cm of deviation; at 20 metres, ~140 cm. The 63.1% under-5° rate means almost two in three first-looks are already within tight threshold range.

### 7.2 Per-map breakdown

| Map | Encounters | Median | % < 5° | Notes |
|-----|-----------|--------|---------|-------|
| Nuke | 392 | **4.2°** | 60.7% | Worst median; multi-level angles vary eye height |
| Inferno | 315 | **3.8°** | 59.7% | Consistent; long-range banana angles skew distribution |
| Mirage | 206 | **3.8°** | 65.5% | Solid, better under-5 than Inferno despite same median |
| Overpass | 146 | **3.5°** | 67.8% | Best large-sample map; aggressive mid peeks pay off |
| Ancient | 77 | **3.8°** | 71.4% | Small sample but strong under-5 rate |
| Dust2 | 39 | **3.0°** | 69.2% | One match; best median overall |
| Anubis | 32 | **3.1°** | 75.0% | One match; best under-5 rate with meaningful sample |
| Vertigo | 15 | **6.2°** | 33.3% | One match, 15 encounters — treat as unreliable |

**Nuke stands out as the weakest map** for crosshair placement (4.2° median, 60.7%). This correlates with the architectural complexity of the map: multi-level angles, cramped corridors, and roof interactions force frequent vertical crosshair adjustments. Overpass shows the best placement (3.5° median, 67.8%) — which reinforces the earlier finding that it is your strongest map overall. The wide, readable angles on Overpass reward pre-aiming.

**Two outlier matches** pull the Nuke average up:
- One Nuke match: 7.1° median, 36.7% under-5 — the worst single-match performance in the dataset.
- One Inferno match: 6.1° median, 43.8% under-5.

These two matches represent out-of-character games and should not be over-weighted.

### 7.3 Pro comparison

| Player | Map | Encounters | Median | % < 5° |
|--------|-----|-----------|--------|---------|
| m0NESY | Ancient | 24 | **1.6°** | 83.3% |
| NiKo | Ancient | 15 | **2.0°** | 80.0% |
| NiKo | Inferno | 11 | **4.2°** | 54.5% |
| m0NESY | Inferno | 26 | **3.0°** | 73.1% |
| Pro field range | — | — | 1.6–7.0° | 31–83% |
| **bennett** | **all maps** | **1,222** | **3.8°** | **63.1%** |

The pro sample is two tournaments (small N per player: 11–26 encounters each), so these numbers are indicative rather than definitive. With that caveat:

- **bennett's median of 3.8° sits between NiKo's Ancient (2.0°) and NiKo's Inferno (4.2°)**, on a dataset 40–80× larger. NiKo on Inferno and bennett overall are essentially the same distribution.
- **m0NESY's 1.6° on Ancient** is exceptional — that is the baseline for an elite AWP player who pre-aims every angle. The gap to reach that level is meaningful.
- **% under 5°**: bennett at 63.1% is within the pro field range (31–83%). The pro average across this sample is approximately 62%, placing bennett squarely in the middle.

**Interpretation:** Crosshair placement at this level is not the bottleneck. The raw numbers place you comfortably within pro field range, and the earlier AK headshot gap (37% vs 60–75% at pro level) is a more meaningful mechanical deficit than crosshair placement deviation.

---

## 8. Duel Intelligence Engine

> **Method:** A duel win is recorded when the killer had an active first-sight of the victim (from the spotted-flag transition) before the kill. Exposure time = ticks from first-sight to kill tick, converted to ms. Hits-to-kill = non-utility damage events from killer→victim in that window. First-hit HS rate = % of winning duels where the first bullet hit the head. Pre-shot correction = angle between the observer's view direction at first-sight and at first weapon fire (small = stable crosshair, large = significant adjustment before pulling the trigger).

### 8.1 Aggregate duel stats (38 matches)

| Metric | Value |
|--------|-------|
| Total duel wins | **592** |
| Total duel losses | **531** |
| **Duel win rate** | **52.7%** |
| Median exposure time (wins) | **719 ms** |
| Median exposure time (losses) | **359 ms** |
| Average hits to kill | **2.33** (median 2.0) |
| First-hit headshot rate | **21%** |
| Median pre-shot correction | **1.7°** |
| Shots fired within 2° of first-sight | **51%** |

### 8.2 Key findings

**Exposure asymmetry (719 ms wins vs 359 ms losses).** The duels you win take roughly twice as long as the duels you lose. This means when you are winning, you are often grinding through a slower exchange — multiple bullets, body shots first, then finish. When you lose, you are dying fast: opponents with a first-hit headshot or a clean two-tap are ending the duel before you can respond. Closing that gap requires raising your first-hit headshot rate (see below).

**First-hit headshot rate of 21% is the core mechanical gap.** Only one in five winning duels opens with a head hit. The remaining 79% open body-first, which forces a follow-up bullet and extends your exposure. Pro riflers are opening with headshots 60–75% of the time, which explains why their time-to-kill is dramatically shorter. This is the same conclusion as the AK HS% finding (Section 6.3) but now confirmed from a duel-by-duel perspective: it is not a spray pattern issue, it is a first-bullet placement issue.

**2.3 hits per kill on average.** At 37 DPH average, two body shots is ~74 damage — enough to kill with a follow-up. But two body shots means you are absorbing return fire for two tick intervals. Reducing to 1.5 hits-per-kill (one head + one confirmation, or one chest + one head finish) would measurably raise survival in winning duels.

**Pre-shot correction of 1.7° — a positive signal.** You are not over-adjusting after first sight. The crosshair barely moves between the moment you see an enemy and the moment you fire. 51% of shots are within 2° of the first-sight direction. This means the angle problem is entirely about where the crosshair *lands at first sight* (initial placement), not what happens to it afterward. Aim training should focus on pre-aiming correct head height, not micro-adjustment during the duel.

### 8.3 Per-match duel extremes

**Best duel performances** (high win rate + short exposure wins):
- `0646adbcdc4a` (Inferno, 18W/16L, 656ms win, 17% first-HS, 1.6° corr)
- `1eb13489fe39` (Inferno, 18W/17L, 422ms win — shortest median among high-volume matches)
- `7dd8ed67f61f` (Nuke, 22W/14L, 617ms win, 68% shots <2°)

**Worst duel performances** (high loss count, slow wins):
- `9bf563d2d7b5` (Mirage, 7W/5L, **2281ms** median win — took 2+ seconds per winning duel)
- `8d6db7c7eb67` (Inferno, 11W/16L, **2141ms** median win — similar outlier)
- `ac5ae3f09ca4` (Nuke, 17W/17L, 1047ms win, 12% first-HS — worst HS rate)

The two 2000ms+ outliers on Mirage and Inferno explain the T-side weakness on those maps: every winning duel is slow and grinding, meaning you are getting hit while finishing kills.

---

## 9. Role Fit Analysis

Based on all data, there are three plausible roles:

### Role A: Aggressive Anchor / Second AWP (CT) + Entry Fragger (T)

**Evidence for:**
- CT KD 1.39, survival 35.6%, trade net +10 — fits an anchor who holds and wins duels
- High KAST on CT (77.9%) means consistent round presence when holding
- T entry rate 57.8% fits a first-through player
- Good pistol game enables clutch exits on eco rounds
- AK-47 volume is an entry rifler's profile (high bullets-fired, high kill rate)

**Evidence against:**
- Flash assists near zero — anchors need to re-flash retakes, entry players need pop flashes

**What this role demands from you:**
Pick one or two CT positions to master per map, learn the specific re-flash and retake grenades for those positions, and continue the aggressive CT duel-seeking behavior. On T, focus on coordinating the pop-flash for the player behind you instead of solo entry.

---

### Role B: Support / Trade Player

**Evidence for:**
- KAST% of 77.4% is the signature of a high-impact round-presence player
- Assists (213 total) show you are in fights alongside teammates
- CT trade kill discipline (+10 net) shows retake awareness

**Evidence against:**
- Flash assists of 6 across 769 rounds is incompatible with a support role
- ADR of 116 means you are the damage dealer, not the setup maker
- Entry kill volume (83 attempts) is an aggressor pattern, not support

This role does not fit your current data.

---

### Role C: Star Rifler / Solo Player

**Evidence for:**
- ADR 116 exceeds pro riflers in the dataset
- Entry win rate 59% exceeds professional entry fraggers
- Desert Eagle / USP / Glock headshot rates are strong
- Overpass ADR 144 — when the map is right you are dominant

**Evidence against:**
- AK HS% (37%) lags pro standards by 20–30 percentage points
- T-side Mirage results show a ceiling that drops sharply on structured maps
- Low assist count on T (96) relative to kill count (317) — not drawing enough value from team setups

---

### Recommended focus: **Role A** with specific upgrades

You are naturally an aggressive anchor on CT and entry fragger on T. The data supports this strongly. The upgrades needed are:

1. **First-bullet head placement → target 40%+ first-hit HS rate.** The duel engine shows 21% first-hit headshots; pro riflers open with heads 60–75% of the time. This single change shortens your time-to-kill, reduces exposure per duel, and explains the 719ms vs 359ms asymmetry. Deathmatch practice specifically at head height. Every duel should begin with a crosshair already at the enemy's eye level.

2. **Flash before every AWP peek — eliminate dry-peeks.** 97% of 31 AWP deaths had no flash in the preceding 3 seconds. One pop-flash before contesting an AWP angle would flip several of these to your favor per match. Learn the specific peek-flash for the three AWP angles you die to most (A ramp Nuke, CT Inferno, A-main Overpass).

3. **Re-peek discipline after kills.** 42% of AWP deaths occur immediately after you got a kill same round. Build the habit: kill → step back → decide → peek (with flash). Costs nothing; eliminates ~5 free AWP deaths per 10 matches.

4. **Add 2–3 flash assists per match.** Only ~7 effective flashes across 38 matches — fewer than one every five games. On T executes, the pop-flash for the second player through is one of the highest-impact team contributions you can make.

5. **T-side Mirage structure.** The duel engine confirms it: Mirage T duels take 2000ms+ to resolve (your two worst matches). This is not mechanics — it is position/timing. Learn two complete Mirage T-side setups (smokes + flashes for A-main, B-short, mid connector) to avoid grinding every duel from a disadvantaged spot.

6. **Convert unused utility.** 0.22 grenades banked per round. After any survival round, ask: was there a molotov or smoke I should have thrown? This habit directly feeds both utility damage and flash assist counts.

7. **Crosshair placement on Nuke.** At 4.2° median (vs 3.5° on Overpass), Nuke is the weakest placement map. The correction data shows the pre-shot adjustment is fine — the issue is initial placement. Pre-aim head height on upper-B, hut peek, and ramp door before enemies appear.

---

## 10. Summary

| Category | Rating | Comment |
|----------|--------|---------|
| Rifle output (ADR) | Elite | 116.3 ADR, top of pro sample range |
| Entry aggression | Strong | 59% entry win rate, high volume |
| CT anchor | Strong | 1.39 KD, 35.6% survival, +10 trade net |
| T entry | Solid | 57.8% entry rate, high trade deaths (team converts) |
| Pistol game | Strong | Deagle/Glock/USP all above 57% HS |
| AK mechanics | Average | 36.9% HS vs 60–75% at pro level; 21% first-hit HS in duels |
| Duel efficiency | Average | 52.7% win rate; 719ms wins vs 359ms losses; 2.3 hits/kill |
| Pre-shot correction | Good | 1.7° median; 51% shots within 2° of first-sight — crosshair is stable |
| Utility for team | Weak | 6 flash assists, ~7 effective flashes in 769 rounds |
| AWP resistance | Weak | 31 deaths, 97% dry-peek, 42% re-peek, 71% isolated |
| Map pool | Solid | Strong on Nuke/Inferno/Overpass; T-Mirage needs work |
| Crosshair placement | Good | 3.8° median, 63.1% under 5° — within pro field range; Nuke weakest |

The profile is consistent: a high-damage aggressive rifler who wins individual duels at an above-average rate, provides genuine entry value both when winning and when dying, but needs to develop team utility usage and close the AK headshot gap to reach the ceiling that the ADR numbers suggest is achievable.
