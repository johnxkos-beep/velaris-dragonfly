package schematic

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

// Save is /save <name> <corner1> <corner2> — op only. Saves every block
// in the inclusive box between the two given coordinates to
// Dir/<name>.rfschem. Corner1/Corner2 use dragonfly's cmd.Vec3-style
// parsing (three numbers each, e.g. "100 64 -20"; supports "~" relative
// coordinates the same way /tp's Destination field does).
type Save struct {
	Name    string     `cmd:"name"`
	Corner1 mgl64.Vec3 `cmd:"corner1"`
	Corner2 mgl64.Vec3 `cmd:"corner2"`
}

func (Save) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (s Save) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	size, err := Export(tx, s.Name, cube.PosFromVec3(s.Corner1), cube.PosFromVec3(s.Corner2))
	if err != nil {
		p.Messagef("§c%s", err)
		return
	}
	p.Messagef("§aSaved %dx%dx%d (%d blocks) to §e%s§a.", size.X, size.Y, size.Z, size.Volume(), s.Name)
}

// Load is /load <name> [origin] — op only. Pastes Dir/<name>.rfschem
// into the world with its min corner at origin (defaults to the
// executor's current position if omitted).
type Load struct {
	Name   string                   `cmd:"name"`
	Origin cmd.Optional[mgl64.Vec3] `cmd:"origin"`
}

func (Load) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (l Load) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}

	if !Exists(l.Name) {
		known := List()
		if len(known) == 0 {
			p.Messagef("§cNo schematic named %q, and none are uploaded yet. Drop .rfschem files into the %s/ folder (SFTP or the Pterodactyl file manager).", l.Name, Dir)
		} else {
			p.Messagef("§cNo schematic named %q. Known: %s", l.Name, strings.Join(known, ", "))
		}
		return
	}

	origin := p.Position()
	if v, has := l.Origin.Load(); has {
		origin = v
	}

	size, err := Import(tx, l.Name, cube.PosFromVec3(origin))
	if err != nil {
		p.Messagef("§c%s", err)
		return
	}
	p.Messagef("§aLoaded %dx%dx%d (%d blocks) from §e%s§a.", size.X, size.Y, size.Z, size.Volume(), l.Name)
}
