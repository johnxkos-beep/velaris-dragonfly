package pvp

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// On is /pvp on — turns PvP on for the whole server (anyone can fight
// anyone, anywhere). Op only.
type On struct {
	On cmd.SubCommand `cmd:"on"`
}

func (On) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (On) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	Cfg.SetEnabled(true)
	for p := range state.Server.Players(tx) {
		p.Message("§aPvP has been enabled server-wide.")
	}
	output.Print("PvP enabled server-wide.")
}

// Off is /pvp off — turns PvP off server-wide. Players can still fight
// each other inside PvP zones regardless (see Config.CombatAllowed). Op
// only.
type Off struct {
	Off cmd.SubCommand `cmd:"off"`
}

func (Off) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Off) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	Cfg.SetEnabled(false)
	for p := range state.Server.Players(tx) {
		p.Message("§cPvP has been disabled server-wide. PvP zones are still active.")
	}
	output.Print("PvP disabled server-wide.")
}

// Block is /pvp block — gives the executor exactly 2 PvP marker blocks
// (obsidian). Place both to define a PvP zone cuboid between
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
