package commands

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
	dfworlds "velaris-dragonfly/worlds"
)

// WorldCreate is /worldcreate <name> — creates a brand new DFWorlds
// destination world (or loads it, if a folder with that name already
// exists on disk under worlds/) and registers it under name. Op only.
type WorldCreate struct {
	Name string `cmd:"name"`
}

func (WorldCreate) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (w WorldCreate) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if state.Worlds == nil {
		output.Print("World manager is not initialised.")
		return
	}
	if _, err := state.Worlds.Open(dfworlds.Definition{Name: w.Name}); err != nil {
		output.Printf("Failed to create world %q: %s", w.Name, err.Error())
		return
	}
	output.Printf("World %q created/loaded. Use /worldtp %s to go there.", w.Name, w.Name)
}

// WorldTP is /worldtp <name> — travels the executing player to a loaded
// DFWorlds destination.
type WorldTP struct {
	Name string `cmd:"name"`
}

func (w WorldTP) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	if state.WorldRouter == nil {
		output.Print("World router is not initialised.")
		return
	}
	if err := state.WorldRouter.SendPlayerTx(tx, p, w.Name); err != nil {
		output.Printf("Failed to travel to %q: %s", w.Name, err.Error())
	}
}

// WorldList is /worldlist — lists every currently loaded DFWorlds
// destination.
type WorldList struct{}

func (WorldList) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if state.Worlds == nil {
		output.Print("World manager is not initialised.")
		return
	}
	names := state.Worlds.Names()
	if len(names) == 0 {
		output.Print("No worlds loaded.")
		return
	}
	output.Printf("Loaded worlds: %s", strings.Join(names, ", "))
}

// WorldDelete is /worlddelete <name> — permanently closes a loaded DFWorlds
// destination and deletes its save data from disk. Op only. Refuses to
// delete "overworld" (registered, not owned by the manager — see
// worlds.Manager.Delete) and doesn't move any players out first, so send
// everyone elsewhere before running it.
type WorldDelete struct {
	Name string `cmd:"name"`
}

func (WorldDelete) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (w WorldDelete) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if state.Worlds == nil {
		output.Print("World manager is not initialised.")
		return
	}
	if err := state.Worlds.Delete(w.Name); err != nil {
		output.Printf("Failed to delete world %q: %s", w.Name, err.Error())
		return
	}
	output.Printf("World %q deleted.", w.Name)
}
