package mobs

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/world"
)

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry in main.go (mirrors
// restrict.EntityTypes() / scoreboard.EntityTypes() / demonking.EntityRegistry()).
func EntityTypes() []world.EntityType { return []world.EntityType{CowType, ChickenType} }

// Register registers the cow and chicken spawn egg items, AND adds them to
// the creative inventory. Call this once at startup, alongside the other
// world.RegisterItem/Register calls (e.g. demonking.Register() in main.go)
// — must happen before conf.New(), same requirement as every other custom
// item in this repo.
//
// UNVERIFIED: world.RegisterItem makes an item exist (so it can be given,
// held, and used), but does NOT by itself add it to the creative menu —
// that list is separate and, in Dragonfly, is populated via
// item/creative.RegisterItem. This is a different mechanism from each
// item's own Category() method (Category only controls which creative TAB
// an already-listed item shows up under, not whether it's listed at all)
// — Category() alone is why these vanished from creative once we stopped
// reusing vanilla identifiers (which the client already had pre-baked
// creative entries for).
//
// creative.RegisterItem takes a creative.Item wrapper struct, not a bare
// item.Stack directly (confirmed by the last build's compiler error) —
// creative.Item{Stack: ...} below is my best-confidence guess at that
// wrapper's field name, still untested against a real build. If this
// build also errors, the error will now be about the Item{} literal
// itself (wrong field name, or extra required fields) rather than a type
// mismatch — paste it and it's a quick fix.
func Register() {
	world.RegisterItem(CowSpawnEgg{})
	world.RegisterItem(ChickenSpawnEgg{})

	creative.RegisterItem(creative.Item{Stack: item.NewStack(CowSpawnEgg{}, 1)})
	creative.RegisterItem(creative.Item{Stack: item.NewStack(ChickenSpawnEgg{}, 1)})
}
