package multiworld

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/state"
)

// All /mw subcommands are op-only, matching MultiWorld's own permission
// model (multiworld.command in the PHP plugin.yml was op-restricted by
// default).

// WorldTypeEnum implements cmd.Enum so /mw create offers a dropdown of
// valid generator types in the client's command UI.
type WorldTypeEnum string

func (WorldTypeEnum) Type() string { return "MWWorldType" }
func (WorldTypeEnum) Options(cmd.Source) []string {
	return []string{"overworld", "nether", "void", "skyblock", "end"}
}

// MWCreate is "/mw create <name> <type> [seed]".
type MWCreate struct {
	Create cmd.SubCommand    `cmd:"create"`
	Name   string            `cmd:"name"`
	Kind   WorldTypeEnum     `cmd:"type"`
	Seed   cmd.Optional[int] `cmd:"seed"`
}

func (MWCreate) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWCreate) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	var seed int64
	if s, ok := m.Seed.Load(); ok {
		seed = int64(s)
	}
	if err := Create(m.Name, WorldType(m.Kind), seed); err != nil {
		output.Printf("Failed to create world: %v", err)
		return
	}
	output.Printf("Created and loaded world %q (%s).", m.Name, m.Kind)
}

// MWDelete is "/mw delete <name>".
type MWDelete struct {
	Delete cmd.SubCommand `cmd:"delete"`
	Name   string         `cmd:"name"`
}

func (MWDelete) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWDelete) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if err := Delete(m.Name); err != nil {
		output.Printf("Failed to delete world: %v", err)
		return
	}
	output.Printf("Deleted world %q.", m.Name)
}

// MWList is "/mw list".
type MWList struct {
	List cmd.SubCommand `cmd:"list"`
}

func (MWList) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (MWList) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	entries := List()
	if len(entries) == 0 {
		output.Print("No worlds have been created yet. Use /mw create.")
		return
	}
	for _, e := range entries {
		_, loaded := Get(e.Name)
		status := "unloaded"
		if loaded {
			status = "loaded"
		}
		output.Printf("%s (%s, %s)", e.Name, e.Type, status)
	}
}

// MWInfo is "/mw info <name>".
type MWInfo struct {
	Info cmd.SubCommand `cmd:"info"`
	Name string         `cmd:"name"`
}

func (MWInfo) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWInfo) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	e, loaded, ok := Info(m.Name)
	if !ok {
		output.Printf("No world named %q.", m.Name)
		return
	}
	status := "unloaded"
	if loaded {
		status = "loaded"
	}
	output.Printf("%s | type: %s | seed: %d | status: %s | folder: worlds/%s", e.Name, e.Type, e.Seed, status, e.Folder)
}

// MWRename is "/mw rename <name> <newname>".
type MWRename struct {
	Rename  cmd.SubCommand `cmd:"rename"`
	Name    string         `cmd:"name"`
	NewName string         `cmd:"newname"`
}

func (MWRename) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWRename) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if err := Rename(m.Name, m.NewName); err != nil {
		output.Printf("Failed to rename world: %v", err)
		return
	}
	output.Printf("Renamed %q to %q.", m.Name, m.NewName)
}

// MWLoad is "/mw load <name>" — re-opens a world that was unloaded.
type MWLoad struct {
	Load cmd.SubCommand `cmd:"load"`
	Name string         `cmd:"name"`
}

func (MWLoad) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWLoad) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if err := Load(m.Name); err != nil {
		output.Printf("Failed to load world: %v", err)
		return
	}
	output.Printf("Loaded world %q.", m.Name)
}

// MWUnload is "/mw unload <name>". Teleport everyone out first — this does
// not check for or move remaining players, and closing a world's storage
// out from under someone standing in it is not something to find out the
// hard way.
type MWUnload struct {
	Unload cmd.SubCommand `cmd:"unload"`
	Name   string         `cmd:"name"`
}

func (MWUnload) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWUnload) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	if err := Unload(m.Name); err != nil {
		output.Printf("Failed to unload world: %v", err)
		return
	}
	output.Printf("Unloaded world %q.", m.Name)
}

// MWTeleport is "/mw tp <world>" — moves the executor into that world at
// its spawn point. The world must already be loaded (/mw load it first).
type MWTeleport struct {
	Teleport cmd.SubCommand `cmd:"tp"`
	World    string         `cmd:"world"`
}

func (MWTeleport) Allow(src cmd.Source) bool { return state.IsOpSource(src) }
func (m MWTeleport) Run(src cmd.Source, output *cmd.Output, tx *world.Tx) {
	p, ok := src.(*player.Player)
	if !ok {
		output.Print("This command can only be run by a player.")
		return
	}
	w, ok := Get(m.World)
	if !ok {
		output.Printf("World %q isn't loaded. Run /mw load %s first.", m.World, m.World)
		return
	}
	// Confirmed against the real API docs: World.Spawn() returns cube.Pos,
	// and cube.Pos.Vec3() is a long-standing stable method (unaffected by
	// the recent Exec/Do churn) that converts it to mgl64.Vec3.
	pos := w.Spawn().Vec3()
	TeleportTo(tx, p, w, pos)
	output.Printf("Teleporting you to %q.", m.World)
}
