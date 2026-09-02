// Custom crosshair, replicating the add-on's own real mechanism — read
// directly from its script (scripts/main.js, the crosshair.ts feature),
// not guessed. Confirmed: this is the EXACT same "bar:<pct>" trick as the
// cooldown bar (see hud.go), just a different prefixed title string. The
// add-on keeps a map from weapon ID to a private-use-area Unicode symbol
// character, sends `crosshair:"<symbol>"` as a permanently-refreshed
// (1-tick stay, 0 fade) title every single tick, and the resource pack's
// ui/hud_screen.json intercepts titles starting with "crosshair:" and
// draws the matching crosshair graphic instead of the text — and REPLACES
// the vanilla crosshair entirely while doing so. That's why "no crosshair
// at all, even empty-handed" happened originally: the pack takes over
// crosshair rendering unconditionally, and if nothing's sending it a
// recognized "crosshair:X" title, it has nothing to draw, full stop.
//
// ROUND 2 — GLOBAL TICKER, NOT PER-PLAYER: the first version spawned one
// ticker per player, started the first time THAT player used a legendary
// weapon — which meant a real, reported problem: a player just holding a
// legendary (or standing there empty-handed) with no ability use yet saw
// no crosshair at all, since nothing had triggered their personal ticker.
// Fixed by inverting the design: there's now exactly ONE ticker for the
// whole server, started by the first legendary weapon use from ANYONE,
// and once running it walks every currently-online player each tick and
// sends each of them their own correct crosshair — regardless of whether
// that specific player has ever touched a legendary weapon themselves.
// This still can't start at the exact instant the very first player
// connects (same underlying limit as before: no proven-safe *world.Tx
// source in this codebase outside of item.Usable's Use/item.Releasable's
// Release/entity Tick, none of which fire at connection time) — but once
// the server-wide first trigger happens, it's on for everyone from then
// on, including players who join afterward and players already online who
// never use a legendary weapon at all. Practically, on a server built
// around these weapons, that first trigger happens within moments of the
// server accepting its first player action, not per-player.
package legendary

import (
	"log"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/state"
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

// crosshairTickerRunning tracks whether the single global ticker has
// already been spawned, so EnsureCrosshair is a cheap no-op after the
// first call from anywhere on the server.
var (
	crosshairTickerMu      sync.Mutex
	crosshairTickerRunning bool
)

// EnsureCrosshair starts the global crosshair ticker if it isn't already
// running. Call this from anywhere that already has a safe tx — currently
// called from OnUse (see abilities.go) so it starts the first time any
// player uses any legendary weapon. Cheap and safe to call repeatedly;
// it's a no-op after the very first successful call for the whole server.
func EnsureCrosshair(tx *world.Tx) {
	crosshairTickerMu.Lock()
	if crosshairTickerRunning {
		crosshairTickerMu.Unlock()
		return
	}
	crosshairTickerRunning = true
	crosshairTickerMu.Unlock()

	handle := world.EntitySpawnOpts{}.New(CrosshairTickerType, CrosshairConfig{})
	tx.AddEntity(handle)
}

// CrosshairTickerType is the entity type for the always-on, server-wide
// crosshair ticker — exactly one of these ever exists.
var CrosshairTickerType crosshairTickerType

// CrosshairTypes returns the entity types this file adds, for merging
// into the server's entity registry — see the wiring note in main.go.
func CrosshairTypes() []world.EntityType { return []world.EntityType{CrosshairTickerType} }

var crosshairBBox = cube.Box(0, 0, 0, 0, 0, 0)

type crosshairTickerType struct{}

func (crosshairTickerType) EncodeEntity() string        { return "bey:crosshair_ticker" }
func (crosshairTickerType) BBox(world.Entity) cube.BBox { return crosshairBBox }
func (crosshairTickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &crosshairTicker{tx: tx, handle: handle, data: data}
}
func (crosshairTickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (crosshairTickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// CrosshairConfig configures the (single) crosshair ticker. Empty — it
// doesn't track a specific owner anymore, since it now serves every
// online player rather than one.
type CrosshairConfig struct{}

func (CrosshairConfig) Apply(data *world.EntityData) {}

type crosshairTicker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *crosshairTicker) H() *world.EntityHandle  { return e.handle }
func (e *crosshairTicker) Position() mgl64.Vec3    { return e.data.Pos }
func (e *crosshairTicker) Rotation() cube.Rotation { return e.data.Rot }
func (e *crosshairTicker) Close() error            { return nil }

// Tick runs EVERY server tick (no throttling — matches the add-on's own
// 1-tick stay duration, which needs a refresh every single tick to avoid
// flickering) and sends every currently-online player their correct
// crosshair, based on whatever they're individually holding right now.
func (e *crosshairTicker) Tick(tx *world.Tx, current int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in crosshairTicker.Tick: %v", r)
		}
	}()
	for p := range state.Server.Players(tx) {
		symbol := defaultCrosshairSymbol
		held, _ := p.HeldItems()
		if w, ok := held.Item().(legendaryItem); ok {
			if sym, ok := crosshairMap[w.WeaponDef().ID]; ok {
				symbol = sym
			}
		}
		p.SendTitle(title.New("crosshair:" + symbol).
			WithFadeInDuration(0).
			WithDuration(50 * time.Millisecond).
			WithFadeOutDuration(0))
	}
}
