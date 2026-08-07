package multiworld

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

// ---------------------------------------------------------------------
// world.Config{Dim, Generator, Provider, Log}.New() below is confirmed
// against the real, current Dragonfly API (checked directly against
// pkg.go.dev's docs for the actual tagged v0.11.1 release, which is what
// your go.mod should point to instead of the untagged pseudo-version it
// had). Cross-world player moves in TeleportTo (further down this file)
// use world.Call, which blocks until the re-add into the destination
// world's transaction actually completes — see the comment on TeleportTo
// itself for why that matters (World.Do alone caused an instant
// disconnect by leaving the player worldless for a moment).
// ---------------------------------------------------------------------

var (
	mu     sync.Mutex
	opened = map[string]*world.World{} // world name -> live *world.World
)

// Deps bundles the pieces manager.go needs from main.go but shouldn't
// import a cycle to get (main already imports worldgen and this package
// both). Call SetDeps once at startup.
type Deps struct {
	Log          *slog.Logger
	OverworldGen func(seed int64) world.Generator
	NetherGen    func(seed int64) world.Generator
}

var deps Deps

// SetDeps wires in your worldgen constructors so overworld/nether-type
// multiworld worlds reuse them instead of duplicating terrain generation.
// Call this in main() before any /mw command can run — right after
// InitRegistry.
func SetDeps(d Deps) { deps = d }

// Create makes a brand new world of the given type and opens it
// immediately. name must be unique among all worlds ever created.
func Create(name string, t WorldType, seed int64) error {
	if _, exists := getEntry(name); exists {
		return fmt.Errorf("a world named %q already exists", name)
	}
	folder := name
	dir := filepath.Join(Root, folder)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if seed == 0 {
		seed = rand.Int63()
	}
	e := &Entry{Name: name, Type: t, Seed: seed, Folder: folder}
	if err := addEntry(e); err != nil {
		return err
	}
	return open(e)
}

// dimensionFor returns the world.Dimension a given WorldType should be
// opened under. Only "nether" uses world.Nether — everything else
// (including "end", which is a stand-in end-stone build space, not the
// real singleton End dimension) runs as world.Overworld so it behaves like
// a normal buildable world.
func dimensionFor(t WorldType) world.Dimension {
	if t == TypeNether {
		return world.Nether
	}
	return world.Overworld
}

func open(e *Entry) error {
	dir := filepath.Join(Root, e.Folder)
	provider, err := mcdb.Config{Log: deps.Log}.Open(dir)
	if err != nil {
		return fmt.Errorf("open world storage for %q: %w", e.Name, err)
	}

	var overworldGen, netherGen world.Generator
	if deps.OverworldGen != nil {
		overworldGen = deps.OverworldGen(e.Seed)
	}
	if deps.NetherGen != nil {
		netherGen = deps.NetherGen(e.Seed)
	}
	gen := generatorFor(e.Type, e.Seed, overworldGen, netherGen)

	conf := world.Config{
		Log:       deps.Log,
		Dim:       dimensionFor(e.Type),
		Provider:  provider,
		Generator: gen,
	}
	w := conf.New()

	mu.Lock()
	opened[e.Name] = w
	mu.Unlock()
	return nil
}

// Unload closes a world's storage and removes it from memory. Players
// currently in it are NOT moved out first — teleport everyone out before
// unloading, or Unload will refuse if the world has anyone in it (checked
// via the caller's tx in commands.go, since checking players requires a
// transaction).
func Unload(name string) error {
	mu.Lock()
	w, ok := opened[name]
	if ok {
		delete(opened, name)
	}
	mu.Unlock()
	if !ok {
		return fmt.Errorf("world %q isn't currently loaded", name)
	}
	return w.Close()
}

// Load re-opens a previously created world that was unloaded (or is being
// loaded for the first time after a server restart).
func Load(name string) error {
	if _, already := Get(name); already {
		return fmt.Errorf("world %q is already loaded", name)
	}
	e, ok := getEntry(name)
	if !ok {
		return fmt.Errorf("no world named %q", name)
	}
	return open(e)
}

// Get returns the live *world.World for name, if it's currently loaded.
func Get(name string) (*world.World, bool) {
	mu.Lock()
	defer mu.Unlock()
	w, ok := opened[name]
	return w, ok
}

// Delete unloads (if loaded) and permanently deletes a world's save data
// and registry entry. There's no undo — this matches MultiWorld's own
// /mw delete, which is similarly irreversible.
func Delete(name string) error {
	e, ok := getEntry(name)
	if !ok {
		return fmt.Errorf("no world named %q", name)
	}
	if _, loaded := Get(name); loaded {
		if err := Unload(name); err != nil {
			return err
		}
	}
	if err := removeEntry(name); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(Root, e.Folder))
}

// Rename changes a world's registry name. The on-disk folder name is left
// as-is (it's tracked separately in Entry.Folder), so this works even while
// the world is loaded.
func Rename(oldName, newName string) error {
	if err := renameEntry(oldName, newName); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if w, ok := opened[oldName]; ok {
		delete(opened, oldName)
		opened[newName] = w
	}
	return nil
}

// List returns every registered world's entry, loaded or not.
func List() []*Entry { return allEntries() }

// Info returns a world's entry plus whether it's currently loaded.
func Info(name string) (*Entry, bool, bool) {
	e, ok := getEntry(name)
	if !ok {
		return nil, false, false
	}
	_, loaded := Get(name)
	return e, loaded, true
}

// TeleportTo moves p into world w, arriving at pos. Must be called from
// within a world.Tx belonging to p's CURRENT world (i.e. from inside a
// command's Run(src, output, tx) — tx is exactly that transaction).
//
// FIXED: your build's dragonfly (a commit right around the v0.11.1 tag) no
// longer has World.Exec at all — it was replaced by World.Do(f func(tx
// *world.Tx)) *world.Task. Cross-world moves now go: RemoveEntity on the
// current tx to get the EntityHandle, then w.Do(...) to open a transaction
// on the destination world and AddEntityAt to drop the handle in already at
// the right position.
func TeleportTo(tx *world.Tx, p *player.Player, w *world.World, pos mgl64.Vec3) {
	slog.Info("multiworld: starting cross-world teleport", "player", p.Name(), "pos", pos)
	handle := tx.RemoveEntity(p)
	// world.Call must run off the owner goroutine (see docs: calling it
	// from inside a scheduled callback/Handler deadlocks) — this function
	// runs inside a command's Run(), which is exactly that, so the actual
	// cross-world work happens in its own goroutine here.
	//
	// DIAGNOSTIC: previously discarded the error entirely (`_, _ =`), which
	// meant a failure here would look identical to a client-side kick —
	// nothing logged either way. Logging it now so the next attempt tells
	// us something concrete instead of nothing.
	go func() {
		_, err := world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
			tx.AddEntityAt(handle, pos)
			return struct{}{}, nil
		})
		if err != nil {
			slog.Error("multiworld: failed to add player to destination world", "player", p.Name(), "pos", pos, "error", err)
			return
		}
		slog.Info("multiworld: player added to destination world", "player", p.Name(), "pos", pos)
	}()
}
