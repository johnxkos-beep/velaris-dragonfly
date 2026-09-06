// Package enderdragon implements a from-scratch Ender Dragon boss fight for
// velaris-dragonfly: the dragon herself, the 10 End Crystals that heal her,
// the obsidian pillar arena, and a Dragon Egg drop on death.
//
// IDENTIFIERS AND RESOURCE PACKS — both the dragon and the crystal need a
// resource pack to render at all on Dragonfly (confirmed in practice: the
// crystal was invisible with no pack, then rendered correctly once a
// pack defining "minecraft:end_crystal" was installed).
//   - End Crystal (crystal.go) uses the REAL vanilla identifier
//     "minecraft:end_crystal" — this turned out fine to override; see the
//     velaris_end_crystal.mcpack that ships with this fight.
//   - The Ender Dragon here uses a CUSTOM identifier, "velaris:ender_dragon"
//     — NOT "minecraft:ender_dragon". A resource pack overriding the real
//     vanilla dragon identifier was tried first and rendered nothing at
//     all; the likely reason is that pack's render controllers gate
//     selection on query.death_ticks, a MoLang query tied to the vanilla
//     server's own dragon-death bookkeeping, which Dragonfly never
//     populates — if that query errors, none of the four gated
//     render-controller entries evaluate true and nothing gets drawn. The
//     fix keeps the SAME real model, texture, and procedural neck/tail/wing
//     animation script (that part is pure client-side math off the
//     entity's own movement history — nothing Dragonfly-specific — so it
//     didn't need touching), but swaps the render-controller SELECTION for
//     two unconditional entries pointing at the pack's own already-safe
//     controller definitions (no query inside either of them). See
//     velaris_ender_dragon.mcpack.
//
// VANILLA MECHANICS PORTED:
//   - 200 HP, regenerates while any End Crystal is still alive (breaking
//     all 10 crystals is the intended way to stop the healing and let
//     damage stick).
//   - Circles the arena at height, periodically dive-bombs the nearest
//     player (contact damage + knockback), and periodically breathes fire
//     at a player's position (AoE damage, no crystal/pillar destruction —
//     see caveat).
//   - On death: a short invulnerable death sequence, then a Dragon Egg is
//     placed on top of the centre tower, and a real exit portal opens
//     around its base — walking into it sends you back to the overworld
//     near (0, 0). See finishDeath and arena.go's buildExitPortalRing.
//
// ENGINE-LEVEL CAVEAT — PLEASE READ (same spirit as demonking's):
// Dragonfly has no built-in flying-mob AI, so "circling"/"diving" here is
// hand-rolled trigonometry (orbit around a centre point, then a straight-
// line lerp toward the target during a dive) — not the vanilla dragon's
// real waypoint-based flight controller. It also won't break blocks
// (vanilla dragons can fly through/destroy End stone); this one just flies
// over the arena. Good enough for a real fight, not a byte-for-byte port.
package enderdragon

import (
	"math"
	"math/rand"
	"sync"

	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/endportal"
	"velaris-dragonfly/state"
)

// Tuning constants. Not pulled from any add-on this time — these are
// hand-picked to feel like a real fight at the arena size arena.go builds
// (see BuildArena's pillarRingRadius).
const (
	dragonMaxHP    = 600.0
	regenPerTick   = 0.1 // HP/tick while >=1 crystal is alive (~2 HP/sec)
	aggroRadius    = 100.0
	loseTargetRadius = 140.0

	orbitAngularSpeed = 0.018 // radians/tick — full lap in ~350 ticks (~17.5s)
	orbitBobAmplitude = 2.5   // blocks of vertical bob layered on the orbit

	diveSpeed         = 1.1  // blocks/tick while diving — faster than the orbit
	diveDuration       = 50   // ticks a dive lasts before forcing a return to orbit
	diveCooldownBase   = 500  // ticks between dives (~25s) + jitter below — at least 20s between attacks, as requested
	diveCooldownJitter = 100
	diveRange          = 3.0  // contact range for the dive-bomb hit
	diveDamage         = 10.0
	diveKnockUp        = 0.5
	diveKnockForce      = 0.9

	breathCooldownBase   = 500 // ticks between breath attacks (~25s) + jitter below — at least 20s, as requested
	breathCooldownJitter = 100
	breathDamage         = 6.0
	breathRadius         = 4.0

	returnSpeed = 0.9 // blocks/tick while flying from a dive/perch back to the orbit circle
	perchCooldownBase   = 500 // ticks between perches on the centre tower (~25s) + jitter below
	perchCooldownJitter = 300
	perchApproachSpeed  = 0.6 // blocks/tick while flying in to land — slower/more deliberate than a dive
	perchArriveRange    = 1.2
	perchDuration       = 140 // ticks spent standing on the tower before taking off again (~7s)

	knockbackScale = 0.08 // dragon barely reacts to hits — she's huge
	knockbackDecay = 0.6

	deathTicks = 100 // 5s invulnerable death sequence before despawn+egg
)

// phase describes what the dragon is currently doing.
type phase int

const (
	phaseCircling phase = iota
	phaseDiving
	phaseReturning // flying back from a dive/perch to a point on the orbit circle, smoothly — see tickReturning
	phaseLanding   // flying in toward the centre tower to perch
	phasePerched   // standing on the centre tower
	phaseDying
	phaseDead
)

// crystalCounter is shared (by pointer) between the dragon and every End
// Crystal spawned with her, so crystals can tell the dragon "I died" and the
// dragon can check "is anyone still healing me" without either side needing
// to scan the world's entity list every tick.
type crystalCounter struct {
	mu    sync.Mutex
	alive int
}

func newCrystalCounter(n int) *crystalCounter { return &crystalCounter{alive: n} }

func (c *crystalCounter) Alive() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.alive
}

func (c *crystalCounter) dec() {
	c.mu.Lock()
	c.alive--
	if c.alive < 0 {
		c.alive = 0
	}
	c.mu.Unlock()
}

// fightState is the dragon's mutable fight data, stashed in
// world.EntityData.Data for the entity's lifetime.
type fightState struct {
	Phase  phase
	HP, MaxHP float64
	Target *world.EntityHandle

	Center mgl64.Vec3
	Radius float64 // orbit radius
	Height float64 // orbit height above Center.Y

	Angle float64 // current orbit angle, radians

	DiveCooldown   int
	DiveTimer      int
	DiveHit        bool
	DiveTarget     mgl64.Vec3 // committed dive destination — see tickCircling's dive trigger
	BreathCooldown int

	PerchPos      mgl64.Vec3 // top of the centre tower — see arena.go's buildPodium
	PerchCooldown int
	PerchTimer    int

	ReturnTarget mgl64.Vec3 // used by phaseReturning — see tickReturning

	StageTimer int // death countdown

	Speed float64 // satisfies the shared movement-speed interface; informational only here

	Crystals *crystalCounter
}

func newState(center mgl64.Vec3, radius, height float64, crystals *crystalCounter, perch mgl64.Vec3) *fightState {
	return &fightState{
		Phase: phaseCircling, HP: dragonMaxHP, MaxHP: dragonMaxHP,
		Center: center, Radius: radius, Height: height,
		Speed: orbitAngularSpeed, Crystals: crystals, PerchPos: perch,
		// Staggered, non-zero starting cooldowns — without these both a dive
		// AND a breath attack (each independently checking "<=0") could fire
		// in the same first tick after spawn.
		DiveCooldown:   200,
		BreathCooldown: 350,
		PerchCooldown:  perchCooldownBase,
	}
}

// Type is the world.EntityType for the Ender Dragon. Register it via
// EntityRegistry() (see register.go) before starting the server.
var Type dragonType

type dragonType struct{}

func (dragonType) EncodeEntity() string { return "velaris:ender_dragon" }
func (dragonType) BBox(world.Entity) cube.BBox { return dragonBBox }
func (dragonType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*fightState)
	if !ok || st == nil {
		st = newState(mgl64.Vec3{}, 35, 40, newCrystalCounter(0), mgl64.Vec3{})
		data.Data = st
	}
	return &EnderDragon{tx: tx, handle: handle, data: data, fight: st}
}
func (dragonType) DecodeNBT(m map[string]any, data *world.EntityData) { decodeDragonNBT(m, data) }
func (dragonType) EncodeNBT(data *world.EntityData) map[string]any    { return encodeDragonNBT(data) }

// dragonBBox approximates the real ender dragon's collision box
// (16 wide x 8 tall in vanilla) — halved for the box's centre-relative
// extents.
var dragonBBox = cube.Box(-8, 0, -8, 8, 8, 8)

func decodeDragonNBT(m map[string]any, data *world.EntityData) {
	st := newState(mgl64.Vec3{}, 35, 40, newCrystalCounter(0), mgl64.Vec3{})
	if hp, ok := m["DragonHP"].(float64); ok {
		st.HP = hp
	}
	// NOTE: Center/Radius/Height/Crystals/PerchPos are deliberately NOT
	// restored from NBT — the crystal counter is an in-memory pointer
	// shared with live crystal entities and can't round-trip through save
	// data. A dragon reloaded mid-fight (server restart) will keep her HP
	// but stop regenerating and orbit/perch around the world origin instead
	// of the arena centre. Acceptable for now; flag it if that's a problem
	// in practice.
	data.Data = st
}

func encodeDragonNBT(data *world.EntityData) map[string]any {
	st, _ := data.Data.(*fightState)
	if st == nil {
		st = newState(mgl64.Vec3{}, 35, 40, newCrystalCounter(0), mgl64.Vec3{})
	}
	return map[string]any{"DragonHP": st.HP}
}

// Config spawns a dragon with a fresh fight around centre, orbiting at
// radius/height, healed by crystals (pass the same *crystalCounter given to
// the arena's End Crystals), and periodically landing on perch (the top of
// the centre tower — see arena.go's buildPodium).
type Config struct {
	Center         mgl64.Vec3
	Radius, Height float64
	Crystals       *crystalCounter
	Perch          mgl64.Vec3
}

func (c Config) Apply(data *world.EntityData) {
	data.Data = newState(c.Center, c.Radius, c.Height, c.Crystals, c.Perch)
}

// Spawn creates and adds an Ender Dragon to tx, orbiting centre and
// periodically landing on perch.
func Spawn(tx *world.Tx, centre mgl64.Vec3, radius, height float64, crystals *crystalCounter, perch mgl64.Vec3) *EnderDragon {
	pos := centre.Add(mgl64.Vec3{radius, height, 0})
	handle := world.EntitySpawnOpts{Position: pos}.New(Type, Config{Center: centre, Radius: radius, Height: height, Crystals: crystals, Perch: perch})
	e := tx.AddEntity(handle)
	d, _ := e.(*EnderDragon)
	return d
}

// EnderDragon is the live, in-transaction entity implementation.
type EnderDragon struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	fight  *fightState
}

func (e *EnderDragon) H() *world.EntityHandle  { return e.handle }
func (e *EnderDragon) Position() mgl64.Vec3    { return e.data.Pos }
func (e *EnderDragon) Rotation() cube.Rotation { return e.data.Rot }
func (e *EnderDragon) Close() error            { return nil }

// Living-interface plumbing — same shape as demonking.DemonKing (see that
// file's comment for why each of these exists even though most are no-ops).
func (e *EnderDragon) Health() float64    { return e.fight.HP }
func (e *EnderDragon) MaxHealth() float64 { return e.fight.MaxHP }
func (e *EnderDragon) SetMaxHealth(health float64) {
	e.fight.MaxHP = health
	if e.fight.HP > e.fight.MaxHP {
		e.fight.HP = e.fight.MaxHP
	}
}
func (e *EnderDragon) SetSpeed(speed float64) { e.fight.Speed = speed }
func (e *EnderDragon) Speed() float64         { return e.fight.Speed }
func (e *EnderDragon) SetVelocity(v mgl64.Vec3) { e.data.Vel = v }
func (e *EnderDragon) Velocity() mgl64.Vec3     { return e.data.Vel }
func (e *EnderDragon) Dead() bool               { return e.fight.Phase == phaseDead }
func (e *EnderDragon) AddEffect(effect.Effect)    {}
func (e *EnderDragon) Effects() []effect.Effect   { return nil }
func (e *EnderDragon) RemoveEffect(effect.Type)   {}
func (e *EnderDragon) Heal(health float64, _ world.HealingSource) float64 {
	before := e.fight.HP
	e.fight.HP += health
	if e.fight.HP > e.fight.MaxHP {
		e.fight.HP = e.fight.MaxHP
	}
	return e.fight.HP - before
}

// KnockBack gives the dragon a small, heavily-scaled-down shove — she's
// huge and mostly ignores it, but a total no-op felt unresponsive.
func (e *EnderDragon) KnockBack(src mgl64.Vec3, force, height float64) {
	dir := e.data.Pos.Sub(src)
	dir[1] = 0
	if dir.Len() > 0.001 {
		dir = dir.Normalize()
	}
	e.data.Vel = mgl64.Vec3{
		dir.X() * force * knockbackScale,
		height * knockbackScale,
		dir.Z() * force * knockbackScale,
	}
}

// Hurt applies damage. No invulnerability windows — unlike Demon King, the
// dragon is always damageable; her only real defence is the crystal regen.
func (e *EnderDragon) Hurt(dmg float64, _ world.DamageSource) (float64, bool) {
	if e.fight.Phase == phaseDying || e.fight.Phase == phaseDead || dmg < 0 {
		return 0, false
	}
	e.fight.HP -= dmg
	for _, v := range e.tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.HurtAction{})
	}
	return dmg, true
}

// Tick runs the fight AI.
func (e *EnderDragon) Tick(tx *world.Tx, current int64) {
	e.tx = tx
	st := e.fight

	if st.Phase == phaseDying {
		st.StageTimer--
		if current%20 == 0 {
			tx.AddParticle(e.data.Pos, particle.HugeExplosion{})
		}
		if st.StageTimer <= 0 {
			e.finishDeath(tx)
		}
		return
	}
	if st.Phase == phaseDead {
		return
	}

	if st.HP <= 0 {
		st.Phase = phaseDying
		st.StageTimer = deathTicks
		if p, ok := entityFromHandle(tx, st.Target).(*player.Player); ok {
			p.Message("§dThe Ender Dragon begins to disintegrate!")
			p.RemoveBossBar()
		}
		return
	}

	// Crystal-fed regeneration.
	if st.Crystals != nil && st.Crystals.Alive() > 0 && st.HP < st.MaxHP {
		st.HP += regenPerTick
		if st.HP > st.MaxHP {
			st.HP = st.MaxHP
		}
	}

	// Absorb one tick of any pending knockback before running AI, same
	// pattern as demonking (see its Tick for why: otherwise AI movement
	// instantly overwrites the shove and it's never visible).
	if e.data.Vel.Len() > 0.03 {
		e.data.Pos = e.data.Pos.Add(e.data.Vel)
		e.data.Vel = e.data.Vel.Mul(knockbackDecay)
		e.broadcastMovement(tx)
		return
	}

	target := e.findTarget(tx)
	st.Target = targetHandle(target)

	if current%10 == 0 && target != nil {
		e.updateBossBar(target)
	}

	if st.DiveCooldown > 0 {
		st.DiveCooldown--
	}
	if st.BreathCooldown > 0 {
		st.BreathCooldown--
	}
	if st.PerchCooldown > 0 {
		st.PerchCooldown--
	}

	switch st.Phase {
	case phaseCircling:
		e.tickCircling(tx, target)
	case phaseDiving:
		e.tickDiving(tx, target)
	case phaseReturning:
		e.tickReturning(tx)
	case phaseLanding:
		e.tickLanding(tx)
	case phasePerched:
		e.tickPerched(tx)
	}

	e.broadcastMovement(tx)
}

// yawFacing converts a direction vector into a yaw angle (degrees) for the
// dragon to face that direction of travel.
//
// The +180 here is a real fix, not guesswork restated as one: every yaw in
// this file used the standard atan2(-dx, dz) formula (the same one used
// elsewhere in this repo, e.g. demonking, and it's the normal Minecraft
// yaw-from-direction formula), so the AI logic was always pointing this
// value at the correct direction of travel. The dragon still looked like
// she was flying backwards constantly (not just sometimes, which is what
// pointed here) — meaning the MODEL ITSELF (the geometry from the "Better
// Ender Dragon" pack) is authored front/back-flipped relative to what that
// formula assumes, so the correct direction-of-travel angle rendered as her
// tail leading and head trailing. Rotating the final value 180° corrects
// for that model-authoring offset without needing to touch any AI/movement
// code again.
func yawFacing(delta mgl64.Vec3) float64 {
	yaw := math.Atan2(-delta.X(), delta.Z())*180/math.Pi + 180
	if yaw > 180 {
		yaw -= 360
	}
	return yaw
}

// tickCircling advances the orbit and may kick off a dive, a breath attack,
// or a perch on the centre tower.
func (e *EnderDragon) tickCircling(tx *world.Tx, target *player.Player) {
	st := e.fight

	if st.PerchCooldown <= 0 {
		st.Phase = phaseLanding
		st.PerchCooldown = perchCooldownBase + rand.Intn(perchCooldownJitter)
		return
	}

	st.Angle += orbitAngularSpeed
	if st.Angle > math.Pi*2 {
		st.Angle -= math.Pi * 2
	}

	x := st.Center.X() + st.Radius*math.Cos(st.Angle)
	z := st.Center.Z() + st.Radius*math.Sin(st.Angle)
	y := st.Center.Y() + st.Height + math.Sin(st.Angle*3)*orbitBobAmplitude
	pos := mgl64.Vec3{x, y, z}

	// Face the direction of travel (tangent to the orbit circle).
	tangentX, tangentZ := -math.Sin(st.Angle), math.Cos(st.Angle)
	yaw := yawFacing(mgl64.Vec3{tangentX, 0, tangentZ})
	e.data.Pos = pos
	e.data.Rot = cube.Rotation{yaw, 0}

	if target == nil {
		return
	}
	if st.DiveCooldown <= 0 {
		st.Phase = phaseDiving
		st.DiveTimer = diveDuration
		st.DiveHit = false
		// Commit to the target's position NOW, once — tickDiving flies a
		// straight line at this fixed point rather than continuously
		// re-steering toward the target's live position every tick. Always
		// chasing the live position was the "flies backwards" bug: if the
		// target moved past/behind her mid-dive, she'd instantly reverse
		// course to keep tracking them, which reads as flying backwards. A
		// committed charge (matching how vanilla's actual dive works — you
		// can dodge it by moving) fixes that; the hit check below still
		// uses the target's live position, so she can still land the hit
		// if they're near where she committed to.
		st.DiveTarget = target.Position()
		st.DiveCooldown = diveCooldownBase + rand.Intn(diveCooldownJitter)
		// UNVERIFIED sound choice: sound.Explosion{} is the only sound type
		// confirmed to exist in this Dragonfly version anywhere in this
		// repo (see breathAttack below) — there's no confirmed "roar" sound
		// to use instead, so this is an audible cue, not an accurate one.
		// If Dragonfly ships something like sound.EnderDragonGrowl{} or
		// similar, swap it in here and in breathAttack/tickLanding.
		tx.PlaySound(e.data.Pos, sound.Explosion{})
		return
	}
	if st.BreathCooldown <= 0 {
		e.breathAttack(tx, target)
		st.BreathCooldown = breathCooldownBase + rand.Intn(breathCooldownJitter)
	}
}

// tickDiving flies in a straight line at the point the target was standing
// at when the dive was triggered (st.DiveTarget, set once in tickCircling)
// — NOT continuously re-steering toward wherever they are now, which used
// to cause sudden reversals if they moved past/behind her mid-dive (see the
// comment where DiveTarget is set). The actual hit check still uses the
// target's live position, so she can land the hit if they're still nearby.
func (e *EnderDragon) tickDiving(tx *world.Tx, target *player.Player) {
	st := e.fight
	st.DiveTimer--

	pos := e.data.Pos
	delta := st.DiveTarget.Sub(pos)
	dist := delta.Len()

	if dist > 0.01 {
		yaw := yawFacing(delta)
		e.data.Rot = cube.Rotation{yaw, 0}
		step := delta.Normalize().Mul(diveSpeed)
		e.data.Pos = pos.Add(step)
	}

	if target != nil && !st.DiveHit && target.Position().Sub(pos).Len() <= diveRange {
		target.Hurt(diveDamage, dfentity.AttackDamageSource{Attacker: e})
		// SetVelocity (not a guessed KnockBack call) — confirmed working
		// on *player.Player elsewhere in this repo, see
		// legendary/abilities.go's Poseidon Trident riptide for the same
		// pattern (a direction vector scaled outward + an upward kick).
		away := target.Position().Sub(pos)
		away[1] = 0
		if away.Len() > 0.001 {
			away = away.Normalize()
		} else {
			away = mgl64.Vec3{1, 0, 0}
		}
		target.SetVelocity(mgl64.Vec3{away.X() * diveKnockForce, diveKnockUp, away.Z() * diveKnockForce})
		target.Message("§dThe Ender Dragon slams into you!")
		st.DiveHit = true
	}

	distFromCentre := e.data.Pos.Sub(st.Center).Len()
	if st.DiveTimer <= 0 || distFromCentre > st.Radius*1.8 {
		e.beginReturn()
	}
}

// beginReturn switches to phaseReturning, computing a point on the orbit
// circle (at the dragon's current angle relative to Center) as the target
// to fly back to. Replaces the old instant-snap resumeOrbit — jumping
// straight to the angle-driven circling formula from an arbitrary dive/
// perch position could put her anywhere relative to the correct circle,
// which read as sudden/backwards-looking movement. This flies her there
// properly first.
func (e *EnderDragon) beginReturn() {
	st := e.fight
	dx := e.data.Pos.X() - st.Center.X()
	dz := e.data.Pos.Z() - st.Center.Z()
	angle := math.Atan2(dz, dx)
	st.Angle = angle
	x := st.Center.X() + st.Radius*math.Cos(angle)
	z := st.Center.Z() + st.Radius*math.Sin(angle)
	y := st.Center.Y() + st.Height
	st.ReturnTarget = mgl64.Vec3{x, y, z}
	st.Phase = phaseReturning
}

// tickReturning flies in a straight line from wherever the dragon ended up
// (after a dive or a perch) to ReturnTarget, then resumes normal circling.
// This is what keeps her from instantly teleporting/snapping onto the
// orbit circle and looking like she's flying backwards.
func (e *EnderDragon) tickReturning(tx *world.Tx) {
	st := e.fight
	delta := st.ReturnTarget.Sub(e.data.Pos)
	dist := delta.Len()
	if dist <= 1.5 {
		st.Phase = phaseCircling
		return
	}
	step := delta.Normalize().Mul(returnSpeed)
	e.data.Pos = e.data.Pos.Add(step)
	yaw := yawFacing(delta)
	e.data.Rot = cube.Rotation{yaw, 0}
}

// tickLanding flies the dragon in toward the centre tower's top (st.PerchPos)
// to begin a perch.
func (e *EnderDragon) tickLanding(tx *world.Tx) {
	st := e.fight
	delta := st.PerchPos.Sub(e.data.Pos)
	dist := delta.Len()

	if dist <= perchArriveRange {
		e.data.Pos = st.PerchPos
		st.Phase = phasePerched
		st.PerchTimer = perchDuration
		tx.PlaySound(st.PerchPos, sound.Explosion{})
		for p := range state.Server.Players(tx) {
			if p.Position().Sub(st.PerchPos).Len() <= aggroRadius {
				p.Message("§dThe Ender Dragon lands on the tower!")
			}
		}
		return
	}

	step := delta.Normalize().Mul(perchApproachSpeed)
	e.data.Pos = e.data.Pos.Add(step)
	yaw := yawFacing(delta)
	e.data.Rot = cube.Rotation{yaw, 0}
}

// tickPerched holds the dragon stationary on the tower — still fully
// damageable, still regenerating from any live crystals — until PerchTimer
// runs out, then takes back off into the orbit.
func (e *EnderDragon) tickPerched(tx *world.Tx) {
	st := e.fight
	st.PerchTimer--
	if st.PerchTimer <= 0 {
		e.beginReturn()
	}
}

// breathAttack is a ranged fire-breath AoE centred on target's current
// position — no real projectile travel time, matching demonking's
// abstraction of its own AoE abilities.
//
// Deliberately NOT using particle.HugeExplosion here — that's the heaviest
// particle effect available and this fires every few seconds during a
// normal fight; on mobile clients that was tanking FPS to single digits.
// HugeExplosion is now reserved for the rare one-off moments (a crystal
// breaking, the dragon's death) where a big effect is actually warranted.
func (e *EnderDragon) breathAttack(tx *world.Tx, target *player.Player) {
	pos := target.Position()
	tx.PlaySound(pos, sound.Explosion{})
	target.Message("§dThe Ender Dragon breathes fire at you!")
	for p := range state.Server.Players(tx) {
		if p.Dead() {
			continue
		}
		if p.Position().Sub(pos).Len() <= breathRadius {
			p.Hurt(breathDamage, dfentity.AttackDamageSource{Attacker: e})
		}
	}
}

func (e *EnderDragon) broadcastMovement(tx *world.Tx) {
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, true)
	}
}

// finishDeath removes the dragon and PLACES the Dragon Egg (as a real
// block, not a dropped item — matching vanilla, where the egg materialises
// sitting on top of the exit portal, not as something you pick up off the
// ground) on top of the centre tower. Also opens the real exit portal — a
// ring of End Portal blocks around the tower's base, on the bowl's sunken
// floor — that sends anyone who steps into it back to the overworld near
// (0, 0), landing on solid ground there rather than wherever the overworld
// spawn happens to put them.
//
// UNVERIFIED: block.DragonEgg{} — this repo's other blocks (Obsidian,
// Bedrock, EndStone, etc., see arena.go/worldgen) are all already confirmed
// against this Dragonfly version, but this specific one wasn't. If it
// doesn't compile, tell me and I'll swap back to a custom item you'd pick
// up instead — nothing else in this file needs to change either way.
func (e *EnderDragon) finishDeath(tx *world.Tx) {
	st := e.fight
	st.Phase = phaseDead
	// st.PerchPos is the tower's top surface, offset to a block's centre
	// (+0.5 on X/Z — see BuildArena) for the entity to stand on; flooring
	// that back out gives the actual block coordinate the egg sits in.
	towerX := int(math.Floor(st.PerchPos.X()))
	towerZ := int(math.Floor(st.PerchPos.Z()))
	towerTopY := int(st.PerchPos.Y())
	eggBlockPos := cube.Pos{towerX, towerTopY, towerZ}
	tx.SetBlock(eggBlockPos, block.DragonEgg{}, nil)

	ringY := towerTopY - podiumTowerHeight
	min, max := buildExitPortalRing(tx, towerX, ringY, towerZ)
	endportal.SpawnSentinel(tx, min, max, endportal.Destination{World: "overworld", LandX: 0, LandZ: 0, LandRadius: 24})

	tx.PlaySound(st.PerchPos, sound.Explosion{})
	for p := range state.Server.Players(tx) {
		p.Message("§d§lThe Ender Dragon has been defeated! An exit portal has opened.")
	}
	tx.RemoveEntity(e)
}

func (e *EnderDragon) findTarget(tx *world.Tx) *player.Player {
	st := e.fight
	if cur := entityFromHandle(tx, st.Target); cur != nil {
		if p, ok := cur.(*player.Player); ok && !p.Dead() && isTargetable(p) {
			if p.Position().Sub(st.Center).Len() <= loseTargetRadius {
				return p
			}
		}
	}
	var (
		nearest    *player.Player
		nearestDst = math.MaxFloat64
	)
	for p := range state.Server.Players(tx) {
		if p.Dead() || !isTargetable(p) {
			continue
		}
		d := p.Position().Sub(st.Center).Len()
		if d <= aggroRadius && d < nearestDst {
			nearest, nearestDst = p, d
		}
	}
	return nearest
}

func isTargetable(p *player.Player) bool {
	gm := p.GameMode()
	return gm != world.GameModeCreative && gm != world.GameModeSpectator
}

func (e *EnderDragon) updateBossBar(p *player.Player) {
	pct := e.fight.HP / e.fight.MaxHP
	if pct < 0 {
		pct = 0
	} else if pct > 1 {
		pct = 1
	}
	title := "§5Ender Dragon"
	if e.fight.Crystals != nil && e.fight.Crystals.Alive() > 0 {
		title += "§7 (crystals active)"
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
