package dfworlds

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	dfworld "github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Spawn describes where a player should land inside a managed world.
type Spawn struct {
	Position mgl64.Vec3
	Rotation cube.Rotation
}

// SpawnAt returns a spawn pointer for use in a Definition.
func SpawnAt(pos mgl64.Vec3, rot cube.Rotation) *Spawn {
	spawn := Spawn{Position: pos, Rotation: rot}
	return &spawn
}

// SpawnFromWorld returns a Dragonfly world's saved spawn with a neutral
// rotation.
func SpawnFromWorld(w *dfworld.World) Spawn {
	if w == nil {
		return Spawn{}
	}
	return Spawn{Position: w.Spawn().Vec3Centre()}
}

// Definition declares a named destination world. It keeps map loading,
// metadata, and travel behaviour in one place so callers don't pass loose
// strings and coordinates around the codebase.
type Definition struct {
	// Name is the public destination identifier, such as "overworld" or "arena".
	Name string
	// Folder is the child directory inside Config.Root. It defaults to Name.
	Folder string
	// Dimension controls Dragonfly dimension rules for the world. It defaults
	// to world.Overworld.
	Dimension dfworld.Dimension
	// Spawn is the default landing point for players travelling to this world.
	// If nil, the world's saved spawn is used.
	Spawn *Spawn
	// GameMode is applied to players travelling to this destination and also
	// set as the world's default game mode when opened. If nil, the world's
	// saved default is preserved.
	GameMode dfworld.GameMode
	// Handler overrides Config.Handler for this world.
	Handler dfworld.Handler
	// Generator overrides Config.Generator for this world.
	Generator func(dim dfworld.Dimension) dfworld.Generator
	// Configure may make final changes to the Dragonfly world.Config after the
	// manager-level Configure hook has run.
	Configure func(conf dfworld.Config) dfworld.Config
}

// LoadedWorld is a snapshot of a managed destination.
type LoadedWorld struct {
	Name     string
	Folder   string
	Path     string
	World    *dfworld.World
	Spawn    Spawn
	GameMode dfworld.GameMode
	Owned    bool
}
