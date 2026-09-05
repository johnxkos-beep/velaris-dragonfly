package border

import (
	"strconv"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Command is "/border" and "/border <size>" — port of BorderCommand.php.
//
// Bare "/border" shows the current size to anyone — BorderCommand.php
// gated this behind a "hoplite.border" permission, which read as
// "granted to everyone" for a plain view command. "/border <size>"
// changes it and requires op — port of BorderCommand.php's
// "hoplite.border.admin" check.
//
// DEVIATION: Dragonfly's cmd package has no per-permission-node model
// the way PocketMine's does, so the view/change split that PHP expressed
// as two separate permissions is done here with a single command and a
// runtime op check inside Run when a size argument is actually given —
// same "op check only when the args imply a change" shape as this repo's
// koth.Activate/Name (which are unconditionally op-only) but adapted
// since /border's view half deliberately isn't.
type Command struct {
	Size cmd.Optional[string] `cmd:"size"`
}

// Allow always returns true — the permission split for changing the
// border happens inside Run once we know whether a size was actually
// given (see the doc comment above).
func (Command) Allow(cmd.Source) bool { return true }

func (c Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	raw, changing := c.Size.Load()
	if !changing {
		output.Printf("Current world border: %dx%d", Cfg.Size(), Cfg.Size())
		return
	}

	if !state.IsOpSource(src) {
		output.Print("You don't have permission to change the border.")
		return
	}

	size, err := strconv.Atoi(raw)
	if err != nil {
		output.Printf("%q isn't a valid border size.", raw)
		return
	}
	if size < minSize {
		output.Printf("Border size must be at least %d.", minSize)
		return
	}

	Cfg.SetSize(size)
	if p, ok := src.(*player.Player); ok {
		EnsureTicker(tx, p.Position())
	}
	output.Printf("World border set to %dx%d.", size, size)
}
