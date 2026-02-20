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
}

type RawDamage struct {
	Tick, RoundNumber                   int
	AttackerSteamID, VictimSteamID     uint64
	AttackerTeam                        Team
	HealthDamage                        int
	Weapon                              string
	IsUtility                           bool // HE/molotov/incendiary
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
	DemoHash  string
	MapName   string
	MatchDate string
	MatchType string
	Tickrate  float64
	CTScore   int
	TScore    int
}
