# Playstyle Analysis — Mochardi / Carloncho (76561198033448674)

**Dataset:** 28 matchmaking demos · 566 rounds played
**Pro reference:** Same 10 pro demos as bennett analysis (Falcons/NAVI, mouz/Vitality)
**Note:** Player used two names across the dataset (Carloncho in 12 matches, Mochardi in 16)
**Generated:** 2026-02-20

---

## 1. Overall Snapshot

| Metric | CT | T | Total |
|--------|----|---|-------|
| Matches | 17 | 11 | 28 |
| Rounds | 358 | 208 | 566 |
| K / A / D | 192 / 83 / 240 | 107 / 53 / 150 | 299 / 136 / 390 |
| K/D | **0.80** | **0.71** | **0.77** |
| HS% | 31.3% | 41.1% | 34.8% |
| ADR | **70.0** | **74.0** | **71.6** |
| KAST% | **73.2%** | **68.8%** | **71.3%** |
| Entry K / D | 21 / 21 | 13 / 23 | 34 / 44 |
| Entry win rate | **50.0%** | **36.1%** | **43.6%** |
| Trade kills | 33 | 22 | 55 |
| Trade deaths | 39 | 19 | 58 |
| Flash assists | 1 | 0 | 1 |
| Utility damage | 2,237 | 680 | 2,917 |
| Unused util/round | 0.35 | 0.28 | 0.32 |

**The headline:** A K/D below 1.0 and ADR in the low 70s are typical of a newer player still developing game sense and rifle mechanics. However, a KAST of 71.3% tells a more encouraging story — contributing meaningfully to more than two thirds of all rounds played. The ceiling is visible: several individual matches show clean 1.45 KD, 16K/11D performances, which means the fundamentals are there and consistency is the main hurdle.

---

## 2. Side Comparison — CT vs T

**CT is clearly the stronger side**, which is consistent with newer players: CT is mostly reactive (hold, respond, trade) while T requires active coordination, timed executes, and utility usage. The data reflects this strongly.

| Metric | CT | T | Delta |
|--------|----|---|-------|
| K/D | 0.80 | 0.71 | CT +0.09 |
| ADR | 70.0 | 74.0 | T +4.0 |
| KAST% | 73.2% | 68.8% | CT +4.4% |
| Entry win% | 50.0% | 36.1% | CT +13.9% |
| Survival rate | **36.8%** | **25.5%** | CT +11.3% |
| Assist rounds | 18.6%/rd | **27.6%/rd** | T higher |
| Traded rounds | 7.5%/rd | **18.9%/rd** | T higher |

**Notable:** ADR is slightly *higher* on T (74.0 vs 70.0) despite the lower K/D and KAST. This means you deal more damage per round on T but convert less of it into kills — the damage is there, the finishing blow often isn't. This is one of the clearest new-player patterns in the dataset.

### KAST breakdown (per-round level)

| Component | CT (280 rds) | % | T (286 rds) | % |
|-----------|-------------|---|-------------|---|
| Kill rounds | 120 | 42.9% | 108 | 37.8% |
| Assist rounds | 52 | 18.6% | **79** | **27.6%** |
| Survive rounds | 103 | **36.8%** | 73 | 25.5% |
| Traded rounds | 21 | 7.5% | **54** | **18.9%** |
| KAST total | 203 | **72.5%** | 202 | **70.6%** |

The T-side assist count (79 rounds with an assist, 27.6% of rounds) is very high. For context, bennett has a lower assist rate despite being a more experienced player. This means on T-side, you are regularly damaging enemies that teammates then finish. The damage is contributing to team success even when the kill doesn't land on you.

The 54 "traded" rounds on T is also notable. Your deaths are being avenged by teammates 54 times — teammates are cleaning up behind you. This mirrors entry-fragger-style dying but without the deliberate entry intent.

---

## 3. CT Side Deep Dive

### 3.1 What is working

**Survival rate 36.8%.** More than one in three rounds on CT you end alive. For a player with a 0.80 K/D, this is a positive signal — you are not dying recklessly in every round. When you hold a position, you often hold it long enough to see the round out.

**Entry balance 50% (21 kills, 21 deaths).** When CT-side duels happen at the opening phase of a round, you are breaking even. This is solid for a new player; many beginners give up entry duels at rates of 30–40%. A 50% rate means you are not being caught out of position on CT.

**KAST 73.2%.** High round presence. When you are on CT, you matter in most rounds through kills, assists, or surviving.

**Utility damage 2,237 in 358 rounds (6.2/round).** Decent. HE grenades are landing and dealing real damage. The pattern is similar to bennett's CT utility profile.

### 3.2 Where CT breaks down

**Trade balance: 33 kills vs 39 deaths (net -6).** This is the mirror image of bennett's +10. On CT you are losing more trades than you win. The mechanism is likely incorrect response timing on retakes — arriving at a fight just after a teammate dies without proper angle clearing, and running into the enemy in an unfavorable position.

**USP-S: 2 kills, 19 deaths.** This is the most expensive weakness in your entire dataset. The CT pistol round happens in almost every match (sometimes twice after a half reset). You are losing nearly every one of them with the USP. For 19 rounds across 28 matches, your team is starting the match or the second half at a numerical or economic disadvantage because of CT pistol losses.

**Unused utility 0.35/round.** One in three rounds you have a grenade at round end on CT. That is higher than bennett (0.24) and means roughly a third of CT round purchases are being partially wasted.

---

## 4. T Side Deep Dive

### 4.1 What is working

**T-side AK-47 HS% 41.7%.** When looking at pure headshot rate on the AK, this is actually higher than bennett's 36.9%. It likely reflects landing headshots on already-damaged or low-HP enemies (the chip damage connects a HS finishing shot), but it does show aim at head height is a habit.

**Assist rate 27.6% of rounds.** Already covered above — but worth emphasising as a positive. You are always in the fights on T, always dealing damage, and your teammates are completing the kills. The impact is real even if the kill count does not fully show it.

**T-side trade deaths 54.** Teammates are saving rounds by trading you. While getting traded is not the goal, it shows you are playing positions where teammates can support you rather than dying in isolated locations where no follow-up is possible.

### 4.2 Where T breaks down

**Entry deaths: 23 vs 13 entry kills (36.1% win rate).** This is the single biggest problem on T side. You are dying first in rounds at a rate that significantly disadvantages your team. In 23 rounds, you died before your team got any kills — the round was effectively 4v5 from that point. An entry win rate below 50% on T is a clear signal that entries are happening without preparation: no utility to cover the angle, no information on where the enemy is holding, or peeking wide positions without a partner ready to trade.

**ADR 74 but K/D 0.71.** The gap between dealing 74 damage per round and only getting 0.71 kills per round means a lot of damaged-but-surviving enemies. These enemies go on to kill your teammates. In CS2, the "70 HP enemy" is nearly as dangerous as a full-HP enemy — the chip damage is only valuable if the kill follows.

**Flash assists: 0 on T side.** No pop flashes for your own peeks, no flashes before site entry. This makes every T-side duel a 50/50 raw aim fight that you are currently losing at a 36% win rate on entries. One pop-flash per execute can turn a losing entry into a winning one.

---

## 5. Weapon Breakdown

### 5.1 The AK-47 assist paradox

| Metric | Mochardi (AK) | bennett (AK) |
|--------|---------------|--------------|
| Kills | 72 | 295 |
| Assists | **60** | 64 |
| HS% | 41.7% | 36.9% |
| DPH | 35.6 | 37.4 |

Mochardi has **60 AK assists with only 72 kills**. That means for every AK kill secured, there is 0.83 AK assists alongside it. Bennett, by comparison, has 64 assists against 295 kills — one assist for every 4.6 kills.

This is the most revealing number in the entire analysis. It tells a specific story: you are spraying the AK into targets, dealing 60–80 damage, and a teammate is finishing the kill. This is the spray-down pattern: holding down the mouse button, bullets going wide after the first few, doing damage but not enough concentrated hits to secure the kill. The fix is mechanical — learn the AK-47 first bullet accuracy (standing still), two-tap technique (fire 2, pause, fire 2), and counter-strafe. When practiced, the same bullets deal 200+ damage in 4 shots rather than 60 damage in 10.

### 5.2 Primary weapons

| Weapon | K | HS% | Assists | DPH |
|--------|---|-----|---------|-----|
| M4A1 | **111** | 31.5% | 17 | 32.8 |
| AK-47 | 72 | 41.7% | 60 | 35.6 |
| Galil AR | 22 | 22.7% | 10 | 28.2 |
| FAMAS | 15 | 26.7% | 2 | 28.5 |

**M4A1 is your most effective weapon.** 111 kills, lower assist-to-kill ratio (17 assists vs 72 kills = 0.15 assists/kill), and consistent use. The M4A1's lower rate of fire and forgiving spray pattern seems to suit your playstyle better than the AK. This is actually common among newer players — the M4A1's reduced fire rate nudges toward more deliberate shooting.

**Galil HS% 22.7% — lowest of all rifles.** Force-buy rounds with the Galil are producing poor headshot rates, meaning most Galil fights are turning into spray duels at close range. On force buy rounds, positioning conservatively and letting the enemy come to you (rather than running into their rifle) will naturally raise the HS% and DPH.

### 5.3 Pistol performance

| Pistol | K | HS% | DPH | Deaths |
|--------|---|-----|-----|--------|
| Desert Eagle | 17 | **64.7%** | 65.9 | 15 |
| Five-SeveN | 12 | 41.7% | 33.2 | 0 |
| Tec-9 | 11 | 36.4% | 37.9 | 3 |
| Glock-18 | 7 | 42.9% | 26.0 | 17 |
| USP-S | 2 | 50.0% | 23.3 | **19** |

**Desert Eagle is your best individual weapon by HS%.** 64.7% HS with 65.9 DPH is excellent — these numbers reflect composed, deliberate shots at head height. This Desert Eagle competence is a real asset on eco rounds.

**USP-S is the urgent problem.** 2 kills, 19 deaths. The USP-S at CT pistol round is a precision weapon that punishes spraying very hard. You likely know the feeling: the first shot is accurate, then the next shots go wide. The USP requires tapping with full stops between shots. Given your Desert Eagle accuracy (64.7% HS), the aim is fine — the issue is probably not respecting the USP's accuracy penalty on the second and third shot.

**Five-SeveN: 12 kills, 0 deaths.** When you buy the Five-SeveN on a force-buy round on CT, you are converting well. Consider prioritising this over the USP if pistol round confidence is low (though learning the USP is the better long-term path).

### 5.4 M4A4 red flag

| | M4A4 |
|-|------|
| Kills | 3 |
| Deaths | 30 |
| DPH | 30.6 |

3 kills, 30 deaths with the M4A4. This is almost certainly rounds where the M4A4 was purchased and then the round went badly — high death counts in the same weapon bucket happen because the weapon is repeatedly bought, not because you died 30 times in one match. The M4A1 is clearly your preferred rifle. Stopping M4A4 purchases entirely and defaulting to M4A1 whenever you can afford CT rifle is the right call.

### 5.5 Utility

| Type | K | Damage | Hits |
|------|---|--------|------|
| HE Grenade | 10 | 2,412 | 126 |
| Molotov | 0 | 258 | 74 |
| Incendiary | 0 | 247 | 60 |

HE grenade damage is substantial (2,412 total) and you are even securing HE kills (10 — more than bennett's 5). This suggests you are throwing HEs at known player positions and hitting them. The Molotov/Incendiary damage is also meaningful, meaning utility is being used for area denial, not just purchased and held.

The problem remains flash assists (1 total across 566 rounds). All utility usage is damage-oriented, none is aimed at creating openings for teammates.

---

## 6. Map-by-Map

| Map | Matches | Rounds | K/D | ADR | KAST% | Entry K/D | Trade K/D |
|-----|---------|--------|-----|-----|-------|-----------|-----------|
| Nuke | 8 | 172 | **0.83** | **76.5** | **75.6%** | 11/16 | 15/18 |
| Inferno | 6 | 117 | **0.83** | 73.5 | 72.6% | 7/7 | 13/16 |
| Mirage | 4 | 91 | 0.60 | 65.4 | 60.4% | 4/7 | 10/5 |
| Overpass | 4 | 79 | 0.67 | 69.6 | 68.4% | 4/8 | 8/7 |
| Ancient | 3 | 50 | 0.89 | 58.9 | 76.0% | 2/4 | 3/5 |
| Dust2 | 1 | 23 | 0.47 | 72.9 | 65.2% | 3/1 | 3/4 |
| Anubis | 1 | 21 | **1.07** | 84.1 | **81.0%** | 0/0 | 3/2 |
| Vertigo | 1 | 13 | **1.14** | 64.3 | **84.6%** | 3/1 | 0/1 |

**Nuke is your best main map.** Highest rounds played and most consistent numbers. KAST of 75.6%, KD 0.83, ADR 76.5. The specific match breakdown shows one outlier performance of 18K/11D at 1.64 KD and 102.5 ADR — which is a very strong game at any level. Nuke clearly suits your style.

**Anubis and Vertigo** show KD above 1.0 in small samples. Encouragingly, these are maps with clearer structure and tighter angles — maps where holding corners and reacting tends to reward newer players.

**Mirage is the weakest map.** KD 0.60, ADR 65.4, KAST 60.4%. These are considerably below your own averages. The individual T-side results are 0.65, 0.40, 0.73 — all well below 1.0. Mirage T-side requires specific smoke and flash lineups to execute onto sites; without them, riflers walk into held crossfires repeatedly. Mirage can be de-prioritised for now until map-specific utility is learned.

**Overpass entry deaths** (4K/8D = 33% win rate) stand out as particularly bad. Overpass has long, exposed pathways that punish players who push without information or utility coverage. The high entry death rate on Overpass is a positioning issue.

---

## 7. Compared to bennett (teammate reference)

Both players appear together in 28 of Mochardi's matches. This is a useful direct comparison since they faced identical opponents.

| Metric | Mochardi | bennett | Gap |
|--------|----------|---------|-----|
| K/D | 0.77 | 1.30 | -0.53 |
| ADR | 71.6 | 116.3 | **-44.7** |
| KAST% | 71.3% | 77.4% | -6.1% |
| Entry win% | 43.6% | 59.3% | -15.7% |
| AK kills | 72 | 295 | -223 |
| AK DPH | 35.6 | 37.4 | -1.8 |
| AK assists | 60 | 64 | nearly equal |
| M4A1 kills | 111 | 134 | comparable |
| M4A1 DPH | 32.8 | 32.7 | **identical** |

Two observations from this table stand out:

**M4A1 DPH is virtually identical (32.7 vs 32.8).** On the M4A1, you and bennett deal the same damage per bullet hit. Your aim with the M4A1 is not meaningfully worse — the difference in kills comes from number of opportunities created (bennett takes more duels) and trade efficiency, not from mechanical quality on the CT rifle.

**AK DPH is only 1.8 lower (35.6 vs 37.4) but kills are 223 fewer.** The DPH gap is minor. The massive kills gap (72 vs 295) with similar DPH means the kills difference comes from **how often the AK is in the hands** (bennett plays far more T-side rounds and buys more freely), **how many duels are taken**, and the **assist rate** problem described above — not from fundamentally different aim. This is an encouraging sign for development: your aim ceiling is closer to bennett's than the K/D gap would suggest.

**ADR gap is 44.7 points.** This is the real difference in match impact. 44 damage per round, across 566 rounds, is enormous — but it is entirely explained by:
- Fewer kills confirmed (opponents staying alive after being hit)
- Lower duel volume on T (being more passive / dying on entry)
- Not taking the aggressive CT holds that bennett does

---

## 8. Compared to Pro Players

This comparison needs honest context: the pro sample is tier-1 CS2 play. The numbers are directional benchmarks, not targets for near-term performance. They illustrate what elite fundamentals look like.

| Player | Role | ADR | KD | AK HS% | AK DPH | KAST% |
|--------|------|-----|-----|--------|--------|-------|
| m0NESY | Star AWP | 160.9 | 2.42 | 83.3% | 51.8 | 86.5% |
| ropz | Anchor | 102.6 | 1.51 | 70.6% | 45.7 | 81.6% |
| flameZ | Entry | 102.3 | 1.10 | 75.0% | 48.0 | 73.7% |
| xertioN | Second rifle | 97.3 | 0.96 | 59.1% | 43.4 | 67.1% |
| apEX | Entry/IGL | 83.1 | 0.83 | 71.4% | 47.2 | 67.1% |
| **bennett** | — | **116.3** | **1.30** | 36.9% | 37.4 | 77.4% |
| **Mochardi** | — | **71.6** | **0.77** | 41.7% | 35.6 | 71.3% |

**AK headshot rate context.** Pro riflers operate at 59–83% AK HS. Both you and bennett are far below this. Bennett is at 36.9%, you at 41.7%. Interestingly, your AK HS% is slightly *higher* than bennett's — but as noted, this is likely a sample effect (HS on low-HP targets) rather than a tap-shooting advantage. The pro benchmark shows that the two-tap, single-tap, and counter-strafe technique is not optional at high levels — it is the foundation of every AK kill.

**KAST comparison.** Your KAST of 71.3% is actually comparable to apEX (67.1%) and xertioN (67.1%) — two professional players. This means your round contribution rate, purely as a metric, is not far behind players at the highest level. The difference is in *what kind* of KAST rounds they have (mostly kill-dominant) versus yours (assist and survive-heavy).

**K/D and apEX.** apEX (professional IGL and entry fragger for Vitality) has a 0.83 KD in this sample with 83.1 ADR. You are at 0.77 and 71.6 ADR. The gap to a professional entry fragger is 12 ADR and 0.06 KD — not enormous, and entirely explained by the AK spray pattern and the T-side entry problem.

---

## 9. Priority Training Areas

Ordered by expected impact per time invested:

### 9.1 AK-47 tap and burst control (highest priority)

The AK-47 assist-to-kill ratio (0.83:1) is the most consistent signal in the data. Every AK fight starts with you dealing damage — the issue is not aim, it is shooting discipline. Practice:
- **Standing still, single shots at head height** on aim_botz or similar
- **The two-tap**: two shots, full stop, two shots. At medium range (20–40m), two AK bullets to the chest are lethal. You do not need to spray.
- **Counter-strafe before shooting**: move left → press right briefly → shoot. The bullet goes where the crosshair is.

This single change would raise DPH from 35 toward the 43–48 range of professional riflers, which would convert many of the 60 AK assists into AK kills.

### 9.2 USP-S on CT pistol rounds (high priority)

19 USP deaths across 28 matches means CT pistol rounds are almost certainly being lost. The USP-S first-shot accuracy when standing still is one of the best in the game. The second shot is nearly useless. The full routine: stop moving completely, one shot, small re-adjust to head, second shot. Never hold the trigger.

Given the Desert Eagle accuracy (64.7% HS), the aim is clearly capable — the USP just requires a different rhythm than the Deagle.

Winning even half these pistol rounds would convert several lost match starts into won ones.

### 9.3 T-side entry preparation (high priority)

23 entry deaths vs 13 entry kills (36.1%) means walking into enemies one-by-one. Before any T-side entry:
- Is there a smoke on the angle that can kill you?
- Is there a flash you can throw first?
- Is your teammate ready to trade if you die?

Even just asking these three questions before each entry will raise the win rate above 50%. The data shows you already have the aim and damage to win these duels — they are being lost to positioning and timing, not mechanics.

### 9.4 Learn one flash per key position (medium priority)

One pop-flash per map, per your most common role/position:
- Nuke: flash for the B-hut push or ramp entry
- Inferno: A-main pop flash or B-site entrance flash

A single flash creates an assist (which you currently almost never generate on T-side) and turns a coin-flip duel into a winning one. Your KAST would increase materially from even 2–3 flash assists per match.

### 9.5 Stop buying M4A4 (low effort, immediate gain)

3 kills, 30 deaths with the M4A4. If budget doesn't reach M4A1, default to FAMAS (or Five-SeveN + armor) rather than M4A4.

---

## 10. Role Fit

For a newer player, fundamentals matter more than role specialisation. That said, the data points to a natural fit:

**Recommended: CT Anchor / Information Player**

The evidence:
- CT side is consistently stronger (KD 0.80 vs 0.71, KAST 73.2% vs 68.8%, survival 36.8% vs 25.5%)
- When not forced into entry situations, round presence is high (assists + survival)
- Best individual performance came in structured CT games on Nuke and Inferno
- Overpass and Mirage T-side entry deaths reflect that uncoordinated T-side aggression is currently costly

**What to avoid for now:** Being the entry fragger on T. The 36.1% entry win rate means you are handing opponents free-man advantages. Until T-side utility usage and duel preparation improve, playing second or third into a site behind a coordinated entry is significantly more effective.

**Natural path:** As the fundamentals (AK control, USP, T-side prep) develop, the data suggests a second-entry / trade-fragging role on T would suit your profile — your assists show you are always near the fights, and your survival rate shows you avoid dying uselessly. A second-entry player arrives just behind the first man and converts the trade — that matches what you already do instinctively, just without the deliberate setup.

---

## 11. Summary

| Category | Rating | Comment |
|----------|--------|---------|
| CT positioning | Decent | Balanced entry duels (50%), reasonable survival |
| CT rifle (M4A1) | Solid | Identical DPH to bennett; mechanics are there |
| T-side preparation | Weak | 36% entry win rate; walking into enemies |
| AK-47 control | Developing | Good HS% but assist rate exposes spray habit |
| USP-S | Weak | 2K/19D — biggest single point of improvement |
| Pistol game (Deagle/5-7) | Solid | 64.7% Deagle HS shows genuine aim quality |
| Utility for team | Very Weak | 1 flash assist in 566 rounds |
| Consistency | Developing | High variance per match; ceiling is clearly above average |
| Map pool | Nuke/Inferno | Mirage is the clear outlier to avoid for now |

The overall picture is encouraging for a newer player: damage is being dealt every round, round presence is in the 70s, and the individual ceiling (shown by the best performances) is well above the average numbers. The gap to bennett is real but not mechanical — it is mostly about how many winning duels are created per match, and that is driven by T-side preparation and AK discipline, both of which are directly trainable.

The most efficient path forward, in order: fix the AK spray → fix the USP pistol round → stop dying first on T. These three changes alone would push the K/D above 1.0 and the ADR into the 90s within a reasonable volume of matches.
