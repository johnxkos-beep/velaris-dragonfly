package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// chickenBBox matches vanilla's chicken collision box (width 0.4, height 0.7).
var chickenBBox = cube.Box(-0.2, 0, -0.2, 0.2, 0.7, 0.2)

var chickenSpec = mobSpec{
	MaxHP:     4, // vanilla chicken health
	WalkSpeed: 0.04,
	XPMin:     1, // vanilla: chickens grant 1-3 XP on death
	XPMax:     3,
	Drops:     chickenDrops,
}

// chickenDrops matches vanilla drop ranges: 1 raw chicken, 0-2 feathers.
//
// UNVERIFIED: item.Chicken{} and item.Feather{} are my best-confidence
// guess at Dragonfly's item type names — untested against a real build.
// Same note as cowDrops in cow.go applies: if `go build` complains, tell
// me the real names/fields and it's a one-line fix.
func chickenDrops() []item.Stack {
	drops := []item.Stack{item.NewStack(item.Chicken{}, 1)}
	if n := rand.Intn(3); n > 0 { // 0, 1, or 2
		drops = append(drops, item.NewStack(item.Feather{}, n))
	}
	return drops
}

// chickenEntityType is the world.EntityType for chickens. Uses the real
// vanilla identifier — no custom resource pack/texture needed.
type chickenEntityType struct{}

func (chickenEntityType) EncodeEntity() string        { return "minecraft:chicken" }
func (chickenEntityType) BBox(world.Entity) cube.BBox { return chickenBBox }
func (chickenEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openMob(tx, handle, data, &chickenSpec)
}
func (chickenEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeMobNBT(m, data, &chickenSpec)
}
func (chickenEntityType) EncodeNBT(data *world.EntityData) map[string]any { return encodeMobNBT(data) }

// ChickenType is the registered entity type for chickens — include it
// wherever your server builds its entity registry (see register.go).
var ChickenType chickenEntityType

// SpawnChicken creates and adds a chicken to tx at pos.
func SpawnChicken(tx *world.Tx, pos mgl64.Vec3) *Mob {
	return spawn(tx, ChickenType, &chickenSpec, pos)
}
