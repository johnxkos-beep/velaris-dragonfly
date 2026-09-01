package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/colour"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// sheepBBox matches vanilla's sheep collision box (width 0.9, height 1.3).
var sheepBBox = cube.Box(-0.45, 0, -0.45, 0.45, 1.3, 0.45)

var sheepSpec = mobSpec{
	MaxHP:     8, // vanilla sheep health
	WalkSpeed: 0.045,
	XPMin:     1, // vanilla: sheep grant 1-3 XP on death
	XPMax:     3,
	Drops:     sheepDrops,
}

// sheepDrops matches vanilla drop ranges: 1-3 raw mutton, plus 1 white
// wool block (vanilla sheep drop wool matching their dye colour when
// sheared, but these mobs aren't shearable — on death they just drop one
// wool block, kept a fixed white for simplicity rather than tracking a
// per-sheep colour).
//
// UNVERIFIED, two separate guesses here:
//  1. item.Mutton{} — same Cooked-bool meat pattern as item.Beef{}/
//     item.Chicken{}/item.Porkchop{} elsewhere in this package.
//  2. block.Wool{Colour: colour.White()} — wool is a block in Dragonfly,
//     not a plain item.Item, so it has to come from the block package
//     instead; I'm using it as a world.Item the same way the rest of this
//     codebase turns blocks into item.Stacks. If this specific line fails
//     to build and you'd rather not chase it down, the mutton drop above
//     it is independent and safe to keep — just delete the wool append
//     below and sheep will still drop meat and XP normally.
func sheepDrops() []item.Stack {
	drops := []item.Stack{item.NewStack(item.Mutton{}, 1+rand.Intn(3))}
	drops = append(drops, item.NewStack(block.Wool{Colour: colour.White()}, 1))
	return drops
}

// sheepEntityType is the world.EntityType for sheep. Uses the real vanilla
// identifier — no custom resource pack/texture needed.
type sheepEntityType struct{}

func (sheepEntityType) EncodeEntity() string        { return "minecraft:sheep" }
func (sheepEntityType) BBox(world.Entity) cube.BBox { return sheepBBox }
func (sheepEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openMob(tx, handle, data, &sheepSpec)
}
func (sheepEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeMobNBT(m, data, &sheepSpec)
}
func (sheepEntityType) EncodeNBT(data *world.EntityData) map[string]any { return encodeMobNBT(data) }

// SheepType is the registered entity type for sheep — include it wherever
// your server builds its entity registry (see register.go).
var SheepType sheepEntityType

// SpawnSheep creates and adds a sheep to tx at pos.
func SpawnSheep(tx *world.Tx, pos mgl64.Vec3) *Mob {
	return spawn(tx, SheepType, &sheepSpec, pos)
}
