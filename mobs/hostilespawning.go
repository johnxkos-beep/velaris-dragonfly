// Ambient/natural spawning for the four hostile mobs in this package,
// deliberately mirroring spawning.go's passive-mob spawner as closely as
// possible: same 100x100-per-player area (areaHalfSize), same interval
// (spawnerIntervalTicks), same per-attempt chance (spawnAttemptChance)
// and attempt count (spawnAttemptsPerCycle), same area/global population
// caps (areaMobCap/globalMobCap), same ground-validity check
// (validSpawnGround/findGroundNear), and the same "re-home an invisible
// ticker entity to a live player every tick so its own chunk never
// unloads" trick. All of those constants/helpers are reused directly
// from spawning.go (same package) rather than copy-pasted, so the two
// spawners can't drift out of sync on tuning by accident.
//
// WHAT'S DIFFERENT FROM THE PASSIVE SPAWNER, ON PURPOSE:
//
//   - NIGHT ONLY: gated on !isDaytime(tx) instead of isDaytime(tx).
//     Zombies/skeletons/spiders/creepers only naturally spawn while the
//     world clock is in the night part of the cycle, matching vanilla.
//     (Any that are still alive when day breaks don't despawn from this
//     — the undead ones burn to death in direct sun instead, see
//     tickSunlightBurn in hostilemob.go. Spiders/creepers just persist
//     until they wander out of every player's area.)
//
//   - ONE-PASS AREA COUNTING FOR 100-PLAYER SAFETY: the passive spawner
//     calls countMobsInArea (an O(total mobs) scan) once per spawn
//     ATTEMPT — up to spawnAttemptsPerCycle times per player, so with a
//     full server that's up to 300 separate O(mobs) scans every
//     interval just for count-checking, before any actual spawning
//     happens. That's fine at low player counts but scales badly
//     towards 100 players. This spawner instead does ONE combined pass
//     over tx.Entities() per interval (buildAreaCounts) that both runs
//     the despawn sweep AND tallies how many hostile mobs are already in
//     each online player's area, then the attempt loop below just reads
//     and increments that in-memory map instead of re-scanning entities
//     per attempt. Same spawn/despawn behaviour and rate as the passive
//     spawner, just computed once instead of redundantly.
package mobs

import (
	"fmt"
	"math"
	"math/rand"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EnsureHostileSpawner spawns the single hostile ambient-spawn ticker
// entity the first time it's needed. Safe to call repeatedly; only
// spawns once. Call it from the same site as mobs.EnsureSpawner (right
// after a player joins) — see main.go.
func EnsureHostileSpawner(tx *world.Tx, near mgl64.Vec3) {
	hostileSpawnerMu.Lock()
	if hostileSpawnerSpawned {
		hostileSpawnerMu.Unlock()
		return
	}
	hostileSpawnerSpawned = true
	hostileSpawnerMu.Unlock()

	handle := world.EntitySpawnOpts{Position: near}.New(HostileSpawnerType, hostileSpawnerConfig{})
	tx.AddEntity(handle)
	fmt.Printf("[mobs] hostile ambient spawner started near (%.1f, %.1f, %.1f)\n", near.X(), near.Y(), near.Z())
}

var (
	hostileSpawnerMu      sync.Mutex
	hostileSpawnerSpawned bool
)

// HostileSpawnerType is the entity type for the invisible night-spawn ticker.
var HostileSpawnerType hostileSpawnerType

type hostileSpawnerType struct{}

func (hostileSpawnerType) EncodeEntity() string        { return "velaris:hostile_mob_spawner" }
func (hostileSpawnerType) BBox(world.Entity) cube.BBox  { return spawnerBBox } // reuses passive spawner's zero-size bbox
func (hostileSpawnerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &hostileSpawner{tx: tx, handle: handle, data: data}
}
func (hostileSpawnerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (hostileSpawnerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

type hostileSpawnerConfig struct{}

func (hostileSpawnerConfig) Apply(data *world.EntityData) {}

type hostileSpawner struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (s *hostileSpawner) H() *world.EntityHandle  { return s.handle }
func (s *hostileSpawner) Position() mgl64.Vec3    { return s.data.Pos }
func (s *hostileSpawner) Rotation() cube.Rotation { return s.data.Rot }
func (s *hostileSpawner) Close() error            { return nil }

// Tick re-homes to a live player every tick (same reasoning as the
// passive spawner), then runs one combined despawn+count sweep and the
// night-only spawn attempts every spawnerIntervalTicks.
func (s *hostileSpawner) Tick(tx *world.Tx, current int64) {
	if p, ok := anyPlayer(tx); ok {
		s.data.Pos = p.Position()
	}

	if current%spawnerIntervalTicks != 0 {
		return
	}

	areaCounts, total := sweepHostileMobs(tx)

	if isDaytime(tx) {
		return
	}
	if total >= globalMobCap {
		return
	}

	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		for i := 0; i < spawnAttemptsPerCycle; i++ {
			if total >= globalMobCap {
				return
			}
			if rand.Float64() > spawnAttemptChance {
				continue
			}
			if attemptHostileSpawn(tx, p, areaCounts) {
				total++
			}
		}
	}
}

// sweepHostileMobs does ONE pass over every entity in tx: it removes any
// hostile mob that's outside every online player's 100x100 area (same
// despawn rule as the passive spawner), and tallies how many surviving
// hostile mobs land in each online player's area. The returned map and
// total are then used by the spawn-attempt loop instead of re-scanning
// entities per attempt — see the file doc comment for why this matters
// at 100 players.
func sweepHostileMobs(tx *world.Tx) (areaCounts map[*player.Player]int, total int) {
	var players []*player.Player
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			players = append(players, p)
		}
	}
	areaCounts = make(map[*player.Player]int, len(players))
	for _, p := range players {
		areaCounts[p] = 0
	}

	if len(players) == 0 {
		// Nobody online: nothing to despawn against, nothing to count.
		// Mirrors the passive spawner's despawnOutOfRange, which also
		// leaves mobs alone rather than mass-despawning an empty server.
		return areaCounts, 0
	}

	removed := 0
	for e := range tx.Entities() {
		m, ok := e.(*HostileMob)
		if !ok {
			continue
		}
		pos := m.Position()

		inRange := false
		for _, p := range players {
			center := p.Position()
			if math.Abs(pos.X()-center.X()) <= areaHalfSize && math.Abs(pos.Z()-center.Z()) <= areaHalfSize {
				inRange = true
				areaCounts[p]++
				// Deliberately don't break: a mob can sit in more than
				// one online player's overlapping area, and each of
				// those players' local caps should see it — matches how
				// countMobsInArea would count it for each player
				// separately if called per-player like the passive
				// spawner does.
			}
		}
		if !inRange {
			tx.RemoveEntity(m)
			removed++
			continue
		}
		total++
	}

	if removed > 0 {
		fmt.Printf("[mobs] despawned %d hostile mob(s) that fell outside every player's area\n", removed)
	}
	return areaCounts, total
}

// attemptHostileSpawn tries one random night-spawn somewhere inside p's
// current area, using (and incrementing) the precomputed areaCounts
// instead of re-scanning entities. Returns true if a mob was spawned.
func attemptHostileSpawn(tx *world.Tx, p *player.Player, areaCounts map[*player.Player]int) bool {
	if areaCounts[p] >= areaMobCap {
		return false
	}

	pp := p.Position()
	candidate := mgl64.Vec3{
		pp.X() + (rand.Float64()*2-1)*areaHalfSize,
		pp.Y(),
		pp.Z() + (rand.Float64()*2-1)*areaHalfSize,
	}

	pos, ok := findGroundNear(tx, candidate, spawnGroundSearchRange)
	if !ok {
		return false
	}

	var kind string
	switch rand.Intn(4) {
	case 0:
		SpawnZombie(tx, pos)
		kind = "zombie"
	case 1:
		SpawnSkeleton(tx, pos)
		kind = "skeleton"
	case 2:
		SpawnSpider(tx, pos)
		kind = "spider"
	default:
		SpawnCreeper(tx, pos)
		kind = "creeper"
	}
	areaCounts[p]++
	fmt.Printf("[mobs] natural spawn: %s near %s at (%.1f, %.1f, %.1f)\n", kind, p.Name(), pos.X(), pos.Y(), pos.Z())
	return true
}
