package parserv2

import (
	common "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"

	"github.com/pable/go-cs-metrics/internal/csraw2"
)

// grenadeTypeFromEquipment maps a demoinfocs EquipmentType to the v2
// grenade enum. Returns 0 (i.e. "not a grenade") for non-grenades.
// EqIncendiary collapses to molotov — they share the same on-ground
// behaviour and the demo doesn't reliably distinguish them at the
// projectile level.
func grenadeTypeFromEquipment(t common.EquipmentType) uint8 {
	switch t {
	case common.EqSmoke:
		return csraw2.GrenadeSmoke
	case common.EqFlash:
		return csraw2.GrenadeFlash
	case common.EqHE:
		return csraw2.GrenadeHE
	case common.EqMolotov, common.EqIncendiary:
		return csraw2.GrenadeMolotov
	case common.EqDecoy:
		return csraw2.GrenadeDecoy
	}
	return 0
}

// teamLabel maps a demoinfocs Team to the spec's "T" / "CT" string used in
// header.players[].starting_team and header.rounds[].winner.
func teamLabel(t common.Team) string {
	switch t {
	case common.TeamTerrorists:
		return "T"
	case common.TeamCounterTerrorists:
		return "CT"
	}
	return "DRAW"
}

// teamID maps a demoinfocs Team to the v2 PlayerSample.Team enum.
func teamID(t common.Team) uint8 {
	switch t {
	case common.TeamTerrorists:
		return csraw2.TeamT
	case common.TeamCounterTerrorists:
		return csraw2.TeamCT
	case common.TeamSpectators:
		return csraw2.TeamSpectators
	}
	return csraw2.TeamUnknown
}

// hitGroupID maps a demoinfocs HitGroup to its v2 enum value. The mapping
// is direct except that demoinfocs lacks a "Generic" — we treat HitGroup(0)
// as Generic.
func hitGroupID(hg events.HitGroup) uint8 {
	switch hg {
	case events.HitGroupHead:
		return csraw2.HitGroupHead
	case events.HitGroupChest:
		return csraw2.HitGroupChest
	case events.HitGroupStomach:
		return csraw2.HitGroupStomach
	case events.HitGroupLeftArm:
		return csraw2.HitGroupLeftArm
	case events.HitGroupRightArm:
		return csraw2.HitGroupRightArm
	case events.HitGroupLeftLeg:
		return csraw2.HitGroupLeftLeg
	case events.HitGroupRightLeg:
		return csraw2.HitGroupRightLeg
	case events.HitGroupNeck:
		return csraw2.HitGroupNeck
	case events.HitGroupGear:
		return csraw2.HitGroupGear
	}
	return csraw2.HitGroupGeneric
}

// bombSiteID maps a demoinfocs Bombsite to the v2 enum.
func bombSiteID(s events.Bombsite) uint8 {
	switch s {
	case events.BombsiteA:
		return csraw2.BombSiteA
	case events.BombsiteB:
		return csraw2.BombSiteB
	}
	return csraw2.BombSiteNA
}

// roundEndReasonString converts a demoinfocs RoundEndReason to a stable
// snake_case label. Mirrors the engine "win reason" strings.
func roundEndReasonString(r events.RoundEndReason) string {
	switch r {
	case events.RoundEndReasonTargetBombed:
		return "t_win_bomb_exploded"
	case events.RoundEndReasonVIPEscaped:
		return "ct_win_vip_escape"
	case events.RoundEndReasonVIPKilled:
		return "t_win_vip_killed"
	case events.RoundEndReasonTerroristsEscaped:
		return "t_win_escape"
	case events.RoundEndReasonCTStoppedEscape:
		return "ct_win_stopped_escape"
	case events.RoundEndReasonTerroristsStopped:
		return "ct_win_stopped_t_escape"
	case events.RoundEndReasonBombDefused:
		return "ct_win_bomb_defused"
	case events.RoundEndReasonCTWin:
		return "ct_win_elimination"
	case events.RoundEndReasonTerroristsWin:
		return "t_win_elimination"
	case events.RoundEndReasonHostagesNotRescued:
		return "t_win_hostages_not_rescued"
	case events.RoundEndReasonHostagesRescued:
		return "ct_win_hostages_rescued"
	case events.RoundEndReasonTargetSaved:
		return "ct_win_target_saved"
	case events.RoundEndReasonTerroristsNotEscaped:
		return "ct_win_t_not_escaped"
	case events.RoundEndReasonGameStart:
		return "game_start"
	case events.RoundEndReasonDraw:
		return "draw"
	}
	return "unknown"
}
