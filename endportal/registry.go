package endportal

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// builtPortal is one /endportal placement — everything DeleteAllIn needs to
// undo it again.
type builtPortal struct {
	FootprintMin, FootprintMax cube.Pos
	Handle                     *world.EntityHandle
}

var (
	registryMu sync.Mutex
	// Keyed by *world.World rather than a world name — this only ever runs
	// against a live tx.World() pointer (both when recording and when
	// deleting), so there's no need to resolve names to worlds, and it
	// naturally scopes correctly even for worlds DFWorlds doesn't manage by
	// name (e.g. the raw server Overworld, registered but not "Opened").
	registry = map[*world.World][]builtPortal{}
)

// record adds a newly-built portal to the registry, scoped to the world it
// was built in.
func record(w *world.World, footprintMin, footprintMax cube.Pos, handle *world.EntityHandle) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[w] = append(registry[w], builtPortal{FootprintMin: footprintMin, FootprintMax: footprintMax, Handle: handle})
}

// DeleteAllIn removes every portal recorded for tx's world: clears the
// frame/portal blocks back to air and despawns the sentinel entity. Returns
// how many portals were removed.
func DeleteAllIn(tx *world.Tx) int {
	w := tx.World()
	registryMu.Lock()
	list := registry[w]
	delete(registry, w)
	registryMu.Unlock()

	for _, p := range list {
		for x := p.FootprintMin.X(); x <= p.FootprintMax.X(); x++ {
			for y := p.FootprintMin.Y(); y <= p.FootprintMax.Y(); y++ {
				for z := p.FootprintMin.Z(); z <= p.FootprintMax.Z(); z++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Air{}, nil)
				}
			}
		}
		if ent, ok := p.Handle.Entity(tx); ok {
			tx.RemoveEntity(ent)
		}
	}
	return len(list)
}
