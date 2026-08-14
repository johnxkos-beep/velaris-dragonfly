// Package demonking implements the "Demon King" (Lord Demon / Enma) boss,
// ported from the "all bosses" Bedrock add-on into a native Dragonfly Go
// entity so it can be spawned on velaris-dragonfly without the add-on
// installed on clients (aside from the resource pack for its model — see
// the package README).
//
// PORTED FROM THE ADD-ON (numbers pulled directly from its behaviour pack):
//   - Two real combat phases, each 100 HP (entities/demon king/bss/lord_demon.json
//     and lord_demon_v2.json), joined by a short invulnerable transformation
//     window (minecraft:evoluir timer, originally 10s — kept here at 5s so
//     fights don't stall).
//   - Melee attack: 9 damage (minecraft:attack), speed_multiplier 1.4.
//   - Knockback resistance 1.0 (minecraft:knockback_resistance) — the boss
//     ignores knockback entirely.
//   - Aggro/target radius 25, forgets target beyond 35
//     (minecraft:behavior.nearest_attackable_target).
//   - Two AoE abilities pulled from the animation timelines that actually
//     deal damage:
//     "Lava Eruption" (animations/demon king/hab_lava_1.json): 5 damage,
//     radius 2, explosion sound.
//     "Meteor Strike" (animations/demon king/meteorio.json): 20 damage,
//     radius 2, explosion sound + smoke particles.
//   - Random attack cycling on ~3.5s intervals (controller.animation.boss_end
//     in animation_controllers/boss.json).
//
// DELIBERATELY NOT PORTED (per request — no drops/items):
//   - The "eye of the boss" sealed-statue wake-up ritual. The spawn egg
//     summons the boss already awake and hostile.
//   - All weapon drops, the legendary weapon item abilities (katana/dagger/
//     sword/book — those are PLAYER-held items in the add-on, not boss
//     attacks) and loot tables.
//
// ENGINE-LEVEL CAVEAT — PLEASE READ:
// Dragonfly, unlike PocketMine-MP, does not ship any built-in hostile-mob
// AI or pathfinding (no A*, no navigation mesh). There is no equivalent of
// Bedrock's minecraft:navigation.walk here. This implementation moves the
// boss in a straight line toward its target (a "hover" style approach, not
// unlike how the add-on's teleport ability already breaks pathing anyway).
// It will not path around obstacles. If you want proper ground pathing
// later, that's a separate, substantial addition on top of this.
package demonking

import (
	"math"
	"math/rand"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

// Tuning constants pulled from the add-on's entity JSON (see file header).
const (
	phaseHealth       = 200.0 // doubled per request (was 100 per phase, matching the add-on exactly)
	meleeDamage       = 9.0
	meleeRange        = 2.5
	meleeCooldown     = 16 // ticks (~0.8s, from speed_multiplier 1.4 on a ~1.1s base swing)
	aggroRadius       = 25.0
	loseTargetRadius  = 35.0
	moveSpeed         = 0.22 // blocks/tick (~4.4 blocks/sec) — see hover-movement caveat above
	abilityCooldown   = 70   // ticks (~3.5s, from controller.animation.boss_end)
	abilityRange      = 20.0
	teleportCooldown  = 90   // ticks (~4.5s — matches the add-on's separate teleport-check timer in boss_end, distinct from the attack-combo cycle)
	teleportMinRange  = 6.0  // won't teleport-strike if you're already this close — only used to close distance, not to spam
	teleportDamage    = 14.0 // bonus strike dealt on arrival — new addition, not from the add-on (which doesn't have this ability on the boss at all, only a generic teleport behaviour reused across bosses in that file)
	lavaDamage        = 5.0
	lavaRadius        = 4.0
	meteorDamage      = 20.0
	meteorRadius      = 3.0
	transformTicks    = 100 // 5s invulnerable "evolving" window between phases
	knockbackScale    = 0.4 // fraction of a normal hit's knockback he takes (was 1.0 — too much, dialed back down)
	knockbackDecay    = 0.65 // velocity multiplier applied each tick while reeling from a hit (higher = travels further before stopping)
	deathTicks        = 40  // 2s death animation window before despawn
)

// phase describes which stage of the fight the boss is in.
type phase int

const (
	phaseOne phase = iota
	phaseTransforming
	phaseTwo
	phaseDying
	phaseDead
)

// fightState is the boss's mutable fight data. A pointer to this is stashed in
// world.EntityData.Data for the lifetime of the entity.
type fightState struct {
	Phase          phase
	HP, MaxHP      float64
	Target         *world.EntityHandle
	AttackCooldown int
	AbilityCooldown int
	TeleportCooldown int
	StageTimer     int // counts down during transform/death windows
	Speed          float64
}

func newState() *fightState {
	return &fightState{Phase: phaseOne, HP: phaseHealth, MaxHP: phaseHealth, Speed: moveSpeed}
}

// Type is the world.EntityType for the Demon King. Register it with
// Register() (see register.go) before starting the server.
var (
	// Type is phase 1 — spawned by the spawn egg. Register all four of
	// these in EntityRegistry(), not just Type.
	Type demonKingType
	// TransformType is the brief invulnerable cutscene between phase 1
	// and phase 2, rendered using the add-on's dedicated "trasformar"
	// model instead of reusing the phase-1 model.
	TransformType demonKingTransformType
	// TypeV2 is phase 2, rendered using the add-on's dedicated "v2" model.
	TypeV2 demonKingV2Type
	// DeathType is the final death animation window, rendered using the
	// add-on's dedicated "morte" model, before he's removed for good.
	DeathType demonKingDeathType
)

type demonKingType struct{}

// EncodeEntity returns the identifier the add-on's original resource pack
// (all bosses RE / entity/demon_king/bss/lord_demon.json) already expects,
// so dropping that RP onto the server gives you the model/animations for
// free — no client-side changes needed. See the package README.
func (demonKingType) EncodeEntity() string { return "tnt:lord_demon" }
func (demonKingType) BBox(e world.Entity) cube.BBox { return demonKingBBox }
func (demonKingType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openDemonKing(tx, handle, data)
}
func (demonKingType) DecodeNBT(m map[string]any, data *world.EntityData) { decodeDemonKingNBT(m, data) }
func (demonKingType) EncodeNBT(data *world.EntityData) map[string]any    { return encodeDemonKingNBT(data) }

// demonKingTransformType, demonKingV2Type, and demonKingDeathType are
// identical to demonKingType in every way except EncodeEntity — that's
// deliberate: switching identifiers (which means despawning and
// respawning, see respawnAs below) is the only way to change which model
// the client renders, since EncodeEntity can't read fight state. All four
// share the same DemonKing.Tick logic via openDemonKing.
type demonKingTransformType struct{}

func (demonKingTransformType) EncodeEntity() string { return "tnt:lord_demon_trasformar" }
func (demonKingTransformType) BBox(e world.Entity) cube.BBox { return demonKingBBox }
func (demonKingTransformType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openDemonKing(tx, handle, data)
}
func (demonKingTransformType) DecodeNBT(m map[string]any, data *world.EntityData) { decodeDemonKingNBT(m, data) }
func (demonKingTransformType) EncodeNBT(data *world.EntityData) map[string]any    { return encodeDemonKingNBT(data) }

type demonKingV2Type struct{}

func (demonKingV2Type) EncodeEntity() string { return "tnt:lord_demon_v2" }
func (demonKingV2Type) BBox(e world.Entity) cube.BBox { return demonKingBBox }
func (demonKingV2Type) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openDemonKing(tx, handle, data)
}
func (demonKingV2Type) DecodeNBT(m map[string]any, data *world.EntityData) { decodeDemonKingNBT(m, data) }
func (demonKingV2Type) EncodeNBT(data *world.EntityData) map[string]any    { return encodeDemonKingNBT(data) }

type demonKingDeathType struct{}

func (demonKingDeathType) EncodeEntity() string { return "tnt:lord_demon_morte" }
func (demonKingDeathType) BBox(e world.Entity) cube.BBox { return demonKingBBox }
func (demonKingDeathType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openDemonKing(tx, handle, data)
}
func (demonKingDeathType) DecodeNBT(m map[string]any, data *world.EntityData) { decodeDemonKingNBT(m, data) }
func (demonKingDeathType) EncodeNBT(data *world.EntityData) map[string]any    { return encodeDemonKingNBT(data) }

// demonKingBBox matches minecraft:collision_box (width 0.6, height 1.8)
// from the source entity file — same across all four models for simplicity.
var demonKingBBox = cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)

func openDemonKing(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*fightState)
	if !ok || st == nil {
		st = newState()
		data.Data = st
	}
	return &DemonKing{tx: tx, handle: handle, data: data, fight: st}
}

func decodeDemonKingNBT(m map[string]any, data *world.EntityData) {
	st := newState()
	if hp, ok := m["BossHP"].(float64); ok {
		st.HP = hp
	}
	if ph, ok := m["BossPhase"].(int32); ok {
		st.Phase = phase(ph)
	}
	data.Data = st
}

func encodeDemonKingNBT(data *world.EntityData) map[string]any {
	st, _ := data.Data.(*fightState)
	if st == nil {
		st = newState()
	}
	return map[string]any{
		"BossHP":    st.HP,
		"BossPhase": int32(st.Phase),
	}
}

// Config configures a newly spawned Demon King. Zero value is fine for a
// normal fresh spawn.
type Config struct{}

func (Config) Apply(data *world.EntityData) {
	if data.Data == nil {
		data.Data = newState()
	}
}

// stateConfig spawns a Demon King carrying over an EXISTING fight state
// (same HP, phase, target, etc.) instead of starting fresh — used by
// respawnAs to preserve everything across a mid-fight model swap.
type stateConfig struct{ state *fightState }

func (c stateConfig) Apply(data *world.EntityData) { data.Data = c.state }

// Spawn creates and adds a Demon King boss to tx at pos, awake and hostile.
func Spawn(tx *world.Tx, pos mgl64.Vec3) *DemonKing {
	handle := world.EntitySpawnOpts{Position: pos}.New(Type, Config{})
	e := tx.AddEntity(handle)
	dk, _ := e.(*DemonKing)
	return dk
}

// DemonKing is the live, in-transaction entity implementation.
type DemonKing struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	fight  *fightState
}

func (e *DemonKing) H() *world.EntityHandle   { return e.handle }
func (e *DemonKing) Position() mgl64.Vec3     { return e.data.Pos }
func (e *DemonKing) Rotation() cube.Rotation  { return e.data.Rot }
func (e *DemonKing) Close() error             { return nil }

// Health-related accessors. Player.AttackEntity (and anything else in
// Dragonfly that deals damage to an arbitrary world.Entity) looks for an
// entity implementing something in this shape — exact method set can vary
// by Dragonfly version. If `go build` complains that DemonKing doesn't
// satisfy an expected "Living"/damageable interface, tell me the interface
// Dragonfly is expecting (the compiler error will name it) and I'll adjust
// these signatures — the fight logic itself won't need to change.
func (e *DemonKing) Health() float64    { return e.fight.HP }
func (e *DemonKing) MaxHealth() float64 { return e.fight.MaxHP }

// SetMaxHealth changes the boss's max HP, clamping current HP down if needed.
func (e *DemonKing) SetMaxHealth(health float64) {
	e.fight.MaxHP = health
	if e.fight.HP > e.fight.MaxHP {
		e.fight.HP = e.fight.MaxHP
	}
}

// SetSpeed changes the boss's movement speed (blocks/tick) — hooked into
// the hover-movement code, so speed/slowness-style effects will actually
// change how fast he closes distance, not just satisfy the interface.
func (e *DemonKing) SetSpeed(speed float64) { e.fight.Speed = speed }

// Speed returns the boss's current movement speed (blocks/tick).
func (e *DemonKing) Speed() float64 { return e.fight.Speed }

// SetVelocity stores an externally-applied velocity (e.g. from an
// explosion or another source of knockback). Combined with knockback
// resistance 1.0 (see KnockBack above), this mostly just satisfies the
// interface — the boss ignores incoming knockback per the add-on.
func (e *DemonKing) SetVelocity(v mgl64.Vec3) { e.data.Vel = v }

// Velocity returns the boss's current velocity.
func (e *DemonKing) Velocity() mgl64.Vec3 { return e.data.Vel }

func (e *DemonKing) Dead() bool         { return e.fight.Phase == phaseDead }

// AddEffect, Effects, and RemoveEffect satisfy the potion-effect part of
// entity.Living. The boss doesn't use potion effects, so these are no-ops —
// if the compiler still complains after adding this, it's telling us the
// exact method it wants next; paste that error and it's a one-line fix.
func (e *DemonKing) AddEffect(eff effect.Effect)            {}
func (e *DemonKing) Effects() []effect.Effect               { return nil }
func (e *DemonKing) RemoveEffect(t effect.Type)              {}

// Heal restores health, used by regeneration effects/healing items — not
// really relevant to a boss, but required by the interface. Returns the
// amount actually healed.
func (e *DemonKing) Heal(health float64, src world.HealingSource) float64 {
	before := e.fight.HP
	e.fight.HP += health
	if e.fight.HP > e.fight.MaxHP {
		e.fight.HP = e.fight.MaxHP
	}
	return e.fight.HP - before
}

// KnockBack does nothing: the source add-on gives this boss
// knockback_resistance 1.0 (total immunity).
// KnockBack shoves the boss away from src (the attacker's position). The
// add-on gives this boss total knockback resistance, but per your request
// this now applies a (reduced, boss-sized) real shove instead of ignoring
// it — see knockbackScale below to tune how much it moves him.
func (e *DemonKing) KnockBack(src mgl64.Vec3, force, height float64) {
	dir := e.data.Pos.Sub(src)
	dir[1] = 0
	if dir.Len() > 0.001 {
		dir = dir.Normalize()
	} else {
		dir = mgl64.Vec3{0, 0, 0}
	}
	e.data.Vel = mgl64.Vec3{
		dir.X() * force * knockbackScale,
		height * knockbackScale,
		dir.Z() * force * knockbackScale,
	}
}

// Hurt applies damage from src to the boss. Invulnerable during the
// transform/death cutscene windows, matching the add-on's damage_sensor
// (deals_damage: false) on those states.
func (e *DemonKing) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	if e.fight.Phase == phaseTransforming || e.fight.Phase == phaseDying || e.fight.Phase == phaseDead {
		return 0, false
	}
	if dmg < 0 {
		return 0, false
	}
	e.fight.HP -= dmg

	// Broadcast the hurt event so viewers see the red damage flash/hear
	// the hurt sound — this never happened before, which is why hits
	// looked like they weren't registering even when they were.
	for _, v := range e.tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.HurtAction{})
	}

	return dmg, true
}

// Tick runs the fight AI. Called ~20 times/sec by Dragonfly for any entity
// implementing TickerEntity.
func (e *DemonKing) Tick(tx *world.Tx, current int64) {
	e.tx = tx
	st := e.fight

	switch st.Phase {
	case phaseTransforming, phaseDying:
		st.StageTimer--
		if st.StageTimer <= 0 {
			e.advanceStage(tx)
		}
		return
	case phaseDead:
		return
	}

	if st.HP <= 0 {
		e.beginTransition(tx)
		return
	}

	// Apply and decay any active knockback velocity (from KnockBack)
	// before running normal AI — while he's still reeling from a hit he
	// skips the chase/attack logic so the shove is actually visible
	// instead of being instantly overridden by AI movement.
	if e.data.Vel.Len() > 0.03 {
		e.data.Pos = e.snapToGround(tx, e.data.Pos.Add(e.data.Vel))
		e.data.Vel = e.data.Vel.Mul(knockbackDecay)
		for _, v := range tx.Viewers(e.data.Pos) {
			v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, true)
		}
		return
	}

	target := e.findTarget(tx)
	st.Target = targetHandle(target)
	if target == nil {
		return
	}

	if current%10 == 0 {
		e.updateBossBar(target)
	}

	pos := e.data.Pos
	tpos := target.Position()
	delta := tpos.Sub(pos)
	delta[1] = 0 // keep the boss level with the ground plane it's standing on
	dist := delta.Len()

	// Face the target.
	if dist > 0.01 {
		yaw := math.Atan2(-delta.X(), delta.Z()) * 180 / math.Pi
		e.data.Rot = cube.Rotation{yaw, 0}
	}

	if st.AttackCooldown > 0 {
		st.AttackCooldown--
	}
	if st.AbilityCooldown > 0 {
		st.AbilityCooldown--
	}
	if st.TeleportCooldown > 0 {
		st.TeleportCooldown--
	}

	// Movement and abilities are independent: he always closes distance
	// when out of melee range, and can ALSO fire an ability off cooldown
	// on the same tick — previously an ability being ready would skip
	// movement entirely, which is why he could stand still indefinitely
	// while you were within ability range but outside melee range.
	if dist <= meleeRange {
		if st.AttackCooldown <= 0 {
			target.Hurt(meleeDamage, dfentity.AttackDamageSource{Attacker: e})
			e.playAttackAnimation(tx)
			st.AttackCooldown = meleeCooldown
		}
	} else {
		step := delta.Normalize().Mul(st.Speed)
		e.data.Pos = e.snapToGround(tx, pos.Add(step))
	}
	if dist <= abilityRange && st.AbilityCooldown <= 0 {
		e.useAbility(tx)
		st.AbilityCooldown = abilityCooldown + rand.Intn(20)
	}
	if dist > teleportMinRange && dist <= abilityRange && st.TeleportCooldown <= 0 {
		e.teleportStrike(tx, target)
		st.TeleportCooldown = teleportCooldown + rand.Intn(20)
	}

	// Broadcast the (possibly new) position/rotation to every nearby
	// viewer. This is the actual fix for "he doesn't move on my screen" —
	// writing e.data.Pos updates the server's own record of where he is
	// (which is why the debug pings showed it changing), but nothing
	// tells a connected client to move the model unless we explicitly
	// notify each viewer, which is what this loop does.
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, true)
	}
}

// findTarget returns the nearest player within aggroRadius, sticking with
// the current target until it dies or leaves loseTargetRadius.
func (e *DemonKing) findTarget(tx *world.Tx) *player.Player {
	if cur := entityFromHandle(tx, e.fight.Target); cur != nil {
		if p, ok := cur.(*player.Player); ok && !p.Dead() {
			if p.Position().Sub(e.data.Pos).Len() <= loseTargetRadius {
				return p
			}
		}
	}

	var (
		nearest    *player.Player
		nearestDst = math.MaxFloat64
	)
	for p := range state.Server.Players(tx) {
		if p.Dead() {
			continue
		}
		d := p.Position().Sub(e.data.Pos).Len()
		if d <= aggroRadius && d < nearestDst {
			nearest, nearestDst = p, d
		}
	}
	return nearest
}

// useAbility fires one of the boss's two AoE attacks, damage numbers taken
// straight from the add-on's animation timelines (see package doc).
// snapToGround adjusts pos's Y so the boss follows terrain instead of
// staying frozen at his original spawn height (which is what let him walk
// through hills/walls and float over drops before this fix). It searches
// downward from just above the current position for the first solid block
// and stands him on top of it; if nothing solid is found within range
// (e.g. walking off a ledge) it keeps sinking gradually instead of
// snapping instantly, so falls still look like falls.
//
// UNVERIFIED: tx.Block(pos) and block.Air{} are my best-confidence guess
// at Dragonfly's block-lookup API, but untested against a real build —
// if this doesn't compile, the error will tell us the right names.
func (e *DemonKing) snapToGround(tx *world.Tx, pos mgl64.Vec3) mgl64.Vec3 {
	const searchUp, searchDown = 2, 10
	bp := cube.PosFromVec3(pos)
	for i := searchUp; i >= -searchDown; i-- {
		below := cube.Pos{bp.X(), bp.Y() + i, bp.Z()}
		if _, isAir := tx.Block(below).(block.Air); !isAir {
			return mgl64.Vec3{pos.X(), float64(below.Y() + 1), pos.Z()}
		}
	}
	// Nothing solid found nearby — let him sink instead of floating.
	return mgl64.Vec3{pos.X(), pos.Y() - 0.3, pos.Z()}
}

// playAttackAnimation broadcasts an attack action to nearby viewers, which
// sets Bedrock's built-in variable.attack_time client-side — that's what
// the resource pack's controller.animation.ataque_boss (bosses/demon king/
// animation_controllers/ataque_boss.json) watches to advance through his
// ataque_1 → ataque_2 → ataque_3 combo, so calling this on every hit/cast
// both plays a swing animation and naturally cycles the combo, matching
// the add-on's own design.
//
// UNVERIFIED: dfentity.SwingArmAction — same method Dragonfly's own Player.SwingArm() uses (same
// family as HurtAction, which is confirmed working) but untested against
// a real build. If the compiler complains, paste the error — likely just
// a naming tweak.
func (e *DemonKing) playAttackAnimation(tx *world.Tx) {
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.SwingArmAction{})
	}
}

func (e *DemonKing) useAbility(tx *world.Tx) {
	pos := e.data.Pos
	// His model doesn't have dedicated ability-cast animations — hab_lava
	// and meteorio belong to the player-held legendary weapons in the
	// original add-on, not to the boss himself — so this reuses his own
	// attack animation as the closest available cue that something
	// happened, plus a particle/sound burst at the impact point so the
	// two abilities at least feel distinct from a plain melee hit. Also
	// announces itself in chat to the target so it's unambiguous which
	// attack landed, since visually the swing looks the same as melee.
	//
	// UNVERIFIED: particle.HugeExplosion{} and sound.Explosion{} are
	// best-guess type names (matched to what a Bedrock explosion effect
	// is usually called) — tx.AddParticle/PlaySound themselves are
	// confirmed real methods, just not these exact type names. If the
	// compiler complains it'll name the real package contents.
	e.playAttackAnimation(tx)
	tx.PlaySound(pos, sound.Explosion{})
	tx.AddParticle(pos, particle.HugeExplosion{})
	if p, ok := entityFromHandle(tx, e.fight.Target).(*player.Player); ok {
		if rand.Intn(2) == 0 {
			p.Message("§6Demon King uses §cLava Eruption§6!")
		} else {
			p.Message("§6Demon King uses §cMeteor Strike§6!")
		}
	}
	if rand.Intn(2) == 0 {
		e.damageInRadius(tx, pos, lavaRadius, lavaDamage)
	} else {
		e.damageInRadius(tx, pos, meteorRadius, meteorDamage)
	}
}

// teleportStrike is a new addition (not from the add-on's boss-specific
// files — the source only had a generic teleport behaviour shared across
// bosses in that file, with no damage tied to it). Teleports him next to
// the target and immediately follows up with a bonus strike, mainly so he
// can catch up to a kiting/fleeing player instead of just plodding after
// them at moveSpeed forever.
func (e *DemonKing) teleportStrike(tx *world.Tx, target *player.Player) {
	old := e.data.Pos
	tx.AddParticle(old, particle.HugeExplosion{})

	tpos := target.Position()
	dir := old.Sub(tpos)
	if dir.Len() < 0.001 {
		dir = mgl64.Vec3{1, 0, 0}
	} else {
		dir = dir.Normalize()
	}
	newPos := e.snapToGround(tx, tpos.Add(dir.Mul(1.5)))
	e.data.Pos = newPos

	tx.PlaySound(newPos, sound.Explosion{})
	tx.AddParticle(newPos, particle.HugeExplosion{})
	// UNVERIFIED: ViewEntityTeleport is a guess at the method name for an
	// instant (non-interpolated) position jump, as opposed to
	// ViewEntityMovement's smooth walking interpolation — if this doesn't
	// exist under this name, ViewEntityMovement(e, newPos, e.data.Rot,
	// true) is the safe fallback (just looks like a very fast slide
	// instead of an instant blink).
	for _, v := range tx.Viewers(newPos) {
		v.ViewEntityTeleport(e, newPos)
	}

	target.Message("§5Demon King teleports behind you!")
	target.Hurt(teleportDamage, dfentity.AttackDamageSource{Attacker: e})
	e.playAttackAnimation(tx)
}

func (e *DemonKing) damageInRadius(tx *world.Tx, centre mgl64.Vec3, radius, dmg float64) {
	for p := range state.Server.Players(tx) {
		if p.Dead() {
			continue
		}
		if p.Position().Sub(centre).Len() <= radius {
			p.Hurt(dmg, dfentity.AttackDamageSource{Attacker: e})
		}
	}
}

// beginTransition starts the phase-1→phase-2 transform, or the final death
// sequence if this was already phase 2.
func (e *DemonKing) beginTransition(tx *world.Tx) {
	st := e.fight
	if st.Phase == phaseOne {
		st.Phase = phaseTransforming
		st.StageTimer = transformTicks
		e.respawnAs(tx, TransformType)
		return
	}
	st.Phase = phaseDying
	st.StageTimer = deathTicks
	e.respawnAs(tx, DeathType)
}

// respawnAs despawns e and immediately spawns a new Demon King of type t at
// the same position/rotation, carrying over the SAME *fightState pointer —
// so HP, phase, target, and cooldowns all persist across the swap. This is
// how the model changes for the transform/phase-2/death stages, since
// EncodeEntity can't read fight state and switch identifier on its own;
// the add-on itself does the equivalent (despawn+respawn) for its own
// phase transitions.
func (e *DemonKing) respawnAs(tx *world.Tx, t world.EntityType) {
	pos, rot := e.data.Pos, e.data.Rot
	st := e.fight
	tx.RemoveEntity(e)
	handle := world.EntitySpawnOpts{Position: pos, Rotation: rot}.New(t, stateConfig{state: st})
	tx.AddEntity(handle)
}

// advanceStage runs once a transform/death timer hits zero.
func (e *DemonKing) advanceStage(tx *world.Tx) {
	st := e.fight
	switch st.Phase {
	case phaseTransforming:
		st.Phase = phaseTwo
		st.HP = phaseHealth
		st.MaxHP = phaseHealth
		e.respawnAs(tx, TypeV2)
	case phaseDying:
		st.Phase = phaseDead
		if p := entityFromHandle(tx, st.Target); p != nil {
			if pl, ok := p.(*player.Player); ok {
				pl.RemoveBossBar()
			}
		}
		tx.RemoveEntity(e)
	}
}

// updateBossBar shows/refreshes a boss bar on p reflecting his current
// phase and HP. Called periodically from Tick rather than every tick to
// avoid spamming boss bar packets.
func (e *DemonKing) updateBossBar(p *player.Player) {
	pct := e.fight.HP / e.fight.MaxHP
	if pct < 0 {
		pct = 0
	} else if pct > 1 {
		pct = 1
	}
	title := "§5Demon King"
	switch e.fight.Phase {
	case phaseOne:
		title += " §7- Phase 1"
	case phaseTwo:
		title += " §7- Phase 2"
	}
	p.SendBossBar(bossbar.New(title).WithHealthPercentage(pct).WithColour(bossbar.Purple()))
}

func targetHandle(p *player.Player) *world.EntityHandle {
	if p == nil {
		return nil
	}
	return p.H()
}

func entityFromHandle(tx *world.Tx, h *world.EntityHandle) world.Entity {
	if h == nil {
		return nil
	}
	ent, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return ent
}

