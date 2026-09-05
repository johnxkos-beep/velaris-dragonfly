package commands

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
	"velaris-dragonfly/worldgen"
	dfworlds "velaris-dragonfly/worlds"
)

// endWorldName is the DFWorlds destination name /end opens/travels to. Kept
// separate from the server's own built-in End dimension (which stays flat,
// via dimensionGenerator in main.go) so this doesn't touch anything players
// may already have built there — it's a brand new, dedicated destination.
const endWorldName = "the_end"

// End is /end — for now, a simple op-gated stand-in for the real
// eye-of-ender/stronghold portal (deferred until that's built): it
// creates-if-missing a dedicated "the_end" DFWorlds destination (proper End
// dimension rules + worldgen.NewEnd's single-island terrain, no outer
// islands per request) and travels the player there. Run
// /spawnenderdragon (op) once you're there to build the arena and start
// the fight.
type End struct{}

func (End) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	if state.Worlds == nil || state.WorldRouter == nil {
		output.Print("World manager is not initialised.")
		return
	}

	if _, exists := state.Worlds.Destination(endWorldName); !exists {
		seed := nameSeed(endWorldName) // reuses commands/worlds.go's deterministic name->seed hash
		if _, err := state.Worlds.Open(dfworlds.Definition{
			Name:      endWorldName,
			Dimension: world.End,
			Generator: func(dim world.Dimension) world.Generator { return worldgen.NewEnd(seed) },
		}); err != nil {
			output.Printf("Failed to open the End: %s", err.Error())
			return
		}
	}

	if err := state.WorldRouter.SendPlayerTx(tx, p, endWorldName); err != nil {
		output.Printf("Failed to travel to the End: %s", err.Error())
		return
	}
	output.Print("Welcome to the End. An op can run /spawnenderdragon here to start the fight.")
}
