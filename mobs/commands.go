package mobs

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// SpawnCowCommand is /spawncow — spawns a cow at the executing player's
// position. Register it in main.go next to the other cmd.Register calls:
//
//	cmd.Register(cmd.New("spawncow", "Spawns a cow.", nil, mobs.SpawnCowCommand{}))
//
// Op only, same as every other admin command in this repo.
type SpawnCowCommand struct{}

func (SpawnCowCommand) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	SpawnCow(tx, p.Position())
	output.Print("Spawned a cow.")
}

func (SpawnCowCommand) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

// SpawnChickenCommand is /spawnchicken — spawns a chicken at the executing
// player's position. Register it the same way:
//
//	cmd.Register(cmd.New("spawnchicken", "Spawns a chicken.", nil, mobs.SpawnChickenCommand{}))
type SpawnChickenCommand struct{}

func (SpawnChickenCommand) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	SpawnChicken(tx, p.Position())
	output.Print("Spawned a chicken.")
}

func (SpawnChickenCommand) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

// SpawnPigCommand is /spawnpig — spawns a pig at the executing player's
// position. Register it the same way:
//
//	cmd.Register(cmd.New("spawnpig", "Spawns a pig.", nil, mobs.SpawnPigCommand{}))
type SpawnPigCommand struct{}

func (SpawnPigCommand) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	SpawnPig(tx, p.Position())
	output.Print("Spawned a pig.")
}

func (SpawnPigCommand) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

// SpawnSheepCommand is /spawnsheep — spawns a sheep at the executing
// player's position. Register it the same way:
//
//	cmd.Register(cmd.New("spawnsheep", "Spawns a sheep.", nil, mobs.SpawnSheepCommand{}))
type SpawnSheepCommand struct{}

func (SpawnSheepCommand) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	SpawnSheep(tx, p.Position())
	output.Print("Spawned a sheep.")
}

func (SpawnSheepCommand) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
