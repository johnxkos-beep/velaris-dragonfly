package demonking

import (
	dfentity "github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// Tick looks up online players via this repo's existing velaris-dragonfly/state
// package (state.Server), the same global already set in main.go right
// after conf.New() (`state.Server = srv`) — no new global needed here.

// EntityRegistry returns an EntityRegistry containing every default
// Dragonfly entity type plus all four Demon King model states (phase 1,
// transforming, phase 2, dying — see respawnAs in entity.go for why there
// are four). Assign this to your server.UserConfig/Config's Entities field
// before calling srv creation — see the package README for the exact
// main.go wiring, since it depends on how this repo currently builds its
// Config.
func EntityRegistry() world.EntityRegistry {
	types := append(dfentity.DefaultRegistry.Types(), Type, TransformType, TypeV2, DeathType)
	return dfentity.DefaultRegistry.Config().New(types)
}

// Register registers the spawn egg and trophy items. Call this once at
// startup, alongside any other world.RegisterItem calls (e.g.
// legendaryweapons.Register() in this repo) — must happen before
// conf.New()/server.New(), same requirement as every other custom item here.
func Register() {
	world.RegisterItem(SpawnEgg{})
	world.RegisterItem(Trophy{})
}
