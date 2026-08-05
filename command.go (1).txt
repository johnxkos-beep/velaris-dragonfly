package legendary

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Command is /legendary — opens the legendary codex. Matches the original
// plugin's hoplite.legendary permission (default: true, so every player can
// open it; no Allow restriction needed).
type Command struct{}

func (Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("The legendary codex can only be opened in-game.")
		return
	}
	if Mgr == nil {
		output.Print("The legendary codex isn't loaded yet.")
		return
	}
	sendCodexForm(p)
}
