// Package mobs implements simple passive Bedrock mobs (currently cow and
// chicken) as native Dragonfly Go entities, following the same pattern
// already used by bosses/demonking in this repo (see that package's doc
// comment for the full rationale — same engine-level caveats apply here).
//
// ENGINE-LEVEL CAVEAT (same as demonking): Dragonfly has no built-in
// pathfinding. These mobs "wander" by picking a random direction and
// walking a straight line for a short burst, then pausing — they will not
// path around obstacles, and may occasionally walk into a wall or off a
// short ledge (snapToGround keeps them on the ground either way, they just
// won't clip a corner nicely). That matches how demonking already moves.
//
// PERFORMANCE: everything in Tick is O(1) — a handful of int/float
// comparisons and, when moving, one downward block-search (snapToGround)
// plus a broadcast to nearby viewers (tx.Viewers already scopes that to
// players who can actually see the mob, so it doesn't grow with total
// player count). There is no per-tick loop over all players or all
// entities. A few dozen of these mobs on a server should be unnoticeable;
// if you eventually want hundreds, the main thing to revisit is the
// wander/idle tick ranges below (fewer, longer bursts spread the
// snapToGround cost out further).
//
// WHAT'S IMPLEMENTED: wandering movement, fleeing briefly after being hit,
// real knockback (no resistance, unlike the boss), health/death, item
// drops on death, and XP granted to whichever player landed the killing
// blow. NOT implemented (deliberately, to keep this simple and cheap):
// natural/ambient world spawning, breeding, babies, eating grass/wheat,
// chicken egg-laying, water avoidance/swimming, and fall damage. Any of
// those are separate, larger additions on top of this if you want them
// later.
package mobs

import (
	"math"
	"math/rand"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Tuning constants shared by every mob species. Per-species numbers (HP,
// walk speed, XP, drops) live in each species' own file (cow.go, chicken.go).
const (
	knockbackDecay      = 0.65 // velocity multiplier applied per tick while reeling from a hit — same value demonking uses
	fleeDurationTicks    = 60   // ~3s of running away after being hit
	fleeSpeedMultiplier = 1.8  // how much faster than normal walk speed a mob flees
	idleMinTicks        = 40
	idleMaxTicks        = 120
	wanderMinTicks       = 30
	wanderMaxTicks      = 90
)

// mobSpec holds the per-species numbers and behaviour that differ between
// cow and chicken. A *mobSpec is shared (read-only) across every instance
// of that species — only mobState is per-entity.
type mobSpec struct {
	MaxHP     float64
	WalkSpeed float64 // blocks/tick
	XPMin     int     // XP granted to the killer is XPMin..XPMax inclusive
	XPMax     int
	Drops     func() []item.Stack // called once on death
}

// mobState is the mutable per-entity data, stashed in world.EntityData.Data
// for the lifetime of the entity — same approach as demonking's fightState.
type mobState struct {
	HP, MaxHP    float64
	Speed        float64 // current walk speed (blocks/tick) — externally settable via SetSpeed
	WanderDir    mgl64.Vec3
	WanderTicks  int
	IdleTicks    int
	FleeDir      mgl64.Vec3
	FleeTicks    int
	LastAttacker *world.EntityHandle // who to credit XP to on death
	Dead         bool
}

func newMobState(spec *mobSpec) *mobState {
	return &mobState{HP: spec.MaxHP, MaxHP: spec.MaxHP, Speed: spec.WalkSpeed}
}

// mobConfig configures a freshly spawned mob of the given species.
type mobConfig struct{ spec *mobSpec }

func (c mobConfig) Apply(data *world.EntityData) {
	if data.Data == nil {
		data.Data = newMobState(c.spec)
	}
}

// spawn is the shared implementation behind SpawnCow/SpawnChicken.
func spawn(tx *world.Tx, t world.EntityType, spec *mobSpec, pos mgl64.Vec3) *Mob {
	handle := world.EntitySpawnOpts{Position: pos}.New(t, mobConfig{spec: spec})
	e := tx.AddEntity(handle)
	m, _ := e.(*Mob)
	return m
}

func openMob(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData, spec *mobSpec) world.Entity {
	st, ok := data.Data.(*mobState)
	if !ok || st == nil {
		st = newMobState(spec)
		data.Data = st
	}
	return &Mob{tx: tx, handle: handle, data: data, state: st, spec: spec}
}

func decodeMobNBT(m map[string]any, data *world.EntityData, spec *mobSpec) {
	st := newMobState(spec)
	if hp, ok := m["MobHP"].(float64); ok {
		st.HP = hp
	}
	data.Data = st
}

func encodeMobNBT(data *world.EntityData) map[string]any {
	st, _ := data.Data.(*mobState)
	if st == nil {
		return map[string]any{}
	}
	return map[string]any{"MobHP": st.HP}
}

// Mob is the live, in-transaction entity implementation shared by every
// passive mob species in this package.
type Mob struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *mobState
	spec   *mobSpec
}

func (e *Mob) H() *world.EntityHandle  { return e.handle }
func (e *Mob) Position() mgl64.Vec3    { return e.data.Pos }
func (e *Mob) Rotation() cube.Rotation { return e.data.Rot }
func (e *Mob) Close() error            { return nil }

// Health-related accessors — same interface shape demonking already
// implements successfully (see its entity.go for the fuller explanation).
func (e *Mob) Health() float64    { return e.state.HP }
func (e *Mob) MaxHealth() float64 { return e.state.MaxHP }

func (e *Mob) SetMaxHealth(health float64) {
	e.state.MaxHP = health
	if e.state.HP > e.state.MaxHP {
		e.state.HP = e.state.MaxHP
	}
}

func (e *Mob) SetSpeed(speed float64) { e.state.Speed = speed }
func (e *Mob) Speed() float64         { return e.state.Speed }

func (e *Mob) SetVelocity(v mgl64.Vec3) { e.data.Vel = v }
func (e *Mob) Velocity() mgl64.Vec3     { return e.data.Vel }

func (e *Mob) Dead() bool { return e.state.Dead }

// AddEffect, Effects, RemoveEffect satisfy the potion-effect part of
// entity.Living. Not used by these mobs — no-ops, same as demonking.
func (e *Mob) AddEffect(eff effect.Effect)   {}
func (e *Mob) Effects() []effect.Effect      { return nil }
func (e *Mob) RemoveEffect(t effect.Type)    {}

func (e *Mob) Heal(health float64, src world.HealingSource) float64 {
	before := e.state.HP
	e.state.HP += health
	if e.state.HP > e.state.MaxHP {
		e.state.HP = e.state.MaxHP
	}
	return e.state.HP - before
}

// KnockBack shoves the mob away from src at full force — unlike the boss,
// these mobs have no knockback resistance, matching vanilla cow/chicken.
func (e *Mob) KnockBack(src mgl64.Vec3, force, height float64) {
	dir := e.data.Pos.Sub(src)
	dir[1] = 0
	if dir.Len() > 0.001 {
		dir = dir.Normalize()
	} else {
		dir = mgl64.Vec3{0, 0, 0}
	}
	e.data.Vel = mgl64.Vec3{dir.X() * force, height, dir.Z() * force}
}

// Hurt applies damage, flashes the hurt animation, and — if the source was
// an identifiable attacker — starts the mob fleeing away from it and, if
// the attacker is a player, remembers them so death can credit XP to the
// right person.
func (e *Mob) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	if e.state.Dead || dmg <= 0 {
		return 0, false
	}
	e.state.HP -= dmg

	for _, v := range e.tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.HurtAction{})
	}

	if asrc, ok := src.(dfentity.AttackDamageSource); ok && asrc.Attacker != nil {
		dir := e.data.Pos.Sub(asrc.Attacker.Position())
		dir[1] = 0
		if dir.Len() > 0.001 {
			e.state.FleeDir = dir.Normalize()
		} else {
			e.state.FleeDir = mgl64.Vec3{1, 0, 0}
		}
		e.state.FleeTicks = fleeDurationTicks
		e.state.WanderTicks = 0
		e.state.IdleTicks = 0

		if p, ok := asrc.Attacker.(*player.Player); ok {
			e.state.LastAttacker = p.H()
		}
	}

	return dmg, true
}

// Tick runs the wander/flee AI. See the package doc comment above for the
// performance notes on why this is safe to run for many entities.
func (e *Mob) Tick(tx *world.Tx, current int64) {
	e.tx = tx
	st := e.state

	if st.Dead {
		return
	}
	if st.HP <= 0 {
		e.die(tx)
		return
	}

	// Apply and decay knockback before running normal AI, so a hit is
	// actually visible instead of being instantly overridden.
	if e.data.Vel.Len() > 0.03 {
		e.data.Pos = e.snapToGround(tx, e.data.Pos.Add(e.data.Vel))
		e.data.Vel = e.data.Vel.Mul(knockbackDecay)
		e.broadcastMove(tx)
		return
	}

	if st.FleeTicks > 0 {
		st.FleeTicks--
		step := st.FleeDir.Mul(st.Speed * fleeSpeedMultiplier)
		e.data.Pos = e.snapToGround(tx, e.data.Pos.Add(step))
		e.faceDirection(st.FleeDir)
		e.broadcastMove(tx)
		return
	}

	if st.WanderTicks > 0 {
		st.WanderTicks--
		step := st.WanderDir.Mul(st.Speed)
		e.data.Pos = e.snapToGround(tx, e.data.Pos.Add(step))
		e.broadcastMove(tx)
		return
	}

	if st.IdleTicks > 0 {
		st.IdleTicks--
		return
	}

	e.pickNextAction()
}

// pickNextAction chooses the mob's next behaviour once it's finished both
// wandering and standing still: either pause again, or pick a new random
// direction to wander in. Only runs once every wander/idle cycle (roughly
// every 1–3 seconds), not every tick.
func (e *Mob) pickNextAction() {
	st := e.state
	if rand.Intn(3) == 0 {
		st.IdleTicks = idleMinTicks + rand.Intn(idleMaxTicks-idleMinTicks)
		return
	}
	angle := rand.Float64() * 2 * math.Pi
	st.WanderDir = mgl64.Vec3{math.Cos(angle), 0, math.Sin(angle)}
	st.WanderTicks = wanderMinTicks + rand.Intn(wanderMaxTicks-wanderMinTicks)
	e.faceDirection(st.WanderDir)
}

func (e *Mob) faceDirection(dir mgl64.Vec3) {
	if dir.Len() < 0.001 {
		return
	}
	yaw := math.Atan2(-dir.X(), dir.Z()) * 180 / math.Pi
	e.data.Rot = cube.Rotation{yaw, 0}
}

func (e *Mob) broadcastMove(tx *world.Tx) {
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, true)
	}
}

// die drops loot, grants XP to the last attacker (if a player), and
// removes the mob. Guarded so it only ever runs once.
func (e *Mob) die(tx *world.Tx) {
	if e.state.Dead {
		return
	}
	e.state.Dead = true

	e.dropLoot(tx)

	if e.state.LastAttacker != nil {
		if ent, ok := e.state.LastAttacker.Entity(tx); ok {
			if p, ok := ent.(*player.Player); ok {
				gain := e.spec.XPMin
				if e.spec.XPMax > e.spec.XPMin {
					gain += rand.Intn(e.spec.XPMax - e.spec.XPMin + 1)
				}
				// UNVERIFIED: AddExperience is my best-confidence guess at
				// how Dragonfly's player XP bar is incremented — untested
				// against a real build. If `go build` says *player.Player
				// has no such method, tell me the error and/or the method
				// this Dragonfly version actually exposes for granting XP
				// (might be named differently, or live on a sub-manager
				// like p.Experience()) and it's a one-line fix — nothing
				// else in this file depends on it.
				p.AddExperience(gain)
			}
		}
	}

	tx.RemoveEntity(e)
}

func (e *Mob) dropLoot(tx *world.Tx) {
	for _, stack := range e.spec.Drops() {
		handle := dfentity.NewItem(world.EntitySpawnOpts{Position: e.data.Pos}, stack)
		tx.AddEntity(handle)
	}
}

// snapToGround keeps the mob following terrain instead of floating/sinking.
// Identical approach to demonking's snapToGround — see that file for the
// full explanation of why it searches down for the first solid block.
func (e *Mob) snapToGround(tx *world.Tx, pos mgl64.Vec3) mgl64.Vec3 {
	const searchUp, searchDown = 3, 64
	bp := cube.PosFromVec3(pos)
	for i := searchUp; i >= -searchDown; i-- {
		below := cube.Pos{bp.X(), bp.Y() + i, bp.Z()}
		if _, isAir := tx.Block(below).(block.Air); !isAir {
			return mgl64.Vec3{pos.X(), float64(below.Y() + 1), pos.Z()}
		}
	}
	return pos
}
