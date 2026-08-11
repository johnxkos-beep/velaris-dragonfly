package demonking

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

//go:embed textures/demon_king_spawn_egg.png
var spawnEggIconBytes []byte

// SpawnEgg summons an awake, hostile Demon King boss when used on a block.
// Identifier is "tnt:lord_demon_spawn_egg" — placeholder icon included
// (textures/demon_king_spawn_egg.png); swap in a real one from the add-on's
// texture pack if you'd rather match its art style exactly.
type SpawnEgg struct{}

func (SpawnEgg) EncodeItem() (name string, meta int16) {
	return "tnt:lord_demon_spawn_egg", 0
}

func (SpawnEgg) Name() string { return "§5Demon King Spawn Egg" }

func (SpawnEgg) Texture() image.Image {
	img, _, err := image.Decode(bytes.NewReader(spawnEggIconBytes))
	if err != nil {
		panic("demonking: failed to decode spawn egg icon: " + err.Error())
	}
	return img
}

func (SpawnEgg) Category() category.Category { return category.Items() }

// UseOnBlock spawns the boss one block above the clicked face, matching
// vanilla spawn egg placement behaviour.
//
// UNVERIFIED SIGNATURE WARNING: this method's exact parameter list
// (UsableOnBlock interface) wasn't checked against a live build — there was
// no Go toolchain available while writing this. If `go build` reports this
// method doesn't satisfy item.UsableOnBlock (or that interface doesn't
// exist under that name), paste me the compiler error and I'll fix the
// signature — the spawn logic itself (demonking.Spawn) is solid.
func (SpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ item.User, ctx *item.UseContext) bool {
	spawnPos := pos.Side(face).Vec3Centre()
	Spawn(tx, spawnPos)
	ctx.SubtractFromCount(1)
	return true
}
