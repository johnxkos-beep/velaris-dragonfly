// PILOT — Midas Sword only, not yet wired into Init()/NewWeaponStack.
//
// WHY A SEPARATE TYPE
// Dragonfly's server.go decides how to render a registered custom item like
// this, checking the ITEM value itself (not some separate block):
//
//	_, isCustomBlock := it.(world.CustomBlock)
//
// So the same Go type has to implement both world.CustomItem (flat-icon
// fallback, name, category) AND world.CustomBlockBuildable (Geometry() +
// Textures()) for the in-hand render to use real 3D geometry instead of a
// flat plane. That's why this can't just bolt block methods onto the shared
// Weapon type — doing so would make ALL 8 weapons resolve isCustomBlock=true
// and require geometry for all of them. MidasSwordItem exists only for
// "bey:midas_sword"; the other 7 keep using plain Weapon (flat icon, same as
// 6 of 8 weapons always were in the original PMMP plugin).
//
// NOT YET WIRED UP. To test this pilot, in items.go:
//   - NewWeaponStack: for id == "bey:midas_sword", build
//     item.NewStack(MidasSwordItem{}, 1) instead of Weapon{def: d}.
//   - Init: world.RegisterItem(MidasSwordItem{}) instead of/alongside the
//     Weapon registration for "bey:midas_sword".
// I left these two changes out of this diff on purpose so the pilot type can
// be reviewed/built in isolation first.
//
// UNVERIFIED (no compiler access where I wrote this — see items.go's file
// header for why): customblock.Properties{} zero value, and the exact
// Handler.HandleBlockPlace cancel call needed in main.go to stop the block
// from actually being placeable (it has empty model/no collision as a
// fallback, but placement should still be cancelled outright). Build it,
// paste me the first compile error, and I'll fix it.
package legendary

import (
	"bytes"
	"embed"
	"fmt"
	"image"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/customblock"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed blockassets/*.geo.json blockassets/*.png
var blockAssets embed.FS

// midasSwordGeometry is the block-geometry JSON pulled from your mcpack
// (models/blocks/midas_sword.geo - Converted.geo.json), already in Bedrock's
// block geometry format (format_version 1.21.20).
var midasSwordGeometry = mustReadBlockAsset("midas_sword.geo.json")

// midasSwordBlockTexture is the weapon texture the geometry's UVs reference
// (textures/weapon/midas_sword.png in your pack — different from the flat
// inventory icon in legendary/assets, which is textures/icons/midas_sword.png).
var midasSwordBlockTexture = mustDecodeBlockTexture("midas_sword_block.png")

func mustReadBlockAsset(name string) []byte {
	b, err := blockAssets.ReadFile("blockassets/" + name)
	if err != nil {
		panic(fmt.Sprintf("legendary: missing block asset %q: %v", name, err))
	}
	return b
}

func mustDecodeBlockTexture(name string) image.Image {
	b := mustReadBlockAsset(name)
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		panic(fmt.Sprintf("legendary: failed to decode block texture %q: %v", name, err))
	}
	return img
}

// midasSwordBlockHash is the unique base hash for this pseudo-block,
// obtained once via block.NextHash() — Dragonfly's documented way for
// custom blocks to get a collision-free identifier.
var midasSwordBlockHash = block.NextHash()

// MidasSwordItem is the Midas Sword, implementing both world.CustomItem and
// world.CustomBlockBuildable on the same type so Dragonfly renders it in
// hand using real geometry (see file header). It intentionally does NOT
// reuse the generic Weapon type from items.go.
type MidasSwordItem struct{}

// EncodeItem implements world.Item. Same ID as before so existing
// crafted/dropped/claimed items keep working.
func (MidasSwordItem) EncodeItem() (name string, meta int16) {
	return "bey:midas_sword", 0
}

// Name implements world.CustomItem (and world.CustomBlockBuildable, which
// also requires Name()).
func (MidasSwordItem) Name() string { return Defs["bey:midas_sword"].DisplayName }

// Texture implements world.CustomItem — the flat inventory icon (still
// used for the creative menu / item frame render, just not the in-hand
// render once this is fully wired up).
func (MidasSwordItem) Texture() image.Image { return Defs["bey:midas_sword"].texture }

// Category implements world.CustomItem.
func (MidasSwordItem) Category() category.Category { return category.Equipment() }

// EncodeBlock implements world.Block. No properties/permutations — one
// static geometry, and this "block" is never meant to actually occupy a
// world position (see Model below and the placement-cancel note above).
func (MidasSwordItem) EncodeBlock() (string, map[string]any) {
	return "velaris:midas_sword_display_block", nil
}

// Hash implements world.Block.
func (MidasSwordItem) Hash() (uint64, uint64) { return midasSwordBlockHash, 0 }

// Model implements world.Block. No bbox, no solid faces on any side — even
// if a placement attempt somehow isn't cancelled server-side, this should
// behave like nothing is there.
func (MidasSwordItem) Model() world.BlockModel { return displayBlockModel{} }

// Properties implements world.CustomBlock. Zero value — UNVERIFIED, see file
// header.
func (MidasSwordItem) Properties() customblock.Properties {
	return customblock.Properties{}
}

// Geometry implements world.CustomBlockBuildable.
func (MidasSwordItem) Geometry() []byte { return midasSwordGeometry }

// Textures implements world.CustomBlockBuildable.
func (MidasSwordItem) Textures() map[string]image.Image {
	return map[string]image.Image{"default": midasSwordBlockTexture}
}

// NewMidasSwordStack returns a full stack of 1 Midas Sword using the
// block-item render path. Mirrors NewWeaponStack's naming/lore behaviour so
// swapping the dispatch in NewWeaponStack is a one-line change.
func NewMidasSwordStack() item.Stack {
	d := Defs["bey:midas_sword"]
	s := item.NewStack(MidasSwordItem{}, 1)
	named := s.WithCustomName(d.DisplayName).WithLore(d.Lore...)
	if named.Empty() || named.Count() < 1 {
		return s
	}
	return named
}

// displayBlockModel is a world.BlockModel with no collision and no solid
// faces — shared by any future "*_display_block" pseudo-blocks used for
// other weapons' in-hand renders.
type displayBlockModel struct{}

func (displayBlockModel) BBox(cube.Pos, world.BlockSource) []cube.BBox { return nil }

func (displayBlockModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
