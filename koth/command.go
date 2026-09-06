package koth

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// Register all 5 of these under one command in main.go's registerCommands,
// same overload style already used for /pvp and /team in this repo:
//
//	cmd.Register(cmd.New("koth", "Manage King of the Hill zones.", nil,
//	    koth.Block{}, koth.Name{}, koth.Activate{}, koth.Time{}, koth.List{}))
//
// DEVIATION FROM THE PHP ORIGINAL: KothCommand.php's bare "/koth" (no
// args) gave the red-concrete placer and "/koth platform <size>"
// instant-built a platform — both replaced here by "/koth block" (2
// corner markers), per the zone-definition deviation explained in
// koth.go's package doc comment. "/koth name/activate/time/list" are
// otherwise the same 4 subcommands as the original, op-only like every
// other world-affecting admin command in this repo.

// Block is "/koth block" — gives the executor 2 KOTH zone corner
// markers (gold blocks). Place both to define a zone; breaking either
// one removes it (once named — see Name below). Op only.
type Block struct {
	Sub cmd.SubCommand `cmd:"block"`
}

func (Block) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (Block) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command must be run by a player.")
		return
	}
	marker, ok := MarkerBlock()
	if !ok {
		output.Print("Marker block (" + markerBlockName + ") isn't registered in this Dragonfly version — can't give zone blocks.")
		return
	}
	markerItem, ok := marker.(world.Item)
	if !ok {
		output.Print("Marker block (" + markerBlockName + ") doesn't implement world.Item in this Dragonfly version — can't give zone blocks.")
		return
	}
	if Cfg.HasPendingClaim(p.XUID()) {
		p.Message("§eYour previous unfinished KOTH zone selection was cancelled.")
	}
	Cfg.BeginClaim(p.XUID())
	p.Inventory().AddItem(item.NewStack(markerItem, 2))
	p.Message("§aGiven 2 KOTH zone blocks. Place both to mark out a zone — name it afterwards with §e/koth name <id>§a. Break either block to remove a named zone.")
}

// Name is "/koth name <id>" — names the most recently completed (but
// not yet named) zone. Op only.
type Name struct {
	Sub cmd.SubCommand `cmd:"name"`
	ID  string         `cmd:"id"`
}

func (Name) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (n Name) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if Cfg.HasZone(n.ID) {
		output.Printf("A KOTH zone is already named %q.", n.ID)
		return
	}
	if Cfg.NameLatestPending(n.ID) {
		output.Printf("Saved KOTH zone %q. Start it with /koth activate %s", n.ID, n.ID)
	} else {
		output.Print("There's no freshly-built zone waiting to be named. Use /koth block first, and place both corners.")
	}
}

// Activate is "/koth activate <id> [duration]" — starts a capture on a
// named zone. duration accepts the same formats as ParseDuration (e.g.
// "10min", "600s", "1h"); if omitted, DurationSeconds (12 minutes) is
// used. Op only.
type Activate struct {
	Sub      cmd.SubCommand       `cmd:"activate"`
	ID       string               `cmd:"id"`
	Duration cmd.Optional[string] `cmd:"duration"`
}

func (Activate) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (a Activate) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if !Cfg.HasZone(a.ID) {
		output.Printf("No KOTH zone named %q.", a.ID)
		return
	}

	duration := 0
	if raw, ok := a.Duration.Load(); ok {
		parsed, ok := ParseDuration(raw)
		if !ok {
			output.Printf("Couldn't parse duration %q. Try something like 10min, 600s, or 1h.", raw)
			return
		}
		duration = parsed
	}

	announcement, ok := Cfg.Activate(a.ID, duration)
	if !ok {
		output.Printf("%q is already active.", a.ID)
		return
	}
	// Make sure this zone actually has a ticker anchored at it — see
	// ticker.go's SpawnZoneTicker doc comment: this is a no-op if one
	// already exists (the normal case, spawned back when the zone's
	// second corner was placed), and is what gives a zone defined before
	// this fix shipped its first-ever correctly-anchored ticker.
	if zone, zok := Cfg.ZoneByName(a.ID); zok {
		SpawnZoneTicker(tx, zone.SpawnPosition(), zone.Corner1)
	}
	for p := range state.Server.Players(tx) {
		p.Message(announcement)
	}
	output.Printf("Activated %q.", a.ID)
}

// Time is "/koth time <id> <duration>" — sets the remaining time (from
// now) on an already-active zone. Op only.
type Time struct {
	Sub      cmd.SubCommand `cmd:"time"`
	ID       string         `cmd:"id"`
	Duration string         `cmd:"duration"`
}

func (Time) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (t Time) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	duration, ok := ParseDuration(t.Duration)
	if !ok {
		output.Printf("Couldn't parse duration %q. Try something like 10min, 600s, or 1h.", t.Duration)
		return
	}
	if Cfg.SetRemainingTime(t.ID, duration) {
		output.Printf("%q now has %s left.", t.ID, t.Duration)
	} else {
		output.Printf("%q isn't currently active.", t.ID)
	}
}

// List is "/koth list" — lists every named zone, marking which are
// currently active. Op only (same restriction as the rest of this
// command; the original left /koth's default permission open to anyone
// with hoplite.koth, but everything else in this file is op-gated, and
// list is read-only admin info, so it's kept consistent rather than
// carved out as the one public subcommand).
type List struct {
	Sub cmd.SubCommand `cmd:"list"`
}

func (List) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (List) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	all := Cfg.ZoneNames()
	if len(all) == 0 {
		output.Print("No KOTH zones have been named yet.")
		return
	}
	active := map[string]bool{}
	for _, n := range Cfg.ActiveZoneNames() {
		active[n] = true
	}
	msg := "KOTH zones:"
	for _, n := range all {
		if active[n] {
			msg += "\n§a" + n + " (active)"
		} else {
			msg += "\n§7" + n
		}
	}
	output.Print(msg)
}

