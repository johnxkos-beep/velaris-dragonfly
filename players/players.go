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
	"github.com/go-gl/mathgl/mgl64"

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
// for non-ops.
//
// NOTE: this handler does NOT spawn a KOTH zone's ticker on corner
// completion, on purpose — an earlier version tried to, right here, and
// it crashed the server every time. HandleBlockPlace runs deep inside
// Dragonfly's block-placement/inventory-transaction handling, and
// world.Tx.AddEntity cannot be called from that context at all (see
// koth/ticker.go's package doc comment for the crash and the fix).
// Ticker spawning happens from the join handler in main.go instead, via
// koth.EnsureAllZoneTickers.
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
	if msg, ok := koth.Cfg.OnBlockPlace(h.p.XUID(), pos, b); ok {
		h.p.Message(msg)
	} else if !state.Ops.IsOp(h.p.XUID()) && koth.Cfg.IsProtected(pos.X(), pos.Z()) {
		ctx.Cancel()
		h.p.Message("§cThis area is protected as part of a KOTH zone.")
	}
}

// HandleItemUseOnBlock closes a gap in the KOTH protection check above:
// filling/emptying a bucket doesn't go through HandleBlockPlace at all.
// Dragonfly places and picks up liquids as a direct item-use action
// (tx.SetLiquid under the hood), not through the normal block-placement
// pipeline — so water was never being checked against
// koth.Cfg.IsProtected, even though solid blocks correctly were. This
// hook fires on right-clicking any block with an item in hand, which
// covers bucket use along with everything else in that category (bone
// meal, flint and steel, hoe tilling, etc.) — so for consistency, all of
// those get blocked inside a protected KOTH zone for non-ops now, not
// just water specifically.
//
// pos is the block being right-clicked, not necessarily where the
// resulting liquid/block ends up — that's pos.Side(face) instead (the
// same distinction HandleBlockPlace doesn't need, since Dragonfly hands
// it the already-resolved placement position directly). Both are
// checked here to be safe.
//
// UNVERIFIED SIGNATURE: player.Handler's exact method name and
// parameters for this hook aren't independently confirmed against this
// Dragonfly version (no network access from this environment to check).
// This is a best-confidence guess at Dragonfly's real
// HandleItemUseOnBlock. Because PlayerHandler embeds player.NopHandler,
// getting this wrong won't break the build — it'll just silently never
// fire, which looks identical to "the fix didn't work" rather than a
// compile error. If water is still placeable after this, that's the
// first thing to check: look up player.Handler's actual method list
// (`go doc github.com/df-mc/dragonfly/server/player.Handler` in your
// module cache) and tell me what it's actually called.
func (h *PlayerHandler) HandleItemUseOnBlock(ctx *player.Context, pos cube.Pos, face cube.Face, clickPos mgl64.Vec3) {
	if state.Ops.IsOp(h.p.XUID()) {
		return
	}
	target := pos.Side(face)
	if koth.Cfg.IsProtected(pos.X(), pos.Z()) || koth.Cfg.IsProtected(target.X(), target.Z()) {
		ctx.Cancel()
		h.p.Message("§cThis area is protected as part of a KOTH zone.")
	}
}
