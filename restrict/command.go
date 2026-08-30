package restrict

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Command is /restrict — gives the executor exactly 2 marker blocks
// (obsidian, same marker block the pvp package uses). Place both to
// define a restricted zone cuboid between wherever they land; breaking
// either one removes the zone (and its wall). A real shell of barrier
// blocks gets built around the cuboid — see the enforcer in restrict.go
// — which blocks everyone equally, ops included: barriers are ordinary
// client-side collision with no concept of player permissions. To get
// inside as an op, use /tp to teleport past the wall rather than walking
// through it. The first time this command ever runs, it also spawns the
// enforcer entity that builds/clears walls — see ensureEnforcer. Op only,
// same as every other world-affecting admin command in this repo.
type Command struct{}

func (Command) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	Cfg.ensureEnforcer(tx)
	if Cfg.HasPendingClaim(p.XUID()) {
		p.Message("§eYour previous unfinished restrict zone selection was cancelled.")
	}
	Cfg.BeginClaim(p.XUID())
	p.Inventory().AddItem(item.NewStack(MarkerBlock(), 2))
	p.Message("§aGiven 2 restrict zone blocks. Place both to build a walled-off zone — break either one to remove it. Once built, get inside with /tp, not by walking in.")
}
