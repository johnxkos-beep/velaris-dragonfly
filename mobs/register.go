package mobs

import "github.com/df-mc/dragonfly/server/world"

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry in main.go (mirrors
// restrict.EntityTypes() / scoreboard.EntityTypes() / demonking.EntityRegistry()).
func EntityTypes() []world.EntityType { return []world.EntityType{CowType, ChickenType} }

// Register registers the cow and chicken spawn egg items. Call this once
// at startup, alongside the other world.RegisterItem/Register calls (e.g.
// demonking.Register() in main.go) — must happen before conf.New(), same
// requirement as every other custom item in this repo.
func Register() {
	world.RegisterItem(CowSpawnEgg{})
	world.RegisterItem(ChickenSpawnEgg{})
}
