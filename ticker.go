package border

import (
	"log"
	"math"
	"strconv"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---------------------------------------------------------------------
// TickerType: a single, invisible, always-on entity that enforces the
// border every few ticks. Same "avoid touching player/world state from
// an independent goroutine" reasoning as koth's/restrict's/countdown's
// own ticker entities in this repo. Exactly one is ever spawned — see
// EnsureTicker.
// ---------------------------------------------------------------------

// TickerType is the entity type for the invisible border ticker.
var TickerType tickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in main.go
// (mirrors koth.EntityTypes()/restrict.EntityTypes()/countdown.EntityTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{TickerType} }

var tickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type tickerType struct{}

func (tickerType) EncodeEntity() string        { return "velaris:border_ticker" }
func (tickerType) BBox(world.Entity) cube.BBox { return tickerBBox }
func (tickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &ticker{tx: tx, handle: handle, data: data}
}
func (tickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (tickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// tickerConfig is an empty EntitySpawnOpts config for TickerType, which
// needs no spawn-time configuration — mirrors koth's/countdown's own
// tickerConfig.
type tickerConfig struct{}

func (tickerConfig) Apply(data *world.EntityData) {}

var tickerSpawned bool

// EnsureTicker spawns the single border-enforcement ticker entity the
// first time it's needed. Safe to call repeatedly; only spawns once.
// Call this from main.go's player-join loop (near should be the joining
// player's own position, via p.Position(), with tx from p.Tx()) — same
// "spawn next to a guaranteed-loaded chunk" pattern koth.EnsureTicker/
// track.EnsureTicker/restrict's ensureEnforcer already use, and for the
// same reason: a ticker entity spawned in a permanently-unloaded chunk
// gets created but its Tick method never fires. Unlike koth/restrict
// (whose tickers are only needed once a zone exists), border should be
// enforced from the moment the server has any player online at all, so
// this is called unconditionally on every join in main.go rather than
// lazily from a command.
func EnsureTicker(tx *world.Tx, near mgl64.Vec3) {
	if tickerSpawned {
		return
	}
	tickerSpawned = true
	handle := world.EntitySpawnOpts{Position: near}.New(TickerType, tickerConfig{})
	tx.AddEntity(handle)
	log.Printf("[border] ticker entity spawned at %v", near)
}

type ticker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (t *ticker) H() *world.EntityHandle  { return t.handle }
func (t *ticker) Position() mgl64.Vec3    { return t.data.Pos }
func (t *ticker) Rotation() cube.Rotation { return t.data.Rot }
func (t *ticker) Close() error            { return nil }

// scanInterval is how often (in ticks) the border gets checked against
// every online player. BorderListener.php ran this on every single
// PlayerMoveEvent, which fires far more often than once a second — koth's
// ticker (this package's closest model) only needs a once-a-second
// resolution for capture progress, but a fast-moving/sprinting/flying
// player could travel well past the edge in a full second before a
// push-back ever triggers. 4 ticks (5 times a second) keeps the position
// check (cheap — just arithmetic per online player, no block/particle
// work) frequent enough that normal movement speeds don't meaningfully
// overshoot the edge, without sending an action-bar title packet every
// single tick.
const scanInterval = 4

// pushBackStrength/pushBackUp mirror BorderListener.php's
// pushBackInsideBorder(): a fixed horizontal knockback strength toward
// the border's center (0,0) — the border is always centered on the
// origin (see Config), so pushing toward (0,0) is a safe direction
// without needing to know which specific edge was crossed — plus a small
// upward component so the push reads as a bounce instead of a shove
// along the ground.
const (
	pushBackStrength = 0.6
	pushBackUp       = 0.25
)

// actionBar shows msg on p's action bar. Same mechanism as koth/ticker.go
// (an empty title.Title with only ActionText set — Dragonfly has no
// separate "send action bar" call; see that file's "CONFIRMED API"
// comment for where this was first verified working in this repo).
func actionBar(p *player.Player, msg string) {
	p.SendTitle(title.New("").
		WithActionText(msg).
		WithFadeInDuration(0).
		WithDuration(1100 * time.Millisecond).
		WithFadeOutDuration(200 * time.Millisecond))
}

// Tick runs every scanInterval ticks — port of BorderListener::onMove(),
// adapted from a per-move event cancel+knockback into a periodic scan
// (see this package's doc comment in border.go for why). Every online
// player is checked against the border: outside it, they're pushed back
// toward center exactly like pushBackInsideBorder() did; within
// warnDistance of the edge (but still inside), they get the same
// action-bar warning BorderListener.php sent.
func (t *ticker) Tick(tx *world.Tx, current int64) {
	if current%scanInterval != 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[border] recovered panic in ticker.Tick: %v", r)
		}
	}()

	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		pos := p.Position()
		x, z := pos[0], pos[2]

		if !Cfg.IsInside(x, z) {
			pushBackInside(p, x, z)
			p.Message("§cYou've reached the world border.")
			continue
		}

		if Cfg.ShouldWarn(x, z) {
			dist := int(Cfg.DistanceToEdge(x, z))
			actionBar(p, "§cWarning: you are "+strconv.Itoa(dist)+" blocks from the world border!")
		}
	}
}

// pushBackInside is the direct port of BorderListener::pushBackInsideBorder().
func pushBackInside(p *player.Player, x, z float64) {
	dx, dz := -x, -z
	length := math.Hypot(dx, dz)
	if length < 0.001 {
		dx, dz = 0, 1
		length = 1
	}
	p.SetVelocity(mgl64.Vec3{
		(dx / length) * pushBackStrength,
		pushBackUp,
		(dz / length) * pushBackStrength,
	})
}
