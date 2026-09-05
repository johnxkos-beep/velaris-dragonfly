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

// Register is a no-op now that the death drop uses Dragonfly's own
// block.DragonEgg{} (a real vanilla block/item — see entity.go's
// finishDeath) instead of a custom item, so there's nothing left to
// register. Kept so main.go's enderdragon.Register() call doesn't need to
// be removed.
//
// UNVERIFIED: this assumes block.DragonEgg{} exists in this Dragonfly
// version. If `go build` says it doesn't, the fix is reverting to a custom
// item — tell me and I'll bring back the placeholder-icon version from
// before.
func Register() {}

