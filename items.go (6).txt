// Package legendaryweapons registers Dragonfly-side stand-ins for the
// custom weapon items HopliteLegendary (running via the PMMP bridge)
// hands out — things like the Midas Sword.
//
// These are deliberately minimal. All real behavior — abilities, kill
// tracking, upgrades, everything — lives in the PHP plugin and is
// applied via the pmmpbridge action system, same as any other item.
// Dragonfly only needs to recognize the item well enough to store it in
// an inventory and put it in a player's hand. The actual in-hand model
// and animation come from the HopliteV1 resource pack's attachable
// files (attachables/bey_*.json), which key off the exact same
// identifier registered here — Bedrock clients match items to
// attachables by identifier automatically, no server-side model
// geometry needed for that part.
//
// A plain world.Item isn't enough here — Dragonfly only assigns a
// custom item a network runtime ID (required for it to work at all) if
// it implements the richer world.CustomItem interface, which needs a
// display name, an icon texture, and a creative-inventory category on
// top of the basic EncodeItem() every item needs.
package legendaryweapons

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"

	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed textures/midas_sword.png
var midasSwordIconBytes []byte

// Weapon is a minimal custom item. id must exactly match both what
// HopliteLegendary registers server-side (see ItemRegistrar.php) and
// the "identifier" field in the matching attachables/bey_*.json file in
// the resource pack — that's what makes the client render the right
// model in hand. icon is the item's icon in the inventory/hotbar (a
// small square PNG) — this is separate from, and doesn't need to
// match, the in-hand 3D look, which comes entirely from the resource
// pack's attachable.
type Weapon struct {
	id   string
	name string
	icon image.Image
}

func (w Weapon) EncodeItem() (name string, meta int16) {
	return w.id, 0
}

func (w Weapon) Name() string {
	return w.name
}

func (w Weapon) Texture() image.Image {
	return w.icon
}

func (w Weapon) Category() category.Category {
	return category.Equipment()
}

// Register registers every known legendary weapon with Dragonfly. Must
// be called before conf.New() — Dragonfly bakes its resource pack from
// whatever custom items are registered at that point, same requirement
// as any other custom item.
//
// To add another weapon: embed its icon PNG the same way as
// midasSwordIconBytes above (from textures/icons/<name>.png in the
// resource pack), decode it, and add a Weapon{} entry in the register
// calls at the bottom of this function. The id must match the
// attachable's identifier exactly (check attachables/bey_<name>.json's
// "minecraft:attachable" > "description" > "identifier" field).
func Register() {
	midasSwordIcon, err := decodePNG(midasSwordIconBytes)
	if err != nil {
		panic("legendaryweapons: failed to decode midas_sword icon: " + err.Error())
	}

	world.RegisterItem(Weapon{
		id:   "bey:midas_sword",
		name: "Midas Sword",
		icon: midasSwordIcon,
	})

	// Add more here, following the same pattern:
	// world.RegisterItem(Weapon{id: "bey:golem_hammer", name: "Golem Hammer", icon: golemHammerIcon})
}

func decodePNG(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}
