package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// cowBBox matches vanilla's cow collision box (width 0.9, height 1.4).
var cowBBox = cube.Box(-0.45, 0, -0.45, 0.45, 1.4, 0.45)

var cowSpec = mobSpec{
	MaxHP:     10, // vanilla cow health
	WalkSpeed: 0.045,
	XPMin:     1, // vanilla: cows grant 1-3 XP on death
	XPMax:     3,
	Drops:     cowDrops,
}

// cowDrops matches vanilla drop ranges: 1-3 raw beef, 0-2 leather.
//
// UNVERIFIED: item.Beef{} and item.Leather{} are my best-confidence guess
// at Dragonfly's item type names (matches the Cow{}/Cooked-bool pattern
// used elsewhere for meats in this version of Dragonfly) — untested
// against a real build. If `go build` complains these don't exist, tell me
// the real names/fields (e.g. if it's item.Beef{Cooked: false} instead of
// a bare item.Beef{}) and it's a one-line fix.
func cowDrops() []item.Stack {
	drops := []item.Stack{item.NewStack(item.Beef{}, 1+rand.Intn(3))}
	if n := rand.Intn(3); n > 0 { // 0, 1, or 2
		drops = append(drops, item.NewStack(item.Leather{}, n))
	}
	return drops
}

// cowEntityType is the world.EntityType for cows. EncodeEntity uses the
// real vanilla identifier, so no custom resource pack or texture is
// needed — every Bedrock client already knows how to render a cow.
type cowEntityType struct{}

func (cowEntityType) EncodeEntity() string          { return "minecraft:cow" }
func (cowEntityType) BBox(world.Entity) cube.BBox   { return cowBBox }
func (cowEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openMob(tx, handle, data, &cowSpec)
}
func (cowEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeMobNBT(m, data, &cowSpec)
}
func (cowEntityType) EncodeNBT(data *world.EntityData) map[string]any { return encodeMobNBT(data) }

// CowType is the registered entity type for cows — include it wherever
// your server builds its entity registry (see register.go).
var CowType cowEntityType

// SpawnCow creates and adds a cow to tx at pos.
func SpawnCow(tx *world.Tx, pos mgl64.Vec3) *Mob {
	return spawn(tx, CowType, &cowSpec, pos)
}
