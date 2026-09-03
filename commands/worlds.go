package commands

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
	dfworlds "velaris-dragonfly/worlds"
)

package commands

import (
	"strings"

	dfblock "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"

	"velaris-dragonfly/state"
	"velaris-dragonfly/worldgen"
	dfworlds "velaris-dragonfly/worlds"
)

// WorldTypeEnum implements cmd.Enum so /worldcreate can offer a dropdown of
// valid terrain types in the client's command UI instead of a free-typed
// string.
type WorldTypeEnum string

func (WorldTypeEnum) Type() string { return "WorldType" }
func (WorldTypeEnum) Options(cmd.Source) []string {
	return []string{"flat", "normal"}
}

// worldGenerator resolves a WorldTypeEnum value (or "" for the default) into
// the dfworlds.Definition.Generator to use. "flat" is a bare grass
// platform; "normal" is worldgen.Overworld — rolling hills, basic caves,
// lake water — seeded off the world's own name, so each named world gets
// its own terrain but regenerates identically across restarts. Either way,
// a Nether-dimension destination still gets the real cave generator from
// nether.go instead of flat.
func worldGenerator(name string, typ WorldTypeEnum) func(dim world.Dimension) world.Generator {
	seed := nameSeed(name)
	return func(dim world.Dimension) world.Generator {
		if dim == world.Nether {
			return worldgen.NewNether(seed)
		}
		if typ == "normal" {
			return worldgen.NewOverworld(seed)
		}
		return worldgen.NewFlat(dim, biome.Plains{}, []world.Block{dfblock.Grass{}, dfblock.Dirt{}, dfblock.Dirt{}})
	}
}

// WorldCreate is /worldcreate <name> — creates a flat DFWorlds destination
// world (the default terrain type) and registers it under name. Op only.
type WorldCreate struct {
	Name string `cmd:"name"`
}

func (WorldCreate) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (w WorldCreate) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	createWorld(output, w.Name, "flat")
}

// WorldCreateTyped is the /worldcreate <name> <type> overload — same as
// WorldCreate but lets you pick "flat" or "normal" (rolling-hills) terrain.
// Op only.
type WorldCreateTyped struct {
	Name string        `cmd:"name"`
	Type WorldTypeEnum `cmd:"type"`
}

func (WorldCreateTyped) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (w WorldCreateTyped) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	createWorld(output, w.Name, w.Type)
}

func createWorld(output *cmd.Output, name string, typ WorldTypeEnum) {
	if state.Worlds == nil {
		output.Print("World manager is not initialised.")
		return
	}
	if _, err := state.Worlds.Open(dfworlds.Definition{
		Name:      name,
		Generator: worldGenerator(name, typ),
	}); err != nil {
		output.Printf("Failed to create world %q: %s", name, err.Error())
		return
	}
	label := string(typ)
	if label == "" {
		label = "flat"
	}
	output.Printf("World %q (%s) created/loaded. Use /worldtp %s to go there.", name, label, name)
}

// nameSeed derives a deterministic int64 seed from a world name, so each
// named world gets its own distinct terrain but regenerates identically
// across restarts.
func nameSeed(name string) int64 {
	var h uint64 = 1469598103934665603
	for _, b := range []byte(name) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return int64(h)
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
