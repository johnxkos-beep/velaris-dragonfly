package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// pigBBox matches vanilla's pig collision box (width 0.9, height 0.9).
var pigBBox = cube.Box(-0.45, 0, -0.45, 0.45, 0.9, 0.45)

var pigSpec = mobSpec{
	MaxHP:     10, // vanilla pig health
	WalkSpeed: 0.045,
	XPMin:     1, // vanilla: pigs grant 1-3 XP on death
	XPMax:     3,
	Drops:     pigDrops,
}

// pigDrops matches vanilla drop range: 1-3 raw porkchop.
//
// UNVERIFIED: item.Porkchop{} is my best-confidence guess at Dragonfly's
// item type name (same Cooked-bool meat pattern as item.Beef{}/
// item.Chicken{} used in cow.go/chicken.go) — untested against a real
// build. Same fix as those: if `go build` complains, tell me the real
// name/fields and it's a one-line change.
func pigDrops() []item.Stack {
	return []item.Stack{item.NewStack(item.Porkchop{}, 1+rand.Intn(3))}
}

// pigEntityType is the world.EntityType for pigs. Uses the real vanilla
// identifier — no custom resource pack/texture needed.
type pigEntityType struct{}

func (pigEntityType) EncodeEntity() string        { return "minecraft:pig" }
func (pigEntityType) BBox(world.Entity) cube.BBox { return pigBBox }
func (pigEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openMob(tx, handle, data, &pigSpec)
}
func (pigEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeMobNBT(m, data, &pigSpec)
}
func (pigEntityType) EncodeNBT(data *world.EntityData) map[string]any { return encodeMobNBT(data) }

// PigType is the registered entity type for pigs — include it wherever
// your server builds its entity registry (see register.go).
var PigType pigEntityType

// SpawnPig creates and adds a pig to tx at pos.
func SpawnPig(tx *world.Tx, pos mgl64.Vec3) *Mob {
	return spawn(tx, PigType, &pigSpec, pos)
}
