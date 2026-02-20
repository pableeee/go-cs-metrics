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

type RawKill struct {
	Tick, RoundNumber               int
	KillerSteamID, VictimSteamID   uint64
	AssisterSteamID                 uint64 // 0 if none
	KillerTeam, VictimTeam          Team
	Weapon                          string
	IsHeadshot, AssistedFlash       bool
	NearbyVictimTeammates           int // alive teammates of victim within 512 units at kill tick (0 = isolated)
}

type RawDamage struct {
	Tick, RoundNumber                   int
	AttackerSteamID, VictimSteamID     uint64
	AttackerTeam                        Team
	HealthDamage                        int
	Weapon                              string
	IsUtility                           bool   // HE/molotov/incendiary
	HitGroup                            string // "head", "chest", "stomach", "left_arm", "right_arm", "left_leg", "right_leg", "other"
}

type RawFlash struct {
	Tick, RoundNumber               int
	AttackerSteamID, VictimSteamID uint64
	AttackerTeam, VictimTeam       Team
	FlashDuration                  time.Duration
}

type PlayerRoundEndState struct {
	SteamID64    uint64
	IsAlive      bool
	Team         Team
	GrenadeCount int
}

type RawRound struct {
	Number, StartTick, FreezeEndTick, EndTick int
	WinnerTeam                                Team
	PlayerEndState                            map[uint64]PlayerRoundEndState
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

// RawWeaponFire is emitted by the parser each time a player fires a weapon.
type RawWeaponFire struct {
	Tick        int
	RoundNumber int
	ShooterID   uint64
	Weapon      string
	PitchDeg    float64 // normalized view pitch at fire tick
	YawDeg      float64 // view yaw at fire tick
}

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

type PlayerMatchStats struct {
	DemoHash   string
	SteamID    uint64
	Name       string
	Team       Team

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
}

func (s *PlayerMatchStats) KDRatio() float64 {
	if s.Deaths == 0 {
		return float64(s.Kills)
	}
	return float64(s.Kills) / float64(s.Deaths)
}

func (s *PlayerMatchStats) HSPercent() float64 {
	if s.Kills == 0 {
		return 0
	}
	return float64(s.HeadshotKills) / float64(s.Kills) * 100
}

func (s *PlayerMatchStats) ADR() float64 {
	if s.RoundsPlayed == 0 {
		return 0
	}
	return float64(s.TotalDamage) / float64(s.RoundsPlayed)
}

func (s *PlayerMatchStats) KASTPct() float64 {
	if s.RoundsPlayed == 0 {
		return 0
	}
	return float64(s.KASTRounds) / float64(s.RoundsPlayed) * 100
}

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
}

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

func (s *PlayerWeaponStats) HSPercent() float64 {
	if s.Kills == 0 {
		return 0
	}
	return float64(s.HeadshotKills) / float64(s.Kills) * 100
}

func (s *PlayerWeaponStats) AvgDamagePerHit() float64 {
	if s.Hits == 0 {
		return 0
	}
	return float64(s.Damage) / float64(s.Hits)
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
	Tier       string // e.g. "faceit-5", "faceit-8"; empty for personal matches
	IsBaseline bool   // true for reference corpus demos
}
