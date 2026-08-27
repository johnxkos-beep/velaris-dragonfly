// Custom red cooldown bar, replicating the add-on's own HUD trick instead
// of relying on Bedrock's native (gray) item-cooldown swipe.
//
// HOW THE ADD-ON ACTUALLY DOES THIS (confirmed by reading its real
// scripts/main.js and the resource pack's ui/hud_screen.json, not
// guessed): it is NOT a real Bedrock item-cooldown category color. It's a
// pure HUD trick — every tick, it sends the player a "title" whose text is
// literally the string "bar:<percentage>" (e.g. "bar:73"), with near-zero
// fade durations so it's effectively a persistent overlay refreshed every
// tick. The resource pack's ui/hud_screen.json has a binding that checks
// whether the current title text starts with "bar:" — confirmed via its
// own Molang condition "((#text - 'bar:') = #text)" — and when it does,
// swaps in a custom red bar graphic sized to the percentage instead of
// showing the text literally. Nothing server-side about "red" at all; the
// color comes entirely from the resource pack's graphic. This same
// technique (a specially-prefixed title string intercepted by the pack)
// is also how the crosshair works — see main.js's `crosshair:"${symbol}"`
// calls — so this file's approach is reusable for that later.
//
// IMPLEMENTATION: a lightweight, invisible-in-intent ticking entity is
// spawned per active cooldown (reusing the same Tick(tx)-gets-a-real-Tx
// pattern already proven safe by the Projectile/DemonKing entities in this
// package — no new unproven mechanism introduced). It recomputes the
// percentage a few times a second from the same cooldownReady timestamp
// already tracked in playerAbilityState (so the actual cooldown logic is
// unchanged — this only adds a visual on top) and sends the title update,
// despawning itself once the cooldown reaches 0.
//
// CONFIRMED API (via real Dragonfly docs, not a guess): *player.Player has
// SendTitle(t title.Title), and title.Title's fields are FadeInDuration(),
// Duration(), FadeOutDuration(), Text(), Subtitle(), ActionText() — all
// confirmed from github.com/df-mc/dragonfly/server/player/player.go's
// actual SendTitle implementation. What ISN'T independently confirmed is
// the exact builder-method names on title.Title itself (title.New(text),
// .WithFadeInDuration(d), .WithDuration(d), .WithFadeOutDuration(d)) —
// these follow the same With-prefixed chainable-builder convention
// already proven elsewhere in this codebase's Dragonfly usage (form.Menu's
// WithBody/WithButtons), so it's a well-grounded guess, not a blind one,
// but if `go build` disagrees on the method names specifically, this file
// is the one place to fix.
package legendary

import (
	"log"
	"strconv"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const hudBarUpdateInterval = 4 // ticks between updates (5/sec) — smooth enough, cheap enough

// HUDBarType is the entity type for the invisible cooldown-bar ticker.
var HUDBarType hudBarType

// HUDTypes returns the entity types this file adds, for merging into the
// server's entity registry alongside DemonKing's and the projectiles' —
// see the wiring note in main.go.
func HUDTypes() []world.EntityType { return []world.EntityType{HUDBarType} }

var hudBarBBox = cube.Box(0, 0, 0, 0, 0, 0)

type hudBarType struct{}

func (hudBarType) EncodeEntity() string        { return "bey:hud_bar_ticker" }
func (hudBarType) BBox(world.Entity) cube.BBox { return hudBarBBox }
func (hudBarType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	st, ok := data.Data.(*hudBarState)
	if !ok || st == nil {
		st = &hudBarState{}
		data.Data = st
	}
	return &hudBar{tx: tx, handle: handle, data: data, state: st}
}
func (hudBarType) DecodeNBT(m map[string]any, data *world.EntityData) { data.Data = &hudBarState{} }
func (hudBarType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

type hudBarState struct {
	Owner      *world.EntityHandle
	WeaponID   string
	StartedAt  time.Time
	Total      time.Duration
	WasShowing bool // was the bar actually on-screen as of the last tick?
}

// HUDBarConfig configures a newly spawned cooldown-bar ticker.
type HUDBarConfig struct {
	Owner    *world.EntityHandle
	WeaponID string
	Total    time.Duration
}

func (c HUDBarConfig) Apply(data *world.EntityData) {
	data.Data = &hudBarState{Owner: c.Owner, WeaponID: c.WeaponID, StartedAt: time.Now(), Total: c.Total}
}

// StartCooldownBar spawns the ticking entity that drives p's red cooldown
// bar for the next `total` duration, for weaponID specifically. Call this
// alongside (not instead of) the real cooldown tracking — this is purely
// visual.
func StartCooldownBar(tx *world.Tx, p *player.Player, weaponID string, total time.Duration) {
	handle := world.EntitySpawnOpts{Position: p.Position()}.New(HUDBarType, HUDBarConfig{
		Owner:    p.H(),
		WeaponID: weaponID,
		Total:    total,
	})
	tx.AddEntity(handle)
}

type hudBar struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *hudBarState
}

func (e *hudBar) H() *world.EntityHandle  { return e.handle }
func (e *hudBar) Position() mgl64.Vec3    { return e.data.Pos }
func (e *hudBar) Rotation() cube.Rotation { return e.data.Rot }
func (e *hudBar) Close() error            { return nil }

// Tick recomputes the bar's percentage every hudBarUpdateInterval ticks
// while the owner holds the tracked weapon, but — this is now the SECOND
// fix for the same underlying issue — checks the held item on EVERY tick
// (not just the throttled interval) so it can react immediately the
// moment the player switches away.
//
// The first version of this fix (gate sending on "is this the item
// currently held", but otherwise do nothing when it isn't) turned out to
// only be a partial fix: it stopped the wrong weapon's bar from showing,
// but a real report after shipping it showed the bar could still freeze
// on-screen at a stale percentage after switching away — passively
// relying on the title's own short stay/fade duration to clear itself
// wasn't reliable enough to confirm from documentation alone. Rather than
// keep guessing at exact Bedrock title-duration semantics, this is now
// robust by construction: the instant the held item stops matching, it
// ACTIVELY sends one explicit "bar:0" clear update right away (not
// waiting for the next throttled interval), and won't send anything
// further until the player switches back — no passive expiry involved.
func (e *hudBar) Tick(tx *world.Tx, current int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[legendary] recovered panic in hudBar.Tick: %v", r)
		}
	}()
	st := e.state

	owner, ok := st.Owner.Entity(tx)
	if !ok {
		tx.RemoveEntity(e)
		return
	}
	p, ok := owner.(*player.Player)
	if !ok {
		tx.RemoveEntity(e)
		return
	}

	elapsed := time.Since(st.StartedAt)
	if elapsed >= st.Total {
		if st.WasShowing {
			p.SendTitle(titleBar(0))
		}
		tx.RemoveEntity(e)
		return
	}

	held, _ := p.HeldItems()
	w, ok := held.Item().(Weapon)
	holding := ok && w.def.ID == st.WeaponID

	if !holding {
		if st.WasShowing {
			// Just switched away this tick — clear immediately rather
			// than leaving the last percentage frozen on-screen.
			p.SendTitle(titleBar(0))
			st.WasShowing = false
		}
		return
	}

	if current%hudBarUpdateInterval != 0 {
		return
	}

	remaining := st.Total - elapsed
	pct := int(remaining * 100 / st.Total)
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	p.SendTitle(titleBar(pct))
	st.WasShowing = true
}

func titleBar(pct int) title.Title {
	return title.New(barText(pct)).
		WithFadeInDuration(0).
		WithDuration(hudBarUpdateInterval * 50 * time.Millisecond).
		WithFadeOutDuration(0)
}

func barText(pct int) string {
	return "bar:" + strconv.Itoa(pct)
}
