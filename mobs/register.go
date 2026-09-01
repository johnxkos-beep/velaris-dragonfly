package mobs

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/world"
)

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry in main.go (mirrors
// restrict.EntityTypes() / scoreboard.EntityTypes() / demonking.EntityRegistry()).
func EntityTypes() []world.EntityType {
	return []world.EntityType{CowType, ChickenType, PigType, SheepType, SpawnerType}
}

// creativeGroupName is a unique name for the creative-inventory group all
// four spawn eggs belong to. Confirmed against the real Dragonfly source
// (server/item/creative/creative.go, v0.11.1): creative.Item only has a
// Stack and a Group (a plain string name) — there's no direct Category
// field on Item itself. The Category actually lives on a creative.Group,
// which you register separately with RegisterGroup, and Item.Group just
// has to match that Group's Name by string. Earlier versions of this file
// left Group empty, which is exactly why the eggs never showed up: an
// empty Group didn't point at any registered group, so the client had
// nowhere to place them.
const creativeGroupName = "velaris_mobs"

// Register registers the cow, chicken, pig, and sheep spawn egg items, and
// adds them to the creative inventory under their own group. Call this
// once at startup, alongside the other world.RegisterItem/Register calls
// (e.g. demonking.Register() in main.go) — must happen before conf.New(),
// same requirement as every other custom item in this repo.
func Register() {
	world.RegisterItem(CowSpawnEgg{})
	world.RegisterItem(ChickenSpawnEgg{})
	world.RegisterItem(PigSpawnEgg{})
	world.RegisterItem(SheepSpawnEgg{})

	cowStack := item.NewStack(CowSpawnEgg{}, 1)
	chickenStack := item.NewStack(ChickenSpawnEgg{}, 1)
	pigStack := item.NewStack(PigSpawnEgg{}, 1)
	sheepStack := item.NewStack(SheepSpawnEgg{}, 1)

	// One shared group for all four eggs, shown in the Items tab (matches
	// each egg's own Category() in spawneggs.go). Icon is just which item
	// represents the group visually if the client ever collapses it.
	creative.RegisterGroup(creative.Group{
		Category: creative.ItemsCategory(),
		Name:     creativeGroupName,
		Icon:     cowStack,
	})

	creative.RegisterItem(creative.Item{Stack: cowStack, Group: creativeGroupName})
	creative.RegisterItem(creative.Item{Stack: chickenStack, Group: creativeGroupName})
	creative.RegisterItem(creative.Item{Stack: pigStack, Group: creativeGroupName})
	creative.RegisterItem(creative.Item{Stack: sheepStack, Group: creativeGroupName})
}
