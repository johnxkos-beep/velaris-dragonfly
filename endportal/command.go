package endportal

import (
	"errors"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
	"velaris-dragonfly/worldgen"
	dfworlds "velaris-dragonfly/worlds"
)

var errNoWorldManager = errors.New("world manager is not initialised")

// EndDestinationName matches commands/end.go's endWorldName — the same
// "the_end" DFWorlds destination /end already travels to. Duplicated here
// as a plain string rather than importing the commands package (which
// would import this one back for nothing) — if that name ever changes,
// update both.
const EndDestinationName = "the_end"

// islandCentreX/Z is where worldgen.NewEnd's island is centred — see
// worldgen/end.go, the generator builds outward from world (0,0), so this
// is genuinely the middle of the main island's surface, not a guess.
const islandCentreX, islandCentreZ = 0, 0

// Command is /endportal — op-only, same tier as /spawnenderdragon: builds
// a real, working End portal at the player's position. Walking into it
// sends you to the "the_end" destination (auto-created here if /end was
// never run first — same Definition commands.End builds, see
// ensureEndWorld below), landing on the main island's surface near its
// centre — not wherever that world's saved spawn happens to be.
type Command struct{}

func (Command) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (Command) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	if err := ensureEndWorld(); err != nil {
		output.Printf("Failed to prepare the End: %s", err.Error())
		return
	}
	base := cube.PosFromVec3(p.Position())
	min, max := BuildEntryPortal(tx, base)
	SpawnSentinel(tx, min, max, Destination{World: EndDestinationName, LandX: islandCentreX, LandZ: islandCentreZ})
	output.Print("An End portal opens beneath you.")
}

// ensureEndWorld opens the "the_end" DFWorlds destination if it doesn't
// exist yet — identical Definition to commands.End's, so the island is the
// same regardless of whether /end or /endportal happens to run first.
func ensureEndWorld() error {
	if state.Worlds == nil {
		return errNoWorldManager
	}
	if _, exists := state.Worlds.Destination(EndDestinationName); exists {
		return nil
	}
	seed := nameSeed(EndDestinationName)
	_, err := state.Worlds.Open(dfworlds.Definition{
		Name:      EndDestinationName,
		Dimension: world.End,
		Generator: func(dim world.Dimension) world.Generator { return worldgen.NewEnd(seed) },
	})
	return err
}

// nameSeed is a duplicate of commands/worlds.go's unexported function of
// the same name — needed here too so a world created by /endportal first
// gets the exact same seed (and therefore identical terrain) as one
// created by /end first. Keep both in sync if this ever changes.
func nameSeed(name string) int64 {
	var h uint64 = 1469598103934665603
	for _, b := range []byte(name) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return int64(h)
}

