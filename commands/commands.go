package commands

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

// ---------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------

// Ping is a minimal example command: /ping just replies "Pong!".
type Ping struct{}

func (Ping) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if p, ok := src.(*player.Player); ok {
		p.Message("§aPong!")
		return
	}
	output.Print("Pong!")
}

// GameModeEnum implements cmd.Enum so /gamemode can offer a dropdown of
// valid modes in the client's command UI instead of a free-typed string.
type GameModeEnum string

func (GameModeEnum) Type() string { return "GameMode" }
func (GameModeEnum) Options(cmd.Source) []string {
	return []string{"survival", "creative", "adventure", "spectator", "s", "c", "a", "sp"}
}

// resolveGameMode turns a full name or short letter into a world.GameMode.
// Returns false if the value isn't recognised.
func resolveGameMode(value string) (world.GameMode, bool) {
	switch value {
	case "survival", "s":
		return world.GameModeSurvival, true
	case "creative", "c":
		return world.GameModeCreative, true
	case "adventure", "a":
		return world.GameModeAdventure, true
	case "spectator", "sp":
		return world.GameModeSpectator, true
	}
	return nil, false
}

// GameMode is /gamemode <mode> — changes the executor's own game mode.
// Accepts either the full name (survival, creative, adventure, spectator)
// or the short form (s, c, a, sp). Op only.
// NOTE: this only affects the player running the command for now. Targeting
// other players (/gamemode creative Steve) needs cmd.Target support, which
// isn't wired up yet — ask if you want that added.
type GameMode struct {
	Mode GameModeEnum `cmd:"mode"`
}

func (GameMode) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (g GameMode) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	mode, ok := resolveGameMode(string(g.Mode))
	if !ok {
		output.Printf("Unknown game mode: %s", g.Mode)
		return
	}
	p.SetGameMode(mode)
	p.Messagef("§aGame mode set to %s.", g.Mode)
}

// gmShortcut is shared logic for the /gms, /gmc, /gma, /gmsp quick commands
// below — each just sets a fixed game mode with no argument needed.
func gmShortcut(src cmd.Source, output *cmd.Output, mode world.GameMode, name string) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	p.SetGameMode(mode)
	p.Messagef("§aGame mode set to %s.", name)
}

// Gms is /gms — shortcut for /gamemode survival. Op only.
type Gms struct{}

func (Gms) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Gms) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeSurvival, "survival")
}

// Gmc is /gmc — shortcut for /gamemode creative. Op only.
type Gmc struct{}

func (Gmc) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Gmc) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeCreative, "creative")
}

// Gma is /gma — shortcut for /gamemode adventure. Op only.
type Gma struct{}

func (Gma) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Gma) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeAdventure, "adventure")
}

// Gmsp is /gmsp — shortcut for /gamemode spectator. Op only.
type Gmsp struct{}

func (Gmsp) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Gmsp) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	gmShortcut(src, output, world.GameModeSpectator, "spectator")
}

// Tp is /tp <x> <y> <z> — teleports the executor to the given coordinates.
type Tp struct {
	Destination mgl64.Vec3 `cmd:"destination"`
}

func (Tp) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (t Tp) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	p.Teleport(t.Destination)
	p.Messagef("§aTeleported to %.1f, %.1f, %.1f.", t.Destination[0], t.Destination[1], t.Destination[2])
}

// Feed is /feed — refills the executor's hunger and saturation to max.
type Feed struct{}

func (Feed) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Feed) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	p.SetFood(20)
	p.Message("§aHunger restored.")
}

// Coords is /coords — toggles the on-screen coordinate display.
type Coords struct{}

func (Coords) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	state.CoordsMu.Lock()
	shown := state.CoordsState[p.XUID()]
	shown = !shown
	state.CoordsState[p.XUID()] = shown
	state.CoordsMu.Unlock()

	if shown {
		p.ShowCoordinates()
		p.Message("§aCoordinates shown.")
	} else {
		p.HideCoordinates()
		p.Message("§aCoordinates hidden.")
	}
}

// SetWorldSpawn is /setworldspawn — sets the world spawn to the executing
// player's current position. Op only.
type SetWorldSpawn struct{}

func (SetWorldSpawn) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (SetWorldSpawn) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	// NOTE: cube.PosFromVec3 and World.SetSpawn are my best read of the
	// current API — if this line doesn't compile, send the exact error and
	// we'll fix the method/function name.
	pos := cube.PosFromVec3(p.Position())
	tx.World().SetSpawn(pos)
	output.Printf("Set the world spawn to %v.", pos)
}

// TimeEnum implements cmd.Enum for the /time set subcommand.
type TimeEnum string

func (TimeEnum) Type() string { return "TimeValue" }
func (TimeEnum) Options(cmd.Source) []string {
	return []string{"day", "noon", "night", "midnight"}
}

// TimeSet is /time set <value> — sets the world time of day. Op only.
type TimeSet struct {
	Set   cmd.SubCommand `cmd:"set"`
	Value TimeEnum       `cmd:"value"`
}

func (TimeSet) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (t TimeSet) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	var ticks int
	switch t.Value {
	case "day":
		ticks = 1000
	case "noon":
		ticks = 6000
	case "night":
		ticks = 13000
	case "midnight":
		ticks = 18000
	}
	tx.World().SetTime(ticks)
	output.Printf("Set the time to %s.", t.Value)
}

// TimeQuery is /time query — reports the current world time. Anyone can run
// this (not op-restricted).
type TimeQuery struct {
	Query cmd.SubCommand `cmd:"query"`
}

func (TimeQuery) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	output.Printf("The time is %d.", tx.World().Time())
}

// WeatherEnum implements cmd.Enum for /weather.
type WeatherEnum string

func (WeatherEnum) Type() string { return "Weather" }
func (WeatherEnum) Options(cmd.Source) []string {
	return []string{"clear", "rain", "thunder"}
}

// Weather is /weather <type> — changes the world weather. Op only.
type Weather struct {
	Type WeatherEnum `cmd:"type"`
}

func (Weather) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (w Weather) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	wo := tx.World()
	switch w.Type {
	case "clear":
		wo.StopRaining()
		wo.StopThundering()
	case "rain":
		wo.StartRaining(24 * time.Hour)
	case "thunder":
		wo.StartThundering(24 * time.Hour)
	}
	output.Printf("Set the weather to %s.", w.Type)
}

// Op is /op <player> — grants op to an online player by name. Op only.
type Op struct {
	Target string `cmd:"player"`
}

func (Op) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (o Op) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	target, ok := state.FindOnline(o.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", o.Target)
		return
	}
	if err := state.Ops.SetOp(target.XUID(), true); err != nil {
		output.Printf("Failed to save op status: %v", err)
		return
	}
	target.Message("§aYou are now a server operator.")
	output.Printf("Made %s a server operator.", target.Name())
}

// Deop is /deop <player> — removes op from an online player by name.
// Op only.
type Deop struct {
	Target string `cmd:"player"`
}

func (Deop) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (d Deop) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	target, ok := state.FindOnline(d.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", d.Target)
		return
	}
	if err := state.Ops.SetOp(target.XUID(), false); err != nil {
		output.Printf("Failed to save op status: %v", err)
		return
	}
	target.Message("§cYou are no longer a server operator.")
	output.Printf("Removed %s as a server operator.", target.Name())
}

// Kick is /kick <player> [reason] — disconnects an online player. Op only.
type Kick struct {
	Target string               `cmd:"player"`
	Reason cmd.Optional[string] `cmd:"reason"`
}

func (Kick) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (k Kick) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	// findOnlineTx, not findOnline: Disconnect must be called on a player
	// reference that's valid within this tx, or it can panic on dragonfly's
	// internal tick goroutine instead of here. See findOnlineTx's comment.
	target, ok := state.FindOnlineTx(tx, k.Target)
	if !ok {
		output.Printf("Player '%s' is not online.", k.Target)
		return
	}
	reason := "Kicked by an operator."
	if r, ok := k.Reason.Load(); ok {
		reason = r
	}
	target.Disconnect(reason)
	output.Printf("Kicked %s: %s", target.Name(), reason)
}

// Ban is /ban <player> [reason] — bans an online player by XUID and
// disconnects them. The ban persists across sessions and is checked when
// anyone joins (see main()). Op only.
type Ban struct {
	Target string               `cmd:"player"`
	Reason cmd.Optional[string] `cmd:"reason"`
}

func (Ban) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (b Ban) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	// findOnlineTx, not findOnline — see Kick.Run's comment above.
	target, ok := state.FindOnlineTx(tx, b.Target)
	if !ok {
		output.Printf("Player '%s' is not online (only online players can be banned right now).", b.Target)
		return
	}
	reason := "Banned by an operator."
	if r, ok := b.Reason.Load(); ok {
		reason = r
	}
	if err := state.Bans.Ban(target.XUID(), reason); err != nil {
		output.Printf("Failed to save ban: %v", err)
		return
	}
	target.Disconnect(reason)
	output.Printf("Banned %s: %s", target.Name(), reason)
}

