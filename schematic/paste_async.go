package schematic

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// This file exists because of a real crash: pasting a 550,550-block
// schematic in one go (the old, fully-synchronous PasteAt, still
// available for anyone who calls it directly) lined up right before a
// "assignment to entry in nil map" panic deep inside Dragonfly's own
// subchunk-viewer code (session.subChunkEntry / world.EntityHandle.
// runScheduled), immediately after the player dug down and re-pasted
// at ground level. The likely mechanism: setting half a million blocks
// inside a single world transaction means every nearby player's client
// gets an entire structure's worth of chunk updates queued in one
// tick — a burst far outside anything the engine was likely tested
// against. This can't fix a bug inside the dragonfly module itself,
// but it directly removes the probable trigger: no single tick ever
// has to generate that big a burst again.
const (
	// pasteBatchSize is how many blocks PasteAsync places per tick.
	// Lower = smaller bursts (safer, slower); higher = fewer ticks
	// (faster, bigger bursts). 4096 was picked as a cautious starting
	// point, not measured — worth tuning if pastes still feel too slow
	// or still cause trouble.
	pasteBatchSize = 4096
	// pasteBatchDelay is a short pause between batches on top of
	// whatever a tick naturally takes, so a big paste doesn't hog every
	// consecutive tick back-to-back and starve everything else running
	// on the world (players moving, other blocks ticking, etc.).
	pasteBatchDelay = 50 * time.Millisecond
)

// PasteAsync places the clipboard at origin over many small batches
// instead of all at once, then messages the player once it's done.
//
// handle is the pasting player's persistent entity handle — get it via
// p.H() in the command that calls this (see commands.go's Paste.Run),
// since the *world.Tx and *player.Player values a command receives are
// only valid for that single tick and can't be held onto across the
// goroutine this starts. EntityHandle is designed to be held onto
// exactly like this (its own doc calls it "a persistent identifier of
// an entity"), and is already used the same way elsewhere in this repo
// (mobs/mob.go, mobs/hostilemob.go) — just not across a goroutine
// before now.
//
// Each batch runs via handle.ExecWorld(func(tx *world.Tx, e world.
// Entity)), which finds whatever world the player is currently in and
// opens a transaction there — confirmed against the real, public
// df-mc/dragonfly API docs (pkg.go.dev), not a guess: an earlier
// version of this code tried w.Exec on *world.World directly, which
// turned out to only be public in a newer dragonfly release than
// v0.11.4 pins — this version has it as an unexported method, which
// was the actual compile error. ExecWorld is the one this repo's
// pinned version does expose.
//
// If the player disconnects mid-paste, ExecWorld returns false instead
// of running the closure; PasteAsync stops there rather than continuing
// to place blocks for someone who isn't around to see them or get the
// completion message.
func (c *Clipboard) PasteAsync(handle *world.EntityHandle, origin cube.Pos) {
	go func() {
		total := len(c.Ids)
		skipped := 0
		for start := 0; start < total; start += pasteBatchSize {
			end := start + pasteBatchSize
			if end > total {
				end = total
			}
			ran := handle.ExecWorld(func(tx *world.Tx, e world.Entity) {
				skipped += c.pasteRange(tx, origin, start, end)
			})
			if !ran {
				return // player disconnected — stop, nothing left to message either
			}
			if end < total {
				time.Sleep(pasteBatchDelay)
			}
		}

		handle.ExecWorld(func(tx *world.Tx, e world.Entity) {
			p, ok := e.(*player.Player)
			if !ok {
				return
			}
			if skipped > 0 {
				p.Messagef("§aFinished pasting §e%s§a (%dx%dx%d, %d blocks, §c%d skipped (furnaces/chests/etc)§a).", c.Name, c.Size.X, c.Size.Y, c.Size.Z, c.Size.Volume(), skipped)
			} else {
				p.Messagef("§aFinished pasting §e%s§a (%dx%dx%d, %d blocks).", c.Name, c.Size.X, c.Size.Y, c.Size.Z, c.Size.Volume())
			}
		})
	}()
}
