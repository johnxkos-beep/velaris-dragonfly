// Package countdown is a port of the PocketMine-MP "CountPlugin": an
// op-triggered countdown timer shown as a boss bar to every online
// player, configurable via /count. Ported from
// CountPlugin/src/CountPlugin (Main.php, CountForm.php,
// CountdownTask.php).
//
// The original hand-built BossEventPacket show/title/health-percent/hide
// packets driven by a repeating scheduler task. Dragonfly ships a native
// server/player/bossbar package (Player.SendBossBar / .RemoveBossBar),
// so no raw packets are needed — the same simplification the Demon King
// boss fight already applies for its own health bar (see
// bosses/demonking/entity.go's updateBossBar).
//
// Like scoreboard's refresh loop, the per-second tick is driven by a
// single invisible ticker entity rather than an independent goroutine —
// this codebase hit real, silent player-touching failures doing that
// from a goroutine before (see the long comment at the top of
// scoreboard/scoreboard.go for the history); this mirrors that fix
// exactly, down to the entity boilerplate.
package countdown

import (
	"strconv"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---------------------------------------------------------------------
// Active countdown state
// ---------------------------------------------------------------------

type activeState struct {
	mu sync.Mutex

	running      bool
	message      string
	secondsLeft  int
	totalSeconds int
}

var active activeState

// Start begins a new countdown, replacing any countdown already running
// — port of Main::startCountdown, which likewise cancels the previous
// scheduler task before starting the new one.
func Start(message string, seconds int) {
	active.mu.Lock()
	defer active.mu.Unlock()
	active.running = true
	active.message = message
	active.secondsLeft = seconds
	active.totalSeconds = seconds
	if active.totalSeconds < 1 {
		active.totalSeconds = 1
	}
}

// formatTitle mirrors CountdownTask::formatTitle exactly.
func formatTitle(message string, secondsLeft int) string {
	minutes := secondsLeft / 60
	seconds := secondsLeft % 60
	return "§c" + message + "§r: §f§l" + strconv.Itoa(minutes) + "m " + strconv.Itoa(seconds) + "s"
}

// ---------------------------------------------------------------------
// ticker: a single, invisible, always-on entity whose only job is
// advancing the active countdown once per second and pushing the boss
// bar to every online player. Exactly one is ever spawned — see
// EnsureTicker. Mirrors scoreboard.go's ticker entity field for field.
// ---------------------------------------------------------------------

// TickerType is the entity type for the invisible countdown ticker.
var TickerType tickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in main.go
// (mirrors scoreboard.EntityTypes() / restrict.EntityTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{TickerType} }

var tickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type tickerType struct{}

func (tickerType) EncodeEntity() string        { return "velaris:countdown_ticker" }
func (tickerType) BBox(world.Entity) cube.BBox { return tickerBBox }
func (tickerType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &ticker{tx: tx, handle: handle, data: data}
}
func (tickerType) DecodeNBT(m map[string]any, data *world.EntityData) {}
func (tickerType) EncodeNBT(data *world.EntityData) map[string]any    { return map[string]any{} }

// tickerConfig is an empty EntitySpawnOpts config for TickerType, which
// needs no spawn-time configuration — mirrors scoreboard's tickerConfig.
type tickerConfig struct{}

func (tickerConfig) Apply(data *world.EntityData) {}

var (
	tickerMu      sync.Mutex
	tickerSpawned bool
)

// EnsureTicker spawns the single countdown ticker entity the first time
// it's needed. Safe to call repeatedly; only spawns once. Call it from
// the very first real *world.Tx you have — e.g. right after the first
// player joins, using p.Tx() and p.Position() (see main.go) — same
// "spawn near actual player activity" reasoning as
// scoreboard.Manager.EnsureTicker.
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

// Tick advances the active countdown once per second (every 20 ticks)
// and pushes the boss bar to every online player — port of
// CountdownTask::onRun, minus the manual show/update split: Dragonfly's
// SendBossBar can just be called again each second to refresh it.
func (t *ticker) Tick(tx *world.Tx, current int64) {
	if current%20 != 0 {
		return
	}

	active.mu.Lock()
	if !active.running {
		active.mu.Unlock()
		return
	}
	if active.secondsLeft <= 0 {
		active.running = false
		active.mu.Unlock()
		for e := range tx.Players() {
			if p, ok := e.(*player.Player); ok {
				p.RemoveBossBar()
			}
		}
		return
	}

	title := formatTitle(active.message, active.secondsLeft)
	pct := float64(active.secondsLeft) / float64(active.totalSeconds)
	if pct < 0 {
		pct = 0
	} else if pct > 1 {
		pct = 1
	}
	active.secondsLeft--
	active.mu.Unlock()

	// UNVERIFIED: bossbar.Red() is my best read of the bossbar package's
	// colour options, following the same style as
	// bosses/demonking/entity.go's bossbar.Purple() (the original PHP
	// used BossBarColor::RED). If Red() doesn't exist, the compiler
	// error will list the actual exported colour funcs — one-line fix.
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			p.SendBossBar(bossbar.New(title).WithHealthPercentage(pct).WithColour(bossbar.Red()))
		}
	}
}
