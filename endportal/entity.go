// Package endportal implements a real, walk-through End portal system:
// a trigger region a player can physically step into, which teleports them
// to another registered world/destination and repositions them onto solid
// ground there (rather than wherever that world's saved spawn happens to
// be, which could be mid-air or underground relative to a specific target
// x/z).
//
// Dragonfly has no block-touch/"entity inside this block" event of its own
// (confirmed by restrict/restrict.go's own package doc — it tried three
// different approaches to intercepting player movement from Go code and
// all three broke the client connection). So, same as restrict's solution,
// this doesn't try to hook block interaction at all: it spawns an
// invisible, zero-size sentinel entity (identical pattern to
// restrict.enforcer) that Ticks every server tick and checks whether any
// player's position falls inside a registered box. That only works while
// the sentinel's own chunk is loaded and simulating — which is exactly the
// situation that matters, since a portal only needs to fire while someone
// is standing near it.
package endportal

import (
	"log"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
	dfworlds "velaris-dragonfly/worlds"
)

// retriggerCooldownTicks keeps a portal from firing again on the same
// player every single tick they stand in it (each teleport takes at least
// one tick to complete, so without this they'd bounce back and forth as
// soon as any nearby portal happened to be within range of the destination
// point too).
const retriggerCooldownTicks = 60 // ~3s

// Destination is where a portal sends players, and where to set them down
// once they arrive — LandX/LandZ, not the destination's saved spawn.
type Destination struct {
	World        string
	LandX, LandZ int
}

// EntityTypes returns the entity types this package adds, for merging into
// the server's entity registry in main.go — same pattern as
// restrict.EntityTypes()/mobs.EntityTypes()/enderdragon.EntityTypes().
func EntityTypes() []world.EntityType { return []world.EntityType{SentinelType} }

// SpawnSentinel creates a sentinel that watches the axis-aligned box
// [min,max] (inclusive, block coordinates) and teleports any player whose
// position falls inside it to dest. Spawned at the centre of that box, so
// its own chunk is exactly the one that needs to stay loaded for the
// portal to work — same reasoning as restrict's ensureEnforcer spawning
// next to the calling player instead of at a fixed/possibly-unloaded spot.
func SpawnSentinel(tx *world.Tx, min, max cube.Pos, dest Destination) {
	centre := mgl64.Vec3{
		float64(min.X()+max.X())/2 + 0.5,
		float64(min.Y()) + 0.5,
		float64(min.Z()+max.Z())/2 + 0.5,
	}
	handle := world.EntitySpawnOpts{Position: centre}.New(SentinelType, sentinelConfig{Min: min, Max: max, Dest: dest})
	tx.AddEntity(handle)
}

// SentinelType is the world.EntityType for the invisible portal-trigger
// entity. Register via EntityTypes() before starting the server.
var SentinelType sentinelType

type sentinelType struct{}

func (sentinelType) EncodeEntity() string        { return "velaris:portal_sentinel" }
func (sentinelType) BBox(world.Entity) cube.BBox { return cube.Box(0, 0, 0, 0, 0, 0) }
func (sentinelType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*sentinelState)
	if !ok || st == nil {
		st = &sentinelState{cooldowns: map[string]int{}}
		data.Data = st
	}
	return &sentinel{tx: tx, handle: handle, data: data, st: st}
}
func (sentinelType) DecodeNBT(m map[string]any, data *world.EntityData) {
	data.Data = &sentinelState{cooldowns: map[string]int{}}
}
func (sentinelType) EncodeNBT(data *world.EntityData) map[string]any { return map[string]any{} }

type sentinelConfig struct {
	Min, Max cube.Pos
	Dest     Destination
}

func (c sentinelConfig) Apply(data *world.EntityData) {
	data.Data = &sentinelState{Min: c.Min, Max: c.Max, Dest: c.Dest, cooldowns: map[string]int{}}
}

type sentinelState struct {
	Min, Max  cube.Pos
	Dest      Destination
	cooldowns map[string]int // player name -> ticks left before they can trigger this portal again
}

type sentinel struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	st     *sentinelState
}

func (s *sentinel) H() *world.EntityHandle  { return s.handle }
func (s *sentinel) Position() mgl64.Vec3    { return s.data.Pos }
func (s *sentinel) Rotation() cube.Rotation { return s.data.Rot }
func (s *sentinel) Close() error            { return nil }

func (s *sentinel) Tick(tx *world.Tx, _ int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[endportal] recovered panic in sentinel.Tick: %v", r)
		}
	}()

	st := s.st
	for name, left := range st.cooldowns {
		if left <= 1 {
			delete(st.cooldowns, name)
		} else {
			st.cooldowns[name] = left - 1
		}
	}

	for p := range state.Server.Players(tx) {
		pos := p.Position()
		if pos.X() < float64(st.Min.X()) || pos.X() > float64(st.Max.X())+1 {
			continue
		}
		if pos.Z() < float64(st.Min.Z()) || pos.Z() > float64(st.Max.Z())+1 {
			continue
		}
		if pos.Y() < float64(st.Min.Y())-1 || pos.Y() > float64(st.Max.Y())+2 {
			continue
		}
		if _, onCooldown := st.cooldowns[p.Name()]; onCooldown {
			continue
		}
		st.cooldowns[p.Name()] = retriggerCooldownTicks
		s.travel(tx, p)
	}
}

func (s *sentinel) travel(tx *world.Tx, p *player.Player) {
	dest := s.st.Dest
	err := state.Worlds.TravelPlayerTx(tx, p, dest.World, withAfter(func(tx2 *world.Tx, p2 *player.Player) error {
		landSafely(tx2, p2, dest.LandX, dest.LandZ)
		return nil
	}))
	if err != nil {
		p.Message("§cThe portal fizzles: " + err.Error())
	}
}

// withAfter builds a dfworlds.TravelOption setting the After hook directly
// — dfworlds only exports a WithSpawn/WithGameMode constructor pair, not
// one for Before/After, but TravelOptions.After is itself an exported
// field, so a small option func here reaches it the same way WithSpawn
// does internally.
func withAfter(hook dfworlds.TravelHook) dfworlds.TravelOption {
	return func(o *dfworlds.TravelOptions) { o.After = hook }
}

// landSafely scans straight down from a generous height at (x, z) in tx's
// world for the first solid block, then teleports p to stand on its
// surface. This is what keeps a portal from ever dropping someone inside
// solid ground or leaving them stranded in mid-air — it doesn't trust the
// destination's saved spawn point at all for this, since that's tuned for
// a specific spawn location, not necessarily near (x, z).
//
// UNVERIFIED scan bounds: 320 down to -64 is a generous guess covering
// most Bedrock height configurations rather than a confirmed value for
// this Dragonfly version's world height range. If a destination world is
// taller than that in the wrong direction, widen these.
func landSafely(tx *world.Tx, p *player.Player, x, z int) {
	const scanFrom, scanTo = 320, -64
	for y := scanFrom; y > scanTo; y-- {
		if _, air := tx.Block(cube.Pos{x, y, z}).(block.Air); !air {
			p.Teleport(mgl64.Vec3{float64(x) + 0.5, float64(y + 1), float64(z) + 0.5})
			return
		}
	}
	// Nothing solid found in the whole scanned range — leave the player
	// wherever the ordinary travel/spawn logic already placed them rather
	// than risk setting them adrift in the void on a bad scan.
}
