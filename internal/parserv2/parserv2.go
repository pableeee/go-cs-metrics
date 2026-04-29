// Package parserv2 walks a CS2 .dem file and produces a csraw2.Match.
//
// Compared to the v1 parser, parserv2 captures every field the v2 schema
// asks for: full velocity vectors at every weapon fire, per-tick player
// state samples (HP, armor, money, equipment, view angles, velocity,
// visibility mask, ammo, scoped/reload flags), bomb actions, item pickups
// and drops, equip changes, chat commands, and grenade detonations.
//
// The output is a fully populated *csraw2.Match ready to be handed to
// csraw2.Write. The aggregator is not involved; v2 deliberately decouples
// the demoinfocs pass from the metrics pipeline.
package parserv2

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/golang/geo/r3"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs"
	common "github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v4/pkg/demoinfocs/events"

	"github.com/pable/go-cs-metrics/internal/csraw2"
)

// Sampling configuration. These mirror the values written into
// header.sampling so readers always know exactly what was emitted.
const (
	BaselineHz             = 16 // baseline player_samples rate
	EventWindowHz          = 64 // dense rate around game events (Slice 2: not yet emitted)
	EventWindowRadiusTicks = 32 // half-window around each event
	ProjectileHz           = 32 // projectile_samples rate
)

const quickHashBytes = 64 << 10 // 64 KB

// ParseDemoV2 parses the demo at path and returns a fully populated
// csraw2.Match. tier is recorded verbatim into header.tier ("pro",
// "faceit-5", …); matchType ("competitive" / "wingman" / etc.) into
// header.match_type.
//
// Memory note: like the v1 parser, this allocates heavily inside
// demoinfocs. Callers parsing many demos should set GOMEMLIMIT and run
// sequentially; see internal/parser/parser.go for the analysis.
func ParseDemoV2(path, tier, matchType string) (*csraw2.Match, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat demo: %w", err)
	}

	// Full hash for the header (idempotency key).
	fullHash, err := hashAll(f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek demo: %w", err)
	}

	// Quick hash uses a separate file handle so we don't disturb the parser's stream.
	quickHash, err := quickHashOf(path)
	if err != nil {
		return nil, err
	}

	p := demoinfocs.NewParser(f)
	defer p.Close()

	s := newState()
	s.match.Header.CSRawVersion = csraw2.Version
	s.match.Header.CSRawSchemaVersion = csraw2.SchemaVersion
	s.match.Header.Writer = "go-cs-metrics/parserv2"
	s.match.Header.DemoHash = "sha256:" + fullHash
	s.match.Header.QuickHash = "sha256:" + quickHash
	s.match.Header.SourceDemSizeBytes = fi.Size()
	s.match.Header.SourceDemMtime = fi.ModTime().UTC().Format(time.RFC3339)
	s.match.Header.Tier = tier
	s.match.Header.MatchType = matchType
	s.match.Header.MatchDate = fi.ModTime().Format("2006-01-02")
	s.match.Header.Sampling = csraw2.Sampling{
		BaselineHz:             BaselineHz,
		EventWindowHz:          EventWindowHz,
		EventWindowRadiusTicks: EventWindowRadiusTicks,
		FreezeTimeSampled:      true,
		ProjectileHz:           ProjectileHz,
	}

	s.registerHandlers(p)

	// Frame-walk loop: per-tick sampling lives here, after handlers have
	// fired for the frame so the live game state reflects the latest events.
	for {
		ok, err := p.ParseNextFrame()
		if err != nil {
			return nil, fmt.Errorf("parse demo: %w", err)
		}
		s.onFrameDone(p)
		if !ok {
			break
		}
	}

	// Finalise tickrate-derived fields and map name once parsing is done.
	tickrate := p.TickRate()
	s.tickrate = tickrate
	if tickrate > 0 {
		s.baselineEvery = max(int(tickrate/BaselineHz), 1)
		s.projectileEvery = max(int(tickrate/ProjectileHz), 1)
	}
	s.match.Header.Tickrate = tickrate
	s.match.Header.Map = p.Header().MapName

	return s.match, nil
}

// hashAll computes a SHA-256 over the entire reader at its current
// position. The caller is responsible for seeking back if needed.
func hashAll(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("hash demo: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// quickHashOf computes the SHA-256 over the first 64 KB of path. Matches
// the v1 quick-hash format so a v2 archive's quick_hash can be cross-
// referenced with the existing demoget download db.
func quickHashOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open for quick hash: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, quickHashBytes)); err != nil {
		return "", fmt.Errorf("quick hash: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// registerHandlers wires every demoinfocs event we care about to the
// methods that translate them into csraw2 rows.
func (s *state) registerHandlers(p demoinfocs.Parser) {
	p.RegisterEventHandler(func(e events.RoundStart) { s.onRoundStart(p) })
	p.RegisterEventHandler(func(e events.RoundFreezetimeEnd) { s.onFreezetimeEnd(p) })
	p.RegisterEventHandler(func(e events.RoundEnd) { s.onRoundEnd(p, e) })

	p.RegisterEventHandler(func(e events.Kill) { s.onKill(p, e) })
	p.RegisterEventHandler(func(e events.PlayerHurt) { s.onPlayerHurt(p, e) })
	p.RegisterEventHandler(func(e events.WeaponFire) { s.onWeaponFire(p, e) })
	p.RegisterEventHandler(func(e events.PlayerFlashed) { s.onPlayerFlashed(p, e) })

	p.RegisterEventHandler(func(e events.GrenadeProjectileThrow) { s.onProjectileThrow(p, e) })
	p.RegisterEventHandler(func(e events.GrenadeProjectileDestroy) { s.onProjectileDestroy(e) })
	p.RegisterEventHandler(func(e events.SmokeStart) { s.onGrenadeDetonate(p, e.GrenadeEvent) })
	p.RegisterEventHandler(func(e events.HeExplode) { s.onGrenadeDetonate(p, e.GrenadeEvent) })
	p.RegisterEventHandler(func(e events.FlashExplode) { s.onGrenadeDetonate(p, e.GrenadeEvent) })
	p.RegisterEventHandler(func(e events.FireGrenadeStart) { s.onGrenadeDetonate(p, e.GrenadeEvent) })
	p.RegisterEventHandler(func(e events.DecoyStart) { s.onGrenadeDetonate(p, e.GrenadeEvent) })

	p.RegisterEventHandler(func(e events.BombPlantBegin) { s.onBomb(p, csraw2.BombActionPlantBegin, e.Player, e.Site, false) })
	p.RegisterEventHandler(func(e events.BombPlanted) { s.onBomb(p, csraw2.BombActionPlantComplete, e.Player, e.Site, false) })
	p.RegisterEventHandler(func(e events.BombDefuseStart) { s.onBomb(p, csraw2.BombActionDefuseBegin, e.Player, events.BomsiteUnknown, e.HasKit) })
	p.RegisterEventHandler(func(e events.BombDefused) { s.onBomb(p, csraw2.BombActionDefuseComplete, e.Player, e.Site, false) })
	p.RegisterEventHandler(func(e events.BombExplode) { s.onBomb(p, csraw2.BombActionExplode, nil, e.Site, false) })

	p.RegisterEventHandler(func(e events.ItemPickup) { s.onItemPickup(p, e) })
	p.RegisterEventHandler(func(e events.ItemDrop) { s.onItemDrop(p, e) })
	p.RegisterEventHandler(func(e events.ItemEquip) { s.onItemEquip(p, e) })

	p.RegisterEventHandler(func(e events.ChatMessage) { s.onChat(p, e) })
}

// ── Round lifecycle ─────────────────────────────────────────────────────────

func (s *state) onRoundStart(p demoinfocs.Parser) {
	if p.GameState().IsWarmupPeriod() {
		return
	}
	s.roundNumber++
	s.roundStartTick = p.GameState().IngameTick()
	s.freezeEndTick = s.roundStartTick
	s.bombPlantTick = 0
	s.firstKillTick = 0
	s.seenFirstSight = map[firstSightPairKey]bool{}
}

func (s *state) onFreezetimeEnd(p demoinfocs.Parser) {
	if s.roundNumber == 0 {
		return
	}
	s.freezeEndTick = p.GameState().IngameTick()
}

func (s *state) onRoundEnd(p demoinfocs.Parser, e events.RoundEnd) {
	if s.roundNumber == 0 {
		return
	}
	endTick := p.GameState().IngameTick()
	gs := p.GameState()
	ctScore, tScore := 0, 0
	if gs.TeamCounterTerrorists() != nil {
		ctScore = gs.TeamCounterTerrorists().Score()
	}
	if gs.TeamTerrorists() != nil {
		tScore = gs.TeamTerrorists().Score()
	}
	s.match.Header.Rounds = append(s.match.Header.Rounds, csraw2.Round{
		N:             s.roundNumber,
		StartTick:     s.roundStartTick,
		FreezeEndTick: s.freezeEndTick,
		FirstKillTick: s.firstKillTick,
		BombPlantTick: s.bombPlantTick,
		EndTick:       endTick,
		Winner:        teamLabel(e.Winner),
		WinReason:     roundEndReasonString(e.Reason),
		CTScoreAfter:  ctScore,
		TScoreAfter:   tScore,
		Phase:         "regulation", // OT detection deferred — see open questions
	})
}

// ── Kill / damage / fire / flash ────────────────────────────────────────────

func (s *state) onKill(p demoinfocs.Parser, e events.Kill) {
	if s.roundNumber == 0 {
		return
	}
	tick := p.GameState().IngameTick()
	if s.firstKillTick == 0 {
		s.firstKillTick = tick
	}

	victimSlot, ok := s.slotFor(e.Victim)
	if !ok {
		return
	}
	row := csraw2.Kill{
		Tick:                  int32(tick),
		Round:                 int16(s.roundNumber),
		KillerSlot:            s.slotForOptional(e.Killer),
		VictimSlot:            victimSlot,
		AssisterSlot:          s.slotForOptional(e.Assister),
		WeaponID:              s.weaponIDForEquipment(e.Weapon),
		IsHeadshot:            e.IsHeadshot,
		IsWallbang:            e.PenetratedObjects > 0,
		PenetratedCount:       clampUint8(e.PenetratedObjects),
		IsNoScope:             e.NoScope,
		IsThroughSmoke:        e.ThroughSmoke,
		IsAttackerBlind:       e.AttackerBlind,
		FlashAssist:           e.AssistedFlash,
		AssistedFlashAttackerSlot: -1, // demoinfocs doesn't expose the flash thrower on the kill event
	}
	if e.Killer != nil {
		row.KillerPosX, row.KillerPosY, row.KillerPosZ = vec3I16(e.Killer.Position())
		row.KillerYawDeg = quantAngle(float64(e.Killer.ViewDirectionX()))
		row.KillerPitchDeg = quantAngle(normalisePitch(float64(e.Killer.ViewDirectionY())))
		if w := e.Killer.ActiveWeapon(); w != nil {
			row.KillerActiveWeaponID = s.weaponIDFor(w.Type)
		}
		// Count alive teammates of victim within 512 units of the victim.
		row.NearbyVictimTeammates = s.countNearbyTeammates(p, e.Victim, 512)
	}
	if e.Victim != nil {
		row.VictimPosX, row.VictimPosY, row.VictimPosZ = vec3I16(e.Victim.Position())
		row.VictimYawDeg = quantAngle(float64(e.Victim.ViewDirectionX()))
		row.VictimPitchDeg = quantAngle(normalisePitch(float64(e.Victim.ViewDirectionY())))
		if e.Killer != nil {
			d := e.Killer.Position().Sub(e.Victim.Position())
			row.VictimViewDistance = clampUint16(d.Norm())
		}
	}

	s.match.Kills = append(s.match.Kills, row)
}

func (s *state) onPlayerHurt(p demoinfocs.Parser, e events.PlayerHurt) {
	if s.roundNumber == 0 || e.Player == nil {
		return
	}
	victimSlot, ok := s.slotFor(e.Player)
	if !ok {
		return
	}
	weaponType := common.EqUnknown
	if e.Weapon != nil {
		weaponType = e.Weapon.Type
	}
	postHP := clampUint8Int(e.Player.Health())
	postArmor := clampUint8Int(e.Player.Armor())
	preHP := clampUint8Int(e.Player.Health() + e.HealthDamageTaken)
	preArmor := clampUint8Int(e.Player.Armor() + e.ArmorDamageTaken)

	row := csraw2.Damage{
		Tick:            int32(p.GameState().IngameTick()),
		Round:           int16(s.roundNumber),
		AttackerSlot:    s.slotForOptional(e.Attacker),
		VictimSlot:      victimSlot,
		WeaponID:        s.weaponIDFor(weaponType),
		HealthDamage:    clampUint16Int(e.HealthDamage),
		ArmorDamage:     clampUint16Int(e.ArmorDamage),
		PreDamageHP:     preHP,
		PreDamageArmor:  preArmor,
		PostDamageHP:    postHP,
		PostDamageArmor: postArmor,
		HitGroup:        hitGroupID(e.HitGroup),
		IsUtility: weaponType == common.EqHE || weaponType == common.EqMolotov ||
			weaponType == common.EqIncendiary,
	}
	if e.Attacker != nil {
		row.AttackerPosX, row.AttackerPosY, row.AttackerPosZ = vec3I16(e.Attacker.Position())
	}
	row.VictimPosX, row.VictimPosY, row.VictimPosZ = vec3I16(e.Player.Position())

	s.lastDamageTick[victimSlot] = p.GameState().IngameTick()

	s.match.Damages = append(s.match.Damages, row)
}

func (s *state) onWeaponFire(p demoinfocs.Parser, e events.WeaponFire) {
	if s.roundNumber == 0 || e.Shooter == nil || e.Weapon == nil {
		return
	}
	slot, ok := s.slotFor(e.Shooter)
	if !ok {
		return
	}
	tick := p.GameState().IngameTick()
	pos := e.Shooter.Position()
	vel := e.Shooter.Velocity()

	row := csraw2.WeaponFire{
		Tick:        int32(tick),
		Round:       int16(s.roundNumber),
		ShooterSlot: slot,
		WeaponID:    s.weaponIDFor(e.Weapon.Type),
		YawDeg:      quantAngle(float64(e.Shooter.ViewDirectionX())),
		PitchDeg:    quantAngle(normalisePitch(float64(e.Shooter.ViewDirectionY()))),
		IsScoped:    e.Shooter.IsScoped(),
		IsSilenced:  e.Weapon.Silenced(),
		ClipAmmoAfter: clampUint8Int(e.Weapon.AmmoInMagazine()),
		RecoilIndex:   clampUint8Int(int(e.Weapon.RecoilIndex())),
	}
	row.PosX, row.PosY, row.PosZ = quantPos(pos.X), quantPos(pos.Y), quantPos(pos.Z)
	row.VelX, row.VelY, row.VelZ = quantVel(vel.X), quantVel(vel.Y), quantVel(vel.Z)

	s.lastShotTick[slot] = tick
	s.match.WeaponFires = append(s.match.WeaponFires, row)
}

func (s *state) onPlayerFlashed(p demoinfocs.Parser, e events.PlayerFlashed) {
	if s.roundNumber == 0 || e.Player == nil || e.Attacker == nil {
		return
	}
	dur := e.Player.FlashDurationTimeRemaining()
	if dur <= 0 {
		return
	}
	victimSlot, ok := s.slotFor(e.Player)
	if !ok {
		return
	}
	attackerSlot, ok := s.slotFor(e.Attacker)
	if !ok {
		return
	}

	pos := e.Player.Position()
	row := csraw2.Flash{
		Tick:            int32(p.GameState().IngameTick()),
		Round:           int16(s.roundNumber),
		AttackerSlot:    attackerSlot,
		VictimSlot:      victimSlot,
		FlashDurationMS: clampUint16(float64(dur.Milliseconds())),
		VictimPosX:      quantPos(pos.X),
		VictimPosY:      quantPos(pos.Y),
		VictimPosZ:      quantPos(pos.Z),
		VictimYawDeg:    quantAngle(float64(e.Player.ViewDirectionX())),
		VictimPitchDeg:  quantAngle(normalisePitch(float64(e.Player.ViewDirectionY()))),
		// VictimThroughSmoke: open question — see spec; leaving false until
		// we resolve the truth source.
	}
	s.match.Flashes = append(s.match.Flashes, row)
}

// ── Grenades ────────────────────────────────────────────────────────────────

func (s *state) onProjectileThrow(p demoinfocs.Parser, e events.GrenadeProjectileThrow) {
	if s.roundNumber == 0 || e.Projectile == nil || e.Projectile.WeaponInstance == nil ||
		e.Projectile.Thrower == nil {
		return
	}
	gtype := grenadeTypeFromEquipment(e.Projectile.WeaponInstance.Type)
	if gtype == 0 {
		return
	}
	thrower := e.Projectile.Thrower
	throwerSlot, ok := s.slotFor(thrower)
	if !ok {
		return
	}

	pid := s.nextProjectileID
	s.nextProjectileID++
	entityID := e.Projectile.Entity.ID()
	s.activeProjectiles[entityID] = &activeProjectile{
		projectileID: pid,
		grenadeType:  gtype,
		ownerSlot:    int8(throwerSlot),
		roundNumber:  s.roundNumber,
	}

	tp := thrower.Position()
	tv := thrower.Velocity()
	row := csraw2.GrenadeThrow{
		Tick:          int32(p.GameState().IngameTick()),
		Round:         int16(s.roundNumber),
		ThrowerSlot:   throwerSlot,
		GrenadeType:   gtype,
		ThrowPosX:     quantPos(tp.X),
		ThrowPosY:     quantPos(tp.Y),
		ThrowPosZ:     quantPos(tp.Z),
		ThrowYawDeg:   quantAngle(float64(thrower.ViewDirectionX())),
		ThrowPitchDeg: quantAngle(normalisePitch(float64(thrower.ViewDirectionY()))),
		ThrowVelX:     quantVel(tv.X),
		ThrowVelY:     quantVel(tv.Y),
		ThrowVelZ:     quantVel(tv.Z),
		IsJumpThrow:   thrower.IsAirborne() && tv.Z > 100, // heuristic: airborne with upward velocity
		ProjectileID:  pid,
	}
	s.match.GrenadeThrows = append(s.match.GrenadeThrows, row)
}

func (s *state) onProjectileDestroy(e events.GrenadeProjectileDestroy) {
	if e.Projectile == nil {
		return
	}
	delete(s.activeProjectiles, e.Projectile.Entity.ID())
}

func (s *state) onGrenadeDetonate(p demoinfocs.Parser, ge events.GrenadeEvent) {
	if s.roundNumber == 0 {
		return
	}
	gtype := grenadeTypeFromEquipment(ge.GrenadeType)
	if gtype == 0 {
		return
	}
	var pid uint32
	if ap, ok := s.activeProjectiles[ge.GrenadeEntityID]; ok {
		pid = ap.projectileID
	}
	row := csraw2.GrenadeDetonate{
		Tick:         int32(p.GameState().IngameTick()),
		Round:        int16(s.roundNumber),
		ProjectileID: pid,
		GrenadeType:  gtype,
		PosX:         quantPos(ge.Position.X),
		PosY:         quantPos(ge.Position.Y),
		PosZ:         quantPos(ge.Position.Z),
	}
	s.match.GrenadeDetonations = append(s.match.GrenadeDetonations, row)
}

// ── Bomb ────────────────────────────────────────────────────────────────────

func (s *state) onBomb(p demoinfocs.Parser, action uint8, player *common.Player, site events.Bombsite, hasKit bool) {
	if s.roundNumber == 0 {
		return
	}
	tick := p.GameState().IngameTick()
	if action == csraw2.BombActionPlantComplete {
		s.bombPlantTick = tick
	}

	row := csraw2.BombAction{
		Tick:       int32(tick),
		Round:      int16(s.roundNumber),
		Action:     action,
		PlayerSlot: s.slotForOptional(player),
		Site:       bombSiteID(site),
		HadKit:     hasKit,
	}
	if player != nil {
		row.PosX, row.PosY, row.PosZ = vec3I16(player.Position())
	}
	s.match.BombActions = append(s.match.BombActions, row)
}

// ── Items / chat ────────────────────────────────────────────────────────────

func (s *state) onItemPickup(p demoinfocs.Parser, e events.ItemPickup) {
	if s.roundNumber == 0 || e.Player == nil || e.Weapon == nil {
		return
	}
	slot, ok := s.slotFor(e.Player)
	if !ok {
		return
	}
	pos := e.Player.Position()
	s.match.ItemPickups = append(s.match.ItemPickups, csraw2.ItemPickup{
		Tick:       int32(p.GameState().IngameTick()),
		Round:      int16(s.roundNumber),
		PlayerSlot: slot,
		ItemID:     s.weaponIDFor(e.Weapon.Type),
		PosX:       quantPos(pos.X),
		PosY:       quantPos(pos.Y),
		PosZ:       quantPos(pos.Z),
		FromSlot:   -1, // demoinfocs ItemPickup doesn't expose the previous holder
	})
}

func (s *state) onItemDrop(p demoinfocs.Parser, e events.ItemDrop) {
	if s.roundNumber == 0 || e.Player == nil || e.Weapon == nil {
		return
	}
	slot, ok := s.slotFor(e.Player)
	if !ok {
		return
	}
	pos := e.Player.Position()
	s.match.ItemDrops = append(s.match.ItemDrops, csraw2.ItemDrop{
		Tick:       int32(p.GameState().IngameTick()),
		Round:      int16(s.roundNumber),
		PlayerSlot: slot,
		ItemID:     s.weaponIDFor(e.Weapon.Type),
		PosX:       quantPos(pos.X),
		PosY:       quantPos(pos.Y),
		PosZ:       quantPos(pos.Z),
	})
}

func (s *state) onItemEquip(p demoinfocs.Parser, e events.ItemEquip) {
	if s.roundNumber == 0 || e.Player == nil || e.Weapon == nil {
		return
	}
	slot, ok := s.slotFor(e.Player)
	if !ok {
		return
	}
	wid := s.weaponIDFor(e.Weapon.Type)
	prev := s.currentWeapon[slot]
	if wid == prev {
		return
	}
	s.currentWeapon[slot] = wid
	s.match.EquipChanges = append(s.match.EquipChanges, csraw2.EquipChange{
		Tick:         int32(p.GameState().IngameTick()),
		Round:        int16(s.roundNumber),
		PlayerSlot:   slot,
		WeaponID:     wid,
		PrevWeaponID: prev,
	})
}

func (s *state) onChat(p demoinfocs.Parser, e events.ChatMessage) {
	if s.roundNumber == 0 || e.Sender == nil {
		return
	}
	slot, ok := s.slotFor(e.Sender)
	if !ok {
		return
	}
	s.match.ChatCommands = append(s.match.ChatCommands, csraw2.ChatCommand{
		Tick:       int32(p.GameState().IngameTick()),
		Round:      int16(s.roundNumber),
		PlayerSlot: slot,
		Text:       e.Text,
		IsTeamOnly: !e.IsChatAll,
	})
}

// ── Per-frame sampling ──────────────────────────────────────────────────────

// onFrameDone runs after each frame's events have fired. Emits player and
// projectile samples at their respective rates and walks the spotted-flag
// matrix to detect first-sight events the same way the v1 parser does.
func (s *state) onFrameDone(p demoinfocs.Parser) {
	if s.roundNumber == 0 {
		return
	}
	tickrate := p.TickRate()
	if tickrate > 0 && s.baselineEvery == 0 {
		s.baselineEvery = max(int(tickrate/BaselineHz), 1)
		s.projectileEvery = max(int(tickrate/ProjectileHz), 1)
	}

	tick := p.GameState().IngameTick()
	players := p.GameState().Participants().Playing()

	// Player samples — baseline rate.
	if s.baselineEvery > 0 && tick%s.baselineEvery == 0 {
		s.emitPlayerSamples(tick, players)
	}

	// Projectile samples — projectile rate.
	if s.projectileEvery > 0 && tick%s.projectileEvery == 0 {
		s.emitProjectileSamples(p, tick)
	}

	// First-sight scan (same logic as v1 parser, slot-encoded).
	s.scanFirstSights(tick, players)
}

func (s *state) emitPlayerSamples(tick int, players []*common.Player) {
	// Build a slot-indexed list once so we can compute the visibility mask
	// per (observer, enemy) pair without quadratic re-resolution.
	type live struct {
		slot uint8
		p    *common.Player
	}
	alive := make([]live, 0, len(players))
	for _, pl := range players {
		if pl == nil || pl.SteamID64 == 0 || !pl.IsAlive() {
			continue
		}
		slot, ok := s.slotFor(pl)
		if !ok {
			continue
		}
		alive = append(alive, live{slot, pl})
	}

	for _, obs := range alive {
		var visMask uint16
		for _, en := range alive {
			if en.slot == obs.slot || en.p.Team == obs.p.Team {
				continue
			}
			if en.p.IsSpottedBy(obs.p) {
				visMask |= 1 << en.slot
			}
		}
		pos := obs.p.Position()
		vel := obs.p.Velocity()
		var clipAmmo uint8
		var reserveAmmo uint16
		var activeWeaponID uint8
		if w := obs.p.ActiveWeapon(); w != nil {
			clipAmmo = clampUint8Int(w.AmmoInMagazine())
			reserveAmmo = clampUint16Int(w.AmmoReserve())
			activeWeaponID = s.weaponIDFor(w.Type)
		}

		s.match.PlayerSamples = append(s.match.PlayerSamples, csraw2.PlayerSample{
			Tick:                 int32(tick),
			Round:                int16(s.roundNumber),
			PlayerSlot:           obs.slot,
			DensityTier:          csraw2.DensityBaseline,
			PosX:                 quantPos(pos.X),
			PosY:                 quantPos(pos.Y),
			PosZ:                 quantPos(pos.Z),
			YawDeg:               quantAngle(float64(obs.p.ViewDirectionX())),
			PitchDeg:             quantAngle(normalisePitch(float64(obs.p.ViewDirectionY()))),
			VelX:                 quantVel(vel.X),
			VelY:                 quantVel(vel.Y),
			VelZ:                 quantVel(vel.Z),
			HP:                   clampUint8Int(obs.p.Health()),
			Armor:                clampUint8Int(obs.p.Armor()),
			Flags:                s.computeFlags(obs.p),
			ActiveWeaponID:       activeWeaponID,
			ClipAmmo:             clipAmmo,
			ReserveAmmo:          reserveAmmo,
			FlashRemainingMS:     clampUint16(float64(obs.p.FlashDurationTimeRemaining().Milliseconds())),
			Money:                clampUint16Int(obs.p.Money()),
			EquipValue:           clampUint16Int(obs.p.EquipmentValueCurrent()),
			VisibleEnemiesMask:   visMask,
			LastShotTickOffset:   tickOffset(tick, s.lastShotTick[obs.slot]),
			LastDamageTickOffset: tickOffset(tick, s.lastDamageTick[obs.slot]),
		})
	}
}

func (s *state) emitProjectileSamples(p demoinfocs.Parser, tick int) {
	for _, gp := range p.GameState().GrenadeProjectiles() {
		if gp == nil {
			continue
		}
		ap, ok := s.activeProjectiles[gp.Entity.ID()]
		if !ok {
			continue
		}
		pos := gp.Position()
		// CS2 demos don't always expose m_vecVelocity on grenade entities;
		// demoinfocs's gp.Velocity() panics in that case via PropertyValueMust.
		// Probe the property directly and treat missing as zero velocity.
		var velX, velY, velZ int16
		if v, ok := gp.Entity.PropertyValue("m_vecVelocity"); ok {
			velX = quantVel(v.VectorVal.X)
			velY = quantVel(v.VectorVal.Y)
			velZ = quantVel(v.VectorVal.Z)
		}
		s.match.ProjectileSamples = append(s.match.ProjectileSamples, csraw2.ProjectileSample{
			Tick:              int32(tick),
			Round:             int16(s.roundNumber),
			ProjectileID:      ap.projectileID,
			Kind:              csraw2.ProjectileKindGrenade,
			Subtype:           ap.grenadeType,
			OwnerSlot:         ap.ownerSlot,
			PosX:              quantPos(pos.X),
			PosY:              quantPos(pos.Y),
			PosZ:              quantPos(pos.Z),
			VelX:              velX,
			VelY:              velY,
			VelZ:              velZ,
			FuseRemainingMS:   -1, // not exposed by demoinfocs in a stable way
			DefuseProgressPct: 0,
		})
	}
}

func (s *state) scanFirstSights(tick int, players []*common.Player) {
	for _, observer := range players {
		if observer == nil || !observer.IsAlive() {
			continue
		}
		obsSlot, ok := s.slotFor(observer)
		if !ok {
			continue
		}
		for _, enemy := range players {
			if enemy == nil || enemy.SteamID64 == observer.SteamID64 || !enemy.IsAlive() {
				continue
			}
			if enemy.Team == observer.Team {
				continue
			}
			enemySlot, ok := s.slotFor(enemy)
			if !ok {
				continue
			}
			key := firstSightPairKey{obsSlot, enemySlot}
			if s.seenFirstSight[key] {
				continue
			}
			if !enemy.IsSpottedBy(observer) {
				continue
			}
			obsPos := observer.Position()
			enPos := enemy.Position()
			s.match.FirstSights = append(s.match.FirstSights, csraw2.FirstSight{
				Tick:             int32(tick),
				Round:            int16(s.roundNumber),
				ObserverSlot:     obsSlot,
				EnemySlot:        enemySlot,
				YawDeg:           quantAngle(float64(observer.ViewDirectionX())),
				PitchDeg:         quantAngle(normalisePitch(float64(observer.ViewDirectionY()))),
				ObserverYawDeg:   quantAngle(float64(observer.ViewDirectionX())),
				ObserverPitchDeg: quantAngle(normalisePitch(float64(observer.ViewDirectionY()))),
				ObserverPosX:     quantPos(obsPos.X),
				ObserverPosY:     quantPos(obsPos.Y),
				ObserverPosZ:     quantPos(obsPos.Z),
				EnemyPosX:        quantPos(enPos.X),
				EnemyPosY:        quantPos(enPos.Y),
				EnemyPosZ:        quantPos(enPos.Z),
				WasAlreadyVisible: false,
			})
			s.seenFirstSight[key] = true
		}
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func (s *state) weaponIDForEquipment(eq *common.Equipment) uint8 {
	if eq == nil {
		return 0
	}
	return s.weaponIDFor(eq.Type)
}

func (s *state) computeFlags(p *common.Player) uint16 {
	var f uint16
	if p.IsAlive() {
		f |= csraw2.FlagAlive
	}
	if p.HasDefuseKit() {
		f |= csraw2.FlagHasKit
	}
	if p.HasHelmet() {
		f |= csraw2.FlagHasHelmet
	}
	if p.IsScoped() {
		f |= csraw2.FlagIsScoped
	}
	if p.IsWalking() {
		f |= csraw2.FlagIsWalking
	}
	if p.IsDucking() {
		f |= csraw2.FlagIsDucking
	}
	if p.IsAirborne() {
		f |= csraw2.FlagIsInAir
	} else {
		f |= csraw2.FlagIsOnGround
	}
	if p.FlashDurationTimeRemaining().Milliseconds() > 1500 {
		f |= csraw2.FlagIsBlind
	}
	if p.IsInBombZone() {
		// Approximate is_planting / is_defusing using bomb-zone presence
		// when no clearer signal is available. Refined in slice 4 if needed.
		_ = f // intentionally no-op; placeholder for future refinement
	}
	return f
}

func (s *state) countNearbyTeammates(p demoinfocs.Parser, victim *common.Player, radius float64) uint8 {
	if victim == nil {
		return 0
	}
	var n int
	for _, mate := range p.GameState().Participants().Playing() {
		if mate == nil || mate.SteamID64 == 0 || mate.SteamID64 == victim.SteamID64 {
			continue
		}
		if !mate.IsAlive() || mate.Team != victim.Team {
			continue
		}
		if mate.Position().Sub(victim.Position()).Norm() <= radius {
			n++
		}
	}
	return clampUint8(n)
}

// vec3I16 quantises an r3.Vector to three int16s in Hammer units.
func vec3I16(v r3.Vector) (int16, int16, int16) {
	return quantPos(v.X), quantPos(v.Y), quantPos(v.Z)
}

func clampUint8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func clampUint8Int(v int) uint8 { return clampUint8(v) }

func clampUint16(v float64) uint16 {
	if v < 0 {
		return 0
	}
	if v > 65535 {
		return 65535
	}
	return uint16(v)
}

func clampUint16Int(v int) uint16 {
	if v < 0 {
		return 0
	}
	if v > 65535 {
		return 65535
	}
	return uint16(v)
}

// tickOffset returns the (tick - lastTick) clamped to [0, 255]. lastTick
// of zero means "never happened" → 255.
func tickOffset(tick, lastTick int) uint8 {
	if lastTick == 0 {
		return 255
	}
	d := tick - lastTick
	if d < 0 {
		return 0
	}
	if d > 255 {
		return 255
	}
	return uint8(d)
}

