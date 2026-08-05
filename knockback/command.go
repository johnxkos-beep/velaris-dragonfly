package knockback

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Command is /kb — opens the KB configuration editor form. In-game only,
// op only (matches the original plugin's customkb.command.kb permission,
// which defaulted to op).
type Command struct{}

func (Command) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("The /kb config editor can only be opened in-game.")
		return
	}
	if Cfg == nil {
		output.Print("KB config isn't loaded yet.")
		return
	}
	sendConfigForm(p, Cfg)
}
