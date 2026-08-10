// Package legendaryweapons registers Dragonfly-side stand-ins for the
// custom weapon items HopliteLegendary (running via the PMMP bridge)
// hands out — things like the Midas Sword.
//
// These are deliberately minimal. All real behavior — abilities, kill
// tracking, upgrades, everything — lives in the PHP plugin and is
// applied via the pmmpbridge action system, same as any other item.
// Dragonfly only needs to recognize the item well enough to store it in
// an inventory and put it in a player's hand. The actual in-hand model
// and texture come from the HopliteV1 resource pack's attachable files
// (attachables/bey_*.json), which key off the exact same identifier
// registered here — Bedrock clients match items to attachables by
// identifier automatically, no server-side model/geometry code needed.
package legendaryweapons

import "github.com/df-mc/dragonfly/server/world"

// Weapon is a minimal custom item. id must exactly match both what
// HopliteLegendary registers server-side (see ItemRegistrar.php) and
// the "identifier" field in the matching attachables/bey_*.json file in
// the resource pack — that's what makes the client render the right
// model in hand.
type Weapon struct {
	id   string
	name string
}

func (w Weapon) EncodeItem() (name string, meta int16) {
	return w.id, 0
}

// weapons lists every legendary weapon known so far. Add a line here
// and it'll be registered automatically — id must match the
// attachable's identifier in the resource pack exactly (check
// attachables/bey_<name>.json's "minecraft:attachable" > "description"
// > "identifier" field if unsure).
var weapons = map[string]string{
	"bey:midas_sword": "Midas Sword",

	// Not yet added — add as each one is actually needed, following
	// the same "bey:x" -> display name pattern:
	// "bey:golem_hammer":        "Golem Hammer",
	// "bey:villager_wand":       "Villager Wand",
	// "bey:sculkweaver_lantern": "Sculkweaver Lantern",
	// "bey:poseidon_trident":    "Poseidon Trident",
	// "bey:shadow_blade":        "Shadow Blade",
	// "bey:aiglos":              "Aiglos",
	// "bey:ender_bow":           "Ender Bow",
}

// Register registers every known legendary weapon with Dragonfly. Must
// be called before conf.New() — Dragonfly bakes its resource pack from
// whatever custom items are registered at that point, same requirement
// as any other custom item.
func Register() {
	for id, name := range weapons {
		world.RegisterItem(Weapon{id: id, name: name})
	}
}
