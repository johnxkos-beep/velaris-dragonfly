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

// CleanEntities re-saves every chunk column in a square box (radiusChunks
// chunks out from the block position centerX/centerZ, in the given
// dimension) inside the world save at worldDir. This is a fix for the
// "read column: unknown entity type" errors in the console log: those are
// leftover vanilla-Bedrock mob entities (squid/cow/creeper/zombie) baked
// into the Kepler map's original export. Dragonfly has no Go
// implementation for them (it ships no built-in mob AI at all — see
// bosses/demonking/entity.go's own doc comment), so it can never decode
// them; every chunk load re-triggers the same decode error for as long as
// they're still sitting in the save file. Reading a column already makes
// Dragonfly's own loader skip whatever it can't decode and log the error —
// it does NOT fail the whole column — so simply reading a column back out
// and writing it back unchanged already drops anything that failed to
// decode. That repeated error spam, doubled up whenever two players
// converge on the same chunk, is what's been driving the tick lag behind
// the timeout kicks.
//
// UNVERIFIED against your exact Dragonfly version's mcdb API — no network
// access in this environment to check github.com/df-mc/dragonfly directly,
// so the mcdb.Config/DB method names below are my best-documented read of
// the package, not confirmed against your go.sum version. If a field or
// method name here is wrong, `go build` will say so immediately — paste me
// the exact compiler error and it's a quick fix.
//
// Returns the number of chunk columns processed (not the number of
// entities dropped — Dragonfly's loader doesn't hand back a count of what
// it silently discarded, only a log line per bad entity, so watch the
// console on next boot: those "unknown entity type" lines for chunks in
// this box should be gone).
func CleanEntities(worldDir string, dim world.Dimension, centerX, centerZ, radiusChunks int, log *slog.Logger) (processed int, err error) {
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
			col, err := db.ReadColumn(pos, dim)
			if err != nil {
				// Either nothing generated there yet, or the column read
				// itself failed outright rather than just skipping the bad
				// entity — either way there's nothing to write back, so
				// move on instead of aborting the whole run over one chunk.
				continue
			}
			if err := db.WriteColumn(pos, dim, col); err != nil {
				return processed, fmt.Errorf("write column %v: %w", pos, err)
			}
			processed++
		}
	}
	return processed, nil
}
