// Custom crosshair, replicating the add-on's own real mechanism — read
// directly from its script (scripts/main.js, the crosshair.ts feature),
// not guessed. Confirmed: this is the EXACT same "bar:<pct>" trick as the
// cooldown bar (see hud.go), just a different prefixed title string. The
// add-on keeps a map from weapon ID to a private-use-area Unicode symbol
// character, sends `crosshair:"<symbol>"` as a permanently-refreshed
// (1-tick stay, 0 fade) title every single tick, and the resource pack's
// ui/hud_screen.json intercepts titles starting with "crosshair:" and
// draws the matching crosshair graphic instead of the text — and REPLACES
// the vanilla crosshair entirely while doing so. That last part is why
// "no crosshair at all, even empty-handed" happened: the pack takes over
// crosshair rendering unconditionally, and since nothing was ever sending
// it a recognized "crosshair:X" title, it had nothing to draw, full stop
// — not even the default vanilla crosshair understood by the client.
//
// ARCHITECTURAL LIMITATION, STATED UP FRONT: this needs to run
// continuously for a player from the moment they join — but every
// confirmed-safe *world.Tx source in this codebase (item.Usable's Use,
// entity Tick) is tied to actually interacting with SOMETHING, not to
// joining the server. There's no proven-safe way in this codebase to spawn
// an entity (which needs tx.AddEntity) the instant a player connects —
// PlayerHandler's methods don't receive a Tx (same reason HandleItemUse
// couldn't be used for abilities), and the srv.Accept() loop in main.go
// has never been established as tx-safe for entity spawning. So: the
// crosshair ticker starts the first time OnUse fires for ANY legendary
// weapon (a proven-safe trigger point already used throughout this
// package) and then runs for the rest of that player's session. In
// practice, on a server centered on these weapons, most players will hit
// that within their first few actions — but a player who never touches a
// legendary weapon at all in a session won't get a custom crosshair
// either. If you find (or Dragonfly exposes) a safe way to get a Tx at
// join time, this is the one place that needs to change to fix that gap.
package legendary

import (
	"log"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// crosshairMap and defaultCrosshairSymbol are copied directly from the
// add-on's own CROSSHAIR_MAP/DEFAULT_SYMBOL — same private-use-area
// Unicode characters, not reinvented. Only the 8 weapon IDs actually in
// this server's roster are kept; the add-on's map also covered other
// items (villager wand, sculkweaver lantern, etc.) this repo doesn't have.
var crosshairMap = map[string]string{
	"bey:mjolnir":             "\uE211",
	"bey:poseidon_trident":    "\uE211",
	"bey:midas_sword":         "\uE212",
	"bey:shadow_blade":        "\uE212",
	"bey:dragon_katana":       "\uE212",
	"bey:crimson_chain_sword": "\uE212",
	"bey:excalibur":           "\uE212",
	"bey:eagle_eye_bow":       "\uE214",
}

const defaultCrosshairSymbol = "\uE210"

// crosshairActive tracks which players already have a crosshair ticker
// running, so EnsureCrosshair doesn't spawn a duplicate one every single
// time a legendary weapon is used.
var (
	crosshairActiveMu sync.Mutex
	crosshairActive    = map[string]bool{} // xuid -> ticker running
)

// EnsureCrosshair starts p's crosshair ticker if one isn't already
// running. Call this from anywhere that already has a safe tx and a
// player performing a legendary action — currently called from OnUse (see
// abilities.go) so it kicks in the first time any legendary weapon is
// used. Safe to call repeatedly; it's a no-op after the first call per
// player per session.
func EnsureCrosshair(tx *world.Tx, p *player.Player) {
	crosshairActiveMu.Lock()
	if crosshairActive[p.XUID()] {
		crosshairActiveMu.Unlock()
		return
	}
	crosshairActive[p.XUID()] = true
	crosshairActiveMu.Unlock()

	handle := world.EntitySpawnOpts{Position: p.Position()}.New(CrosshairTickerType, CrosshairConfig{Owner: p.H(), XUID: p.XUID()})
	tx.AddEntity(handle)
}

// ClearCrosshairState drops the "ticker running" flag for xuid — call this
// from PlayerHandler.HandleQuit alongside the other ClearPlayer calls, so
// rejoining starts fresh (a leftover true here after quitting is harmless
// either way, since the ticker itself self-removes when the owner handle
// resolves to nothing, but this keeps the map from growing forever on a
// long-running server).
func ClearCrosshairState(xuid string) {
	crosshairActiveMu.Lock()
	delete(crosshairActive, xuid)
	crosshairActiveMu.Unlock()
}

// CrosshairTickerType is the entity type for the always-on crosshair
// ticker.
var CrosshairTickerType crosshairTickerType

// CrosshairTypes returns the entity types this file adds, for merging
// into the server's entity registry — see the wiring note in main.go.
func CrosshairTypes() []world.EntityType { return []world.EntityType{CrosshairTickerType} }

var crosshairBBox = cube.Box(0, 0, 0, 0, 0, 0)

type crosshairTickerType struct{}

func (crosshairTickerType) EncodeEntity() string        { return "bey:crosshair_ticker" }
func (crosshairTickerType) BBox(world.Entity) cube.BBox { return crosshairBBox }
func (crosshairTickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*crosshairState)
	if !ok || st == nil {
		st = &crosshairState{}
		data.Data = st
	}
	return &crosshairTicker{tx: tx, handle: handle, data: data, state: st}
}
func (crosshairTickerType) DecodeNBT(m map[string]any, data *world.EntityData) {
	data.Data = &crosshairState{}
}
func (crosshairTickerType) EncodeNBT(data *world.EntityData) map[string]any { return map[string]any{} }

type crosshairState struct {
	Owner *world.EntityHandle
	XUID  string
}

// CrosshairConfig configures a newly spawned crosshair ticker.
type CrosshairConfig struct {
	Owner *world.EntityHandle
	XUID  string
}

func (c CrosshairConfig) Apply(data *world.EntityData) {
	data.Data = &crosshairState{Owner: c.Owner, XUID: c.XUID}
}

type crosshairTicker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *crosshairState
}

func (e *crosshairTicker) H() *world.EntityHandle  { return e.handle }
func (e *crosshairTicker) Position() mgl64.Vec3    { return e.data.Pos }
func (e *crosshairTicker) Rotation() cube.Rotation { return e.data.Rot }
func (e *crosshairTicker) Close() error            { return nil }

// Tick runs EVERY tick (no throttling, unlike the cooldown bar) — matches
// the add-on's own stayDuration:1 (1 tick), which needs a refresh every
// single tick to avoid flickering.
func (e *crosshairTicker) Tick(tx *world.Tx, current int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in crosshairTicker.Tick: %v", r)
		}
	}()
	owner, ok := e.state.Owner.Entity(tx)
	if !ok {
		crosshairActiveMu.Lock()
		delete(crosshairActive, e.state.XUID)
		crosshairActiveMu.Unlock()
		tx.RemoveEntity(e)
		return
	}
	p, ok := owner.(*player.Player)
	if !ok {
		tx.RemoveEntity(e)
		return
	}

	symbol := defaultCrosshairSymbol
	held, _ := p.HeldItems()
	if w, ok := held.Item().(legendaryItem); ok {
		if sym, ok := crosshairMap[w.WeaponDef().ID]; ok {
			symbol = sym
		}
	}

	p.SendTitle(title.New("crosshair:\"" + symbol + "\"").
		WithFadeInDuration(0).
		WithDuration(50 * time.Millisecond).
		WithFadeOutDuration(0))
}
