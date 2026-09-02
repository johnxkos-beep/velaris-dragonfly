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

// notFoundMessage reports on an unknown/missing schematic name, listing
// what's actually available — shared between bare "/load" and
// "/load <unknown name>".
func notFoundMessage(p *player.Player, name string) {
	known := List()
	if len(known) == 0 {
		p.Messagef("§cNo schematics uploaded yet. Drop .rfschem files into the %s/ folder (SFTP or the Pterodactyl file manager).", Dir)
		return
	}
	if name == "" {
		p.Messagef("§aAvailable schematics: §e%s", strings.Join(known, ", "))
		return
	}
	p.Messagef("§cNo schematic named %q. Available: §e%s", name, strings.Join(known, ", "))
}

// Load is /load [name] — op only. With no name, lists every schematic
// available in Dir. With a name, reads Dir/<name>.rfschem into the
// executor's clipboard (does NOT touch the world) — run /paste
// afterward to place it.
type Load struct {
	Name cmd.Optional[string] `cmd:"name"`
}

func (Load) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (l Load) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}

	name, has := l.Name.Load()
	if !has || name == "" {
		notFoundMessage(p, "")
		return
	}
	if !Exists(name) {
		notFoundMessage(p, name)
		return
	}

	clip, err := ReadFile(name)
	if err != nil {
		p.Messagef("§c%s", err)
		return
	}
	SetClipboard(p.XUID(), clip)
	p.Messagef("§aLoaded §e%s§a (%dx%dx%d, %d blocks) into your clipboard. Run §e/paste§a to place it.", name, clip.Size.X, clip.Size.Y, clip.Size.Z, clip.Size.Volume())
}

// Paste is /paste — op only. Places the executor's currently loaded
// clipboard (see Load) into the world at their current position. Doesn't
// clear the clipboard afterward, so it can be pasted more than once.
type Paste struct{}

func (Paste) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (Paste) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}

	clip, ok := GetClipboard(p.XUID())
	if !ok {
		p.Message("§cNothing loaded. Run §e/load <name>§c first.")
		return
	}

	origin := cube.PosFromVec3(p.Position())
	clip.PasteAt(tx, origin)
	p.Messagef("§aPasted §e%s§a (%dx%dx%d, %d blocks) at your position.", clip.Name, clip.Size.X, clip.Size.Y, clip.Size.Z, clip.Size.Volume())
}
