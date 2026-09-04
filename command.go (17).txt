package track

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Register all four together in main.go's registerCommands(), literal
// subcommands first so "point"/"off" get consumed as literals rather
// than falling through to Track's catch-all name string - same
// literal-subcommand-plus-trailing-string-fallback shape already used by
// teams.TeamChat/schematic.Load elsewhere in this project:
//
//	cmd.Register(cmd.New("track", "...", nil, track.Point{}, track.Off{}, track.Usage{}, track.Track{}))

// Point is "/track point <name>" - op only. Names a track point at the
// executor's exact current position, overwriting any existing point of
// that same name. Replaces the original TrackCommand's marker-block
// placement flow entirely (see the package doc comment in track.go for
// why) - there's no block to place or break here, just the position the
// op was standing at when they ran the command.
type Point struct {
	Point cmd.SubCommand `cmd:"point"`
	Name  string         `cmd:"name"`
}

func (Point) Allow(src cmd.Source) bool { return state.IsOpSource(src) }

func (c Point) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run in-game.")
		return
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		output.Print("Usage: /track point <name>")
		return
	}
	Cfg.SetPoint(name, p.Position())
	p.Messagef("§aTrack point %q set at your position.", name)
}

// Off is "/track off" - stops the executor's own live distance tracking,
// if any. Open to everyone, matching hoplite.track's "default: true" -
// port of TrackManager::stopTracking()'s call site in
// TrackCommand::execute.
type Off struct {
	Off cmd.SubCommand `cmd:"off"`
}

func (Off) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run in-game.")
		return
	}
	Cfg.StopTracking(p.XUID())
	p.Message("§eLive tracking turned off.")
}

// Track is "/track <name>" - starts a live action-bar HUD (see
// ticker.go) showing the executor's distance to the named point. Open to
// everyone - anyone can track any named point once an op has set it, e.g.
// one op sets "spawn" and everyone else can just /track spawn. Port of
// TrackCommand::execute's final branch (the "name an existing point"
// case - naming a *new* point moved to Point above).
type Track struct {
	Name string `cmd:"name"`
}

func (t Track) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run in-game.")
		return
	}
	name := strings.TrimSpace(t.Name)

	if !Cfg.Exists(name) {
		known := Cfg.ListNames()
		if len(known) == 0 {
			p.Messagef("§cNo tracked point named %q. No track points have been set yet - an op can run /track point <name> to set one.", name)
			return
		}
		p.Messagef("§cNo tracked point named %q. Known points: §e%s", name, strings.Join(known, ", "))
		return
	}

	Cfg.StartTracking(p.XUID(), name)
	p.Messagef("§aTracking %q - check the action bar. Run /track off to stop.", name)
}

// Usage is bare "/track" with no arguments - prints a short usage hint.
// Open to everyone, mirroring teams.TeamMenu's bare-command pattern for
// giving no-args invocations a friendly response instead of a generic
// parser error.
type Usage struct{}

func (Usage) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	output.Print("Usage: /track <name> to check distance to a point, /track off to stop, /track point <name> to set one (ops only).")
}
