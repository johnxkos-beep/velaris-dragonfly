// Package restrict is a Go port of the original PMMP plugin's /restrict
// command, adapted to use the same 2-corner marker logic as the pvp
// package's /pvp block instead of the original's single-marker 100x100
// radius: an op runs /restrict, places 2 marker blocks at opposite
// corners, and the cuboid between them becomes a restrict zone.
//
// Enforcement is real barrier blocks around the cuboid's outer shell, not
// server-side movement interception — see the package-level history note
// above HandleMove... except there is no HandleMove anymore. Three
// attempts at blocking movement from Go code (a velocity shove, a
// teleport-back, and finally a bare ctx.Cancel() with no shove at all)
// all produced the identical symptom: the affected player's client
// silently disconnected with a "Block"/ClientDisconnection-90 error and
// nothing in the server log. Since that happened even with a plain
// Cancel() and no other player-touching code involved, the trigger looks
// to be "server repeatedly rejects this player's movement packets" in
// general, not any specific mutating call. Real barrier blocks sidestep
// the whole problem: collision becomes the client's own game engine
// doing ordinary wall physics, with no server intervention on a
// per-movement-packet basis at all.
package restrict

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MarkerBlock is the block used for restrict zone corners. The original
// PMMP plugin used black concrete; that's not available here for the same
// reason noted in the pvp package's MarkerBlock doc comment (v0.11.2 of
// Dragonfly doesn't expose server/block/colour at that path). Earlier
// versions of this file reused block.Obsidian — the same marker pvp uses
// — but that turned out to be a real bug, not just a cosmetic mismatch:
// pvp.Cfg.OnBlockPlace is checked before restrict.Cfg.OnBlockPlace in
// players.go's HandleBlockPlace, so if a player ever had an unfinished
// /pvp block claim sitting open (only one corner placed, never
// completed), every obsidian block they placed afterwards — including
// while testing /restrict — got silently swallowed by that leftover pvp
// claim before restrict's own handler ever saw it. Diamond block avoids
// the collision entirely, and — like barrierBlockName above — is looked
// up by its Bedrock ID string via world.BlockByName rather than a guessed
// Go struct name, for the same "can't drift with this library's internal
// naming" reason.
const markerBlockName = "minecraft:diamond_block"

// MarkerBlock returns the block used for restrict zone corners, or false
// if it isn't registered under markerBlockName in this Dragonfly version.
func MarkerBlock() (world.Block, bool) { return world.BlockByName(markerBlockName, nil) }

// isMarkerBlock reports whether b is the restrict marker block, checked
// by encoded block name — same EncodeBlock()-based approach as
// pvp.isMarkerBlock, for the same reason (doesn't need to know the
// block's exact struct shape).
func isMarkerBlock(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == markerBlockName
}

// barrierBlockName is looked up at runtime via world.BlockByName rather
// than referenced as a Go struct type (e.g. block.Barrier{}) — after
// guessing wrong on block struct names twice already for this repo's
// pinned Dragonfly version (server/block/colour, then block.CoalBlock),
// looking it up by its Bedrock block ID string is the one approach that
// doesn't depend on knowing this version's exact Go API surface: the ID
// "minecraft:barrier" is part of the game's wire protocol, not this
// library's internal naming, so it can't have drifted the same way.
const barrierBlockName = "minecraft:barrier"

func barrierBlock() (world.Block, bool) {
	return world.BlockByName(barrierBlockName, nil)
}

// Zone is a restrict-enforced cuboid, defined by the two exact positions a
// player placed the marker blocks at (not sorted — the bounding box is
// computed on demand so the two corners can be placed in any
// order/orientation). Mirrors pvp.Zone's shape, but additionally gets a
// physical wall of barrier blocks around it — see wallPositions and the
// enforcer type below.
//
// NOTE: like pvp.Zone, this isn't dimension-aware — see the NOTE on
// pvp.Zone in the pvp package for why (same Dragonfly Tx-access
// limitation applies here).
type Zone struct {
	Corner1 cube.Pos `json:"corner1"`
	Corner2 cube.Pos `json:"corner2"`
	Owner   string   `json:"owner"` // XUID of whoever placed the completing (second) corner
}

// wallMinY/wallMaxY are the vertical extent of the wall — bedrock to the
// build height limit, independent of whatever Y the two corners were
// actually placed at. -64/319 is vanilla Bedrock overworld's height
// range; this world uses custom terrain generation (per project notes),
// so if its actual bounds differ, adjust these two constants — nothing
// else needs to change.
const (
	wallMinY = -64
	wallMaxY = 319
)

// wallPositions returns every block position that makes up the zone's
// perimeter wall: the outer edge of the corners' X/Z footprint, at every
// Y from wallMinY to wallMaxY — a full-height fence around the area, not
// a filled-in box.
//
// An earlier version built a "shell" of the full 3D bounding box instead
// (surface of the box between the corners' exact Y positions). That had
// a real bug on top of not being what was asked for: when both corners
// sit at the same Y — the natural way to mark out a building's footprint
// — min.Y() and max.Y() are equal, so the "is this the top or bottom
// face" check became true for every block at that height, filling the
// entire footprint solid and overwriting whatever was inside it. Using
// only X/Z for the footprint and a fixed, corner-Y-independent vertical
// range avoids that failure mode entirely — the interior is never
// touched, regardless of what height the corners were placed at.
//
// The two corner positions themselves are skipped so the marker blocks
// stay intact and breakable at their own height (that's what removes the
// zone); every other Y in that same X/Z column still gets a barrier, so
// this doesn't leave a climbable gap — a single missing block is too
// short for a player to fit through.
//
// maxWallDimension caps the X/Z footprint to avoid a pathological
// one-time cost if corners get placed absurdly far apart — a full-height
// wall is already wallMaxY-wallMinY+1 (384) blocks tall per column, so
// even a modest footprint adds up fast; 150 per axis is a generous
// ceiling for a hand-placed selection, not a realistic one to hit by
// accident, but shrink it if wall-building causes a noticeable tick
// hitch on a large zone.
const maxWallDimension = 150

func (z Zone) wallPositions() ([]cube.Pos, bool) {
	minX, maxX := minInt(z.Corner1.X(), z.Corner2.X()), maxInt(z.Corner1.X(), z.Corner2.X())
	minZ, maxZ := minInt(z.Corner1.Z(), z.Corner2.Z()), maxInt(z.Corner1.Z(), z.Corner2.Z())
	if maxX-minX > maxWallDimension || maxZ-minZ > maxWallDimension {
		return nil, false
	}
	var out []cube.Pos
	for x := minX; x <= maxX; x++ {
		for zc := minZ; zc <= maxZ; zc++ {
			onPerimeter := x == minX || x == maxX || zc == minZ || zc == maxZ
			if !onPerimeter {
				continue
			}
			for y := wallMinY; y <= wallMaxY; y++ {
				pos := cube.Pos{x, y, zc}
				if pos == z.Corner1 || pos == z.Corner2 {
					continue
				}
				out = append(out, pos)
			}
		}
	}
	return out, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// claim tracks an in-progress /restrict corner selection for one player:
// the next 1-2 marker blocks they place complete it. Not persisted — if
// the server restarts mid-selection the player just needs to run
// /restrict again, which is an acceptable rare edge case.
type claim struct {
	hasFirst bool
	first    cube.Pos
}

// data is everything that gets persisted to restrict.json.
type data struct {
	Zones []Zone `json:"zones"`
}

// Config is the active restrict state: live zones, in-progress
// zone-corner selections, and queues of wall work for the enforcer entity
// to pick up on its next tick (see OnBlockPlace/OnBlockBreak and
// enforcer.Tick).
type Config struct {
	mu              sync.RWMutex
	path            string
	d               data
	pending         map[string]*claim
	pendingBuild    []Zone
	pendingClear    []Zone
	enforcerSpawned bool
}

// Cfg is the single active Config, set once in main() via restrict.Load
// before the server starts accepting players — same pattern as pvp.Cfg.
var Cfg *Config

// Load reads the restrict state from the JSON file at path, creating it
// with empty defaults if it doesn't exist yet. Call this once from main()
// before srv.Accept(), then assign the result to restrict.Cfg. Any zones
// loaded from a previous run are queued to have their walls (re)built on
// the enforcer's first tick, since barrier blocks aren't themselves
// persisted — only the corner coordinates are.
func Load(path string) (*Config, error) {
	c := &Config{path: path, pending: map[string]*claim{}}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, c.save()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &c.d); err != nil {
		return nil, err
	}
	c.pendingBuild = append(c.pendingBuild, c.d.Zones...)
	return c, nil
}

func (c *Config) save() error {
	b, err := json.MarshalIndent(c.d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0644)
}

// BeginClaim starts (or silently restarts) a /restrict corner selection
// for xuid: the next 1-2 marker blocks they place become the zone's
// corners. Called by the /restrict command, which is responsible for
// warning the player if this discards an unfinished previous selection —
// see HasPendingClaim.
func (c *Config) BeginClaim(xuid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[xuid] = &claim{}
}

// HasPendingClaim reports whether xuid already has an unfinished
// /restrict corner selection in progress.
func (c *Config) HasPendingClaim(xuid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.pending[xuid]
	return ok
}

// OnBlockPlace should be called from PlayerHandler.HandleBlockPlace for
// every block placement. If xuid has an active /restrict selection and
// just placed a marker block, this records the corner (or, on the second
// one, completes the zone and queues its walls to be built) and returns a
// message to show the player. ok is false for any placement that isn't
// part of an active selection.
func (c *Config) OnBlockPlace(xuid string, pos cube.Pos, b world.Block) (msg string, ok bool) {
	if !isMarkerBlock(b) {
		return "", false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cl, active := c.pending[xuid]
	if !active {
		return "", false
	}

	if !cl.hasFirst {
		cl.hasFirst = true
		cl.first = pos
		return "§aFirst restrict zone corner set. Place the second block to complete the zone.", true
	}

	zone := Zone{Corner1: cl.first, Corner2: pos, Owner: xuid}
	c.d.Zones = append(c.d.Zones, zone)
	c.pendingBuild = append(c.pendingBuild, zone)
	delete(c.pending, xuid)
	_ = c.save()
	return "§aRestricted zone created! Building the wall now — only ops can get in (use /tp — see /restrict's help). Break either corner block to remove it.", true
}

// OnBlockBreak should be called from PlayerHandler.HandleBlockBreak for
// every block break. If pos is a corner of an existing restrict zone,
// that zone is deleted and its walls are queued for clearing. ok is false
// if pos wasn't a zone corner.
func (c *Config) OnBlockBreak(pos cube.Pos) (msg string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, z := range c.d.Zones {
		if z.Corner1 == pos || z.Corner2 == pos {
			c.d.Zones = append(c.d.Zones[:i], c.d.Zones[i+1:]...)
			c.pendingClear = append(c.pendingClear, z)
			_ = c.save()
			return "§eRestricted zone removed. Clearing the wall now.", true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------
// Enforcer: a single, invisible, always-on ticker entity whose only job
// is building/clearing barrier-block walls, queued by OnBlockPlace and
// OnBlockBreak above. It never reads or mutates a *player.Player — pure
// world-block placement, which is a different code path from the
// player-touching operations that caused client disconnects in earlier
// versions of this file. Same Tick(tx)-gets-a-real-Tx pattern as
// legendary/eagleeye.go's eagleDrawTicker.
// ---------------------------------------------------------------------

// EnforcerType is the entity type for the invisible wall-building
// enforcer. Exactly one is ever spawned — see ensureEnforcer.
var EnforcerType enforcerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in main.go
// (mirrors legendary.EagleTypes()/ProjectileTypes()/HUDTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{EnforcerType} }

var enforcerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type enforcerType struct{}

func (enforcerType) EncodeEntity() string        { return "velaris:restrict_enforcer" }
func (enforcerType) BBox(world.Entity) cube.BBox { return enforcerBBox }
func (enforcerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &enforcer{tx: tx, handle: handle, data: data}
}
func (enforcerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (enforcerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// enforcerConfig is an empty EntitySpawnOpts config for EnforcerType,
// which needs no spawn-time configuration — mirrors demonking's own
// empty Config{} + no-op Apply for the same "no config needed" case.
type enforcerConfig struct{}

func (enforcerConfig) Apply(data *world.EntityData) {}

type enforcer struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *enforcer) H() *world.EntityHandle  { return e.handle }
func (e *enforcer) Position() mgl64.Vec3    { return e.data.Pos }
func (e *enforcer) Rotation() cube.Rotation { return e.data.Rot }
func (e *enforcer) Close() error            { return nil }

func (e *enforcer) Tick(tx *world.Tx, _ int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[restrict] recovered panic in enforcer.Tick: %v", r)
		}
	}()

	Cfg.mu.Lock()
	build := Cfg.pendingBuild
	Cfg.pendingBuild = nil
	clear := Cfg.pendingClear
	Cfg.pendingClear = nil
	Cfg.mu.Unlock()

	if len(build) == 0 && len(clear) == 0 {
		return
	}

	if len(build) > 0 {
		barrier, ok := barrierBlock()
		if !ok {
			log.Printf("[restrict] %q not found via world.BlockByName — walls not built for %d pending zone(s)", barrierBlockName, len(build))
		} else {
			log.Printf("[restrict] got barrier block %#v via world.BlockByName(%q) — building %d zone wall(s)", barrier, barrierBlockName, len(build))
			for _, z := range build {
				positions, sized := z.wallPositions()
				if !sized {
					log.Printf("[restrict] zone corners %v/%v too far apart (>%d blocks on an axis) — skipping wall build", z.Corner1, z.Corner2, maxWallDimension)
					continue
				}
				for _, pos := range positions {
					tx.SetBlock(pos, barrier, nil)
				}
				log.Printf("[restrict] placed %d barrier blocks for zone %v/%v", len(positions), z.Corner1, z.Corner2)
			}
		}
	}

	for _, z := range clear {
		positions, sized := z.wallPositions()
		if !sized {
			continue
		}
		for _, pos := range positions {
			tx.SetBlock(pos, nil, nil)
		}
	}
}

// ensureEnforcer spawns the single restrict-wall enforcer entity the
// first time it's needed — called from the /restrict command's Run,
// which is the first point after Load() that this package has a genuine
// *world.Tx handed to it. Safe to call repeatedly; only spawns once.
//
// near is where to spawn it — pass the calling player's own position,
// not a fixed world coordinate. An earlier version hardcoded {0,0,0};
// on a world using custom terrain generation, spawn/gameplay may be
// nowhere near true world origin, so that chunk could sit permanently
// unloaded for the whole server session — which means the entity gets
// created (confirmed by this function's own log line) but its Tick
// method, where all the actual wall-building happens, never once fires.
// That matches exactly what shipped: zones "created" successfully (that
// message comes straight from OnBlockPlace, no ticking required) but no
// wall ever appeared. Spawning next to the player running /restrict
// guarantees a currently-loaded, actively-simulated chunk instead.
func (c *Config) ensureEnforcer(tx *world.Tx, near mgl64.Vec3) {
	c.mu.Lock()
	if c.enforcerSpawned {
		c.mu.Unlock()
		return
	}
	c.enforcerSpawned = true
	c.mu.Unlock()

	handle := world.EntitySpawnOpts{Position: near}.New(EnforcerType, enforcerConfig{})
	tx.AddEntity(handle)
	log.Printf("[restrict] enforcer entity spawned at %v", near)
}
