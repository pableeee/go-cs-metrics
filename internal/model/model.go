// Package model defines the core data types used throughout the pipeline:
// raw events emitted by the parser, aggregated per-player/per-round/per-weapon
// statistics, and summary records used for storage and display.
package model

import "time"

// Team represents which side a player is on.
type Team int

const (
	TeamUnknown    Team = 0
	TeamSpectators Team = 1
	TeamT          Team = 2
	TeamCT         Team = 3
)

// String returns "T", "CT", or "?" for the team value.
func (t Team) String() string {
	switch t {
	case TeamT:
		return "T"
	case TeamCT:
		return "CT"
	default:
		return "?"
	}
}

// ---- Raw events emitted by the parser ----

// RawKill represents a single kill event extracted from a demo tick stream.
type RawKill struct {
	Tick, RoundNumber               int
	KillerSteamID, VictimSteamID   uint64
	AssisterSteamID                 uint64 // 0 if none
	KillerTeam, VictimTeam          Team
	Weapon                          string
	IsHeadshot, AssistedFlash       bool
	NearbyVictimTeammates           int     // alive teammates of victim within 512 units at kill tick (0 = isolated)
	VictimPos                       Vec3    // victim world position at kill tick
	KillerPos                       Vec3    // killer world position at kill tick
	VictimYawDeg                    float64 // victim view yaw (0–360) at kill tick
}

// RawDamage represents a single damage event (PlayerHurt) from the demo.
type RawDamage struct {
	Tick, RoundNumber                   int
	AttackerSteamID, VictimSteamID     uint64
	AttackerTeam                        Team
	HealthDamage                        int
	Weapon                              string
	IsUtility                           bool   // HE/molotov/incendiary
	HitGroup                            string // "head", "chest", "stomach", "left_arm", "right_arm", "left_leg", "right_leg", "other"
	VictimPos                           Vec3   // victim world position at hurt tick
}

// RawFlash represents a flashbang blind event from the demo.
type RawFlash struct {
	Tick, RoundNumber               int
	AttackerSteamID, VictimSteamID uint64
	AttackerTeam, VictimTeam       Team
	FlashDuration                  time.Duration
	VictimPos                      Vec3    // victim world position at blind tick
	VictimYawDeg                   float64 // victim view yaw (0–360) at blind tick
	VictimPitchDeg                 float64 // victim view pitch, normalised to [-90,90] (negative = looking up)
}

// FlashEvent is one row per PlayerFlashed event enriched with the flash's
// explosion position and the derived blind angle. Produced by aggregator pass 13.
// BlindAngleDeg is 0 when the victim was looking directly at the flash and 180
// when facing away; flashes with BlindAngleDeg < 45 typically produce full blinds.
type FlashEvent struct {
	DemoHash       string
	MatchDate      string
	MapName        string
	RoundNumber    int
	Tick           int
	ThrowerSteamID uint64
	ThrowerTeam    Team
	VictimSteamID  uint64
	VictimTeam     Team
	DurationSec    float64
	IsTeamFlash    bool
	VictimPos      Vec3
	VictimYawDeg   float64
	VictimPitchDeg float64
	FlashPos       Vec3    // from matching RawGrenadeEvent; zero if unmatched
	BlindAngleDeg  float64 // 0 = looking at flash, 180 = facing away
	DistanceMeters float64
}

// PlayerRoundEndState captures a player's state at the end of a round,
// including alive status, team side, and remaining grenade count.
type PlayerRoundEndState struct {
	SteamID64    uint64
	IsAlive      bool
	Team         Team
	GrenadeCount int
}

// RawRound holds metadata for a single round, including tick boundaries,
// the winning team, and the end-of-round state for every participant.
type RawRound struct {
	Number, StartTick, FreezeEndTick, EndTick int
	WinnerTeam                                Team
	PlayerEndState                            map[uint64]PlayerRoundEndState
	PlayerEquipValues                         map[uint64]int // USD equipment value per player at freeze-end
	BombPlantTick                             int            // tick when bomb was planted; 0 if not planted this round
}

// RawFirstSight is emitted by the parser each time a player first spots an enemy
// in a given round (server-side m_bSpottedByMask transition 0→1).
type RawFirstSight struct {
	Tick        int
	RoundNumber int
	ObserverID  uint64
	EnemyID     uint64
	AngleDeg    float64 // angular deviation: crosshair → enemy head, in degrees (total)
	PitchDeg    float64 // |pitch_to_enemy − observer_pitch| (deviation, for crosshair split)
	YawDeg      float64 // |yaw_to_enemy − observer_yaw| (deviation, wrapped to [0,180])
	// Absolute observer view angles at first-sight tick (used for pre-shot correction).
	ObserverPitchDeg float64
	ObserverYawDeg   float64
}

// Vec3 is a 3D world-space position in Hammer units.
type Vec3 struct{ X, Y, Z float64 }

// RawWeaponFire is emitted by the parser each time a player fires a weapon.
type RawWeaponFire struct {
	Tick            int
	RoundNumber     int
	ShooterID       uint64
	Weapon          string
	PitchDeg        float64 // normalized view pitch at fire tick
	YawDeg          float64 // view yaw at fire tick
	AttackerPos     Vec3    // shooter world position at fire tick
	HorizontalSpeed float64 // shooter horizontal speed (Hammer units/s) at fire tick
}

// RawGrenadeEvent represents a single grenade throw-to-land event.
// ThrowTick is the tick the projectile left the thrower's hand; EndTick is when
// it detonated (HE/flash), began its effect (smoke/molotov), or was destroyed.
// ThrowPos is the thrower's position at the throw tick; LandPos is the
// projectile's final position. Used for lineup clustering and utility analytics.
type RawGrenadeEvent struct {
	ThrowTick      int
	EndTick        int
	RoundNumber    int
	ThrowerSteamID uint64
	ThrowerTeam    Team
	GrenadeType    string // "smoke" | "flash" | "he" | "molotov" | "decoy"
	ThrowPos       Vec3
	LandPos        Vec3
}

// RawMatch is the fully parsed representation of a single demo file.
// It contains all tick-level events and metadata needed by the aggregator.
type RawMatch struct {
	DemoHash    string
	MapName     string
	MatchDate   string
	MatchType   string
	Tickrate    float64
	TicksPerSecond float64
	Rounds      []RawRound
	Kills       []RawKill
	Damages     []RawDamage
	Flashes     []RawFlash
	FirstSights []RawFirstSight
	WeaponFires []RawWeaponFire
	Grenades    []RawGrenadeEvent
	PlayerNames map[uint64]string
	PlayerTeams map[uint64]Team
}

// ---- Aggregated metrics ----

// PlayerMatchStats holds all aggregated performance metrics for a single
// player within a single demo. This is the primary output of the aggregator
// and the main table stored in SQLite.
type PlayerMatchStats struct {
	DemoHash  string
	MapName   string // populated when queried across demos (JOIN with demos table)
	MatchDate string // populated when queried (JOIN with demos.match_date)
	SteamID   uint64
	Name     string
	Team     Team

	Kills          int
	Assists        int
	Deaths         int
	HeadshotKills  int
	FlashAssists   int

	TotalDamage    int
	UtilityDamage  int
	RoundsPlayed   int

	// Entry
	OpeningKills  int
	OpeningDeaths int

	// Trades
	TradeKills  int
	TradeDeaths int

	// KAST
	KASTRounds int // rounds where K or A or S or T

	// Unused utility at round end
	UnusedUtility int

	// Crosshair placement (Option A — spotted flag approximation)
	CrosshairEncounters    int
	CrosshairMedianDeg     float64
	CrosshairPctUnder5     float64
	CrosshairMedianPitchDeg float64
	CrosshairMedianYawDeg   float64

	// Duel engine (Module 1)
	DuelWins             int
	DuelLosses           int
	MedianExposureWinMs  float64
	MedianExposureLossMs float64
	MedianHitsToKill     float64
	FirstHitHSRate       float64 // % of kill-duels where first bullet hit was to head

	// Pre-shot correction (Module 1 completion)
	MedianCorrectionDeg    float64
	PctCorrectionUnder2Deg float64

	// AWP death classifier (Module 4)
	AWPDeaths         int
	AWPDeathsDry      int // no flash on victim in last 3s
	AWPDeathsRePeek   int // victim had a kill earlier same round
	AWPDeathsIsolated int // NearbyVictimTeammates == 0

	// Flash quality (Module 5)
	EffectiveFlashes int // your flashes where blinded enemy died to your team within 1.5s

	// Role and aim timing metrics
	Role                  string  // "AWPer" | "Entry" | "Support" | "Rifler"
	MedianTTKMs           float64 // median ms first shot fired → kill, multi-hit kills only (attacker POV)
	MedianTTDMs           float64 // median ms enemy's first shot → death, multi-hit only (victim POV)
	OneTapKills           int     // kills where the first shot in the 3s window was the kill shot
	CounterStrafePercent  float64 // % of shots fired while horizontal speed ≤ 34 u/s

	// Round outcome and trade timing
	RoundsWon               int     // rounds where player's team won
	MedianTradeKillDelayMs  float64 // median ms from teammate's death to player's trade kill
	MedianTradeDeathDelayMs float64 // median ms from player's death to teammate's trade kill
}

// KDRatio returns the kill-to-death ratio. If deaths is 0, kills is returned.
func (s *PlayerMatchStats) KDRatio() float64 {
	if s.Deaths == 0 {
		return float64(s.Kills)
	}
	return float64(s.Kills) / float64(s.Deaths)
}

// HSPercent returns the headshot kill percentage (0-100).
func (s *PlayerMatchStats) HSPercent() float64 {
	if s.Kills == 0 {
		return 0
	}
	return float64(s.HeadshotKills) / float64(s.Kills) * 100
}

// ADR returns the average damage per round.
func (s *PlayerMatchStats) ADR() float64 {
	if s.RoundsPlayed == 0 {
		return 0
	}
	return float64(s.TotalDamage) / float64(s.RoundsPlayed)
}

// KASTPct returns the KAST percentage (0-100): fraction of rounds where
// the player recorded a Kill, Assist, Survived, or was Traded.
func (s *PlayerMatchStats) KASTPct() float64 {
	if s.RoundsPlayed == 0 {
		return 0
	}
	return float64(s.KASTRounds) / float64(s.RoundsPlayed) * 100
}

// PlayerRoundStats holds per-round breakdown stats for a single player,
// tracking kills, assists, damage, and KAST-qualifying events within one round.
type PlayerRoundStats struct {
	DemoHash    string
	SteamID     uint64
	RoundNumber int
	Team        Team

	GotKill    bool
	GotAssist  bool
	Survived   bool
	WasTraded  bool
	KASTEarned bool

	IsOpeningKill  bool
	IsOpeningDeath bool
	IsTradeKill    bool
	IsTradeDeath   bool

	Kills   int
	Assists int
	Damage  int

	UnusedUtility int
	BuyType       string // "full" ≥$4500 | "force" ≥$2000 | "half" ≥$1000 | "eco" <$1000

	IsPostPlant      bool // bomb was planted at some point this round
	IsInClutch       bool // player was last alive on their team with ≥1 enemy alive
	ClutchEnemyCount int  // max enemies alive when player entered clutch (0 if not clutch)
	WonRound         bool // player's team won this round
}

// PlayerClutchMatchStats holds per-match clutch attempt/win counts broken down
// by enemy count (1v1 through 1v5) for a single player.
type PlayerClutchMatchStats struct {
	DemoHash string
	SteamID  uint64
	// Attempts[i] and Wins[i]: index 0 unused; 1–5 = 1v1 through 1v5.
	Attempts [6]int
	Wins     [6]int
}

// TotalAttempts returns the total number of clutch situations across all enemy counts.
func (s *PlayerClutchMatchStats) TotalAttempts() int {
	total := 0
	for i := 1; i <= 5; i++ {
		total += s.Attempts[i]
	}
	return total
}

// TotalWins returns the total number of clutches won across all enemy counts.
func (s *PlayerClutchMatchStats) TotalWins() int {
	total := 0
	for i := 1; i <= 5; i++ {
		total += s.Wins[i]
	}
	return total
}

// PlayerWeaponStats holds per-weapon kill/damage/hit breakdown for a single
// player within a single demo.
type PlayerWeaponStats struct {
	DemoHash      string
	SteamID       uint64
	Weapon        string
	Kills         int
	HeadshotKills int
	Assists       int
	Deaths        int
	Damage        int
	Hits          int
}

// HSPercent returns the headshot kill percentage (0-100) for this weapon.
func (s *PlayerWeaponStats) HSPercent() float64 {
	if s.Kills == 0 {
		return 0
	}
	return float64(s.HeadshotKills) / float64(s.Kills) * 100
}

// AvgDamagePerHit returns the average health damage dealt per hit for this weapon.
func (s *PlayerWeaponStats) AvgDamagePerHit() float64 {
	if s.Hits == 0 {
		return 0
	}
	return float64(s.Damage) / float64(s.Hits)
}

// PlayerAggregate holds stats for a single player aggregated across all stored demos.
type PlayerAggregate struct {
	SteamID uint64
	Name    string
	Matches int

	// Integer stats — summed across matches.
	Kills, Assists, Deaths             int
	HeadshotKills                      int
	TotalDamage, RoundsPlayed          int
	KASTRounds                         int
	FlashAssists, EffectiveFlashes     int
	OpeningKills, OpeningDeaths        int
	TradeKills, TradeDeaths            int
	DuelWins, DuelLosses               int
	AWPDeaths, AWPDeathsDry            int
	AWPDeathsRePeek, AWPDeathsIsolated int

	// Float stats — average of per-match medians (approximate).
	AvgExpoWinMs     float64
	AvgExpoLossMs    float64
	AvgCorrectionDeg float64
	AvgHitsToKill    float64

	// Role and aim timing
	Role                   string
	AvgTTKMs               float64
	AvgTTDMs               float64
	OneTapKills            int
	AvgCounterStrafePct    float64

	// Round outcome and trade timing
	RoundsWon                  int
	AvgTradeKillDelayMs        float64
	AvgTradeDeathDelayMs       float64
}

// KDRatio returns the aggregate kill-to-death ratio across all matches.
func (a *PlayerAggregate) KDRatio() float64 {
	if a.Deaths == 0 {
		return float64(a.Kills)
	}
	return float64(a.Kills) / float64(a.Deaths)
}

// HSPercent returns the aggregate headshot kill percentage (0-100).
func (a *PlayerAggregate) HSPercent() float64 {
	if a.Kills == 0 {
		return 0
	}
	return float64(a.HeadshotKills) / float64(a.Kills) * 100
}

// ADR returns the aggregate average damage per round.
func (a *PlayerAggregate) ADR() float64 {
	if a.RoundsPlayed == 0 {
		return 0
	}
	return float64(a.TotalDamage) / float64(a.RoundsPlayed)
}

// KASTPct returns the aggregate KAST percentage (0-100).
func (a *PlayerAggregate) KASTPct() float64 {
	if a.RoundsPlayed == 0 {
		return 0
	}
	return float64(a.KASTRounds) / float64(a.RoundsPlayed) * 100
}

// Rating2 returns the community approximation of HLTV Rating 2.0.
//
//	Impact ≈ 2.13*KPR + 0.42*APR − 0.41
//	Rating ≈ 0.0073*KAST% + 0.3591*KPR − 0.5329*DPR + 0.2372*Impact + 0.0032*ADR + 0.1587
func (a *PlayerAggregate) Rating2() float64 {
	return rating2Proxy(a.Kills, a.Assists, a.Deaths, a.KASTRounds, a.TotalDamage, a.RoundsPlayed)
}

// rating2Proxy is the shared Rating 2.0 community-formula implementation, used
// by PlayerAggregate / PlayerMapSideAggregate / PlayerSideStats. Returns 0 when
// roundsPlayed is 0 (caller invariant: never call with zero rounds when a
// non-zero rating is expected).
func rating2Proxy(kills, assists, deaths, kastRounds, totalDamage, roundsPlayed int) float64 {
	if roundsPlayed == 0 {
		return 0
	}
	kpr := float64(kills) / float64(roundsPlayed)
	apr := float64(assists) / float64(roundsPlayed)
	dpr := float64(deaths) / float64(roundsPlayed)
	kast := 100.0 * float64(kastRounds) / float64(roundsPlayed)
	adr := float64(totalDamage) / float64(roundsPlayed)
	impact := 2.13*kpr + 0.42*apr - 0.41
	return 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impact + 0.0032*adr + 0.1587
}

// PlayerMapSideAggregate holds stats for a single player on one map and one side (CT or T),
// aggregated across all stored demos.
type PlayerMapSideAggregate struct {
	SteamID uint64
	Name    string
	MapName string
	Side    string // "CT" or "T"
	Matches int

	Kills, Assists, Deaths int
	HeadshotKills          int
	TotalDamage, RoundsPlayed int
	KASTRounds             int
	OpeningKills, OpeningDeaths int
	TradeKills, TradeDeaths     int
	OneTapKills                 int

	// Aim timing (averages of per-match medians across matches on this map/side).
	AvgTTKMs           float64
	AvgTTDMs           float64
	AvgCounterStrafePct float64
}

// KDRatio returns the kill-to-death ratio for this map/side combination.
func (a *PlayerMapSideAggregate) KDRatio() float64 {
	if a.Deaths == 0 {
		return float64(a.Kills)
	}
	return float64(a.Kills) / float64(a.Deaths)
}

// HSPercent returns the headshot kill percentage (0-100) for this map/side.
func (a *PlayerMapSideAggregate) HSPercent() float64 {
	if a.Kills == 0 {
		return 0
	}
	return float64(a.HeadshotKills) / float64(a.Kills) * 100
}

// ADR returns the average damage per round for this map/side combination.
func (a *PlayerMapSideAggregate) ADR() float64 {
	if a.RoundsPlayed == 0 {
		return 0
	}
	return float64(a.TotalDamage) / float64(a.RoundsPlayed)
}

// KASTPct returns the KAST percentage (0-100) for this map/side combination.
func (a *PlayerMapSideAggregate) KASTPct() float64 {
	if a.RoundsPlayed == 0 {
		return 0
	}
	return float64(a.KASTRounds) / float64(a.RoundsPlayed) * 100
}

// Rating2 returns the community Rating 2.0 proxy for this map/side combination.
// Identical formula to PlayerAggregate.Rating2 — see that method for coefficients.
func (a *PlayerMapSideAggregate) Rating2() float64 {
	return rating2Proxy(a.Kills, a.Assists, a.Deaths, a.KASTRounds, a.TotalDamage, a.RoundsPlayed)
}

// PlayerSideStats holds per-side (CT/T) basic stats for one player within a single match,
// derived by aggregating player_round_stats.
type PlayerSideStats struct {
	SteamID uint64
	Name    string
	Team    Team // CT or T

	Kills, Assists, Deaths    int
	TotalDamage, RoundsPlayed int
	KASTRounds                int
	OpeningKills, OpeningDeaths int
	TradeKills, TradeDeaths   int
}

// KDRatio returns the kill-to-death ratio for this side.
func (s *PlayerSideStats) KDRatio() float64 {
	if s.Deaths == 0 {
		return float64(s.Kills)
	}
	return float64(s.Kills) / float64(s.Deaths)
}

// ADR returns the average damage per round for this side.
func (s *PlayerSideStats) ADR() float64 {
	if s.RoundsPlayed == 0 {
		return 0
	}
	return float64(s.TotalDamage) / float64(s.RoundsPlayed)
}

// KASTPct returns the KAST percentage (0-100) for this side.
func (s *PlayerSideStats) KASTPct() float64 {
	if s.RoundsPlayed == 0 {
		return 0
	}
	return float64(s.KASTRounds) / float64(s.RoundsPlayed) * 100
}

// Rating2 returns the community Rating 2.0 proxy for this side.
func (s *PlayerSideStats) Rating2() float64 {
	return rating2Proxy(s.Kills, s.Assists, s.Deaths, s.KASTRounds, s.TotalDamage, s.RoundsPlayed)
}

// PlayerDeathEvent is one row per kill assembled for analytics: who died,
// where, how, and in what tactical context. Produced by aggregator pass 12.
// Denormalised match_date + map_name let meta queries skip a JOIN on demos.
type PlayerDeathEvent struct {
	DemoHash       string
	MatchDate      string
	MapName        string
	RoundNumber    int
	Tick           int
	VictimSteamID  uint64
	VictimTeam     Team
	KillerSteamID  uint64 // 0 for world/fall damage (rare)
	KillerTeam     Team
	Weapon         string
	IsHeadshot     bool
	VictimPos      Vec3
	KillerPos      Vec3
	VictimYawDeg   float64
	DistanceMeters float64
	WasFlashed     bool
	WasTraded      bool
	IsOpeningDeath bool
	RoundPhase     string // "pistol" | "early" | "mid" | "late" | "post_plant"
}

// PlayerDuelSegment holds FHHS stats for one (weapon_bucket, distance_bin) segment per player per demo.
type PlayerDuelSegment struct {
	DemoHash        string
	SteamID         uint64
	WeaponBucket    string  // e.g. "AK", "M4", "AWP", "Deagle", "Pistol", "Other"
	DistanceBin     string  // e.g. "10-15m", "unknown"
	DuelCount       int     // duels won in this segment (with a first-sight)
	FirstHitCount   int     // duels where first shot hit (denominator for FHHS-Hit)
	FirstHitHSCount int     // duels where first shot was a head hit (numerator)
	MedianCorrDeg   float64 // median pre-shot correction angle (degrees)
	MedianSightDeg  float64 // median first-sight angular deviation (degrees)
	MedianExpoWinMs float64 // median exposure time for won duels (ms)
}

// MatchSummary is a lightweight record for list/show commands.
type MatchSummary struct {
	DemoHash   string
	MapName    string
	MatchDate  string
	MatchType  string
	Tickrate   float64
	CTScore    int
	TScore     int
	Tier       string // e.g. "pro", "semi-pro", "faceit-5"; empty for personal matches
	IsBaseline bool   // true for reference corpus demos
	EventID    string // event identifier from demoget (e.g. "iem_cologne_2025"); empty if unknown
}

// PlayerRoleStats holds the HLTV-style role decomposition for one player over
// a filtered set of matches. Produced by cmd/player when --roles is set. All
// rates are per round unless noted. Percentages are 0-100.
//
// Coverage caveat: fields marked "(event-table)" depend on player_death_events
// / grenade_events / flash_events, which are only populated for demos
// aggregated after passes 12/13 shipped. Older demos contribute zero rows;
// re-run `replay --dir <event>/ --force` to backfill.
type PlayerRoleStats struct {
	SteamID uint64
	Name    string

	// Sample size
	Matches      int
	RoundsPlayed int
	RoundsWon    int

	// Headline (§1 top rail)
	Rating2Combined float64
	Rating2CT       float64
	Rating2T        float64
	KASTPct         float64
	KPR             float64
	DPR             float64
	ADR             float64
	MultiKillPct    float64 // rounds with ≥2 kills

	// §2.1 Firepower
	RoundsWithKillPct float64
	KillsPerRoundWin  float64
	DamagePerRoundWin float64
	PistolRoundRating float64 // Rating 2.0 over pistol rounds only

	// §2.2 Entrying
	OpeningDeathTradedPct float64
	SupportRoundsPct      float64

	// §2.3 Trading
	DamagePerKill float64

	// §2.4 Opening
	OpeningKPR         float64
	OpeningDPR         float64
	OpeningAttemptsPct float64
	OpeningSuccessPct  float64
	WinAfterOpenPct    float64

	// §2.5 Clutching
	ClutchPointsPerRound float64 // weighted: 1v1=1, 1v2=2, 1v3=4, 1v4=8, 1v5=16
	OneVOneAttempts      int
	OneVOneWins          int
	OneVOneWinPct        float64
	SavesPerLossPct      float64 // % of round losses where the player survived

	// §2.6 Sniping (event-table)
	SniperKillsPerRound        float64
	SniperKillsPct             float64
	RoundsWithSniperKillPct    float64
	SniperMultiKillRoundPct    float64
	SniperOpeningKillsPerRound float64

	// §2.7 Utility
	UtilityDamagePerRound  float64 // from player_match_stats (full coverage)
	UtilityKillsPer100R    float64 // (event-table)
	FlashesThrownPerRound  float64 // (event-table)
	OppFlashSecPerRound    float64 // (event-table)

	// Coverage flags — true if at least one source-table row was found.
	// Lets the renderer mark blank sections instead of showing 0.00 lies.
	HasSniperData    bool
	HasUtilityData   bool
	HasFlashThrowData bool
	HasFlashTimeData bool
}
