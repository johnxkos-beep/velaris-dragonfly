package demonking

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// SpawnEggCommand is /spawndemonking — gives the executor a Demon King
// spawn egg. Register it in main.go next to the other cmd.Register calls:
//
//	cmd.Register(cmd.New("spawndemonking", "Gives you a Demon King spawn egg.", nil, demonking.SpawnEggCommand{}))
//
// Op only, same as every other admin command in this repo.
type SpawnEggCommand struct{}

func (SpawnEggCommand) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	p.Inventory().AddItem(item.NewStack(SpawnEgg{}, 1))
	output.Print("Gave you a Demon King spawn egg.")
}

func (SpawnEggCommand) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
