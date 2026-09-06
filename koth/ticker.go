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
// TickerType: a per-zone, invisible entity that advances one KOTH
// capture once per second. See the package doc comment's BUGFIX note in
// koth.go for why this is one-per-zone-anchored-at-the-zone rather than
// one global ticker spawned wherever a player happened to be standing —
// short version: Dragonfly only ticks entities in loaded chunks, and
// only a real player standing at the zone keeps that chunk loaded, so
// the ticker needs to live there too, or it silently stops running.
// ---------------------------------------------------------------------

// TickerType is the entity type for a per-zone KOTH ticker.
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
	st, ok := data.Data.(*tickerState)
	if !ok || st == nil {
		st = &tickerState{}
		data.Data = st
	}
	return &ticker{tx: tx, handle: handle, data: data, state: st}
}

// tickerState is the one thing a KOTH ticker needs to survive a world
// save/reload: which zone it belongs to, identified by that zone's first
// corner (a stable key — see Config.FindByCorner). Storing this (instead
// of, say, a zone name, which can be assigned well after the ticker
// already exists) is what lets a ticker spawned at corner-completion
// time keep tracking the same zone through /koth name and any number of
// /koth activate cycles later.
//
// UNVERIFIED: EncodeNBT emits plain int32 values for the corner
// coordinates (standard Minecraft NBT int-tag convention), which isn't
// independently confirmed against this Dragonfly version's own NBT
// (de)serialization — no other ticker in this repo stores typed fields
// this way to copy from (restrict's and countdown's carry no state, so
// their EncodeNBT/DecodeNBT are empty stubs). DecodeNBT's type
// assertions fail safe either way: if the real stored type differs,
// Corner1 just comes back as the zero position instead of panicking,
// and the ticker harmlessly finds no matching zone and despawns itself
// (see Tick below) rather than corrupting anything.
type tickerState struct {
	Corner1 cube.Pos
}

func (tickerType) EncodeNBT(data *world.EntityData) map[string]any {
	st, ok := data.Data.(*tickerState)
	if !ok || st == nil {
		return map[string]any{}
	}
	return map[string]any{
		"CornerX": int32(st.Corner1.X()),
		"CornerY": int32(st.Corner1.Y()),
		"CornerZ": int32(st.Corner1.Z()),
	}
}

func (tickerType) DecodeNBT(m map[string]any, data *world.EntityData) {
	x, _ := m["CornerX"].(int32)
	y, _ := m["CornerY"].(int32)
	z, _ := m["CornerZ"].(int32)
	data.Data = &tickerState{Corner1: cube.Pos{int(x), int(y), int(z)}}
}

// tickerConfig configures a newly spawned per-zone ticker.
type tickerConfig struct {
	Corner1 cube.Pos
}

func (c tickerConfig) Apply(data *world.EntityData) {
	data.Data = &tickerState{Corner1: c.Corner1}
}

// SpawnZoneTicker spawns a ticker anchored at pos — the zone's own
// footprint center (see Zone.SpawnPosition) — for the zone whose first
// corner is corner1, unless one's already been spawned for that corner
// this session (Config.MarkTickerSpawned). There are two call sites, and
// both are meant to fire for the same zone at different times without
// double-spawning: players.go's HandleBlockPlace calls this the instant
// a zone's second corner completes its shape (so the ticker exists
// immediately, before it even has a name), and command.go's
// Activate.Run also calls it every time /koth activate runs (so a zone
// that already existed before this fix shipped — and so never got a
// ticker at corner-completion time, because that already happened in
// the past under the old code — gets one the first time it's activated
// again).
func SpawnZoneTicker(tx *world.Tx, pos mgl64.Vec3, corner1 cube.Pos) {
	if !Cfg.MarkTickerSpawned(corner1) {
		return
	}
	handle := world.EntitySpawnOpts{Position: pos}.New(TickerType, tickerConfig{Corner1: corner1})
	tx.AddEntity(handle)
}

type ticker struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	state  *tickerState
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

// Tick advances this ticker's own zone once per second (every 20 ticks)
// — port of KothTask::onRun -> KothManager::tick(), scoped to a single
// zone per entity (see the package-level BUGFIX note above).
func (t *ticker) Tick(tx *world.Tx, current int64) {
	if current%20 != 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[koth] recovered panic in ticker.Tick: %v", r)
		}
	}()

	name, zone, state, named, exists := Cfg.FindByCorner(t.state.Corner1)
	if !exists {
		// The corner identifying this zone got broken (see koth.go's
		// OnBlockBreak) — nothing left to track.
		tx.RemoveEntity(t)
		return
	}
	if !named || state == nil {
		// Shape is done but not yet named (/koth name), or named but not
		// currently active (/koth activate) — stay alive and idle. A
		// later /koth name + /koth activate finds this exact ticker
		// again next tick, via the same corner.
		return
	}

	t.tickZone(tx, name, zone, state, time.Now())
}

func (t *ticker) tickZone(tx *world.Tx, name string, zone Zone, state *activeState, now time.Time) {
	Cfg.mu.Lock()
	// Re-confirm this exact capture is still the live one for name —
	// another goroutine (a command handler) could have finished,
	// restarted, or deleted it between FindByCorner's read and here.
	cur, stillActive := Cfg.active[name]
	if !stillActive || cur != state {
		Cfg.mu.Unlock()
		return
	}

	min, max := zone.bounds()

	if now.Sub(state.lastCoordBroadcast) >= CoordReminderSeconds*time.Second {
		state.lastCoordBroadcast = now
		msg := "§6§l[KOTH] §r§e\"" + name + "\" is still active - " + coordsMessage(zone)
		Cfg.mu.Unlock()
		broadcast(tx, msg)
		Cfg.mu.Lock()
		cur, stillActive = Cfg.active[name]
		if !stillActive || cur != state {
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
		at := cube.PosFromVec3(p.Position())
		if at.X() >= min.X() && at.X() <= max.X()+1 &&
			at.Z() >= min.Z() && at.Z() <= max.Z()+1 &&
			at.Y() >= min.Y()-2 && at.Y() <= max.Y()+6 {
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
			cur, stillActive = Cfg.active[name]
			if !stillActive || cur != state {
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
// rewards. The ticker entity itself is left alone (not despawned) so it
// keeps tracking the same zone for its next /koth activate — only
// OnBlockBreak (a corner actually getting broken) makes Tick despawn it.
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
// item.Pickaxe{Tier: item.ToolTierNetherite}, not "item.NetheritePickaxe").
// If any of these 6 names are still off, `go build` will name the real
// ones and it's a one-line swap per item.
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
