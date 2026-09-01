// Ambient/natural spawning for the four passive mobs in this package. This
// is deliberately simple and cheap, matching the rest of this package's
// performance stance (see mob.go's doc comment):
//
//   - Runs on an interval (spawnerIntervalTicks), not every tick.
//   - Per interval, makes at most ONE spawn attempt per online player, and
//     only with spawnAttemptChance probability — so most intervals, most
//     players, nothing happens at all.
//   - A failed attempt (bad terrain, too crowded) just does nothing; there's
//     no retry-until-success loop.
//   - Two cheap population caps (localClusterCap near the candidate spot,
//     globalMobCap for the whole map) stop this from growing mob count
//     without bound over a long-running server.
//
// LIFECYCLE PATTERN: like scoreboard's refresh loop and restrict's
// enforcer, this runs as a single invisible entity's Tick method rather
// than a background goroutine — see scoreboard.go's package doc comment
// for why (a prior goroutine-based version of that system hit real,
// silent ClientDisconnection failures touching players from outside a
// Tick callback). Same inherited caveat as those two: this entity's Tick
// only fires while ITS OWN chunk stays loaded, so if every player wanders
// far from wherever the spawner was first created and that chunk unloads,
// natural spawning pauses until a player gets back near it. Scoreboard's
// ticker has run with this same limitation already, so it's an accepted
// tradeoff in this codebase, not a new risk.
//
// WHAT THIS DOESN'T DO: no vanilla-style light-level/biome rules, no
// checking that the ground block is specifically grass — just "solid
// block with 2 air blocks above it, not water/lava". Good enough for a
// simple ambient-life feature; tighten validSpawnGround later if you want
// closer-to-vanilla placement rules.
package mobs

import (
	"math"
	"math/rand"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	spawnerIntervalTicks   = 100  // ~5s at 20 ticks/sec
	spawnAttemptChance     = 0.35 // per online player, per interval
	spawnMinRadius         = 12.0 // blocks from the player — not right in their face
	spawnMaxRadius         = 28.0 // blocks from the player — likely still in loaded/simulated range
	spawnGroundSearchRange = 10   // blocks up/down searched from the player's own Y
	localClusterRadius     = 16.0 // blocks
	localClusterCap        = 6    // max existing mobs within localClusterRadius of a candidate spot
	globalMobCap           = 60   // max total mobs (this package's types) in the whole world at once
)

// EnsureSpawner spawns the single ambient-spawn ticker entity the first
// time it's needed. Safe to call repeatedly; only spawns once. Call it
// from the first real *world.Tx you have — same call site as
// scoreboardMgr.EnsureTicker in main.go (right after a player joins).
func EnsureSpawner(tx *world.Tx, near mgl64.Vec3) {
	spawnerMu.Lock()
	if spawnerSpawned {
		spawnerMu.Unlock()
		return
	}
	spawnerSpawned = true
	spawnerMu.Unlock()

	handle := world.EntitySpawnOpts{Position: near}.New(SpawnerType, spawnerConfig{})
	tx.AddEntity(handle)
}

var (
	spawnerMu      sync.Mutex
	spawnerSpawned bool
)

// SpawnerType is the entity type for the invisible ambient-spawn ticker.
var SpawnerType spawnerType

var spawnerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type spawnerType struct{}

func (spawnerType) EncodeEntity() string        { return "velaris:mob_spawner" }
func (spawnerType) BBox(world.Entity) cube.BBox { return spawnerBBox }
func (spawnerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &spawner{tx: tx, handle: handle, data: data}
}
func (spawnerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (spawnerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// spawnerConfig is an empty EntitySpawnOpts config — this entity needs no
// spawn-time configuration, mirrors scoreboard's tickerConfig.
type spawnerConfig struct{}

func (spawnerConfig) Apply(data *world.EntityData) {}

type spawner struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (s *spawner) H() *world.EntityHandle  { return s.handle }
func (s *spawner) Position() mgl64.Vec3    { return s.data.Pos }
func (s *spawner) Rotation() cube.Rotation { return s.data.Rot }
func (s *spawner) Close() error            { return nil }

// Tick runs the ambient spawn attempts on spawnerIntervalTicks. See the
// package doc comment above for the full performance/behaviour rationale.
func (s *spawner) Tick(tx *world.Tx, current int64) {
	if current%spawnerIntervalTicks != 0 {
		return
	}
	if countMobs(tx) >= globalMobCap {
		return
	}
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		if rand.Float64() > spawnAttemptChance {
			continue
		}
		attemptSpawn(tx, p)
	}
}

// attemptSpawn tries exactly one random spawn near p. Does nothing on any
// failure (no ground found, too crowded) — there is deliberately no
// retry loop, keeping the per-player cost bounded and tiny.
func attemptSpawn(tx *world.Tx, p *player.Player) {
	angle := rand.Float64() * 2 * math.Pi
	dist := spawnMinRadius + rand.Float64()*(spawnMaxRadius-spawnMinRadius)
	candidate := p.Position().Add(mgl64.Vec3{math.Cos(angle) * dist, 0, math.Sin(angle) * dist})

	pos, ok := findGroundNear(tx, candidate, spawnGroundSearchRange)
	if !ok {
		return
	}
	if countMobsWithin(tx, pos, localClusterRadius) >= localClusterCap {
		return
	}

	switch rand.Intn(4) {
	case 0:
		SpawnCow(tx, pos)
	case 1:
		SpawnChicken(tx, pos)
	case 2:
		SpawnPig(tx, pos)
	default:
		SpawnSheep(tx, pos)
	}
}

// findGroundNear searches up and down from pos (within searchRange blocks
// of pos's own Y) for the first spot that passes validSpawnGround, and
// returns the position standing on top of it. Bounded and cheap — worst
// case a couple dozen block lookups, and only runs on a spawn attempt
// (already gated by spawnerIntervalTicks + spawnAttemptChance above).
func findGroundNear(tx *world.Tx, pos mgl64.Vec3, searchRange int) (mgl64.Vec3, bool) {
	bp := cube.PosFromVec3(pos)
	for i := 0; i <= searchRange; i++ {
		for _, dy := range [2]int{-i, i} {
			if i == 0 && dy != 0 {
				continue
			}
			y := bp.Y() + dy
			ground := cube.Pos{bp.X(), y, bp.Z()}
			if validSpawnGround(tx, ground) {
				return mgl64.Vec3{pos.X(), float64(y + 1), pos.Z()}, true
			}
		}
	}
	return mgl64.Vec3{}, false
}

// validSpawnGround reports whether ground is solid, non-water, non-lava,
// and has 2 blocks of clear air above it (enough headroom for the
// tallest of the four mobs, the cow, at 1.4 blocks).
func validSpawnGround(tx *world.Tx, ground cube.Pos) bool {
	b := tx.Block(ground)
	if _, ok := b.(block.Air); ok {
		return false
	}
	if _, ok := b.(block.Water); ok {
		return false
	}
	if _, ok := b.(block.Lava); ok {
		return false
	}
	_, air1 := tx.Block(cube.Pos{ground.X(), ground.Y() + 1, ground.Z()}).(block.Air)
	_, air2 := tx.Block(cube.Pos{ground.X(), ground.Y() + 2, ground.Z()}).(block.Air)
	return air1 && air2
}

// countMobs returns the total number of this package's mobs currently
// alive anywhere in tx (i.e. in this world/dimension).
func countMobs(tx *world.Tx) int {
	n := 0
	for e := range tx.Entities() {
		if _, ok := e.(*Mob); ok {
			n++
		}
	}
	return n
}

// countMobsWithin returns how many of this package's mobs are within
// radius blocks of pos.
func countMobsWithin(tx *world.Tx, pos mgl64.Vec3, radius float64) int {
	n := 0
	for e := range tx.Entities() {
		m, ok := e.(*Mob)
		if !ok {
			continue
		}
		if m.Position().Sub(pos).Len() <= radius {
			n++
		}
	}
	return n
}
