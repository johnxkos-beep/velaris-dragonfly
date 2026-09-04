package track

import (
	"strconv"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// distanceUpdateInterval is how often (in ticks) each active tracker's
// action bar gets refreshed - 5 ticks = 4/sec, the same cadence
// legendary/hud.go's cooldown bar already uses (hudBarUpdateInterval),
// which is smooth enough to feel "live" while staying well inside
// title.Title's default 2-second duration (see title.go's title.New
// doc), so the message never visibly gaps between refreshes.
//
// This replaces the original TrackListener::onMove - a PlayerMoveEvent
// hook, which has no clean Dragonfly equivalent that's safe to act on
// (see restrict.go's package doc comment on the client-disconnect
// failures earlier attempts at server-side per-movement-packet handling
// caused in this codebase) - with the same periodic ticker entity
// pattern used by countdown, scoreboard, and news elsewhere in this
// project instead: recompute and resend on a timer rather than react to
// every individual movement packet.
const distanceUpdateInterval = 5

// TickerType is the entity type for the invisible track-distance ticker.
var TickerType tickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry - see the wiring note in main.go
// (mirrors countdown.EntityTypes()/news.EntityTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{TickerType} }

var tickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type tickerType struct{}

func (tickerType) EncodeEntity() string        { return "velaris:track_ticker" }
func (tickerType) BBox(world.Entity) cube.BBox { return tickerBBox }
func (tickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &ticker{tx: tx, handle: handle, data: data}
}
func (tickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (tickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// tickerConfig is an empty EntitySpawnOpts config for TickerType, which
// needs no spawn-time configuration - mirrors countdown/news's
// tickerConfig.
type tickerConfig struct{}

func (tickerConfig) Apply(data *world.EntityData) {}

var (
	tickerMu      sync.Mutex
	tickerSpawned bool
)

// EnsureTicker spawns the single track-distance ticker entity the first
// time it's needed. Safe to call repeatedly; only spawns once. Call it
// from the first real *world.Tx available - e.g. right after the first
// player joins, using p.Tx() and p.Position() (see main.go) - same
// "spawn next to actual player activity so the chunk is guaranteed
// loaded" reasoning as countdown.EnsureTicker/news.EnsureTicker.
func EnsureTicker(tx *world.Tx, near mgl64.Vec3) {
	tickerMu.Lock()
	if tickerSpawned {
		tickerMu.Unlock()
		return
	}
	tickerSpawned = true
	tickerMu.Unlock()

	handle := world.EntitySpawnOpts{Position: near}.New(TickerType, tickerConfig{})
	tx.AddEntity(handle)
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

// Tick refreshes the action-bar distance HUD for every player currently
// live-tracking a point - port of TrackListener::onMove's distance math
// and action bar send, just driven on a timer instead of on every
// movement packet (see distanceUpdateInterval's doc comment above for
// why).
func (t *ticker) Tick(tx *world.Tx, current int64) {
	if current%distanceUpdateInterval != 0 {
		return
	}

	trackers := Cfg.ActiveTrackers()
	if len(trackers) == 0 {
		return
	}

	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		name, tracking := trackers[p.XUID()]
		if !tracking {
			continue
		}

		point, exists := Cfg.GetPoint(name)
		if !exists {
			// Point was removed/renamed out from under them - stop
			// rather than spamming a broken HUD forever, same as the
			// original's "point was removed from under them somehow"
			// handling in TrackListener::onMove.
			Cfg.StopTracking(p.XUID())
			continue
		}

		distance := int(point.Sub(p.Position()).Len() + 0.5)
		text := "§aYou're " + strconv.Itoa(distance) + " blocks away from " + name + "."
		p.SendTitle(title.New("").WithActionText(text))
	}
}
