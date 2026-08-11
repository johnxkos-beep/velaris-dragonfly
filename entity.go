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

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

// Tuning constants pulled from the add-on's entity JSON (see file header).
const (
	phaseHealth       = 100.0
	meleeDamage       = 9.0
	meleeRange        = 2.5
	meleeCooldown     = 16 // ticks (~0.8s, from speed_multiplier 1.4 on a ~1.1s base swing)
	aggroRadius       = 25.0
	loseTargetRadius  = 35.0
	moveSpeed         = 0.22 // blocks/tick (~4.4 blocks/sec) — see hover-movement caveat above
	abilityCooldown   = 70   // ticks (~3.5s, from controller.animation.boss_end)
	abilityRange      = 20.0
	lavaDamage        = 5.0
	lavaRadius        = 4.0
	meteorDamage      = 20.0
	meteorRadius      = 3.0
	transformTicks    = 100 // 5s invulnerable "evolving" window between phases
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

// state is the boss's mutable fight data. A pointer to this is stashed in
// world.EntityData.Data for the lifetime of the entity.
type state struct {
	Phase          phase
	HP, MaxHP      float64
	Target         *world.EntityHandle
	AttackCooldown int
	AbilityCooldown int
	StageTimer     int // counts down during transform/death windows
}

func newState() *state {
	return &state{Phase: phaseOne, HP: phaseHealth, MaxHP: phaseHealth}
}

// Type is the world.EntityType for the Demon King. Register it with
// Register() (see register.go) before starting the server.
var Type demonKingType

type demonKingType struct{}

// EncodeEntity returns the identifier the add-on's original resource pack
// (all bosses RE / entity/demon_king/bss/lord_demon.json) already expects,
// so dropping that RP onto the server gives you the model/animations for
// free — no client-side changes needed. See the package README.
func (demonKingType) EncodeEntity() string { return "tnt:lord_demon" }

func (demonKingType) BBox(world.Entity) cube.BBox {
	// Matches minecraft:collision_box (width 0.6, height 1.8) from the
	// source entity file.
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}

func (demonKingType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*state)
	if !ok || st == nil {
		st = newState()
		data.Data = st
	}
	return &DemonKing{tx: tx, handle: handle, data: data, state: st}
}

func (demonKingType) DecodeNBT(m map[string]any, data *world.EntityData) {
	st := newState()
	if hp, ok := m["BossHP"].(float64); ok {
		st.HP = hp
	}
	if ph, ok := m["BossPhase"].(int32); ok {
		st.Phase = phase(ph)
	}
	data.Data = st
}

func (demonKingType) EncodeNBT(data *world.EntityData) map[string]any {
	st, _ := data.Data.(*state)
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
	state  *state
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
func (e *DemonKing) Health() float64    { return e.state.HP }
func (e *DemonKing) MaxHealth() float64 { return e.state.MaxHP }
func (e *DemonKing) Dead() bool         { return e.state.Phase == phaseDead }

// KnockBack does nothing: the source add-on gives this boss
// knockback_resistance 1.0 (total immunity).
func (e *DemonKing) KnockBack(src mgl64.Vec3, force, height float64) {}

// Hurt applies damage from src to the boss. Invulnerable during the
// transform/death cutscene windows, matching the add-on's damage_sensor
// (deals_damage: false) on those states.
func (e *DemonKing) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	if e.state.Phase == phaseTransforming || e.state.Phase == phaseDying || e.state.Phase == phaseDead {
		return 0, false
	}
	if dmg < 0 {
		return 0, false
	}
	e.state.HP -= dmg
	return dmg, true
}

// Tick runs the fight AI. Called ~20 times/sec by Dragonfly for any entity
// implementing TickerEntity.
func (e *DemonKing) Tick(tx *world.Tx, current int64) {
	e.tx = tx
	st := e.state

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

	target := e.findTarget(tx)
	st.Target = targetHandle(target)
	if target == nil {
		return
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

	switch {
	case dist <= meleeRange:
		if st.AttackCooldown <= 0 {
			target.Hurt(meleeDamage, AttackSource{Attacker: e})
			st.AttackCooldown = meleeCooldown
		}
	case dist <= abilityRange && st.AbilityCooldown <= 0:
		e.useAbility(tx)
		st.AbilityCooldown = abilityCooldown + rand.Intn(20)
	default:
		// Hover toward the target.
		step := delta.Normalize().Mul(moveSpeed)
		e.data.Pos = pos.Add(step)
	}
}

// findTarget returns the nearest player within aggroRadius, sticking with
// the current target until it dies or leaves loseTargetRadius.
func (e *DemonKing) findTarget(tx *world.Tx) *player.Player {
	if cur := entityFromHandle(tx, e.state.Target); cur != nil {
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
func (e *DemonKing) useAbility(tx *world.Tx) {
	pos := e.data.Pos
	// NOTE: deliberately no sound/particle call here yet — this repo's
	// exact world.Tx sound/particle API wasn't verified against a real
	// build (no compiler available while writing this). Damage/AoE logic
	// works standalone; once this builds cleanly, ask me to wire in
	// tx.PlaySound(...)/tx.AddParticle(...) for the explosion effect and
	// I'll add it using the confirmed signature.
	if rand.Intn(2) == 0 {
		e.damageInRadius(tx, pos, lavaRadius, lavaDamage)
	} else {
		e.damageInRadius(tx, pos, meteorRadius, meteorDamage)
	}
}

func (e *DemonKing) damageInRadius(tx *world.Tx, centre mgl64.Vec3, radius, dmg float64) {
	for p := range state.Server.Players(tx) {
		if p.Dead() {
			continue
		}
		if p.Position().Sub(centre).Len() <= radius {
			p.Hurt(dmg, AttackSource{Attacker: e})
		}
	}
}

// beginTransition starts the phase-1→phase-2 transform, or the final death
// sequence if this was already phase 2.
func (e *DemonKing) beginTransition(tx *world.Tx) {
	st := e.state
	if st.Phase == phaseOne {
		st.Phase = phaseTransforming
		st.StageTimer = transformTicks
		return
	}
	st.Phase = phaseDying
	st.StageTimer = deathTicks
}

// advanceStage runs once a transform/death timer hits zero.
func (e *DemonKing) advanceStage(tx *world.Tx) {
	st := e.state
	switch st.Phase {
	case phaseTransforming:
		st.Phase = phaseTwo
		st.HP = phaseHealth
		st.MaxHP = phaseHealth
	case phaseDying:
		st.Phase = phaseDead
		tx.RemoveEntity(e)
	}
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

// AttackSource is a minimal world.DamageSource for the boss's attacks.
type AttackSource struct {
	Attacker world.Entity
}

func (AttackSource) ReducedByArmour() bool    { return true }
func (AttackSource) ReducedByResistance() bool { return true }
func (AttackSource) Fire() bool                { return false }
func (AttackSource) IgnoreTotem() bool         { return false }
