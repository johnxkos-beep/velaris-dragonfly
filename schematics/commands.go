package schematics

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Registered with the literal name "/load" and "/paste" (see main.go) so
// that typing //load / //paste in chat invokes them — the first "/" is the
// normal Bedrock command prefix, the second is part of the command's actual
// registered name, matching the WorldEdit/EasyEdit "//" convention.

// Load is //load [name].
//   - //load          -> lists every .schem file in the schematics folder.
//   - //load "myhouse" -> parses myhouse.schem into memory as your active
//     clipboard. Nothing is placed in the world yet.
type Load struct {
	Name cmd.Optional[string] `cmd:"name"`
}

// Op-only, same as the rest of this repo's building/admin commands — swap
// this out if you want regular players to use it too.
func (Load) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (l Load) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p := src.(*player.Player)

	name, hasName := l.Name.Load()
	if !hasName {
		names, err := List()
		if err != nil {
			output.Printf("Couldn't read the schematics folder: %v", err)
			return
		}
		if len(names) == 0 {
			output.Print("No schematics found. Upload a .schem file to the schematics/ folder over SFTP first.")
			return
		}
		output.Printf("Available schematics: %s", strings.Join(names, ", "))
		return
	}

	if err := LoadForPlayer(p.XUID(), name); err != nil {
		output.Printf("Failed to load %q: %v", name, err)
		return
	}
	p.Messagef("§aLoaded schematic %q. Run //paste to place it.", name)
}

// Paste is //paste — places your currently loaded schematic with its
// (0,0,0) corner at your feet.
type Paste struct{}

func (Paste) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (Paste) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p := src.(*player.Player)

	schem, name, ok := ActiveFor(p.XUID())
	if !ok {
		output.Print("You haven't loaded a schematic yet. Run //load \"name\" first.")
		return
	}

	origin := cube.PosFromVec3(p.Position())
	res := PasteInto(tx, schem, origin)

	if res.Skipped == 0 {
		p.Messagef("§aPasted %q: %d blocks placed.", name, res.Placed)
		return
	}
	p.Messagef("§ePasted %q: %d blocks placed, %d skipped (unmapped: %s). Send me that list to extend the block translator.",
		name, res.Placed, res.Skipped, strings.Join(res.SkippedNames, ", "))
}
