package players

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"velaris-dragonfly/knockback"
	"velaris-dragonfly/legendary"
	"velaris-dragonfly/ranks"
	"velaris-dragonfly/state"
)

// PlayerHandler is attached to every player that joins the server. It
// embeds player.NopHandler so we only need to implement the methods we
// actually care about — every other event is a silent no-op until we add
// it here.
type PlayerHandler struct {
	player.NopHandler

	p        *player.Player
	ranks    *ranks.RankSet
	rankDefs *ranks.RankDefSet
	log      *slog.Logger
}

// NewPlayerHandler creates a PlayerHandler for the given player.
func NewPlayerHandler(p *player.Player, rks *ranks.RankSet, rankDefs *ranks.RankDefSet, log *slog.Logger) *PlayerHandler {
	return &PlayerHandler{p: p, ranks: rks, rankDefs: rankDefs, log: log}
}

// HandleQuit is called when the player disconnects, regardless of the
// reason. This is where you'd persist per-player state.
func (h *PlayerHandler) HandleQuit(p *player.Player) {
	state.TrackQuit(p)
	knockback.ClearPlayer(p.XUID())
	legendary.ClearPlayer(p.XUID())
	h.log.Info("player quit", "name", p.Name(), "xuid", p.XUID())
}

// HandleChat tags every chat message with the player's rank prefix.
func (h *PlayerHandler) HandleChat(ctx *player.Context, message *string) {
	name := h.ranks.Of(h.p.XUID())
	def, ok := h.rankDefs.Get(name)
	if !ok {
		def, _ = h.rankDefs.Get(ranks.DefaultRankName)
	}
	*message = fmt.Sprintf("%s %s", def.ChatPrefix(), *message)
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

// HandleAttackEntity applies the configured knockback force/height and
// enforces the attack cooldown — see the knockback package, ported from
// the CustomKB PocketMine plugin.
func (h *PlayerHandler) HandleAttackEntity(ctx *player.Context, e world.Entity, force, height *float64, critical *bool) {
	knockback.OnAttackEntity(ctx, h.p, force, height, critical)
}

// HandleItemUse enforces the configured projectile-shoot cooldown — see
// the knockback package, ported from the CustomKB PocketMine plugin.
func (h *PlayerHandler) HandleItemUse(ctx *player.Context) {
	knockback.OnItemUse(ctx, h.p)
	legendary.OnItemUse(ctx, h.p)
}

// HandleMove fires on every movement update. Used here to drive Golem
// Hammer's fall-height tracking (legendary.TickGolemFall) continuously
// while the player is airborne.
//
// REPLACES a previous attempt (legendary/falltick.go, now deleted) at a
// standalone background ticker that called srv.World().Exec — that
// doesn't exist on this Dragonfly version (confirmed by a real build
// error: "world.World has no field or method Exec, but does have
// unexported method exec"). This hooks into a real per-move callback
// instead, the same safe pattern as every other Handle* method in this
// file, so it doesn't need to guess at how to get a Tx from outside a
// handler.
//
// UNVERIFIED: "HandleMove(ctx *player.Context, newPos mgl64.Vec3, newRot
// cube.Rotation)" is my best read of the player.Handler interface's move
// hook — if the real method name or parameter types differ, paste the
// compiler error and it's a quick fix (TickGolemFall itself doesn't care
// how it's called, just that it's called often while falling).
func (h *PlayerHandler) HandleMove(ctx *player.Context, newPos mgl64.Vec3, newRot cube.Rotation) {
	legendary.TickGolemFall(h.p)
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
func (h *PlayerHandler) HandleBlockPlace(ctx *player.Context, pos cube.Pos, b world.Block) {
	name, _ := b.EncodeBlock()
	if strings.HasSuffix(name, "_display_block") {
		ctx.Cancel()
	}
}
