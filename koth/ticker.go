package koth

import (
	"log"
	"math/rand"
	"strconv"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---------------------------------------------------------------------
// TickerType: a single, invisible, always-on entity that advances every
// active KOTH capture once per second. Same "avoid touching player/world
// state from an independent goroutine" reasoning as scoreboard's and
// countdown's own ticker entities in this repo — see countdown.go's
// package doc comment for the fuller history of why that matters here.
// Exactly one is ever spawned — see EnsureTicker.
// ---------------------------------------------------------------------

// TickerType is the entity type for the invisible KOTH ticker.
var TickerType tickerType

// EntityTypes returns the entity types this package adds, for merging
// into the server's entity registry — see the wiring note in
// INTEGRATION.md (mirrors countdown.EntityTypes()/restrict.EntityTypes()).
func EntityTypes() []world.EntityType { return []world.EntityType{TickerType} }

var tickerBBox = cube.Box(0, 0, 0, 0, 0, 0)

type tickerType struct{}

func (tickerType) EncodeEntity() string        { return "velaris:koth_ticker" }
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
	tickerSpawned bool
)

// EnsureTicker spawns the single KOTH ticker entity the first time it's
// needed. Safe to call repeatedly; only spawns once. Call it from the
// first real *world.Tx you have, same as countdown.EnsureTicker /
// restrict's ensureEnforcer — see INTEGRATION.md.
func EnsureTicker(tx *world.Tx, near mgl64.Vec3) {
	Cfg.mu.Lock()
	if tickerSpawned {
		Cfg.mu.Unlock()
		return
	}
	tickerSpawned = true
	Cfg.mu.Unlock()

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

// actionBar shows msg on p's action bar (the small line just above the
// hotbar) via an empty title with only ActionText set — Dragonfly has no
// separate "send action bar" call; this is the confirmed mechanism
// already used elsewhere in this repo (see legendary/hud.go's "CONFIRMED
// API" comment: *player.Player.SendTitle(title.Title), and title.Title's
// fields include ActionText()). Since this only gets called once per
// second (see tickZone), the stay duration is long enough to bridge that
// gap without flickering, unlike legendary/crosshair.go's every-tick
// refresh which uses a much shorter one.
func actionBar(p *player.Player, msg string) {
	p.SendTitle(title.New("").
		WithActionText(msg).
		WithFadeInDuration(0).
		WithDuration(1100 * time.Millisecond).
		WithFadeOutDuration(200 * time.Millisecond))
}

// Tick advances every active capture once per second (every 20 ticks) —
// port of KothTask::onRun -> KothManager::tick(). Only sees players in
// its own world's tx, same single-world simplification pvp.Zone and
// restrict.Zone already document (see pvp.Zone's NOTE) — this codebase
// doesn't track a zone's originating world/dimension beyond that.
func (t *ticker) Tick(tx *world.Tx, current int64) {
	if current%20 != 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[koth] recovered panic in ticker.Tick: %v", r)
		}
	}()

	now := time.Now()

	Cfg.mu.Lock()
	// Snapshot the active zone names so finish() (called below, which
	// itself needs Cfg.mu) isn't invoked while still holding the lock.
	names := make([]string, 0, len(Cfg.active))
	for n := range Cfg.active {
		names = append(names, n)
	}
	Cfg.mu.Unlock()

	for _, name := range names {
		t.tickZone(tx, name, now)
	}
}

func (t *ticker) tickZone(tx *world.Tx, name string, now time.Time) {
	Cfg.mu.Lock()
	state, ok := Cfg.active[name]
	if !ok {
		Cfg.mu.Unlock()
		return
	}
	zone := state.zone
	min, max := zone.bounds()

	if now.Sub(state.lastCoordBroadcast) >= CoordReminderSeconds*time.Second {
		state.lastCoordBroadcast = now
		msg := "§6§l[KOTH] §r§e\"" + name + "\" is still active - " + coordsMessage(zone)
		Cfg.mu.Unlock()
		broadcast(tx, msg)
		Cfg.mu.Lock()
		// state may have been deleted (zone broken) while unlocked; re-fetch.
		state, ok = Cfg.active[name]
		if !ok {
			Cfg.mu.Unlock()
			return
		}
	}

	var inside []*player.Player
	for e := range tx.Players() {
		p, isPlayer := e.(*player.Player)
		if !isPlayer {
			continue
		}
		pos := p.Position()
		x, y, z := int(pos[0]), int(pos[1]), int(pos[2])
		if x >= min.X() && x <= max.X()+1 &&
			z >= min.Z() && z <= max.Z()+1 &&
			y >= min.Y()-2 && y <= max.Y()+6 {
			inside = append(inside, p)
		}
	}

	var toFinish *player.Player
	finishNow := false

	switch len(inside) {
	case 1:
		holder := inside[0]
		key := holder.Name()
		state.progress[key]++
		actionBar(holder, "§aCapturing "+name+": "+strconv.Itoa(state.progress[key])+"/"+strconv.Itoa(CaptureSeconds)+"s")

		if state.progress[key] >= CampingWarningSeconds && !state.campWarned[key] {
			state.campWarned[key] = true
			Cfg.mu.Unlock()
			broadcast(tx, "§c§l[KOTH] §r§e"+holder.Name()+"§c is camping \""+name+"\"!")
			Cfg.mu.Lock()
			state, ok = Cfg.active[name]
			if !ok {
				Cfg.mu.Unlock()
				return
			}
		}

		if state.progress[key] >= CaptureSeconds {
			toFinish = holder
			finishNow = true
		}
	default:
		if len(inside) > 1 {
			for _, p := range inside {
				actionBar(p, "§c"+name+" is contested!")
			}
			// Contested again - clear camping warnings so a future solo
			// holder gets warned about again rather than staying silent.
			state.campWarned = map[string]bool{}
		}
	}

	if !finishNow && now.After(state.end) {
		finishNow = true
		best := 0
		var bestName string
		for pname, secs := range state.progress {
			if secs > best {
				best = secs
				bestName = pname
			}
		}
		if bestName != "" {
			for e := range tx.Players() {
				if p, isPlayer := e.(*player.Player); isPlayer && p.Name() == bestName {
					toFinish = p
					break
				}
			}
		}
	}

	Cfg.mu.Unlock()

	if finishNow {
		t.finish(tx, name, toFinish)
	}
}

// broadcast messages every currently-connected player in tx — the
// closest equivalent available here to the PHP original's
// Server::getInstance()->broadcastMessage(), since Dragonfly has no
// single server-wide broadcast call that doesn't go through a
// transaction. Matches the pattern already used by pvp.On/Off's Run
// methods (for p := range state.Server.Players(tx)) — that package
// imports state directly since it already depends on it elsewhere; this
// file uses tx.Players() instead (same set, no import needed) since
// ticker.go otherwise has no reason to import the state package.
func broadcast(tx *world.Tx, msg string) {
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok {
			p.Message(msg)
		}
	}
}

// finish ends a capture — port of KothManager::finish(). Deletes the
// active state and, if there's a winner, broadcasts it and hands out
// rewards.
//
// DEVIATION FROM THE PHP ORIGINAL: the original deposited rewards (10
// diamond-block items, 15 golden apples, one random piece of netherite
// armor, and a custom PurgeToken item) into a custom AwardManager mailbox
// the player collects later via /award. Neither AwardManager nor
// PurgeToken exist anywhere in this Go codebase, so there's nothing to
// port them onto. Instead, rewards go straight into the winner's
// inventory the moment they win — matching how every other
// reward/give-item moment in this codebase already works (e.g.
// restrict.Command.Run's p.Inventory().AddItem(...), pvp.Block.Run's
// same pattern). The PurgeToken is dropped entirely (nothing in this
// codebase corresponds to what it did); everything else is kept.
//
// UNVERIFIED item names: item.Diamond{} and item.GoldenApple{} are my
// best-confidence guesses at this Dragonfly version's exported struct
// names, following the exact same naming pattern already confirmed
// working elsewhere in this repo for other vanilla items
// (item.IronIngot{}, item.GoldIngot{}, item.NetheriteScrap{} in
// players/autosmelt.go). The netherite armor below uses Dragonfly's
// actual tiered-armor design instead of per-material struct names — armor
// items take a Tier field (item.Helmet{Tier: ...}) satisfied by an
// ArmourTier value (item.ArmourTierNetherite{}), the same composition
// pattern Dragonfly uses for tools (a netherite pickaxe is
// item.Pickaxe{Tier: item.ToolTierNetherite}, not "item.NetheritePickaxe")
// — that's why item.NetheriteHelmet{} et al. didn't exist last build. If
// any of these 6 names are still off, `go build` will name the real ones
// and it's a one-line swap per item.
func (t *ticker) finish(tx *world.Tx, name string, winner *player.Player) {
	Cfg.mu.Lock()
	delete(Cfg.active, name)
	Cfg.mu.Unlock()

	if winner == nil {
		broadcast(tx, "§6[KOTH] §e\""+name+"\" ended with no winner.")
		return
	}

	broadcast(tx, "§6§l[KOTH] §r§a"+winner.Name()+" has captured \""+name+"\"!")

	winner.Inventory().AddItem(item.NewStack(item.Diamond{}, 10))
	winner.Inventory().AddItem(item.NewStack(item.GoldenApple{}, 15))

	armor := []world.Item{
		item.Helmet{Tier: item.ArmourTierNetherite{}},
		item.Chestplate{Tier: item.ArmourTierNetherite{}},
		item.Leggings{Tier: item.ArmourTierNetherite{}},
		item.Boots{Tier: item.ArmourTierNetherite{}},
	}
	piece := armor[rand.Intn(len(armor))]
	winner.Inventory().AddItem(item.NewStack(piece, 1))

	winner.Message("§aYou won 10 diamonds, 15 golden apples, and a piece of netherite armor for capturing \"" + name + "\"!")
}
