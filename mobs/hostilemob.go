// Hostile mobs (zombie, skeleton, spider, creeper) share this file's
// HostileMob implementation, the same way cow/chicken/pig/sheep share
// Mob in mob.go. Targeting/aggro logic is lifted from bosses/demonking
// (findTarget/isTargetable there) rather than reinvented — same
// "nearest player within aggro radius, sticky until it dies or leaves
// range" behaviour, just without the boss's phases/abilities.
//
// SAME ENGINE-LEVEL CAVEAT AS mob.go/demonking: no built-in pathfinding.
// A hostile mob walks a straight line at its target and will not path
// around obstacles — it can get stuck on a wall the same way demonking
// can. Acceptable for now, same tradeoff already made for the boss.
//
// ATTACK MODELS — three, not one:
//   - Melee (zombie, spider): walk into AttackRange, then deal damage on
//     a cooldown, identical shape to demonking's melee.
//   - Ranged (skeleton): DELIBERATE SIMPLIFICATION — this does not spawn
//     a real arrow projectile entity. There's no arrow/projectile physics
//     anywhere else in this codebase to build on (checked; knockback/
//     legendary only handle projectiles a PLAYER already fired), and
//     inventing projectile flight + collision from scratch is a much
//     bigger, riskier addition than one mob is worth. Instead, once a
//     target is within AttackRange and there's a clear line, the
//     skeleton deals damage on a cooldown exactly like melee, just at
//     longer range with an arrow-hit sound instead of a swing animation.
//     Visually it stands off and "shoots" on the same cadence a real bow
//     would; there's just no physical arrow model in flight. If you want
//     a real projectile later, that's a separate addition.
//   - Explode (creeper): no ranged/melee damage at all. Closes to
//     explodeRange, then counts down a fuse (resetting if the target
//     escapes the range mid-fuse, same as vanilla backing off), then
//     deals one-shot splash damage to every targetable player within
//     ExplodeRadius and removes itself. Matches vanilla in spirit but
//     NOT in one respect: no terrain/block destruction — this only
//     damages players, same "no block-level side effects" boundary the
//     rest of this package already keeps (nothing here ever calls
//     tx.SetBlock). A creeper that dies to player damage before its fuse
//     finishes still drops gunpowder normally via die(); one that
//     actually explodes does not (matches vanilla).
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

	"velaris-dragonfly/state"
)

// attackKind picks which of the three attack models (see file doc
// comment) a given hostile species uses.
type attackKind int

const (
	attackMelee attackKind = iota
	attackRanged
	attackExplode
)

// Tuning shared by every hostile species. Per-species numbers live in
// each species' own file (zombie.go, skeleton.go, spider.go, creeper.go).
const (
	hostileKnockbackDecay = 0.65 // same value Mob/demonking both use
	hostileFuseResetGrace = 20   // ticks a creeper's fuse survives the target briefly stepping out of range, before it resets to 0

	// Sunlight-burn tuning (zombie/skeleton only, see BurnsInSunlight).
	// Checked every hostileBurnCheckTicks rather than every tick — with
	// up to 100 players and a full hostile mob cap, checking sky light
	// on every burning mob every single tick would be needlessly
	// expensive; once every half-second is imperceptible to a player but
	// cuts that cost by 10x.
	hostileBurnCheckTicks = 10  // ~0.5s
	hostileBurnDamage     = 2.0 // per check — a 20 HP zombie/skeleton burns down in ~2.5s of direct sun
)

// hostileSpec holds the per-species numbers/behaviour that differ between
// zombie, skeleton, spider, and creeper. A *hostileSpec is shared
// (read-only) across every instance of that species — only hostileState
// is per-entity.
type hostileSpec struct {
	MaxHP            float64
	WalkSpeed        float64 // blocks/tick
	XPMin, XPMax     int     // XP granted to the killer, inclusive range
	Drops            func() []item.Stack

	Kind             attackKind
	AttackDamage     float64
	AttackRange      float64
	AttackCooldown   int // ticks between attacks
	AggroRadius      float64
	LoseTargetRadius float64

	// Explode-only (Kind == attackExplode); zero values elsewhere.
	ExplodeRange  float64 // distance at which the fuse starts counting down
	ExplodeRadius float64 // splash-damage radius on detonation
	ExplodeDamage float64
	FuseTicks     int

	// BurnsInSunlight marks undead species (zombie, skeleton) that catch
	// fire and take damage when standing in direct daytime sun, same as
	// vanilla. False for spider/creeper — neither is undead, neither
	// burns. See tickSunlightBurn below for the actual check.
	BurnsInSunlight bool
}

// hostileState is the mutable per-entity data, stashed in
// world.EntityData.Data — same approach as mob.go's mobState.
type hostileState struct {
	HP, MaxHP      float64
	Speed          float64
	Target         *world.EntityHandle
	AttackCooldown int
	FuseTicks      int // creeper only; 0 = not ignited, counts up to spec.FuseTicks
	FuseGrace      int // creeper only; ticks remaining before an interrupted fuse resets
	LastAttacker   *world.EntityHandle // who to credit XP to on death
	Dead           bool
	WanderDir      mgl64.Vec3
	WanderTicks    int
	IdleTicks      int
}

func newHostileState(spec *hostileSpec) *hostileState {
	return &hostileState{HP: spec.MaxHP, MaxHP: spec.MaxHP, Speed: spec.WalkSpeed}
}

// hostileConfig configures a freshly spawned hostile mob of the given species.
type hostileConfig struct{ spec *hostileSpec }

func (c hostileConfig) Apply(data *world.EntityData) {
	if data.Data == nil {
		data.Data = newHostileState(c.spec)
	}
}

// spawnHostile is the shared implementation behind SpawnZombie/SpawnSkeleton/
// SpawnSpider/SpawnCreeper.
func spawnHostile(tx *world.Tx, t world.EntityType, spec *hostileSpec, pos mgl64.Vec3) *HostileMob {
	handle := world.EntitySpawnOpts{Position: pos}.New(t, hostileConfig{spec: spec})
	e := tx.AddEntity(handle)
	m, _ := e.(*HostileMob)
	return m
}

func openHostileMob(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData, spec *hostileSpec) world.Entity {
	st, ok := data.Data.(*hostileState)
	if !ok || st == nil {
		st = newHostileState(spec)
		data.Data = st
	}
	return &HostileMob{tx: tx, handle: handle, data: data, state: st, spec: spec}
}

func decodeHostileMobNBT(m map[string]any, data *world.EntityData, spec *hostileSpec) {
	st := newHostileState(spec)
	if hp, ok := m["HostileHP"].(float64); ok {
		st.HP = hp
	}
	data.Data = st
}

func encodeHostileMobNBT(data *world.EntityData) map[string]any {
	st, _ := data.Data.(*hostileState)
	if st == nil {
		return map[string]any{}
	}
	return map[string]any{"HostileHP": st.HP}
}

// HostileMob is the live, in-transaction entity implementation shared by
// every hostile mob species in this package.
type HostileMob struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *hostileState
	spec   *hostileSpec
}

func (e *HostileMob) H() *world.EntityHandle  { return e.handle }
func (e *HostileMob) Position() mgl64.Vec3    { return e.data.Pos }
func (e *HostileMob) Rotation() cube.Rotation { return e.data.Rot }
func (e *HostileMob) Close() error            { return nil }

func (e *HostileMob) Health() float64    { return e.state.HP }
func (e *HostileMob) MaxHealth() float64 { return e.state.MaxHP }

func (e *HostileMob) SetMaxHealth(health float64) {
	e.state.MaxHP = health
	if e.state.HP > e.state.MaxHP {
		e.state.HP = e.state.MaxHP
	}
}

func (e *HostileMob) SetSpeed(speed float64) { e.state.Speed = speed }
func (e *HostileMob) Speed() float64         { return e.state.Speed }

func (e *HostileMob) SetVelocity(v mgl64.Vec3) { e.data.Vel = v }
func (e *HostileMob) Velocity() mgl64.Vec3     { return e.data.Vel }

func (e *HostileMob) Dead() bool { return e.state.Dead }

// AddEffect, Effects, RemoveEffect — not used by these mobs, no-ops, same
// as Mob/demonking.
func (e *HostileMob) AddEffect(eff effect.Effect) {}
func (e *HostileMob) Effects() []effect.Effect    { return nil }
func (e *HostileMob) RemoveEffect(t effect.Type)  {}

func (e *HostileMob) Heal(health float64, src world.HealingSource) float64 {
	before := e.state.HP
	e.state.HP += health
	if e.state.HP > e.state.MaxHP {
		e.state.HP = e.state.MaxHP
	}
	return e.state.HP - before
}

// KnockBack shoves the mob away from src — no knockback resistance,
// matching vanilla zombie/skeleton/spider/creeper (unlike the boss).
func (e *HostileMob) KnockBack(src mgl64.Vec3, force, height float64) {
	dir := e.data.Pos.Sub(src)
	dir[1] = 0
	if dir.Len() > 0.001 {
		dir = dir.Normalize()
	} else {
		dir = mgl64.Vec3{0, 0, 0}
	}
	e.data.Vel = mgl64.Vec3{dir.X() * force, height, dir.Z() * force}
}

// Hurt applies damage, flashes the hurt animation, remembers the
// attacker for XP credit on death, and — unlike the passive Mob, which
// flees — aggroes straight onto whoever just hit it if it doesn't
// already have a closer/valid target. Matches vanilla: hitting a
// zombie/skeleton/spider/creeper that hasn't noticed you yet makes it
// turn and come after you.
func (e *HostileMob) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	if e.state.Dead || dmg <= 0 {
		return 0, false
	}
	e.state.HP -= dmg

	for _, v := range e.tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.HurtAction{})
	}

	if asrc, ok := src.(dfentity.AttackDamageSource); ok && asrc.Attacker != nil {
		if p, ok := asrc.Attacker.(*player.Player); ok {
			e.state.LastAttacker = p.H()
			if e.state.Target == nil {
				e.state.Target = p.H()
			}
		}
	}

	return dmg, true
}

// tickSunlightBurn checks (every hostileBurnCheckTicks, see that const)
// whether this mob is standing in direct daytime sun — full sky light,
// not indoors/underground/shaded, and not in water — and if so burns it
// for hostileBurnDamage, killing and dropping loot normally once HP
// reaches 0. Only called for species with BurnsInSunlight set
// (zombie/skeleton); spiders and creepers never call this at all.
func (e *HostileMob) tickSunlightBurn(tx *world.Tx, current int64) {
	if current%hostileBurnCheckTicks != 0 {
		return
	}
	if !isDaytime(tx) {
		return
	}
	bp := cube.PosFromVec3(e.data.Pos)
	if tx.SkyLight(bp) < 15 {
		return // shaded, indoors, or underground — not in direct sun
	}
	if _, water := tx.Block(bp).(block.Water); water {
		return
	}

	e.state.HP -= hostileBurnDamage
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.HurtAction{})
	}
	if e.state.HP <= 0 {
		e.die(tx)
	}
}

// Tick runs the aggro/chase/attack AI. See the file doc comment for the
// three attack models this dispatches between.
func (e *HostileMob) Tick(tx *world.Tx, current int64) {
	e.tx = tx
	st := e.state

	if st.Dead {
		return
	}
	if e.spec.BurnsInSunlight {
		e.tickSunlightBurn(tx, current)
		if st.Dead {
			return
		}
	}
	if st.HP <= 0 {
		e.die(tx)
		return
	}

	// Apply and decay knockback before running AI, same as Mob/demonking.
	if e.data.Vel.Len() > 0.03 {
		e.data.Pos = e.snapToGround(tx, e.data.Pos.Add(e.data.Vel))
		e.data.Vel = e.data.Vel.Mul(hostileKnockbackDecay)
		e.broadcastMove(tx)
		return
	}

	target := e.findTarget(tx)
	st.Target = targetHostileHandle(target)

	if target == nil {
		e.wander(tx)
		return
	}

	pos := e.data.Pos
	tpos := target.Position()
	delta := tpos.Sub(pos)
	delta[1] = 0
	dist := delta.Len()

	if dist > 0.01 {
		yaw := math.Atan2(-delta.X(), delta.Z()) * 180 / math.Pi
		e.data.Rot = cube.Rotation{yaw, 0}
	}

	if st.AttackCooldown > 0 {
		st.AttackCooldown--
	}

	switch e.spec.Kind {
	case attackExplode:
		e.tickExplode(tx, target, dist)
	default: // attackMelee, attackRanged — same chase-then-hit-on-cooldown shape
		if dist <= e.spec.AttackRange {
			if st.AttackCooldown <= 0 {
				target.Hurt(e.spec.AttackDamage, dfentity.AttackDamageSource{Attacker: e})
				st.AttackCooldown = e.spec.AttackCooldown
			}
		} else {
			step := delta.Normalize().Mul(st.Speed)
			e.data.Pos = e.snapToGround(tx, pos.Add(step))
		}
	}

	e.broadcastMove(tx)
}

// tickExplode runs the creeper's fuse: closes distance like any other
// hostile mob, then counts the fuse up once in range, resetting it
// (after a short grace period, so a single dodge-step doesn't instantly
// reset it) if the target escapes ExplodeRange before it finishes.
func (e *HostileMob) tickExplode(tx *world.Tx, target *player.Player, dist float64) {
	st := e.state
	spec := e.spec

	if dist <= spec.ExplodeRange {
		st.FuseTicks++
		st.FuseGrace = hostileFuseResetGrace
		if st.FuseTicks >= spec.FuseTicks {
			e.explode(tx)
		}
		return
	}

	if st.FuseTicks > 0 {
		if st.FuseGrace > 0 {
			st.FuseGrace--
		} else {
			st.FuseTicks = 0
		}
	}

	step := target.Position().Sub(e.data.Pos)
	step[1] = 0
	if step.Len() > 0.01 {
		step = step.Normalize().Mul(st.Speed)
		e.data.Pos = e.snapToGround(tx, e.data.Pos.Add(step))
	}
}

// explode deals one-shot splash damage to every targetable player within
// ExplodeRadius and removes the creeper without dropping loot (matches
// vanilla — an exploded creeper drops nothing, only a killed one does).
func (e *HostileMob) explode(tx *world.Tx) {
	if e.state.Dead {
		return
	}
	e.state.Dead = true

	center := e.data.Pos
	for p := range state.Server.Players(tx) {
		if p.Dead() || !isHostileTargetable(p) {
			continue
		}
		d := p.Position().Sub(center).Len()
		if d > e.spec.ExplodeRadius {
			continue
		}
		falloff := 1 - d/e.spec.ExplodeRadius
		p.Hurt(e.spec.ExplodeDamage*falloff, dfentity.AttackDamageSource{Attacker: e})
	}

	tx.RemoveEntity(e)
}

// findTarget returns the nearest player within AggroRadius, sticking
// with the current target until it dies or leaves LoseTargetRadius —
// identical shape to demonking.findTarget.
func (e *HostileMob) findTarget(tx *world.Tx) *player.Player {
	if cur := entityFromHostileHandle(tx, e.state.Target); cur != nil {
		if p, ok := cur.(*player.Player); ok && !p.Dead() && isHostileTargetable(p) {
			if p.Position().Sub(e.data.Pos).Len() <= e.spec.LoseTargetRadius {
				return p
			}
		}
	}

	var (
		nearest    *player.Player
		nearestDst = math.MaxFloat64
	)
	for p := range state.Server.Players(tx) {
		if p.Dead() || !isHostileTargetable(p) {
			continue
		}
		d := p.Position().Sub(e.data.Pos).Len()
		if d <= e.spec.AggroRadius && d < nearestDst {
			nearest, nearestDst = p, d
		}
	}
	return nearest
}

// isHostileTargetable excludes creative/spectator players, same reasoning
// as demonking.isTargetable.
func isHostileTargetable(p *player.Player) bool {
	gm := p.GameMode()
	return gm != world.GameModeCreative && gm != world.GameModeSpectator
}

// wander runs the same idle/wander cycle as passive mobs when there's no
// target, so hostile mobs don't just stand frozen waiting for a player to
// wander into aggro range.
func (e *HostileMob) wander(tx *world.Tx) {
	st := e.state

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

	if rand.Intn(3) == 0 {
		st.IdleTicks = idleMinTicks + rand.Intn(idleMaxTicks-idleMinTicks)
		return
	}
	angle := rand.Float64() * 2 * math.Pi
	st.WanderDir = mgl64.Vec3{math.Cos(angle), 0, math.Sin(angle)}
	st.WanderTicks = wanderMinTicks + rand.Intn(wanderMaxTicks-wanderMinTicks)
	if st.WanderDir.Len() > 0.001 {
		yaw := math.Atan2(-st.WanderDir.X(), st.WanderDir.Z()) * 180 / math.Pi
		e.data.Rot = cube.Rotation{yaw, 0}
	}
}

func (e *HostileMob) broadcastMove(tx *world.Tx) {
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, true)
	}
}

// die drops loot, grants XP to the last attacker (if a player), and
// removes the mob. Guarded so it only ever runs once. Not called by
// explode() — an exploded creeper skips this entirely (see file doc
// comment).
func (e *HostileMob) die(tx *world.Tx) {
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
				p.AddExperience(gain)
			}
		}
	}

	tx.RemoveEntity(e)
}

func (e *HostileMob) dropLoot(tx *world.Tx) {
	for _, stack := range e.spec.Drops() {
		handle := dfentity.NewItem(world.EntitySpawnOpts{Position: e.data.Pos}, stack)
		tx.AddEntity(handle)
	}
}

// snapToGround — identical to Mob.snapToGround/demonking's version.
func (e *HostileMob) snapToGround(tx *world.Tx, pos mgl64.Vec3) mgl64.Vec3 {
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

func targetHostileHandle(p *player.Player) *world.EntityHandle {
	if p == nil {
		return nil
	}
	return p.H()
}

func entityFromHostileHandle(tx *world.Tx, h *world.EntityHandle) world.Entity {
	if h == nil {
		return nil
	}
	ent, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return ent
}
