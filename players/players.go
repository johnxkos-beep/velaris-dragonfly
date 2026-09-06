package players

import (
	"log/slog"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"

	"velaris-dragonfly/knockback"
	"velaris-dragonfly/koth"
	"velaris-dragonfly/legendary"
	"velaris-dragonfly/pvp"
	"velaris-dragonfly/ranks"
	"velaris-dragonfly/restrict"
	"velaris-dragonfly/scoreboard"
	"velaris-dragonfly/state"
)

// PlayerHandler is attached to every player that joins the server. It
// embeds player.NopHandler so we only need to implement the methods we
// actually care about — every other event is a silent no-op until we add
// it here.
type PlayerHandler struct {
	player.NopHandler

	p          *player.Player
	ranks      *ranks.RankSet
	rankDefs   *ranks.RankDefSet
	scoreboard *scoreboard.Manager
	log        *slog.Logger
}

// NewPlayerHandler creates a PlayerHandler for the given player.
func NewPlayerHandler(p *player.Player, rks *ranks.RankSet, rankDefs *ranks.RankDefSet, sb *scoreboard.Manager, log *slog.Logger) *PlayerHandler {
	return &PlayerHandler{p: p, ranks: rks, rankDefs: rankDefs, scoreboard: sb, log: log}
}

// HandleQuit is called when the player disconnects, regardless of the
// reason. This is where you'd persist per-player state.
func (h *PlayerHandler) HandleQuit(p *player.Player) {
	state.TrackQuit(p)
	knockback.ClearPlayer(p.XUID())
	legendary.ClearPlayer(p.XUID())
	h.scoreboard.Remove(p)
	h.log.Info("player quit", "name", p.Name(), "xuid", p.XUID())
}

// HandleChat formats every chat message to match the PocketMine-MP
// "Ranks" plugin's RankChatFormatter exactly: "[Rank] Name: message"
// with gray brackets, the rank's color on both the rank name and the
// player's name, and that rank's (separately editable) message color on
// the message text.
//
// Player.Chat() — which is what invokes this handler — always prefixes
// the outgoing message with the player's plain name before broadcasting
// it to chat.Global (see player.Player.Chat's doc comment: "The message
// is prefixed with the name of the player"). That can't be reordered or
// recolored from in here, since it happens after this handler returns —
// so instead this cancels that default broadcast via ctx.Cancel() and
// writes the fully-formatted line to chat.Global itself.
func (h *PlayerHandler) HandleChat(ctx *player.Context, message *string) {
	ctx.Cancel()

	name := h.ranks.Of(h.p.XUID())
	def, ok := h.rankDefs.Get(name)
	if !ok {
		def, _ = h.rankDefs.Get(ranks.DefaultRankName)
	}
	chat.Global.WriteString(def.FormatChat(h.p.Name(), *message))
}

// HandleHurt is called every time the player takes damage, before it is
// applied. Reduces fall damage by 50%, and — via knockback.OnHurt — plays
// the configurable "ding" sound to a shooter whose projectile just hit
// this player (see the knockback package, ported from the CustomKB
// PocketMine plugin).
func (h *PlayerHandler) HandleHurt(ctx *player.Context, damage *float64, immune bool, immunity *time.Duration, src world.DamageSource) {
	if _, ok := src.(entity.FallDamageSource); ok {
		*damage *= 0.5
	}
	knockback.OnHurt(src)
	legendary.OnHurt(ctx, h.p, damage, src)
}

// HandleAttackEntity gates player-vs-player damage through the pvp
// package (always allowed inside PvP zones; otherwise only while PvP is
// on server-wide via /pvp on) before applying the configured knockback
// force/height and enforcing the attack cooldown — see the knockback
// package, ported from the CustomKB PocketMine plugin.
func (h *PlayerHandler) HandleAttackEntity(ctx *player.Context, e world.Entity, force, height *float64, critical *bool) {
	if _, ok := e.(*player.Player); ok {
		at := cube.PosFromVec3(h.p.Position())
		// A KOTH zone forces PvP on near its center while active,
		// regardless of the server-wide /pvp state — see koth.ForcedPvp.
		if !pvp.Cfg.CombatAllowed(at) && !koth.Cfg.ForcedPvp(at.X(), at.Z()) {
			ctx.Cancel()
			h.p.Message("§cPvP is off — fight inside a PvP zone, or ask an op to turn PvP on.")
			return
		}
	}
	knockback.OnAttackEntity(ctx, h.p, force, height, critical)
}

// HandleItemUse enforces the configured projectile-shoot cooldown — see
// the knockback package, ported from the CustomKB PocketMine plugin.
// Legendary weapon abilities do NOT run from here — see the doc comment
// on legendary.OnUse in legendary/abilities.go for why (this handler
// doesn't get a safe *world.Tx in this Dragonfly version); they run
// through Weapon.Use in legendary/items.go instead, wired up automatically
// by Dragonfly whenever a legendary weapon is used, no call needed here.
func (h *PlayerHandler) HandleItemUse(ctx *player.Context) {
	knockback.OnItemUse(ctx, h.p)
}

// HandleDeath is called when the player dies.
func (h *PlayerHandler) HandleDeath(p *player.Player, src world.DamageSource, keepInv *bool) {
	h.log.Info("player died", "name", p.Name())
	if ads, ok := src.(entity.AttackDamageSource); ok {
		if attacker, ok := ads.Attacker.(*player.Player); ok {
			// NOTE: doesn't cover the PHP original's fall/void kill-credit
			// fallback (crediting a Midas Sword hit from a few seconds
			// earlier if the actual killing blow was fall/void/lava damage
			// after a knockback). That's a small tracked-state addition on
			// top of this if you want it later — this covers the direct-hit
			// case, which is the overwhelming majority of kills.
			legendary.OnKill(attacker, p)
		}
	}
}

// HandleBlockPlace cancels placement of any "*_display_block" — these are
// pseudo-blocks that exist only so a legendary weapon item renders with real
// 3D geometry in hand (see legendary/block_render.go); they must never
// actually occupy a world position. UNVERIFIED against real Dragonfly
// source — if the method signature here doesn't match player.Handler's
// exactly, this is the first thing to check on a build error.
//
// Also forwards every placement to pvp.Cfg.OnBlockPlace,
// restrict.Cfg.OnBlockPlace, and koth.Cfg.OnBlockPlace, each a no-op
// unless the block placed is that package's own marker block — see the
// pvp, restrict, and koth packages. Placements inside a named KOTH
// zone's protection box (see koth.IsProtected) are cancelled outright
// for non-ops. The moment a KOTH zone's second corner completes its
// shape, this also spawns that zone's ticker right there (see
// koth.SpawnZoneTicker) — anchoring it at the zone itself, using this
// exact tx/world, is what makes capturing actually work; see koth.go's
// package doc comment for why a single ticker spawned elsewhere didn't.
func (h *PlayerHandler) HandleBlockPlace(ctx *player.Context, pos cube.Pos, b world.Block) {
	name, _ := b.EncodeBlock()
	if strings.HasSuffix(name, "_display_block") {
		ctx.Cancel()
		return
	}
	if msg, ok := pvp.Cfg.OnBlockPlace(h.p.XUID(), pos, b); ok {
		h.p.Message(msg)
	}
	if msg, ok := restrict.Cfg.OnBlockPlace(h.p.XUID(), pos, b); ok {
		h.p.Message(msg)
	}
	if msg, ok, completed := koth.Cfg.OnBlockPlace(h.p.XUID(), pos, b); ok {
		h.p.Message(msg)
		if completed != nil {
			koth.SpawnZoneTicker(h.p.Tx(), completed.SpawnPosition(), completed.Corner1)
		}
	} else if !state.Ops.IsOp(h.p.XUID()) && koth.Cfg.IsProtected(pos.X(), pos.Z()) {
		ctx.Cancel()
		h.p.Message("§cThis area is protected as part of a KOTH zone.")
	}
}
