// Ambient/natural spawning for the four passive mobs in this package.
//
// HOW IT WORKS NOW (redesigned from an earlier "roll the dice near where
// the player happens to be" version):
//
//   - Every online player effectively carries a 100x100 area around them
//     (±areaHalfSize on X and Z), centered on their CURRENT position, not
//     wherever they were when they joined.
//   - Every spawnerIntervalTicks, for every online player: despawn any of
//     this package's mobs that have fallen outside EVERY online player's
//     area (so walking away from a spot naturally clears it out), then
//     try several spawn attempts inside their current area (so walking
//     INTO a new spot naturally populates it) — this is what makes it
//     "keep going" continuously as someone walks around, instead of a
//     one-shot roll.
//   - Day-only: checked against the real world clock (tx.World().Time(),
//     confirmed against this project's own /time command in
//     commands/commands.go) — ticks 0-11999 count as day, 12000-23999 as
//     night, matching vanilla's day/night tick boundary.
//   - Two population caps (areaMobCap per 100x100 area, globalMobCap for
//     the whole world) keep this bounded even with the higher spawn rate
//     below — without them, "spawn a lot, continuously, while people
//     walk around" would grow mob count without limit.
//
// LIFECYCLE / STAYING ALIVE AS PLAYERS MOVE: this still runs as a single
// invisible entity's Tick method rather than a background goroutine (see
// scoreboard.go's package doc comment for why — a goroutine-based version
// of that system hit real, silent ClientDisconnection failures touching
// players from outside a Tick callback). The earlier version of this file
// left the entity sitting still at wherever it was first created, which
// meant its OWN chunk could eventually unload if every player wandered
// off, silently pausing all natural spawning. Fixed here: every tick, the
// entity re-parents its own position to a currently-online player
// (data.Pos = p.Position()) — since it has zero bounding box and no
// viewers, nobody ever sees it "teleport", but it guarantees its chunk is
// always the same one an actual player is standing in, so it keeps
// ticking for as long as anyone's online, no matter how far they roam.
//
// DEBUGGING: natural spawns and despawn sweeps still print short lines to
// the console (Pterodactyl console tab) so you can watch this working
// without having to go find the animals yourself.
package mobs

import (
	"fmt"
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
	spawnerIntervalTicks = 40 // ~2s at 20 ticks/sec — was 100 (~5s); this alone roughly 2.5x's the rate
	spawnAttemptsPerCycle = 3   // spawn attempts per online player, per interval — was 1
	spawnAttemptChance    = 0.9 // chance per attempt — was 0.35; combined with the above, area fills fast

	areaHalfSize = 50.0 // half of the 100x100 area — spawns/despawns are checked within player.X/Z ± this

	spawnGroundSearchRange = 10 // blocks up/down searched from the candidate spot's own Y

	areaMobCap   = 24  // max mobs allowed inside one player's 100x100 area at once
	globalMobCap = 150 // max total mobs (this package's types) in the whole world at once — was 60

	dayEndTicks = 12000 // vanilla day/night boundary: 0-11999 = day, 12000-23999 = night
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
	fmt.Printf("[mobs] ambient spawner started near (%.1f, %.1f, %.1f)\n", near.X(), near.Y(), near.Z())
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

// Tick re-homes this entity to a live player every tick (see the package
// doc comment above), then runs the despawn sweep + spawn attempts on
// spawnerIntervalTicks.
func (s *spawner) Tick(tx *world.Tx, current int64) {
	if p, ok := anyPlayer(tx); ok {
		s.data.Pos = p.Position()
	}

	if current%spawnerIntervalTicks != 0 {
		return
	}

	despawnOutOfRange(tx)

	if !isDaytime(tx) {
		return
	}
	if n := countMobs(tx); n >= globalMobCap {
		return
	}

	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		for i := 0; i < spawnAttemptsPerCycle; i++ {
			if rand.Float64() > spawnAttemptChance {
				continue
			}
			attemptSpawn(tx, p)
		}
	}
}

// anyPlayer returns an arbitrary currently-online player, used only to
// keep the spawner entity itself in a loaded chunk (see package doc).
func anyPlayer(tx *world.Tx) (*player.Player, bool) {
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			return p, true
		}
	}
	return nil, false
}

// isDaytime reports whether the world clock is currently in the day part
// of the cycle. tx.World().Time()/SetTime() confirmed against this
// project's own /time command (commands/commands.go).
func isDaytime(tx *world.Tx) bool {
	t := tx.World().Time() % 24000
	if t < 0 {
		t += 24000
	}
	return t < dayEndTicks
}

// attemptSpawn tries one random spawn somewhere inside p's current
// 100x100 area. Does nothing on any failure (no ground found, area at
// cap) — no retry loop, keeping the per-attempt cost small and bounded.
func attemptSpawn(tx *world.Tx, p *player.Player) {
	pp := p.Position()
	if countMobsInArea(tx, pp) >= areaMobCap {
		return
	}

	candidate := mgl64.Vec3{
		pp.X() + (rand.Float64()*2-1)*areaHalfSize,
		pp.Y(),
		pp.Z() + (rand.Float64()*2-1)*areaHalfSize,
	}

	pos, ok := findGroundNear(tx, candidate, spawnGroundSearchRange)
	if !ok {
		return
	}

	var kind string
	switch rand.Intn(4) {
	case 0:
		SpawnCow(tx, pos)
		kind = "cow"
	case 1:
		SpawnChicken(tx, pos)
		kind = "chicken"
	case 2:
		SpawnPig(tx, pos)
		kind = "pig"
	default:
		SpawnSheep(tx, pos)
		kind = "sheep"
	}
	fmt.Printf("[mobs] natural spawn: %s near %s at (%.1f, %.1f, %.1f)\n", kind, p.Name(), pos.X(), pos.Y(), pos.Z())
}

// despawnOutOfRange removes any of this package's mobs that are outside
// EVERY currently-online player's 100x100 area. If nobody is online, it
// does nothing — mobs are left alone rather than mass-despawned the
// instant the server is empty.
func despawnOutOfRange(tx *world.Tx) {
	var areas []mgl64.Vec3
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			areas = append(areas, p.Position())
		}
	}
	if len(areas) == 0 {
		return
	}

	removed := 0
	for e := range tx.Entities() {
		m, ok := e.(*Mob)
		if !ok {
			continue
		}
		pos := m.Position()
		inRange := false
		for _, center := range areas {
			if math.Abs(pos.X()-center.X()) <= areaHalfSize && math.Abs(pos.Z()-center.Z()) <= areaHalfSize {
				inRange = true
				break
			}
		}
		if !inRange {
			tx.RemoveEntity(m)
			removed++
		}
	}
	if removed > 0 {
		fmt.Printf("[mobs] despawned %d mob(s) that fell outside every player's area\n", removed)
	}
}

// findGroundNear searches up and down from pos (within searchRange blocks
// of pos's own Y) for the first spot that passes validSpawnGround, and
// returns the position standing on top of it. Bounded and cheap — worst
// case a couple dozen block lookups per attempt.
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

// countMobsInArea returns how many of this package's mobs are within
// center's 100x100 area (±areaHalfSize on X and Z).
func countMobsInArea(tx *world.Tx, center mgl64.Vec3) int {
	n := 0
	for e := range tx.Entities() {
		m, ok := e.(*Mob)
		if !ok {
			continue
		}
		pos := m.Position()
		if math.Abs(pos.X()-center.X()) <= areaHalfSize && math.Abs(pos.Z()-center.Z()) <= areaHalfSize {
			n++
		}
	}
	return n
}
