package schematic

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// pasteBatchSize is how many clipboard blocks the paste ticker places
// per server tick (20 ticks/sec, ~50ms each). Lower = smaller bursts
// (safer, slower); higher = fewer ticks (faster, bigger bursts). 4096
// was picked as a cautious starting point, not measured — worth tuning
// if pastes still feel too slow or still cause trouble.
const pasteBatchSize = 4096

// This file exists because of a real crash: pasting a 550x550-block
// schematic in one go (the old, fully-synchronous PasteAt, still
// available for anyone who calls it directly) lined up right before a
// "assignment to entry in nil map" panic deep inside Dragonfly's own
// subchunk-viewer code, immediately after the player dug down and
// re-pasted at ground level. The likely mechanism: setting half a
// million blocks inside a single world transaction means every nearby
// player's client gets an entire structure's worth of chunk updates
// queued in one tick — a burst far outside anything the engine was
// likely tested against. This can't fix a bug inside the dragonfly
// module itself, but it directly removes the probable trigger: no
// single tick ever has to generate that big a burst again.
//
// An earlier version of this file ran the batches from an independent
// goroutine via handle.ExecWorld(...), reasoning (wrongly, for the
// Dragonfly version this repo is actually pinned to) that ExecWorld was
// the public, thread-safe way to reach back into a player's world from
// outside a Tick call. The real compiler error on this pin is:
//
//	*world.EntityHandle has no field or method ExecWorld,
//	but does have unexported method execWorld
//
// i.e. there is no public way to do that here — only an internal,
// framework-only execWorld exists. This mirrors the exact fix already
// used twice elsewhere in this codebase for that same class of problem
// — restrict's enforcer (restrict/restrict.go) and scoreboard's ticker
// (scoreboard/scoreboard.go), both documented as having hit this after
// an earlier version tried touching the world from a goroutine: spawn a
// single Tick(tx, current)-driven entity instead. Since a server tick is
// already ~50ms, one batch per Tick call reproduces the old
// pasteBatchDelay pacing for free, with no time.Sleep needed.

// PasteAsync spawns a one-shot paste ticker entity (see pasteTicker
// below) near the pasting player that places pasteBatchSize blocks of
// the clipboard per server tick until the whole thing is placed, then
// messages the player and removes itself.
//
// near should be the pasting player's position at the moment /paste ran
// (p.Position()) — same "spawn near actual player activity, not a fixed
// coordinate" reasoning as scoreboard.Manager.EnsureTicker and
// restrict's ensureEnforcer: a Tick method only fires while its own
// chunk is loaded, so spawning next to the player guarantees that at
// least at the start. playerXUID is compared against tx.Players() each
// tick so the ticker can find the right player to message once done; if
// the player has disconnected by the time the paste finishes, the
// completion message is just skipped (this mirrors the old code's
// `ExecWorld returned false -> stop, nothing left to message either`).
func (c *Clipboard) PasteAsync(tx *world.Tx, origin cube.Pos, near mgl64.Vec3, playerXUID string) {
	handle := world.EntitySpawnOpts{Position: near}.New(PasteTickerType, pasteTickerConfig{
		clip:       c,
		origin:     origin,
		playerXUID: playerXUID,
	})
	tx.AddEntity(handle)
}

// ---------------------------------------------------------------------
// pasteTicker: an invisible, one-shot entity that places one batch of
// one schematic paste job per server tick, then messages the pasting
// player and removes itself. Unlike restrict's enforcer or scoreboard's
// ticker (permanent singletons), a fresh pasteTicker is spawned per
// /paste call and is done for good once its job finishes.
// ---------------------------------------------------------------------

// PasteTickerType is the entity type for the invisible paste ticker.
var PasteTickerType pasteTickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in main.go
// (mirrors restrict.EntityTypes() / scoreboard.EntityTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{PasteTickerType} }

var pasteTickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type pasteTickerType struct{}

func (pasteTickerType) EncodeEntity() string        { return "velaris:schematic_paste_ticker" }
func (pasteTickerType) BBox(world.Entity) cube.BBox { return pasteTickerBBox }
func (pasteTickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*pasteTickerState)
	if !ok || st == nil {
		// Shouldn't happen in practice — Apply always sets this at spawn
		// time below — but fail safe into an already-finished ticker
		// rather than a nil-pointer panic if it ever does.
		st = &pasteTickerState{next: 0}
	}
	return &pasteTicker{tx: tx, handle: handle, data: data, state: st}
}
func (pasteTickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (pasteTickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// pasteTickerConfig carries the one-time spawn parameters for a
// pasteTicker into its EntityData.Data via Apply — same pattern as
// mobs/hostilemob.go's hostileConfig.
type pasteTickerConfig struct {
	clip       *Clipboard
	origin     cube.Pos
	playerXUID string
}

func (cfg pasteTickerConfig) Apply(data *world.EntityData) {
	if data.Data == nil {
		data.Data = &pasteTickerState{clip: cfg.clip, origin: cfg.origin, playerXUID: cfg.playerXUID}
	}
}

// pasteTickerState is the paste job's progress: which clipboard, where
// it's going, whose it is, and how far in we've gotten.
type pasteTickerState struct {
	clip       *Clipboard
	origin     cube.Pos
	playerXUID string
	next       int // next clipboard index to place
	skipped    int
}

type pasteTicker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *pasteTickerState
}

func (t *pasteTicker) H() *world.EntityHandle  { return t.handle }
func (t *pasteTicker) Position() mgl64.Vec3    { return t.data.Pos }
func (t *pasteTicker) Rotation() cube.Rotation { return t.data.Rot }
func (t *pasteTicker) Close() error            { return nil }

// Tick places one pasteBatchSize-sized batch of the job's clipboard per
// call. Once the whole clipboard has been placed, it messages the
// pasting player (if still online) and removes itself — a pasteTicker
// never sticks around past its one job.
func (t *pasteTicker) Tick(tx *world.Tx, _ int64) {
	s := t.state
	if s == nil || s.clip == nil {
		tx.RemoveEntity(t)
		return
	}

	total := len(s.clip.Ids)
	end := s.next + pasteBatchSize
	if end > total {
		end = total
	}
	s.skipped += s.clip.pasteRange(tx, s.origin, s.next, end)
	s.next = end

	if s.next < total {
		return
	}

	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok || p.XUID() != s.playerXUID {
			continue
		}
		if s.skipped > 0 {
			p.Messagef("§aFinished pasting §e%s§a (%dx%dx%d, %d blocks, §c%d skipped (furnaces/chests/etc)§a).", s.clip.Name, s.clip.Size.X, s.clip.Size.Y, s.clip.Size.Z, s.clip.Size.Volume(), s.skipped)
		} else {
			p.Messagef("§aFinished pasting §e%s§a (%dx%dx%d, %d blocks).", s.clip.Name, s.clip.Size.X, s.clip.Size.Y, s.clip.Size.Z, s.clip.Size.Volume())
		}
		break
	}
	tx.RemoveEntity(t)
}
