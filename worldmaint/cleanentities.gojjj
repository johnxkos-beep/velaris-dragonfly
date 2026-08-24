// Package worldmaint holds one-off world-save maintenance routines. These
// operate on the save files directly and must run BEFORE the server opens
// its own handle on the world (mcdb/leveldb only allows one open handle on
// a world folder at a time) — call CleanEntities in main() before
// conf.New(), not after.
package worldmaint

import (
	"fmt"
	"log/slog"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// CleanEntities walks every chunk column in a square box (radiusChunks
// chunks out from the block position centerX/centerZ, in the given
// dimension) inside the world save at worldDir, and strips out any saved
// entity whose "identifier" isn't known to reg. This is a fix for the
// "read column: unknown entity type" errors in the console log: those are
// leftover vanilla-Bedrock mob entities (squid/cow/creeper/zombie) baked
// into the Kepler map's original export. Dragonfly has no Go
// implementation for them (it ships no built-in mob AI at all — see
// bosses/demonking/entity.go's own doc comment), so world.(*World).Load
// silently drops them every time that chunk is loaded and logs that error
// — but the raw NBT stays in the save file untouched, since that filtering
// happens only in memory, not when re-saving. That repeated error spam,
// doubled up whenever two players converge on the same chunk, is what's
// been driving the tick lag behind the timeout kicks. This walks the save
// directly and does the same filtering the live server does, but actually
// persists it, so the bad entities stop coming back on every load.
//
// reg must be built the same way main.go builds conf.Entities (i.e.
// demonking.EntityRegistry()), since that's what determines which
// identifiers count as "known".
//
// Confirmed against your Dragonfly v0.11.1 source directly (server/world/
// world.go's columnFrom, server/world/mcdb/db.go, server/world/entity.go's
// EntityRegistry.Lookup) — not a guess.
//
// Returns the number of entities removed.
func CleanEntities(worldDir string, dim world.Dimension, centerX, centerZ, radiusChunks int, reg world.EntityRegistry, log *slog.Logger) (removed int, err error) {
	db, err := mcdb.Config{Log: log}.Open(worldDir)
	if err != nil {
		return 0, fmt.Errorf("open world %q: %w", worldDir, err)
	}
	defer db.Close()

	centerCX := int32(centerX >> 4)
	centerCZ := int32(centerZ >> 4)
	r := int32(radiusChunks)

	for x := centerCX - r; x <= centerCX+r; x++ {
		for z := centerCZ - r; z <= centerCZ+r; z++ {
			pos := world.ChunkPos{x, z}
			col, err := db.LoadColumn(pos, dim)
			if err != nil {
				// Nothing generated there yet — nothing to clean.
				continue
			}
			kept := col.Entities[:0]
			changed := false
			for _, e := range col.Entities {
				id, ok := e.Data["identifier"].(string)
				if !ok {
					removed++
					changed = true
					continue
				}
				if _, ok := reg.Lookup(id); !ok {
					removed++
					changed = true
					continue
				}
				kept = append(kept, e)
			}
			if !changed {
				continue
			}
			col.Entities = kept
			if err := db.StoreColumn(pos, dim, col); err != nil {
				return removed, fmt.Errorf("store column %v: %w", pos, err)
			}
		}
	}
	return removed, nil
}
