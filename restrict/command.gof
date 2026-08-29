package restrict

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Command is /restrict — gives the executor exactly 2 marker blocks
// (coal blocks). Place both to define a restricted zone cuboid between
// wherever they land; breaking either one removes the zone. Only ops can
// enter the zone once created — everyone else is bounced back out (see
// Config.HandleMove). Op only, same as every other world-affecting admin
// command in this repo.
type Command struct{}

func (Command) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	if Cfg.HasPendingClaim(p.XUID()) {
		p.Message("§eYour previous unfinished restrict zone selection was cancelled.")
	}
	Cfg.BeginClaim(p.XUID())
	p.Inventory().AddItem(item.NewStack(MarkerBlock(), 2))
	p.Message("§aGiven 2 restrict zone blocks. Place both to create a zone — break either one to remove it.")
}
