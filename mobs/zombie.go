package mobs

import (
	"math/rand"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// zombieBBox matches vanilla's zombie collision box (width 0.6, height 1.95).
var zombieBBox = cube.Box(-0.3, 0, -0.3, 0.3, 1.95, 0.3)

var zombieSpec = hostileSpec{
	MaxHP:            20, // vanilla zombie health
	WalkSpeed:        0.046,
	XPMin:            1, // vanilla: zombies grant 1-3 XP on death
	XPMax:            3,
	Drops:            zombieDrops,
	Kind:             attackMelee,
	AttackDamage:     3, // vanilla normal-difficulty zombie hit
	AttackRange:      1.8,
	AttackCooldown:   20, // ~1s
	AggroRadius:      16,
	LoseTargetRadius: 24,
	BurnsInSunlight:  true, // vanilla: zombies catch fire and burn in direct daytime sun
}

// zombieDrops matches vanilla drop ranges: 0-2 rotten flesh.
//
// UNVERIFIED: item.RottenFlesh{} is my best-confidence guess at
// Dragonfly's item type name (matches the same bare-struct pattern
// item.Beef{}/item.Leather{} already confirmed working in cow.go) —
// untested against a real build. If `go build` complains it doesn't
// exist, tell me the real name and it's a one-line fix.
func zombieDrops() []item.Stack {
	if n := rand.Intn(3); n > 0 { // 0, 1, or 2
		return []item.Stack{item.NewStack(item.RottenFlesh{}, n)}
	}
	return nil
}

// zombieEntityType is the world.EntityType for zombies. EncodeEntity uses
// the real vanilla identifier, so no custom resource pack/texture is
// needed.
type zombieEntityType struct{}

func (zombieEntityType) EncodeEntity() string        { return "minecraft:zombie" }
func (zombieEntityType) BBox(world.Entity) cube.BBox  { return zombieBBox }
func (zombieEntityType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return openHostileMob(tx, handle, data, &zombieSpec)
}
func (zombieEntityType) DecodeNBT(m map[string]any, data *world.EntityData) {
	decodeHostileMobNBT(m, data, &zombieSpec)
}
func (zombieEntityType) EncodeNBT(data *world.EntityData) map[string]any {
	return encodeHostileMobNBT(data)
}

// ZombieType is the registered entity type for zombies — include it
// wherever your server builds its entity registry (see register.go).
var ZombieType zombieEntityType

// SpawnZombie creates and adds a zombie to tx at pos.
func SpawnZombie(tx *world.Tx, pos mgl64.Vec3) *HostileMob {
	return spawnHostile(tx, ZombieType, &zombieSpec, pos)
}
