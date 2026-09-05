// Package koth is a Go port of the HopliteLegendary PocketMine-MP plugin's
// King of the Hill system (hoplite\core\koth: KothManager, KothCommand,
// KothListener, KothTask) to Dragonfly.
//
// DEVIATION FROM THE PHP ORIGINAL — zone definition: the original detected
// a KOTH zone by having a builder hand-place (or /koth platform-stamp) an
// exact solid 15x15 square of RED CONCRETE, then flood-filling connected
// red concrete blocks to confirm the shape. That relies on PocketMine's
// block-colour API. This Dragonfly build (v0.11.4, per go.mod) has no
// confirmed "server/block/colour" style API for reading a placed block's
// dye colour back out — the pvp and restrict packages in this same repo
// already hit that exact wall and solved it by looking blocks up by their
// raw Bedrock ID string via world.BlockByName instead of a colour-typed
// struct (see pvp.MarkerBlock's doc comment). Rather than build a whole
// KOTH zone system on an unconfirmed colour API, this file follows suit:
// /koth block gives the executor 2 marker blocks (gold_block — thematically
// "king" — looked up by ID the same way restrict's diamond_block marker
// is), and placing both defines the zone as the cuboid between them,
// exactly like /pvp block and /restrict already do in this codebase.
// Breaking either corner removes the zone. This is a real behavior change
// (no more flood-fill / no more free-hand building a platform shape) but
// it reuses a pattern already proven to compile and work twice over in
// this repo, instead of a third guess at an unconfirmed block-colour API.
//
// Everything else is a fairly direct port: a 100x100 (radius 50) box
// around a zone's center protects it from non-op block breaks/places
// once it's named; /koth activate starts a capture timer (default 12
// minutes); holding the zone alone for a solid 10 minutes (600s) captures
// it instantly, or whoever has the most solo-control seconds when the
// timer runs out wins; a 20x20 area around an *active* zone's center
// forces PvP on regardless of the server's /pvp state (see
// players.HandleAttackEntity's integration point in INTEGRATION.md);
// campers get called out after 20 uncontested seconds; the zone's coords
// get re-broadcast every 2 minutes while active.
//
// DEVIATION — rewards: the PHP original deposited rewards into a custom
// AwardManager mailbox and included a custom PurgeToken item, neither of
// which exist in this Go codebase. See ticker.go's finish() for what's
// given instead (added straight to the winner's inventory) and why.
package koth

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// SquareSize is unused by the corner-marker zone model (see the package
// doc comment's DEVIATION note) but kept as a named constant for parity
// with the original's SQUARE_SIZE, in case a future flood-fill-based
// build ever wants it back.
const SquareSize = 15

// ProtectRadius mirrors KothManager::PROTECT_RADIUS — a named zone is
// protected from non-op block breaks/places within this many blocks of
// its center on both the X and Z axis (so a 100x100 box, not a circle).
const ProtectRadius = 50

// DurationSeconds mirrors KothManager::DURATION_SECONDS — how long an
// activated zone runs by default if /koth activate is given no explicit
// duration.
const DurationSeconds = 12 * 60

// CaptureSeconds mirrors KothManager::CAPTURE_SECONDS — holding a zone
// alone for this many seconds straight captures it instantly, before the
// zone's timer would otherwise run out.
const CaptureSeconds = 10 * 60

// CampingWarningSeconds mirrors KothManager::CAMPING_WARNING_SECONDS.
const CampingWarningSeconds = 20

// CoordReminderSeconds mirrors KothManager::COORD_REMINDER_SECONDS.
const CoordReminderSeconds = 120

// PvpZoneHalfSize mirrors KothManager::PVP_ZONE_HALF_SIZE — forced PvP
// applies within this many blocks of an *active* zone's center on each
// axis (so a 20x20 box), clamped down to the zone's own half-size if the
// zone itself is smaller than that.
const PvpZoneHalfSize = 10

// markerBlockName is the KOTH zone corner marker, looked up by its raw
// Bedrock ID rather than a Go struct type — same "doesn't depend on this
// library's internal naming" reasoning as pvp.MarkerBlock/
// restrict.MarkerBlock's doc comments. Gold block doubles as a fitting
// "king of the hill" visual and is a plain block with no colour/variant
// property, so a nil properties argument to world.BlockByName is enough.
const markerBlockName = "minecraft:gold_block"

// MarkerBlock returns the block used for KOTH zone corners, or false if
// it isn't registered under markerBlockName in this Dragonfly version.
func MarkerBlock() (world.Block, bool) { return world.BlockByName(markerBlockName, nil) }

func isMarkerBlock(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == markerBlockName
}

// Zone is one named KOTH zone, defined by the two exact positions a
// player placed the marker blocks at (not sorted — bounds are computed on
// demand so corners can be placed in either order). Mirrors pvp.Zone/
// restrict.Zone's shape.
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

// center returns the zone's X/Z center and its top Y (for messages and
// the forced-PvP box), plus the X/Z half-extents (for clamping the
// forced-PvP box to a small zone — see PvpZoneHalfSize).
func (z Zone) center() (cx, cz float64, topY int, halfX, halfZ float64) {
	min, max := z.bounds()
	cx = float64(min.X()+max.X()) / 2
	cz = float64(min.Z()+max.Z()) / 2
	topY = max.Y()
	halfX = float64(max.X()-min.X()) / 2
	halfZ = float64(max.Z()-min.Z()) / 2
	return
}

func (z Zone) containsXZ(x, zc int) bool {
	min, max := z.bounds()
	return x >= min.X() && x <= max.X() && zc >= min.Z() && zc <= max.Z()
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

// claim tracks an in-progress /koth block corner selection for one
// player, same pattern as pvp.claim/restrict.claim.
type claim struct {
	hasFirst bool
	first    cube.Pos
}

// activeState is a currently-running capture, keyed by zone name in
// Config.active. Mirrors the "active" array entries built in
// KothManager::activate().
type activeState struct {
	zone               Zone
	start              time.Time
	end                time.Time
	progress           map[string]int  // player name -> seconds of sole control
	campWarned         map[string]bool // player name -> already warned for this uncontested streak
	lastCoordBroadcast time.Time
}

// data is everything persisted to koth.json.
type data struct {
	Zones map[string]Zone `json:"zones"`
}

// Config is the active KOTH state: named zones, in-progress corner
// selections, and live captures. Safe for concurrent use from command
// handlers, player event handlers, and the per-second ticker.
type Config struct {
	mu      sync.RWMutex
	path    string
	d       data
	pending map[string]*claim // xuid -> in-progress corner selection

	// pendingUnnamed holds completed-but-not-yet-named zones, most recent
	// last — mirrors KothManager::$pendingUnnamed. /koth name pops the
	// last one off.
	pendingUnnamed []Zone

	active map[string]*activeState // zone name -> live capture
}

// Cfg is the single active Config, set once in main() via koth.Load
// before the server starts accepting players — same pattern as
// pvp.Cfg/restrict.Cfg/knockback.Cfg.
var Cfg *Config

// Load reads the KOTH state from the JSON file at path, creating it with
// empty defaults if it doesn't exist yet. Call this once from main()
// before srv.Accept(), then assign the result to koth.Cfg.
func Load(path string) (*Config, error) {
	c := &Config{
		path:    path,
		d:       data{Zones: map[string]Zone{}},
		pending: map[string]*claim{},
		active:  map[string]*activeState{},
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
	if c.d.Zones == nil {
		c.d.Zones = map[string]Zone{}
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

// BeginClaim starts (or silently restarts) a /koth block corner
// selection for xuid.
func (c *Config) BeginClaim(xuid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[xuid] = &claim{}
}

// HasPendingClaim reports whether xuid already has an unfinished /koth
// block corner selection in progress.
func (c *Config) HasPendingClaim(xuid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.pending[xuid]
	return ok
}

// OnBlockPlace should be called from PlayerHandler.HandleBlockPlace for
// every block placement. If xuid has an active /koth block selection and
// just placed a marker block, this records the corner (or, on the second
// one, completes the zone and queues it for naming) and returns a
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
		return "§aFirst KOTH zone corner set. Place the second block to complete the zone.", true
	}

	zone := Zone{Corner1: cl.first, Corner2: pos, Owner: xuid}
	c.pendingUnnamed = append(c.pendingUnnamed, zone)
	delete(c.pending, xuid)
	return "§aKOTH zone shape complete! Name it with §e/koth name <id>", true
}

// OnBlockBreak should be called from PlayerHandler.HandleBlockBreak for
// every block break. If pos is a corner of an existing named KOTH zone,
// that zone is deleted (and its active capture, if any, is cancelled with
// no winner). ok is false if pos wasn't a zone corner.
func (c *Config) OnBlockBreak(pos cube.Pos) (msg string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, z := range c.d.Zones {
		if z.Corner1 == pos || z.Corner2 == pos {
			delete(c.d.Zones, name)
			delete(c.active, name)
			_ = c.save()
			return "§eKOTH zone \"" + name + "\" removed.", true
		}
	}
	return "", false
}

// IsProtected reports whether (x, z) falls within ProtectRadius of any
// *named* zone's center (active or not) — port of
// KothManager::findProtectionZoneAt, minus the returned zone data (the
// only caller needs a yes/no).
func (c *Config) IsProtected(x, z int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, zone := range c.d.Zones {
		cx, cz, _, _, _ := zone.center()
		if absF(float64(x)-cx) <= ProtectRadius && absF(float64(z)-cz) <= ProtectRadius {
			return true
		}
	}
	return false
}

// IsZoneCorner reports whether pos is a corner of any named zone — used
// to exempt a zone's own marker blocks from the protection check in
// IsProtected, mirroring how the original only cancels *placement* inside
// a protection zone but always allows breaking the zone's own corners
// (that's what deletes it; see OnBlockBreak).
func (c *Config) IsZoneCorner(pos cube.Pos) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, z := range c.d.Zones {
		if z.Corner1 == pos || z.Corner2 == pos {
			return true
		}
	}
	return false
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// ForcedPvp reports whether PVP should be force-enabled at (x, z)
// regardless of the server's own pvp.Cfg.Enabled() state, because it's
// within PvpZoneHalfSize of an *active* KOTH zone's center — port of
// KothManager::isInActivePvpZone. Wire this into
// players.HandleAttackEntity alongside its existing pvp.Cfg.CombatAllowed
// check — see INTEGRATION.md.
func (c *Config) ForcedPvp(x, z int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, state := range c.active {
		cx, cz, _, halfX, halfZ := state.zone.center()
		hx := minF(PvpZoneHalfSize, halfX)
		hz := minF(PvpZoneHalfSize, halfZ)
		if absF(float64(x)-cx) <= hx && absF(float64(z)-cz) <= hz {
			return true
		}
	}
	return false
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// NameLatestPending names the most recently completed-but-unnamed zone —
// port of KothManager::nameLatestPending. Returns false if there's no
// pending zone waiting, or if name is already taken.
func (c *Config) NameLatestPending(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pendingUnnamed) == 0 {
		return false
	}
	if _, taken := c.d.Zones[name]; taken {
		return false
	}
	zone := c.pendingUnnamed[len(c.pendingUnnamed)-1]
	c.pendingUnnamed = c.pendingUnnamed[:len(c.pendingUnnamed)-1]
	c.d.Zones[name] = zone
	_ = c.save()
	return true
}

// HasZone reports whether a zone with this name exists.
func (c *Config) HasZone(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.d.Zones[name]
	return ok
}

// ZoneNames returns every named zone, in no particular order.
func (c *Config) ZoneNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.d.Zones))
	for n := range c.d.Zones {
		out = append(out, n)
	}
	return out
}

// ActiveZoneNames returns every currently-active zone name.
func (c *Config) ActiveZoneNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.active))
	for n := range c.active {
		out = append(out, n)
	}
	return out
}

// coordsMessage mirrors KothManager::coordsMessage.
func coordsMessage(z Zone) string {
	cx, cz, topY, _, _ := z.center()
	return "§7X: §f" + strconv.Itoa(int(cx)) + "§7, Y: §f" + strconv.Itoa(topY+1) + "§7, Z: §f" + strconv.Itoa(int(cz))
}

// Activate starts a capture on a named zone — port of
// KothManager::activate. broadcast is called with the announcement text
// (caller loops state.Server.Players(tx) and messages each one — see
// koth/command.go's Activate.Run and ticker.go's Tick, which both need a
// *world.Tx to do that and this package has no ambient one to use
// itself). Returns false if the zone doesn't exist or is already active.
func (c *Config) Activate(name string, durationSeconds int) (announcement string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	zone, exists := c.d.Zones[name]
	if !exists {
		return "", false
	}
	if _, already := c.active[name]; already {
		return "", false
	}
	if durationSeconds <= 0 {
		durationSeconds = DurationSeconds
	}
	now := time.Now()
	c.active[name] = &activeState{
		zone:               zone,
		start:              now,
		end:                now.Add(time.Duration(durationSeconds) * time.Second),
		progress:           map[string]int{},
		campWarned:         map[string]bool{},
		lastCoordBroadcast: now,
	}

	announcement = "§6§l[KOTH] §r§e\"" + name + "\" is now active! Hold the point for 10 minutes straight or have the most control after " + formatDuration(durationSeconds) + " to win.\n" +
		"§6[KOTH] §r" + coordsMessage(zone)
	return announcement, true
}

// SetRemainingTime sets the remaining time (from now) on an already-active
// zone — port of KothManager::setRemainingTime.
func (c *Config) SetRemainingTime(name string, seconds int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.active[name]
	if !ok {
		return false
	}
	state.end = time.Now().Add(time.Duration(seconds) * time.Second)
	return true
}

// ParseDuration parses durations like "10min", "10m", "600s", "600sec",
// "1h", or a bare number of seconds ("600") — direct port of
// KothManager::parseDuration. Returns false if it can't be parsed.
var durationPattern = regexp.MustCompile(`^(\d+)\s*([a-z]*)$`)

func ParseDuration(raw string) (int, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	m := durationPattern.FindStringSubmatch(raw)
	if m == nil {
		return 0, false
	}
	amount, err := strconv.Atoi(m[1])
	if err != nil || amount <= 0 {
		return 0, false
	}
	var multiplier int
	switch m[2] {
	case "", "s", "sec", "secs", "second", "seconds":
		multiplier = 1
	case "m", "min", "mins", "minute", "minutes":
		multiplier = 60
	case "h", "hr", "hrs", "hour", "hours":
		multiplier = 3600
	default:
		return 0, false
	}
	return amount * multiplier, true
}

func formatDuration(seconds int) string {
	if seconds%60 == 0 {
		return strconv.Itoa(seconds/60) + "min"
	}
	return strconv.Itoa(seconds) + "s"
}
