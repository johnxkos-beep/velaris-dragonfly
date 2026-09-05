package enderdragon

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// SpawnCommand is /spawnenderdragon — builds the 10-pillar arena centred on
// the executing player's position and spawns the Ender Dragon orbiting
// above it. Op only, same as every other admin/spawn command in this repo.
// Best run standing near the middle of an open area — the arena is roughly
// 90 blocks across (see pillarRingRadius in arena.go) — ideally the "the_end"
// DFWorlds destination /end sends players to.
type SpawnCommand struct{}

func (SpawnCommand) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (SpawnCommand) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	dragon := BuildArena(tx, p.Position())
	if dragon == nil {
		output.Print("Failed to spawn the Ender Dragon.")
		return
	}
	output.Print("The arena is built. The Ender Dragon awakens!")
	for other := range state.Server.Players(tx) {
		other.Message("§d§lThe Ender Dragon has awoken!")
	}
}
