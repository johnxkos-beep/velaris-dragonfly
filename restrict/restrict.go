// Package restrict is a Go port of the original PMMP plugin's /restrict
// command, adapted to use the same 2-corner marker logic as the pvp
// package's /pvp block instead of the original's single-marker 100x100
// radius: an op runs /restrict, places 2 marker blocks at opposite
// corners, and the cuboid between them becomes a restrict zone that
// blocks everyone except ops from entering. Breaking either corner
// removes the zone.
package restrict

import (
	"encoding/json"
	"math"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
)

// MarkerBlock is the block used for restrict zone corners. The original
// PMMP plugin used black concrete; that's not available here for the same
// reason noted in the pvp package's MarkerBlock doc comment (v0.11.2 of
// Dragonfly doesn't expose server/block/colour at that path). A first
// attempt at a coal-block stand-in also failed to build ("undefined:
// block.CoalBlock" — that type doesn't exist in this Dragonfly version
// either), so rather than guess a third time, this reuses block.Obsidian
// — the exact same type the pvp package's marker already uses and has
// been confirmed to build successfully. The one downside: pvp and
// restrict zone corners now look identical in-world (both obsidian).
// Swap this to something else once you've confirmed a different block
// name actually compiles against this Dragonfly version — nothing else
// in this package needs to change.
func MarkerBlock() block.Obsidian { return block.Obsidian{} }

// isMarkerBlock reports whether b is the restrict marker block, checked
// by encoded block name — same EncodeBlock()-based approach as
// pvp.isMarkerBlock, for the same reason (doesn't need to know the
// block's exact struct shape).
func isMarkerBlock(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:obsidian"
}

// Zone is a restrict-enforced cuboid, defined by the two exact positions a
// player placed the marker blocks at (not sorted — the bounding box is
// computed on demand so the two corners can be placed in any
// order/orientation). Mirrors pvp.Zone exactly, just for a different
// purpose (blocking movement instead of gating combat).
//
// NOTE: like pvp.Zone, this isn't dimension-aware — see the NOTE on
// pvp.Zone in the pvp package for why (same Dragonfly Tx-access
// limitation applies here).
type Zone struct {
	Corner1 cube.Pos `json:"corner1"`
	Corner2 cube.Pos `json:"corner2"`
	Owner   string   `json:"owner"` // XUID of whoever placed the completing (second) corner
}

func (z Zone) bounds() (min, max cube.Pos) {
	min = cube.Pos{minInt(z.Corner1.X(), z.Corner2.X()), minInt(z.Corner1.Y(), z.Corner2.Y()), minInt(z.Corner1.Z(), z.Corner2.Z())}
	max = cube.Pos{maxInt(z.Corner1.X(), z.Corner2.X()), maxInt(z.Corner1.Y(), z.Corner2.Y()), maxInt(z.Corner1.Z(), z.Corner2.Z())}
	return
}

func (z Zone) contains(pos cube.Pos) bool {
	min, max := z.bounds()
	return pos.X() >= min.X() && pos.X() <= max.X() &&
		pos.Y() >= min.Y() && pos.Y() <= max.Y() &&
		pos.Z() >= min.Z() && pos.Z() <= max.Z()
}

// centre returns the middle of the zone on the X/Z plane, used to push a
// player outward when they were teleported straight into the zone rather
// than walking in (see HandleMove).
func (z Zone) centre() (x, z2 float64) {
	min, max := z.bounds()
	return float64(min.X()+max.X()) / 2, float64(min.Z()+max.Z()) / 2
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

// Config is the active restrict state: live zones and in-progress
// zone-corner selections. Safe for concurrent use from command handlers
// and player event handlers on other goroutines.
type Config struct {
	mu      sync.RWMutex
	path    string
	d       data
	pending map[string]*claim
}

// Cfg is the single active Config, set once in main() via restrict.Load
// before the server starts accepting players — same pattern as pvp.Cfg.
var Cfg *Config

// Load reads the restrict state from the JSON file at path, creating it
// with empty defaults if it doesn't exist yet. Call this once from main()
// before srv.Accept(), then assign the result to restrict.Cfg.
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
// one, completes the zone) and returns a message to show the player. ok
// is false for any placement that isn't part of an active selection —
// including a marker block placed with no claim pending, which is left to
// place as an ordinary decorative block.
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
	delete(c.pending, xuid)
	_ = c.save()
	return "§aRestricted zone created! Only ops can enter that area — break either corner block to remove it.", true
}

// OnBlockBreak should be called from PlayerHandler.HandleBlockBreak for
// every block break. If pos is a corner of an existing restrict zone,
// that zone is deleted. ok is false if pos wasn't a zone corner.
func (c *Config) OnBlockBreak(pos cube.Pos) (msg string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, z := range c.d.Zones {
		if z.Corner1 == pos || z.Corner2 == pos {
			c.d.Zones = append(c.d.Zones[:i], c.d.Zones[i+1:]...)
			_ = c.save()
			return "§eRestricted zone removed.", true
		}
	}
	return "", false
}

// zoneAt returns the zone containing pos, if any.
func (c *Config) zoneAt(pos cube.Pos) (Zone, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, zone := range c.d.Zones {
		if zone.contains(pos) {
			return zone, true
		}
	}
	return Zone{}, false
}

// HandleMove should be called from PlayerHandler.HandleMove. If newPos
// falls inside a restrict zone and p isn't an op, the move is cancelled
// and the player is bounced back out. A flat cancel with no push reads as
// the client fighting a wall (jittery/stuck), since it fights the
// client's own movement prediction with no give — the outward shove on
// top makes it read as a bounce instead, matching the original plugin's
// PlayerMoveEvent handling.
func (c *Config) HandleMove(ctx *player.Context, p *player.Player, newPos mgl64.Vec3) {
	to := cube.PosFromVec3(newPos)
	zone, blocked := c.zoneAt(to)
	if !blocked || state.Ops.IsOp(p.XUID()) {
		return
	}

	ctx.Cancel()

	from := p.Position()
	dx, dz := from.X()-newPos.X(), from.Z()-newPos.Z()
	length := math.Hypot(dx, dz)
	if length < 0.001 {
		// from and to coincide — e.g. the player was teleported straight
		// into the zone rather than walking in. Push away from the
		// zone's centre instead of leaving them with a zero-length,
		// useless vector.
		cx, cz := zone.centre()
		dx, dz = newPos.X()-cx, newPos.Z()-cz
		length = math.Hypot(dx, dz)
		if length < 0.001 {
			dx, dz, length = 0, 1, 1
		}
	}

	const strength = 0.6
	p.SetVelocity(mgl64.Vec3{(dx / length) * strength, 0.25, (dz / length) * strength})
	p.Message("§cThis area is restricted to ops.")
}
