package enderdragon

import (
	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

// Crystal tuning. A real End Crystal dies in one hit from almost anything in
// vanilla — crystalHP is small enough that any weapon (even bare fists,
// eventually) pops it, without making it a literal always-one-shot (so a
// stray arrow graze doesn't necessarily pop it before you're ready).
const (
	crystalHP           = 5.0
	crystalExplodeDamage = 6.0
	crystalExplodeRadius = 6.0
)

// crystalState is the mutable per-crystal data.
type crystalState struct {
	HP       float64
	Counter  *crystalCounter
	Exploded bool
}

// CrystalType is the world.EntityType for the End Crystal. Register it via
// EntityRegistry() (see register.go).
var CrystalType crystalType

type crystalType struct{}

func (crystalType) EncodeEntity() string { return "minecraft:end_crystal" }
func (crystalType) BBox(world.Entity) cube.BBox { return crystalBBox }
func (crystalType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*crystalState)
	if !ok || st == nil {
		st = &crystalState{HP: crystalHP}
		data.Data = st
	}
	return &Crystal{tx: tx, handle: handle, data: data, state: st}
}
func (crystalType) DecodeNBT(m map[string]any, data *world.EntityData) {
	st := &crystalState{HP: crystalHP}
	if hp, ok := m["CrystalHP"].(float64); ok {
		st.HP = hp
	}
	data.Data = st
}
func (crystalType) EncodeNBT(data *world.EntityData) map[string]any {
	st, _ := data.Data.(*crystalState)
	if st == nil {
		st = &crystalState{HP: crystalHP}
	}
	return map[string]any{"CrystalHP": st.HP}
}

// crystalBBox matches the real End Crystal's 2x2x2 collision box.
var crystalBBox = cube.Box(-1, 0, -1, 1, 2, 1)

// CrystalConfig spawns a crystal wired into counter (decrements it, and
// stops contributing to dragon regen, when it dies).
type CrystalConfig struct{ Counter *crystalCounter }

func (c CrystalConfig) Apply(data *world.EntityData) {
	data.Data = &crystalState{HP: crystalHP, Counter: c.Counter}
}

// SpawnCrystal creates and adds an End Crystal at pos, contributing to
// counter's alive count until destroyed.
func SpawnCrystal(tx *world.Tx, pos mgl64.Vec3, counter *crystalCounter) *Crystal {
	handle := world.EntitySpawnOpts{Position: pos}.New(CrystalType, CrystalConfig{Counter: counter})
	e := tx.AddEntity(handle)
	c, _ := e.(*Crystal)
	return c
}

// Crystal is the live, in-transaction entity implementation.
type Crystal struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *crystalState
}

func (e *Crystal) H() *world.EntityHandle  { return e.handle }
func (e *Crystal) Position() mgl64.Vec3    { return e.data.Pos }
func (e *Crystal) Rotation() cube.Rotation { return e.data.Rot }
func (e *Crystal) Close() error            { return nil }

func (e *Crystal) Health() float64            { return e.state.HP }
func (e *Crystal) MaxHealth() float64         { return crystalHP }
func (e *Crystal) SetMaxHealth(float64)       {}
func (e *Crystal) SetSpeed(float64)           {}
func (e *Crystal) Speed() float64             { return 0 }
func (e *Crystal) SetVelocity(mgl64.Vec3)     {}
func (e *Crystal) Velocity() mgl64.Vec3       { return mgl64.Vec3{} }
func (e *Crystal) Dead() bool                 { return e.state.Exploded }
func (e *Crystal) AddEffect(effect.Effect)    {}
func (e *Crystal) Effects() []effect.Effect   { return nil }
func (e *Crystal) RemoveEffect(effect.Type)   {}
func (e *Crystal) Heal(float64, world.HealingSource) float64 { return 0 }

// KnockBack is a no-op — crystals float in place and don't get shoved.
func (e *Crystal) KnockBack(mgl64.Vec3, float64, float64) {}

// Hurt applies damage and explodes the crystal once HP runs out.
func (e *Crystal) Hurt(dmg float64, _ world.DamageSource) (float64, bool) {
	if e.state.Exploded || dmg < 0 {
		return 0, false
	}
	e.state.HP -= dmg
	for _, v := range e.tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, dfentity.HurtAction{})
	}
	if e.state.HP <= 0 {
		e.explode()
	}
	return dmg, true
}

// Tick just keeps e.tx fresh so Hurt (called outside the normal Tick
// callback chain, from whatever hit it) always has a live transaction to
// remove itself and damage nearby players with.
func (e *Crystal) Tick(tx *world.Tx, _ int64) { e.tx = tx }

// explode damages nearby players, removes the crystal, and tells its shared
// counter it's gone (which is what lets the dragon's regen stop once every
// crystal has been broken).
func (e *Crystal) explode() {
	e.state.Exploded = true
	pos := e.data.Pos
	e.tx.PlaySound(pos, sound.Explosion{})
	e.tx.AddParticle(pos, particle.HugeExplosion{})
	for p := range state.Server.Players(e.tx) {
		if p.Dead() {
			continue
		}
		if p.Position().Sub(pos).Len() <= crystalExplodeRadius {
			p.Hurt(crystalExplodeDamage, dfentity.AttackDamageSource{Attacker: e})
		}
	}
	if e.state.Counter != nil {
		e.state.Counter.dec()
	}
	e.tx.RemoveEntity(e)
}
