package pvp

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// On is /pvp on — opts the executor in to PvP outside of zones (both
// players in a fight need this for it to be allowed there; see
// Config.CombatAllowed). Anyone can run this.
type On struct {
	On cmd.SubCommand `cmd:"on"`
}

func (On) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	Cfg.SetToggle(p.XUID(), true)
	p.Message("§aPvP enabled. Other opted-in players can now fight you outside PvP zones.")
}

// Off is /pvp off — opts the executor back out. Anyone can run this.
type Off struct {
	Off cmd.SubCommand `cmd:"off"`
}

func (Off) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	Cfg.SetToggle(p.XUID(), false)
	p.Message("§cPvP disabled. You're safe outside PvP zones.")
}

// Block is /pvp block — gives the executor exactly 2 PvP marker blocks
// (orange concrete). Place both to define a PvP zone cuboid between
// wherever they land; breaking either one removes the zone. Op only, same
// as every other world-affecting admin command in this repo.
type Block struct {
	Block cmd.SubCommand `cmd:"block"`
}

func (Block) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Block) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	if Cfg.HasPendingClaim(p.XUID()) {
		p.Message("§eYour previous unfinished PvP zone selection was cancelled.")
	}
	Cfg.BeginClaim(p.XUID())
	p.Inventory().AddItem(item.NewStack(MarkerBlock(), 2))
	p.Message("§aGiven 2 PvP zone blocks. Place both to create a zone — break either one to remove it.")
}
