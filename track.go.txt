// Package track is a Go port of the PocketMine-MP HopliteLegendary
// plugin's /track command family — ported from
// hoplite/core/tracking/TrackCommand.php, TrackManager.php, and
// TrackListener.php — with one deliberate behavior change from the
// original, requested for this port specifically:
//
// The original's "/track block" gave the executor a green concrete
// block; placing it queued a pending point, and the next /track <name>
// they ran named whatever they'd just placed. That's gone here.
// Instead, "/track point <name>" (op only) directly names a point at
// the executor's own exact current position — no marker block, no
// placement step, no pending state. Naming is otherwise the same:
// points are shared server-wide (op-set once, anyone can query), and
// "/track <name>" (open to everyone, matching the original's
// hoplite.track "default: true") turns on a live action-bar HUD
// showing the distance to it, same as before. "/track off" stops it.
//
// Points aren't dimension-aware — stored as a bare mgl64.Vec3 with no
// world/dimension tag. This mirrors an existing, deliberate simplification
// already made twice in this codebase (see pvp.Zone's and restrict.Zone's
// matching NOTE comments): fine for a single-world server, and avoids
// depending on any world-identity API this project hasn't already needed
// elsewhere. Flag it if Velaris ever needs per-dimension track points.
package track

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
)

// data is everything persisted to track_points.json.
type data struct {
	// Points maps a point's name to its position. Shared across all
	// players - anyone can query any named point, e.g. one op marks
	// "spawn" and everyone can /track spawn. Mirrors
	// TrackManager::$points exactly, minus the "world" field (see the
	// package doc comment above).
	Points map[string]mgl64.Vec3 `json:"points"`
}

// Config is the active track state: persisted named points, plus which
// player (by XUID) is currently live-tracking which point - the latter
// is in-memory only, like TrackManager::$activeTracking, since it's a
// session toggle rather than a permanent record.
type Config struct {
	mu   sync.RWMutex
	path string
	d    data

	activeTracking map[string]string // xuid -> point name
}

// Cfg is the single active Config, set once in main() via track.Load
// before the server starts accepting players - same pattern as
// pvp.Cfg/restrict.Cfg.
var Cfg *Config

// Load reads track state from the JSON file at path, creating it with
// empty defaults if it doesn't exist yet. Call this once from main()
// before srv.Accept(), then assign the result to track.Cfg.
func Load(path string) (*Config, error) {
	c := &Config{
		path:           path,
		d:              data{Points: map[string]mgl64.Vec3{}},
		activeTracking: map[string]string{},
	}

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
	if c.d.Points == nil {
		c.d.Points = map[string]mgl64.Vec3{}
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

// SetPoint names (or renames the position of) a track point - port of
// TrackManager::name(), minus the world argument (see package doc
// comment).
func (c *Config) SetPoint(name string, pos mgl64.Vec3) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.d.Points[name] = pos
	_ = c.save()
}

// GetPoint returns a named point's position - port of TrackManager::get().
func (c *Config) GetPoint(name string) (mgl64.Vec3, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	pos, ok := c.d.Points[name]
	return pos, ok
}

// Exists reports whether a point by that name has been set - port of
// TrackManager::exists().
func (c *Config) Exists(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.d.Points[name]
	return ok
}

// ListNames returns every known point name - port of
// TrackManager::listNames(), used for the "no point named X" error
// message the same way TrackCommand's caller would.
func (c *Config) ListNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.d.Points))
	for name := range c.d.Points {
		names = append(names, name)
	}
	return names
}

// StartTracking turns on live distance tracking of pointName for the
// player identified by xuid - port of TrackManager::startTracking().
func (c *Config) StartTracking(xuid, pointName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeTracking[xuid] = pointName
}

// StopTracking turns off live distance tracking for xuid - port of
// TrackManager::stopTracking().
func (c *Config) StopTracking(xuid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.activeTracking, xuid)
}

// GetTracking returns the point name xuid is currently live-tracking, if
// any - port of TrackManager::getTrackingName().
func (c *Config) GetTracking(xuid string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	name, ok := c.activeTracking[xuid]
	return name, ok
}

// ActiveTrackers returns a snapshot copy of every xuid -> point name
// currently being live-tracked, for the ticker (ticker.go) to iterate
// over without holding Config's lock while it sends action bar messages.
func (c *Config) ActiveTrackers() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.activeTracking))
	for xuid, name := range c.activeTracking {
		out[xuid] = name
	}
	return out
}
