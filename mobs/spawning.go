// Ambient/natural spawning for the four passive mobs in this package.
//
// HOW IT WORKS:
//
//   - Every online player effectively carries a 100x100 area around them
//     (±areaHalfSize on X and Z), centered on their CURRENT position.
//   - Every spawnerIntervalTicks, the despawn sweep runs for ALL online
//     players (cheap — see SCALING below), removing any of this package's
//     mobs that fell outside EVERY online player's area.
//   - Spawn ATTEMPTS (the expensive part — see SCALING) only run for a
//     rotating batch of up to maxSpawnAttemptPlayers players per cycle,
//     not everyone at once. Over multiple cycles every online player
//     still gets attempts regularly; this just caps the worst-case cost
//     of any single cycle regardless of how many players are online.
//   - Day-only: checked against the real world clock (tx.World().Time(),
//     confirmed against this project's own /time command in
//     commands/commands.go) — ticks 0-11999 count as day, 12000-23999 as
//     night, matching vanilla's day/night tick boundary.
//   - Two population caps (areaMobCap per 100x100 area, globalMobCap for
//     the whole world) bound total mob count. globalMobCap is the more
//     important one at high player counts: every live Mob runs its own
//     wander-AI Tick() every single server tick (20/sec), independent of
//     this spawner's own interval — so total mob count, not spawn rate,
//     is what actually determines this feature's steady-state per-tick
//     cost. Keep this fixed and modest rather than scaling it up with
//     player count.
//   - A THIRD cap, perChunkMobCap, caps how many mobs (passive AND
//     hostile combined — see countAllMobsInChunk) can occupy a single
//     16x16 chunk. areaMobCap alone wasn't enough: it's spread over the
//     whole 100x100 area (~39 chunks), but findGroundNear tends to
//     repeatedly succeed on whatever nearby terrain is actually flat/
//     valid and fail elsewhere, so in practice spawns were clustering
//     onto the same handful of chunks — most visibly, the chunk a
//     stationary player happened to be standing in — well before the
//     area-wide cap was ever reached. Checked at the CANDIDATE spawn
//     chunk, not the player's own chunk, so it caps density everywhere
//     in the area, not just under the player's feet.
//
// SCALING TO MANY PLAYERS (e.g. 100 online at once):
//   - The despawn sweep is pure arithmetic (position comparisons) with no
//     world/block access, so it can cheaply run for every player, every
//     cycle, regardless of player count — worst case is on the order of
//     (mobs x players) comparisons, trivial even in the thousands.
//   - Spawn ATTEMPTS are the expensive part, because each one calls
//     findGroundNear, which does real tx.Block() lookups (world data
//     access, not just arithmetic). Attempting spawns for all N online
//     players every cycle would scale block-lookup cost linearly with N —
//     fine at a handful of players, not fine at 100. maxSpawnAttemptPlayers
//     bounds that cost to a constant, independent of N.
//   - findGroundNear's own search range was also trimmed (was 10, now 6)
//     to cut the worst-case lookups per individual attempt.
//
// LIFECYCLE / STAYING ALIVE AS PLAYERS MOVE: this still runs as a single
// invisible entity's Tick method rather than a background goroutine (see
// scoreboard.go's package doc comment for why — a goroutine-based version
// of that system hit real, silent ClientDisconnection failures touching
// players from outside a Tick callback). Every tick, this entity re-homes
// its own position to a live player (data.Pos = p.Position()) — zero
// bounding box and no viewers, so nobody ever sees it "teleport" — which
// guarantees its own chunk stays loaded for as long as anyone's online,
// no matter how far players roam from wherever it first spawned.
//
// DEBUG LOGGING: off by default (spawnerDebug = false below). Flip it to
// true temporarily if you ever need to watch spawn/despawn activity in
// the console again — every natural spawn and despawn sweep will print a
// line, same as during initial testing.
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
	spawnerDebug = false // set true temporarily to print spawn/despawn activity to the console

	spawnerIntervalTicks = 40  // ~2s at 20 ticks/sec
	spawnAttemptsPerCycle = 3   // spawn attempts per player selected this cycle
	spawnAttemptChance    = 0.65 // chance per attempt

	maxSpawnAttemptPlayers = 15 // process spawn attempts for at most this many players per cycle, rotating through everyone over time — see SCALING in the package doc comment

	areaHalfSize = 50.0 // half of the 100x100 area — spawns/despawns are checked within player.X/Z ± this

	spawnGroundSearchRange = 6 // blocks up/down searched from the candidate spot's own Y — was 10, trimmed for cost

	areaMobCap   = 24  // max mobs allowed inside one player's 100x100 area at once
	globalMobCap = 300 // max total mobs (this package's types) in the whole world at once — the main lag safeguard; see package doc comment

	perChunkMobCap = 4 // max mobs (passive + hostile combined) allowed in any single 16x16 chunk — see package doc comment

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
	if spawnerDebug {
		fmt.Printf("[mobs] ambient spawner started near (%.1f, %.1f, %.1f)\n", near.X(), near.Y(), near.Z())
	}
}

var (
	spawnerMu      sync.Mutex
	spawnerSpawned bool

	// spawnerBatchOffset rotates which players get spawn attempts each
	// cycle. Only ever touched from Tick, which the world engine calls
	// single-threaded, so this needs no lock (unlike spawnerSpawned
	// above, which is set from the join-handling goroutine in main.go).
	spawnerBatchOffset int
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
// doc comment above), then runs the despawn sweep + a batch of spawn
// attempts on spawnerIntervalTicks.
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

	players := onlinePlayers(tx)
	if len(players) == 0 {
		return
	}
	players = spawnBatch(players)

	for _, p := range players {
		attemptSpawnsForPlayer(tx, p)
	}
}

// onlinePlayers collects every currently-online *player.Player from tx.
func onlinePlayers(tx *world.Tx) []*player.Player {
	var players []*player.Player
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			players = append(players, p)
		}
	}
	return players
}

// spawnBatch returns at most maxSpawnAttemptPlayers players from all,
// rotating the starting point each call so every player gets attempts
// regularly even when there are far more players than the batch size —
// see SCALING in the package doc comment.
func spawnBatch(all []*player.Player) []*player.Player {
	if len(all) <= maxSpawnAttemptPlayers {
		return all
	}
	start := spawnerBatchOffset % len(all)
	batch := make([]*player.Player, 0, maxSpawnAttemptPlayers)
	for i := 0; i < maxSpawnAttemptPlayers; i++ {
		batch = append(batch, all[(start+i)%len(all)])
	}
	spawnerBatchOffset += maxSpawnAttemptPlayers
	return batch
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

// attemptSpawnsForPlayer computes p's current area population ONCE, then
// makes up to spawnAttemptsPerCycle spawn attempts (capped by remaining
// room in the area) — avoids re-scanning all entities for every single
// attempt.
func attemptSpawnsForPlayer(tx *world.Tx, p *player.Player) {
	pp := p.Position()
	have := countMobsInArea(tx, pp)
	if have >= areaMobCap {
		return
	}
	attempts := spawnAttemptsPerCycle
	if room := areaMobCap - have; attempts > room {
		attempts = room
	}
	for i := 0; i < attempts; i++ {
		if rand.Float64() > spawnAttemptChance {
			continue
		}
		spawnOneNear(tx, p)
	}
}

// spawnOneNear tries one random spawn somewhere inside p's current
// 100x100 area. Does nothing if no valid ground is found — no retry loop.
func spawnOneNear(tx *world.Tx, p *player.Player) {
	pp := p.Position()
	candidate := mgl64.Vec3{
		pp.X() + (rand.Float64()*2-1)*areaHalfSize,
		pp.Y(),
		pp.Z() + (rand.Float64()*2-1)*areaHalfSize,
	}

	pos, ok := findGroundNear(tx, candidate, spawnGroundSearchRange)
	if !ok {
		return
	}
	if cx, cz := chunkCoords(pos); countAllMobsInChunk(tx, cx, cz) >= perChunkMobCap {
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
	if spawnerDebug {
		fmt.Printf("[mobs] natural spawn: %s near %s at (%.1f, %.1f, %.1f)\n", kind, p.Name(), pos.X(), pos.Y(), pos.Z())
	}
}

// despawnOutOfRange removes any of this package's mobs that are outside
// EVERY currently-online player's 100x100 area. If nobody is online, it
// does nothing — mobs are left alone rather than mass-despawned the
// instant the server is empty. Pure position arithmetic, no world/block
// access, so this is cheap to run for every player every cycle even at
// high player counts — see SCALING in the package doc comment.
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
	if removed > 0 && spawnerDebug {
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

// chunkCoords returns the 16x16 chunk coordinates pos falls in.
func chunkCoords(pos mgl64.Vec3) (int, int) {
	return int(math.Floor(pos.X() / 16)), int(math.Floor(pos.Z() / 16))
}

// countAllMobsInChunk returns how many mobs from THIS package — passive
// (*Mob) and hostile (*HostileMob) combined — currently occupy the given
// chunk. Shared by both spawning.go (passive) and hostilespawning.go
// (hostile) so the two spawners enforce one combined per-chunk density
// limit instead of each unknowingly allowing up to perChunkMobCap of
// their own kind on top of whatever the other already placed there.
func countAllMobsInChunk(tx *world.Tx, cx, cz int) int {
	n := 0
	for e := range tx.Entities() {
		var pos mgl64.Vec3
		switch m := e.(type) {
		case *Mob:
			pos = m.Position()
		case *HostileMob:
			pos = m.Position()
		default:
			continue
		}
		px, pz := chunkCoords(pos)
		if px == cx && pz == cz {
			n++
		}
	}
	return n
}
