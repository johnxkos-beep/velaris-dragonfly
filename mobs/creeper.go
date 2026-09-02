package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// creeperBBox matches vanilla's creeper collision box (width 0.6, height 1.7).
var creeperBBox = cube.Box(-0.3, 0, -0.3, 0.3, 1.7, 0.3)

var creeperSpec = hostileSpec{
	MaxHP:            20, // vanilla creeper health
	WalkSpeed:        0.04,
	XPMin:            5, // vanilla: creepers grant 5 XP on death (killed, not exploded)
	XPMax:            5,
	Drops:            creeperDrops,
	Kind:             attackExplode,
	AggroRadius:      16,
	LoseTargetRadius: 24,
	ExplodeRange:      3,  // gets this close before the fuse starts
	ExplodeRadius:     4,  // vanilla creeper explosion radius (roughly)
	ExplodeDamage:     30, // center-of-blast damage, falls off with distance — see explode() in hostilemob.go
	FuseTicks:         30, // ~1.5s, matches vanilla's creeper fuse
}

// creeperDrops matches vanilla drop ranges: 0-2 gunpowder. Only reached
// if the creeper is killed by damage before its fuse finishes — a
// creeper that actually explodes drops nothing (see explode() in
// hostilemob.go).
//
// UNVERIFIED: item.Gunpowder{} is my best-confidence guess at Dragonfly's
// item type name (same bare-struct pattern already confirmed for
// item.Beef{}/item.Leather{} in cow.go) — untested against a real build.
func creeperDrops() []item.Stack {
	if n := rand.Intn(3); n > 0 { // 0, 1, or 2
		return []item.Stack{item.NewStack(item.Gunpowder{}, n)}
	}
	return nil
}

// creeperEntityType is the world.EntityType for creepers. EncodeEntity
// uses the real vanilla identifier, so no custom resource pack/texture
// is needed.
type creeperEntityType struct{}

func (creeperEntityType) EncodeEntity() string       { return "minecraft:creeper" }
func (creeperEntityType) BBox(world.Entity) cube.BBox { return creeperBBox }
func (creeperEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openHostileMob(tx, handle, data, &creeperSpec)
}
func (creeperEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeHostileMobNBT(m, data, &creeperSpec)
}
func (creeperEntityType) EncodeNBT(data *world.EntityData) map[string]any {
	return encodeHostileMobNBT(data)
}

// CreeperType is the registered entity type for creepers — include it
// wherever your server builds its entity registry (see register.go).
var CreeperType creeperEntityType

// SpawnCreeper creates and adds a creeper to tx at pos.
func SpawnCreeper(tx *world.Tx, pos mgl64.Vec3) *HostileMob {
	return spawnHostile(tx, CreeperType, &creeperSpec, pos)
}
