// Real flying projectile entities for Mjolnir's and Poseidon Trident's
// throws, replacing the instant-hitscan version of those two abilities.
// Built closely following bosses/demonking/entity.go's proven world.Entity
// pattern in this exact repo (EncodeEntity/BBox/Open/DecodeNBT/EncodeNBT,
// EntityData.Pos/Rot/Vel, Tick(tx, current), tx.AddEntity/RemoveEntity,
// ViewEntityMovement for client sync) — that pattern is the one thing in
// this whole legendary-weapons effort that's actually been proven to
// compile and run, so this reuses it as directly as possible rather than
// inventing a new approach.
//
// IDENTIFIERS PULLED DIRECTLY FROM THE ADD-ON, NOT GUESSED: "bey:shot_mjolnir"
// and "bey:shot_poseidon_trident" are the exact identifiers the add-on's own
// resource pack (entity/shot_mjolnir.json, entity/shot_poseidon_trident.json)
// already binds real 3D models/textures/animations to. As long as
// EncodeEntity returns these exact strings, the client renders the real
// flying-hammer/flying-trident model automatically — no extra client-side
// work needed, same reason the held-weapon models "just worked" once the
// resource pack was actually being served (see the item.go/main.go history
// on that).
//
// PHYSICS TUNING FROM THE ADD-ON'S OWN behavior pack (entities/shot_mjolnir.json
// minecraft:projectile component): power 1.6, gravity 0.0005 (nearly flat
// trajectory over short/medium range), destroy_on_hit true. Translated
// here as a fast, nearly-gravity-free straight-line flight that despawns
// on the first hit or at max range — matches the add-on's own feel
// closely enough without needing Dragonfly's full arrow-physics internals.
//
// DAMAGE: the add-on's own projectile component lists a fallback 3-5
// "impact_damage" (its plain-vanilla-projectile damage, before the
// add-on's real scripted ability logic overrides it) — this uses
// AttackPoints(weaponID) instead (9 for Mjolnir, 8 for Poseidon Trident),
// same as every other authoritative damage number in this legendary
// system, so it stays consistent with Midas Sword bonuses, armor,
// abilities, etc. rather than introducing a second, differently-tuned
// damage source.
package legendary

import (
	"fmt"
	"log"
	"math"
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

const (
	projectileSpeed        = 1.6  // blocks/tick, matches the add-on's "power": 1.6
	projectileGravity      = 0.0005 // matches the add-on's "gravity": 0.0005 exactly
	projectileMaxRange     = 30.0 // despawns past this distance from launch, matches rangedAbilityRange
	projectileHitRadius    = 0.9  // blocks — how close to a player counts as a hit
	projectileMaxLifeTicks = 100  // 5s hard cap in case something's stuck (shouldn't normally hit this)
)

// projectileState is the mutable per-instance data for a flying legendary
// projectile, stashed in world.EntityData.Data — same pattern as
// demonking's *fightState.
type projectileState struct {
	WeaponID        string
	Owner           *world.EntityHandle // who threw it — credited for damage/kills
	LightningChance float64
	LaunchPos       mgl64.Vec3
	TicksAlive      int
}

// ---------------------------------------------------------------------
// Entity types — one per weapon, since EncodeEntity (and therefore which
// client-side model renders) is fixed per-type, same reason demonking
// needs 4 separate types for its 4 model states.
// ---------------------------------------------------------------------

var (
	MjolnirShotType mjolnirShotType
	TridentShotType tridentShotType
)

// ProjectileTypes returns both projectile entity types, for merging into
// your server's entity registry alongside demonking's — see the wiring
// note in main.go.
func ProjectileTypes() []world.EntityType {
	return []world.EntityType{MjolnirShotType, TridentShotType}
}

var projectileBBox = cube.Box(-0.25, -0.25, -0.25, 0.25, 0.25, 0.25)

type mjolnirShotType struct{}

func (mjolnirShotType) EncodeEntity() string { return "bey:shot_mjolnir" }
func (mjolnirShotType) BBox(world.Entity) cube.BBox { return projectileBBox }
func (mjolnirShotType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openProjectile(tx, handle, data)
}
func (mjolnirShotType) DecodeNBT(m map[string]any, data *world.EntityData) { decodeProjectileNBT(m, data) }
func (mjolnirShotType) EncodeNBT(data *world.EntityData) map[string]any    { return encodeProjectileNBT(data) }

type tridentShotType struct{}

func (tridentShotType) EncodeEntity() string { return "bey:shot_poseidon_trident" }
func (tridentShotType) BBox(world.Entity) cube.BBox { return projectileBBox }
func (tridentShotType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openProjectile(tx, handle, data)
}
func (tridentShotType) DecodeNBT(m map[string]any, data *world.EntityData) { decodeProjectileNBT(m, data) }
func (tridentShotType) EncodeNBT(data *world.EntityData) map[string]any    { return encodeProjectileNBT(data) }

func openProjectile(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*projectileState)
	if !ok || st == nil {
		st = &projectileState{}
		data.Data = st
	}
	return &Projectile{tx: tx, handle: handle, data: data, state: st}
}

// NBT round-trip is minimal here — projectiles are short-lived (seconds),
// so surviving a world save/reload isn't important the way it is for
// demonking's boss fight. Losing WeaponID/Owner across a reload just means
// it silently despawns next tick instead of resuming flight, which is
// harmless.
func decodeProjectileNBT(m map[string]any, data *world.EntityData) { data.Data = &projectileState{} }
func encodeProjectileNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// ProjectileConfig configures a newly spawned projectile.
type ProjectileConfig struct {
	WeaponID        string
	Owner           *world.EntityHandle
	Velocity        mgl64.Vec3
	LightningChance float64
}

func (c ProjectileConfig) Apply(data *world.EntityData) {
	data.Vel = c.Velocity
	data.Data = &projectileState{
		WeaponID:        c.WeaponID,
		Owner:           c.Owner,
		LightningChance: c.LightningChance,
		LaunchPos:       data.Pos,
	}
}

// SpawnProjectile launches a real flying weapon from p in the direction
// p is looking. weaponID must be "bey:mjolnir" or "bey:poseidon_trident".
func SpawnProjectile(tx *world.Tx, p *player.Player, weaponID string, lightningChance float64) {
	var t world.EntityType
	switch weaponID {
	case "bey:mjolnir":
		t = MjolnirShotType
	case "bey:poseidon_trident":
		t = TridentShotType
	default:
		return
	}

	eye := p.Position().Add(mgl64.Vec3{0, 1.62, 0})
	dir := direction(p)
	vel := dir.Mul(projectileSpeed)

	handle := world.EntitySpawnOpts{Position: eye, Rotation: p.Rotation()}.New(t, ProjectileConfig{
		WeaponID:        weaponID,
		Owner:           p.H(),
		Velocity:        vel,
		LightningChance: lightningChance,
	})
	tx.AddEntity(handle)
}

// Projectile is the live, in-transaction entity implementation for a
// flying legendary weapon.
type Projectile struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *projectileState
}

func (e *Projectile) H() *world.EntityHandle  { return e.handle }
func (e *Projectile) Position() mgl64.Vec3    { return e.data.Pos }
func (e *Projectile) Rotation() cube.Rotation { return e.data.Rot }
func (e *Projectile) Velocity() mgl64.Vec3    { return e.data.Vel }
func (e *Projectile) SetVelocity(v mgl64.Vec3) { e.data.Vel = v }
func (e *Projectile) Close() error            { return nil }

// Tick moves the projectile forward each tick, checks for a hit, and
// despawns on hit / max range / max lifetime. Called ~20 times/sec by
// Dragonfly for any entity implementing TickerEntity, same as demonking.
func (e *Projectile) Tick(tx *world.Tx, current int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in Projectile.Tick: %v", r)
		}
	}()
	e.tx = tx
	st := e.state

	st.TicksAlive++
	if st.TicksAlive > projectileMaxLifeTicks {
		tx.RemoveEntity(e)
		return
	}

	// Move forward, applying the add-on's own near-flat gravity.
	e.data.Vel = mgl64.Vec3{e.data.Vel.X(), e.data.Vel.Y() - projectileGravity, e.data.Vel.Z()}
	newPos := e.data.Pos.Add(e.data.Vel)

	// Face the direction of travel.
	if e.data.Vel.Len() > 0.001 {
		yaw := yawFromDirection(e.data.Vel)
		e.data.Rot = cube.Rotation{yaw, 0}
	}

	if newPos.Sub(st.LaunchPos).Len() > projectileMaxRange {
		e.impact(tx, newPos, nil)
		return
	}

	// Check for a hit against any player along the way (using the new
	// position — fine at this speed/tick-rate, no need for sub-tick ray
	// marching at only ~1.6 blocks/tick).
	var owner world.Entity
	if st.Owner != nil {
		owner, _ = st.Owner.Entity(tx)
	}
	for pl := range state.Server.Players(tx) {
		if owner != nil {
			if op, ok := owner.(*player.Player); ok && pl == op {
				continue // don't hit your own thrower
			}
		}
		if pl.Position().Sub(newPos).Len() <= projectileHitRadius {
			e.impact(tx, newPos, pl)
			return
		}
	}

	e.data.Pos = newPos
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, false)
	}
}

// impact applies damage (if target is non-nil), a lightning-strike visual
// on the lightningChance roll, and removes the projectile.
func (e *Projectile) impact(tx *world.Tx, pos mgl64.Vec3, target *player.Player) {
	st := e.state
	if target != nil {
		var attacker world.Entity = e // owner disconnected mid-flight (or never set) — credit the projectile itself rather than crash
		if st.Owner != nil {
			if owner, ok := st.Owner.Entity(tx); ok {
				attacker = owner
			}
		}
		target.Hurt(AttackPoints(st.WeaponID), dfentity.AttackDamageSource{Attacker: attacker})
		if op, ok := attacker.(*player.Player); ok {
			op.Message(fmt.Sprintf("§b§l%s §rstrikes %s!", Defs[st.WeaponID].DisplayName, target.Name()))
		}
	}
	if rand.Float64() < st.LightningChance {
		tx.AddParticle(pos, particle.HugeExplosion{})
		tx.PlaySound(pos, sound.Explosion{})
	} else {
		tx.PlaySound(pos, sound.Explosion{})
	}
	tx.RemoveEntity(e)
}

// yawFromDirection converts a movement direction vector into a yaw angle,
// same formula demonking's entity.go already uses to face its target.
func yawFromDirection(v mgl64.Vec3) float64 {
	return math.Atan2(-v.X(), v.Z()) * 180 / math.Pi
}
