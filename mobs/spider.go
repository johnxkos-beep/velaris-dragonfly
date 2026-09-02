package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spiderBBox matches vanilla's spider collision box (width 1.4, height 0.9).
var spiderBBox = cube.Box(-0.7, 0, -0.7, 0.7, 0.9, 0.7)

var spiderSpec = hostileSpec{
	MaxHP:            16, // vanilla spider health
	WalkSpeed:        0.05,
	XPMin:            1, // vanilla: spiders grant 1-3 XP on death
	XPMax:            3,
	Drops:            spiderDrops,
	Kind:             attackMelee,
	AttackDamage:     2, // vanilla normal-difficulty spider hit
	AttackRange:      2.0,
	AttackCooldown:   16, // slightly faster than a zombie's swing
	AggroRadius:      16,
	LoseTargetRadius: 24,
}

// spiderDrops matches vanilla drop ranges: 0-2 string, 0-1 spider eye
// (kept out — see comment below).
//
// UNVERIFIED: item.String{} is my best-confidence guess at Dragonfly's
// item type name (same bare-struct pattern already confirmed for
// item.Beef{}/item.Leather{} in cow.go) — untested against a real build.
// Spider eye deliberately left out: it's a two-word vanilla item and I'd
// be guessing blind at whether it's item.SpiderEye{} vs some other name,
// vs. String which is a single obvious word — string is the vast
// majority of a spider's drop table anyway. Easy to add once you confirm
// the right type name.
func spiderDrops() []item.Stack {
	if n := rand.Intn(3); n > 0 { // 0, 1, or 2
		return []item.Stack{item.NewStack(item.String{}, n)}
	}
	return nil
}

// spiderEntityType is the world.EntityType for spiders. EncodeEntity uses
// the real vanilla identifier, so no custom resource pack/texture is
// needed.
type spiderEntityType struct{}

func (spiderEntityType) EncodeEntity() string       { return "minecraft:spider" }
func (spiderEntityType) BBox(world.Entity) cube.BBox { return spiderBBox }
func (spiderEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openHostileMob(tx, handle, data, &spiderSpec)
}
func (spiderEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeHostileMobNBT(m, data, &spiderSpec)
}
func (spiderEntityType) EncodeNBT(data *world.EntityData) map[string]any {
	return encodeHostileMobNBT(data)
}

// SpiderType is the registered entity type for spiders — include it
// wherever your server builds its entity registry (see register.go).
var SpiderType spiderEntityType

// SpawnSpider creates and adds a spider to tx at pos.
func SpawnSpider(tx *world.Tx, pos mgl64.Vec3) *HostileMob {
	return spawnHostile(tx, SpiderType, &spiderSpec, pos)
}
