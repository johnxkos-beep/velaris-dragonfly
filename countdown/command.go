package countdown

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Command is /count — opens the countdown setup form. Op only, port of
// CountPlugin\Main's onCommand ("countplugin.command", default op).
type Command struct{}

func (Command) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	sendSetupForm(p)
}
