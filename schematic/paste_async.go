package schematic

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// This file exists because of a real crash: pasting a 550,550-block
// schematic in one go (the old, fully-synchronous PasteAt, still used
// by PasteAt itself for anyone who calls it directly) lined up right
// before a "assignment to entry in nil map" panic deep inside
// Dragonfly's own subchunk-viewer code (session.subChunkEntry /
// world.EntityHandle.runScheduled), immediately after the player dug
// down and re-pasted at ground level. The likely mechanism: setting
// half a million blocks inside a single world transaction means every
// nearby player's client gets an entire structure's worth of chunk
// updates queued in one tick — a burst far outside anything the engine
// was likely tested against. This can't fix a bug inside the
// dragonfly module itself, but it directly removes the probable
// trigger: no single tick ever has to generate that big a burst again.
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
// instead of all at once, then messages the player (found by xuid once
// it's done) with a summary — the same message PasteAt's caller would
// print, just delayed until the real work finishes. w is the clipboard's
// owning world; get it via tx.World() in the command that calls this
// (see commands.go's Paste.Run), since the *world.Tx and *player.Player
// values a command receives are only valid for that single tick and
// can't be held onto across the goroutine this starts.
//
// UNVERIFIED: world.(*World).Exec(func(tx *world.Tx)) (returning a
// channel that closes once the closure has run) is this file's
// best-confidence guess at how Dragonfly lets code outside the normal
// tick/command flow submit work to run inside it. Nothing else in this
// repo calls it — unlike tx.World() and tx.Players(), both already
// proven working elsewhere (commands.go, scoreboard.go) — so it hasn't
// been compiled against your actual v0.11.4 build. If `go build` fails
// on this specific call, paste the error back.
func (c *Clipboard) PasteAsync(w *world.World, origin cube.Pos, xuid string) {
	go func() {
		total := len(c.Ids)
		skipped := 0
		for start := 0; start < total; start += pasteBatchSize {
			end := start + pasteBatchSize
			if end > total {
				end = total
			}
			<-w.Exec(func(tx *world.Tx) {
				skipped += c.pasteRange(tx, origin, start, end)
			})
			if end < total {
				time.Sleep(pasteBatchDelay)
			}
		}

		<-w.Exec(func(tx *world.Tx) {
			for e := range tx.Players() {
				p, ok := e.(*player.Player)
				if !ok || p.XUID() != xuid {
					continue
				}
				if skipped > 0 {
					p.Messagef("§aFinished pasting §e%s§a (%dx%dx%d, %d blocks, §c%d skipped (furnaces/chests/etc)§a).", c.Name, c.Size.X, c.Size.Y, c.Size.Z, c.Size.Volume(), skipped)
				} else {
					p.Messagef("§aFinished pasting §e%s§a (%dx%dx%d, %d blocks).", c.Name, c.Size.X, c.Size.Y, c.Size.Z, c.Size.Volume())
				}
				return
			}
			// Player logged off before it finished — nothing to message,
			// the blocks are already placed either way.
		})
	}()
}
