package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// skeletonBBox matches vanilla's skeleton collision box (same footprint
// as a zombie: width 0.6, height 1.95).
var skeletonBBox = cube.Box(-0.3, 0, -0.3, 0.3, 1.95, 0.3)

var skeletonSpec = hostileSpec{
	MaxHP:            20, // vanilla skeleton health
	WalkSpeed:        0.044,
	XPMin:            1, // vanilla: skeletons grant 1-3 XP on death
	XPMax:            3,
	Drops:            skeletonDrops,
	Kind:             attackRanged, // see hostilemob.go doc comment — no physical arrow entity, see file header there
	AttackDamage:     4,            // roughly vanilla's normal-difficulty arrow hit
	AttackRange:      10,           // stands off and "shoots" rather than closing to melee
	AttackCooldown:   30,           // ~1.5s, matches a real bow's draw time better than melee's faster cooldown
	AggroRadius:      16,
	LoseTargetRadius: 24,
}

// skeletonDrops matches vanilla drop ranges: 0-2 bone, 0-2 arrow.
//
// UNVERIFIED: item.Bone{} and item.Arrow{} are my best-confidence guess
// at Dragonfly's item type names (same bare-struct pattern already
// confirmed for item.Beef{}/item.Leather{} in cow.go) — untested against
// a real build. If `go build` complains either doesn't exist, tell me
// the real name(s) and it's a one-line fix.
func skeletonDrops() []item.Stack {
	var drops []item.Stack
	if n := rand.Intn(3); n > 0 {
		drops = append(drops, item.NewStack(item.Bone{}, n))
	}
	if n := rand.Intn(3); n > 0 {
		drops = append(drops, item.NewStack(item.Arrow{}, n))
	}
	return drops
}

// skeletonEntityType is the world.EntityType for skeletons. EncodeEntity
// uses the real vanilla identifier, so no custom resource pack/texture
// is needed.
type skeletonEntityType struct{}

func (skeletonEntityType) EncodeEntity() string       { return "minecraft:skeleton" }
func (skeletonEntityType) BBox(world.Entity) cube.BBox { return skeletonBBox }
func (skeletonEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openHostileMob(tx, handle, data, &skeletonSpec)
}
func (skeletonEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeHostileMobNBT(m, data, &skeletonSpec)
}
func (skeletonEntityType) EncodeNBT(data *world.EntityData) map[string]any {
	return encodeHostileMobNBT(data)
}

// SkeletonType is the registered entity type for skeletons — include it
// wherever your server builds its entity registry (see register.go).
var SkeletonType skeletonEntityType

// SpawnSkeleton creates and adds a skeleton to tx at pos.
func SpawnSkeleton(tx *world.Tx, pos mgl64.Vec3) *HostileMob {
	return spawnHostile(tx, SkeletonType, &skeletonSpec, pos)
}
