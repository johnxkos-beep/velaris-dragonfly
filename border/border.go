// Package border implements a square world border centered on the origin
// (0,0). Go port of HopliteLegendary's border subsystem (PMMP/PHP):
// BorderManager.php, BorderCommand.php, BorderListener.php, and
// BorderParticleTask.php.
//
// DEVIATION FROM THE PHP ORIGINAL (enforcement mechanism): the PHP
// version enforced the border by cancelling PlayerMoveEvent and calling
// setMotion() on every offending move — see BorderListener.php's onMove().
// That exact shape (cancel a move-style event, then push the player) is
// documented in this repo's restrict package (see restrict.go's
// package-level doc comment) as the one thing that reliably crashes
// players with a "Block"/ClientDisconnection-90 error on this Dragonfly
// version: three separate attempts there — a velocity shove, a
// teleport-back, and finally a bare ctx.Cancel() with no shove at all —
// all reproduced the identical disconnect, even with no other
// player-touching code involved. So instead of touching player movement
// from a HandleMove-style event handler, border reuses koth's/restrict's
// ticker pattern: a single always-on invisible entity whose Tick(tx, ...)
// call — which Dragonfly hands a genuine, currently-valid *world.Tx — 
// scans online players several times a second and, for anyone outside or
// near the edge, calls p.SetVelocity/p.SendTitle from inside that Tick
// call instead. That's the same code path legendary/abilities.go's OnUse
// and bosses/enderdragon's crystal already use successfully for exactly
// this kind of Player mutation (see abilities.go's OnUse doc comment,
// which traces the crash to intercepting the move event itself, not to
// SetVelocity/SendTitle as such). See ticker.go for the actual scan.
//
// A real perimeter wall — the way restrict.go enforces its zones — was
// considered and rejected for enforcement here: a 4000-wide border's
// perimeter alone is wallMaxY-wallMinY+1 (384) blocks tall by 4*4000
// blocks around, well over 6 million barrier blocks for one edge length,
// versus restrict's explicit maxWallDimension=150 cap that exists for
// exactly this cost reason. The push-back approach in ticker.go has no
// such ceiling and matches the original's behavior much more closely
// anyway (a bounce back inward, not a solid wall you can see through).
//
// NOT ported: BorderParticleTask.php's per-tick redstone-dust curtain
// along the edge. This pinned Dragonfly version's server/world/particle
// package is only confirmed (elsewhere in this repo) to expose
// particle.HugeExplosion{}-style effect particles, not a redstone/dust
// particle by a known name — every dust-like struct name in that package
// would be a guess. This repo's own history (see the
// pocketmine-stack notes on documentation-based API guesses being wrong,
// and markerBlockName/barrierBlockName's doc comments in restrict.go) is
// that guessed API names cost more debugging time than they save. The
// action-bar warning in ticker.go covers the same "you're near the edge"
// purpose as the curtain did; the visual curtain can be added once the
// real particle type name is confirmed for this build (e.g. by running
// `go doc github.com/df-mc/dragonfly/server/world/particle` against the
// actual vendored/downloaded module on the build machine).
package border

import (
	"encoding/json"
	"math"
	"os"
	"sync"
)

const (
	// defaultSize is the full width/depth of the border, centered on
	// (0,0) — same default and same "centered on origin" shape as
	// BorderManager.php's $size (4000) and distanceToEdge()'s abs(x)/
	// abs(z) math.
	defaultSize = 4000

	// warnDistance mirrors BorderManager::WARN_DISTANCE.
	warnDistance = 50.0

	// minSize mirrors BorderCommand.php's minimum-size check
	// ("Border size must be at least 100.").
	minSize = 100
)

// data is everything persisted to border.json — mirrors BorderManager's
// own border.json ({"size": ...}) so an existing PMMP border.json can be
// dropped in as-is if you want to carry over a previously-set size.
type data struct {
	Size int `json:"size"`
}

// Config is the active border state. Cfg is the single instance, set once
// in main() via Load before the server starts accepting players — same
// pattern as restrict.Cfg/koth.Cfg/pvp.Cfg elsewhere in this repo.
type Config struct {
	mu   sync.RWMutex
	path string
	size int
}

var Cfg *Config

// Load reads border.json from path, creating it with the default size if
// it doesn't exist yet — port of BorderManager's constructor + load().
func Load(path string) (*Config, error) {
	c := &Config{path: path, size: defaultSize}

	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, c.save()
	}
	if err != nil {
		return nil, err
	}
	var d data
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	// Mirrors BorderManager::load()'s isset($json["size"]) check —
	// only trust a positive size from disk, otherwise keep the default.
	if d.Size > 0 {
		c.size = d.Size
	}
	return c, nil
}

func (c *Config) save() error {
	b, err := json.MarshalIndent(data{Size: c.size}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0644)
}

// Size returns the current full width/depth of the border — port of
// BorderManager::getSize().
func (c *Config) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.size
}

// SetSize changes the border size and persists it — port of
// BorderManager::setSize().
func (c *Config) SetSize(size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.size = size
	_ = c.save()
}

// halfSize is BorderManager::getHalfSize() (unexported here — nothing
// outside this package needs it directly; DistanceToEdge/Clamp already
// wrap it).
func (c *Config) halfSize() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return float64(c.size) / 2
}

// DistanceToEdge returns the distance in blocks from (x,z) to the
// nearest border edge. Negative once already outside — exact port of
// BorderManager::distanceToEdge().
func (c *Config) DistanceToEdge(x, z float64) float64 {
	half := c.halfSize()
	distX := half - math.Abs(x)
	distZ := half - math.Abs(z)
	if distX < distZ {
		return distX
	}
	return distZ
}

// IsInside reports whether (x,z) is inside the border — port of
// BorderManager::isInsideBorder().
func (c *Config) IsInside(x, z float64) bool {
	return c.DistanceToEdge(x, z) >= 0
}

// ShouldWarn reports whether (x,z) is inside the border but close enough
// to the edge to warrant the action-bar warning — port of
// BorderManager::shouldWarn().
func (c *Config) ShouldWarn(x, z float64) bool {
	d := c.DistanceToEdge(x, z)
	return d >= 0 && d <= warnDistance
}

// Clamp returns (x,z) clamped to just inside the border — port of
// BorderManager::clamp(). Kept for parity with the PHP original, though
// note that original never actually called it either (BorderListener.php
// pushes players back with a velocity bounce instead of clamping their
// coordinates directly — see ticker.go's pushBackInside, the equivalent
// this port uses for the same purpose).
func (c *Config) Clamp(x, z float64) (float64, float64) {
	half := c.halfSize() - 1
	clampOne := func(v float64) float64 {
		if v > half {
			return half
		}
		if v < -half {
			return -half
		}
		return v
	}
	return clampOne(x), clampOne(z)
}
