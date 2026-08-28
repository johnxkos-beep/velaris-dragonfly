// Package pvp is a Go port of the original PMMP PvP plugin's /pvp on,
// /pvp off, and /pvp block commands.
//
// /pvp on and /pvp off are a personal opt-in toggle — outside a PvP zone,
// two players can only fight if BOTH have PvP on. /pvp block replaces the
// old "place one orange concrete, get a 100-block radius zone around it"
// behaviour: it now gives the executor exactly 2 marker blocks. Placing
// the first marks corner 1; placing the second marks corner 2 and turns
// the cuboid between them into a PvP zone where combat is forced on
// regardless of anyone's personal toggle. Breaking either corner block
// deletes the zone.
package pvp

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// MarkerBlock is the block used for PvP zone corners. Originally this was
// meant to be orange concrete (matching the original PMMP plugin), but
// v0.11.2 of Dragonfly doesn't expose a "server/block/colour" package at
// the path that would've needed, so this uses plain obsidian instead — a
// solid, distinctive, zero-field block that doesn't depend on any
// colour/variant API that might drift between Dragonfly versions. Swap
// this one function (and the EncodeBlock() name check in isMarkerBlock
// below) out for something else if you'd rather use a different block —
// nothing else in this package needs to change.
func MarkerBlock() block.Obsidian { return block.Obsidian{} }

// isMarkerBlock reports whether b is the PvP marker block. Checked by
// encoded block name rather than a type assertion on block.Obsidian's
// fields, matching the same EncodeBlock()-based pattern already used
// elsewhere in this repo (see the "_display_block" check in
// players/players.go) — it only needs the name to exist and be stable,
// not any particular struct shape.
func isMarkerBlock(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:obsidian"
}

// Zone is a PvP-forced cuboid, defined by the two exact positions a player
// placed the marker blocks at (not sorted — the bounding box is computed
// on demand so the two corners can be placed in any order/orientation).
//
// NOTE: zones aren't dimension-aware — Dragonfly doesn't hand
// HandleBlockPlace/HandleBlockBreak a *world.Tx to read the dimension from
// safely (see the NOTE in players/autosmelt.go about Tx-after-transaction
// panics), so a zone at coordinates X,Y,Z technically also "covers" the
// same coordinates in every other dimension. Fine for a single-world
// server; flag it if Velaris ever needs nether/end PvP zones to be
// independent.
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

// claim tracks an in-progress /pvp block selection for one player: the
// next 1-2 marker blocks they place complete it. Not persisted — if the
// server restarts mid-selection the player just needs to run /pvp block
// again, which is an acceptable rare edge case.
type claim struct {
	hasFirst bool
	first    cube.Pos
}

// data is everything that gets persisted to pvp.json.
type data struct {
	Toggles map[string]bool `json:"toggles"` // XUID -> personal PvP opt-in
	Zones   []Zone          `json:"zones"`
}

// Config is the active PvP state: personal toggles, live zones, and
// in-progress zone-corner selections. Safe for concurrent use from command
// handlers and player event handlers on other goroutines.
type Config struct {
	mu      sync.RWMutex
	path    string
	d       data
	pending map[string]*claim
}

// Cfg is the single active Config, set once in main() via pvp.Load before
// the server starts accepting players — same pattern as knockback.Cfg /
// state.Ranks / state.Ops.
var Cfg *Config

// Load reads the PvP state from the JSON file at path, creating it with
// empty defaults if it doesn't exist yet. Call this once from main()
// before srv.Accept(), then assign the result to pvp.Cfg.
func Load(path string) (*Config, error) {
	c := &Config{path: path, d: data{Toggles: map[string]bool{}}, pending: map[string]*claim{}}

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
	if c.d.Toggles == nil {
		c.d.Toggles = map[string]bool{}
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

// Toggle reports whether xuid has personally opted in to PvP via
// /pvp on. Defaults to false (opted out) for anyone who hasn't run
// /pvp on or /pvp off yet — matches the original plugin's "safe by
// default" behaviour.
func (c *Config) Toggle(xuid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.d.Toggles[xuid]
}

// SetToggle sets xuid's personal PvP opt-in and persists it. Called by
// /pvp on and /pvp off.
func (c *Config) SetToggle(xuid string, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.d.Toggles[xuid] = on
	_ = c.save()
}

// BeginClaim starts (or silently restarts) a /pvp block corner selection
// for xuid: the next 1-2 marker blocks they place become the zone's
// corners. Called by the /pvp block command, which is responsible for
// warning the player if this discards an unfinished previous selection —
// see HasPendingClaim.
func (c *Config) BeginClaim(xuid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[xuid] = &claim{}
}

// HasPendingClaim reports whether xuid already has an unfinished /pvp
// block corner selection in progress.
func (c *Config) HasPendingClaim(xuid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.pending[xuid]
	return ok
}

// OnBlockPlace should be called from PlayerHandler.HandleBlockPlace for
// every block placement. If xuid has an active /pvp block selection and
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
		return "§aFirst PvP zone corner set. Place the second block to complete the zone.", true
	}

	zone := Zone{Corner1: cl.first, Corner2: pos, Owner: xuid}
	c.d.Zones = append(c.d.Zones, zone)
	delete(c.pending, xuid)
	_ = c.save()
	return "§aPvP zone created! Combat is forced on inside that area — break either corner block to remove it.", true
}

// OnBlockBreak should be called from PlayerHandler.HandleBlockBreak for
// every block break. If pos is a corner of an existing PvP zone, that zone
// is deleted. ok is false if pos wasn't a zone corner.
func (c *Config) OnBlockBreak(pos cube.Pos) (msg string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, z := range c.d.Zones {
		if z.Corner1 == pos || z.Corner2 == pos {
			c.d.Zones = append(c.d.Zones[:i], c.d.Zones[i+1:]...)
			_ = c.save()
			return "§cPvP zone removed.", true
		}
	}
	return "", false
}

// CombatAllowed decides whether attackerXUID may damage victimXUID, given
// the attacker's current block position (at). Combat is force-allowed
// anywhere inside a PvP zone regardless of personal toggles; outside a
// zone, both players must have personally opted in with /pvp on.
//
// NOTE: only the attacker's position is checked against zones, not the
// victim's — a hit thrown from just outside a zone boundary at a target
// standing inside it (or vice versa) is judged by the attacker's feet.
// Good enough for melee-range zones; flag it if ranged combat across a
// zone edge turns out to matter.
func (c *Config) CombatAllowed(attackerXUID, victimXUID string, at cube.Pos) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, z := range c.d.Zones {
		if z.contains(at) {
			return true
		}
	}
	return c.d.Toggles[attackerXUID] && c.d.Toggles[victimXUID]
}
