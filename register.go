package enderdragon

import "github.com/df-mc/dragonfly/server/world"

// EntityTypes returns the entity types this package adds, for merging into
// the server's entity registry in main.go — mirrors mobs.EntityTypes() /
// restrict.EntityTypes() / scoreboard.EntityTypes(). Unlike
// demonking.EntityRegistry() (which builds a whole registry because it
// predates this simpler pattern), this just returns the types themselves so
// main.go can append them alongside everything else already going into
// dfentity.DefaultRegistry.Config().New(...).
func EntityTypes() []world.EntityType {
	return []world.EntityType{Type, CrystalType}
}

// Register registers the Dragon Egg item. Call once at startup, alongside
// the other world.RegisterItem/Register calls (e.g. demonking.Register(),
// mobs.Register()) — must happen before conf.New(), same requirement as
// every other custom item in this repo.
func Register() {
	world.RegisterItem(Egg{})
}
