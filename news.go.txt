// Package news is a Go port of the PocketMine-MP HopliteLegendary
// plugin's /news command — ported from
// hoplite/legendary/NewsCommand.php and NewsBroadcastTask.php.
//
// /news <message> broadcasts once, immediately, to every online player
// (chat + action bar, same as the original). Bare /news opens a setup
// form (see form.go) for a REPEATING announcement instead: message
// text, how long to keep repeating for (minutes), and the delay between
// each repeat (seconds) — the same three fields as the original
// CustomForm in NewsCommand::openForm.
//
// The original scheduled a repeating PMMP Task directly via
// scheduleRepeatingTask, ticking independently of any particular
// player. Dragonfly has no player-safe equivalent scheduler, and this
// codebase has already hit real, silent player-touching failures
// driving repeating/periodic work from a bare goroutine instead (see
// the history note atop scoreboard/scoreboard.go, and
// countdown/countdown.go's matching doc comment) — so, like countdown
// and scoreboard, this uses a single invisible ticker entity that gets
// a real *world.Tx on every Tick instead.
package news

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// prefixedText mirrors NewsCommand::execute / NewsBroadcastTask::onRun's
// "§6§l[NEWS] §r§e<message>" formatting (TF::GOLD.TF::BOLD."[NEWS] " .
// TF::RESET . TF::YELLOW . message) using raw Bedrock formatting codes,
// the same style already used throughout this codebase (see e.g.
// restrict.Command.Run's "§a..." messages) rather than a TextFormat-style
// wrapper.
func prefixedText(message string) string {
	return "§6§l[NEWS] §r§e" + message
}

// BroadcastOnce sends message once to every player currently in tx, both
// as a chat message and as an action-bar message — port of the identical
// double send in both NewsCommand::execute (the <message> form) and
// NewsBroadcastTask::onRun (the repeating form).
//
// Action-bar-only delivery uses title.New("").WithActionText(text): an
// empty main title with only the action text set, the same
// CONFIRMED API (title.Title.WithActionText, sent via
// player.Player.SendTitle) already established and used by this
// project's HUD bar and crosshair (see legendary/hud.go's doc comment
// for the confirmation source) — this is simply the first place in this
// project that wants an action-bar message on its own, with no
// title/subtitle alongside it.
func BroadcastOnce(tx *world.Tx, message string) {
	text := prefixedText(message)
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			p.Message(text)
			p.SendTitle(title.New("").WithActionText(text))
		}
	}
}

// ---------------------------------------------------------------------
// Repeating announcement state — set by the setup form (form.go) via
// StartRepeating, advanced by the ticker below. Port of
// NewsBroadcastTask's elapsedTicks/intervalTicks/totalDurationTicks
// fields, just held in package state instead of a per-task struct since
// only one repeating announcement is ever active at a time (starting a
// new one replaces the old one, same as the original: each /news form
// submit calls scheduleRepeatingTask fresh, and the previous task's own
// elapsedTicks/totalDurationTicks check would have no more listeners
// updating it in practice on a real server).
// ---------------------------------------------------------------------

type activeState struct {
	mu sync.Mutex

	running        bool
	message        string
	intervalTicks  int
	ticksSinceLast int // counts up to intervalTicks, then fires and resets
	elapsedTicks   int
	totalTicks     int
}

var active activeState

// StartRepeating begins a new repeating announcement, replacing any
// still running — port of Main::startCountdown-style "replace, don't
// stack" semantics (see countdown.Start's identical doc note) applied to
// NewsCommand's form submit handler. The first broadcast happens on the
// ticker's very next Tick (ticksSinceLast is seeded at intervalTicks so
// it fires immediately), matching the original's default behavior of
// scheduleRepeatingTask() firing its first run after exactly one period
// — for this port that means "as soon as the ticker sees it", not
// "instantly, before the form response is even shown to the player", so
// it's an early but not sub-tick guarantee.
func StartRepeating(message string, intervalTicks, totalDurationTicks int) {
	active.mu.Lock()
	defer active.mu.Unlock()
	if intervalTicks < 1 {
		intervalTicks = 1
	}
	active.running = true
	active.message = message
	active.intervalTicks = intervalTicks
	active.ticksSinceLast = intervalTicks
	active.elapsedTicks = 0
	active.totalTicks = totalDurationTicks
}

// ---------------------------------------------------------------------
// Ticker: a single, invisible, always-on entity whose only job is
// advancing the active repeating announcement and broadcasting it on
// schedule. Exactly one is ever spawned — see EnsureTicker. Mirrors
// countdown.go's ticker entity almost field-for-field; see that file's
// package doc comment for why this shape (entity + real Tx on Tick)
// rather than a goroutine.
// ---------------------------------------------------------------------

// TickerType is the entity type for the invisible news ticker.
var TickerType tickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in main.go
// (mirrors countdown.EntityTypes()/scoreboard.EntityTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{TickerType} }

var tickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type tickerType struct{}

func (tickerType) EncodeEntity() string        { return "velaris:news_ticker" }
func (tickerType) BBox(world.Entity) cube.BBox { return tickerBBox }
func (tickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &ticker{tx: tx, handle: handle, data: data}
}
func (tickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (tickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// tickerConfig is an empty EntitySpawnOpts config for TickerType, which
// needs no spawn-time configuration — mirrors countdown's tickerConfig.
type tickerConfig struct{}

func (tickerConfig) Apply(data *world.EntityData) {}

var (
	tickerMu      sync.Mutex
	tickerSpawned bool
)

// EnsureTicker spawns the single news ticker entity the first time it's
// needed. Safe to call repeatedly; only spawns once. Call it from the
// first real *world.Tx available — e.g. right after the first player
// joins, using p.Tx() and p.Position() (see main.go) — same "spawn next
// to actual player activity so the chunk is guaranteed loaded" reasoning
// as restrict.Config.ensureEnforcer and countdown.EnsureTicker.
func EnsureTicker(tx *world.Tx, near mgl64.Vec3) {
	tickerMu.Lock()
	if tickerSpawned {
		tickerMu.Unlock()
		return
	}
	tickerSpawned = true
	tickerMu.Unlock()

	handle := world.EntitySpawnOpts{Position: near}.New(TickerType, tickerConfig{})
	tx.AddEntity(handle)
}

type ticker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (t *ticker) H() *world.EntityHandle  { return t.handle }
func (t *ticker) Position() mgl64.Vec3    { return t.data.Pos }
func (t *ticker) Rotation() cube.Rotation { return t.data.Rot }
func (t *ticker) Close() error            { return nil }

// Tick advances the active repeating announcement once per server tick
// and broadcasts on schedule — port of NewsBroadcastTask::onRun, minus
// the re-scheduling: this ticker is permanent (it just no-ops while
// nothing is running), rather than a fresh Task cancelling itself.
func (t *ticker) Tick(tx *world.Tx, _ int64) {
	active.mu.Lock()
	if !active.running {
		active.mu.Unlock()
		return
	}
	active.ticksSinceLast++
	if active.ticksSinceLast < active.intervalTicks {
		active.mu.Unlock()
		return
	}
	active.ticksSinceLast = 0
	message := active.message
	active.elapsedTicks += active.intervalTicks
	if active.elapsedTicks >= active.totalTicks {
		active.running = false
	}
	active.mu.Unlock()

	BroadcastOnce(tx, message)
}
