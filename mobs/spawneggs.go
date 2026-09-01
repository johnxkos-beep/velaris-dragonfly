// Spawn egg items for cow, chicken, pig, and sheep, following the exact
// same pattern as bosses/demonking's SpawnEgg (see that file for the fuller
// explanation of the tap-and-hold cooldown guard).
//
// IDENTIFIERS: these use custom "velaris:" identifiers rather than the real
// vanilla ones ("minecraft:cow_spawn_egg" / "minecraft:chicken_spawn_egg").
// An earlier version of this file used the vanilla identifiers directly,
// which caused both eggs to show up in creative under the same name/icon
// and both spawn a chicken — Dragonfly's vanilla item table already has
// internal entries for those exact identifiers (needed for legacy/protocol
// IDs even without full spawn-egg behaviour built in), so registering our
// own item under the same string collided with that built-in entry instead
// of cleanly adding two separate items. Using a namespace nothing else
// claims (matching how demonking's own spawn egg avoids this with
// "tnt:lord_demon_spawn_egg") sidesteps the collision entirely.
//
// The icon textures embedded below are placeholder speckled-egg art
// (brown/cream for cow, cream/red for chicken, pink for pig, cream/pink
// for sheep) generated for this project, not pulled from the real game
// files — swap in the real vanilla PNGs later if you'd rather match
// Bedrock's own icons exactly.
//
// Registering Category() (below) is what makes these show up in the
// creative inventory — same mechanism the Demon King spawn egg already
// uses successfully.
package mobs

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

//go:embed textures/cow_spawn_egg.png
var cowSpawnEggIconBytes []byte

//go:embed textures/chicken_spawn_egg.png
var chickenSpawnEggIconBytes []byte

//go:embed textures/pig_spawn_egg.png
var pigSpawnEggIconBytes []byte

//go:embed textures/sheep_spawn_egg.png
var sheepSpawnEggIconBytes []byte

// spawnEggCooldownMu/spawnEggCooldown guard against a single tap-and-hold on
// mobile spawning many mobs at once (Bedrock resends the "use item" packet
// every tick while held) — shared across both egg types, same fix
// demonking's spawn egg uses.
var (
	spawnEggCooldownMu sync.Mutex
	spawnEggCooldown   = map[*player.Player]time.Time{}
)

func spawnEggOnCooldown(p *player.Player) bool {
	spawnEggCooldownMu.Lock()
	defer spawnEggCooldownMu.Unlock()
	last, seen := spawnEggCooldown[p]
	now := time.Now()
	if seen && now.Sub(last) < 750*time.Millisecond {
		return true
	}
	spawnEggCooldown[p] = now
	return false
}

// CowSpawnEgg spawns a cow when used on a block.
type CowSpawnEgg struct{}

func (CowSpawnEgg) EncodeItem() (name string, meta int16) { return "velaris:cow_spawn_egg", 0 }
func (CowSpawnEgg) Name() string                          { return "Cow Spawn Egg" }

func (CowSpawnEgg) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(cowSpawnEggIconBytes))
	if err != nil {
		panic("mobs: failed to decode cow spawn egg icon: " + err.Error())
	}
	return img
}

func (CowSpawnEgg) Category() category.Category { return category.Items() }

// UseOnBlock spawns a cow one block above the clicked face, matching
// vanilla spawn egg placement behaviour.
//
// UNVERIFIED SIGNATURE WARNING: same caveat as demonking's SpawnEgg —
// this method's exact parameter list (item.UsableOnBlock interface)
// wasn't checked against a live build. If `go build` reports this doesn't
// satisfy the expected interface, paste the compiler error and it's a
// quick fix.
func (CowSpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if p, ok := u.(*player.Player); ok && spawnEggOnCooldown(p) {
		return false
	}
	SpawnCow(tx, pos.Side(face).Vec3Centre())
	ctx.SubtractFromCount(1)
	return true
}

// ChickenSpawnEgg spawns a chicken when used on a block.
type ChickenSpawnEgg struct{}

func (ChickenSpawnEgg) EncodeItem() (name string, meta int16) {
	return "velaris:chicken_spawn_egg", 0
}
func (ChickenSpawnEgg) Name() string { return "Chicken Spawn Egg" }

func (ChickenSpawnEgg) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(chickenSpawnEggIconBytes))
	if err != nil {
		panic("mobs: failed to decode chicken spawn egg icon: " + err.Error())
	}
	return img
}

func (ChickenSpawnEgg) Category() category.Category { return category.Items() }

func (ChickenSpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if p, ok := u.(*player.Player); ok && spawnEggOnCooldown(p) {
		return false
	}
	SpawnChicken(tx, pos.Side(face).Vec3Centre())
	ctx.SubtractFromCount(1)
	return true
}

// PigSpawnEgg spawns a pig when used on a block.
type PigSpawnEgg struct{}

func (PigSpawnEgg) EncodeItem() (name string, meta int16) { return "velaris:pig_spawn_egg", 0 }
func (PigSpawnEgg) Name() string                          { return "Pig Spawn Egg" }

func (PigSpawnEgg) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(pigSpawnEggIconBytes))
	if err != nil {
		panic("mobs: failed to decode pig spawn egg icon: " + err.Error())
	}
	return img
}

func (PigSpawnEgg) Category() category.Category { return category.Items() }

func (PigSpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if p, ok := u.(*player.Player); ok && spawnEggOnCooldown(p) {
		return false
	}
	SpawnPig(tx, pos.Side(face).Vec3Centre())
	ctx.SubtractFromCount(1)
	return true
}

// SheepSpawnEgg spawns a sheep when used on a block.
type SheepSpawnEgg struct{}

func (SheepSpawnEgg) EncodeItem() (name string, meta int16) { return "velaris:sheep_spawn_egg", 0 }
func (SheepSpawnEgg) Name() string                          { return "Sheep Spawn Egg" }

func (SheepSpawnEgg) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(sheepSpawnEggIconBytes))
	if err != nil {
		panic("mobs: failed to decode sheep spawn egg icon: " + err.Error())
	}
	return img
}

func (SheepSpawnEgg) Category() category.Category { return category.Items() }

func (SheepSpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if p, ok := u.(*player.Player); ok && spawnEggOnCooldown(p) {
		return false
	}
	SpawnSheep(tx, pos.Side(face).Vec3Centre())
	ctx.SubtractFromCount(1)
	return true
}
