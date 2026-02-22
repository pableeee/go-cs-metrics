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
	NearbyVictimTeammates           int // alive teammates of victim within 512 units at kill tick (0 = isolated)
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
	TradeKills, TradeDeaths int
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
